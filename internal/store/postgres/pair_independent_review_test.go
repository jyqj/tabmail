package postgres_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"tabmail/internal/app"
	"tabmail/internal/company"
	"tabmail/internal/models"
	"tabmail/internal/testutil"
)

// Review-owned probes use existing seed/router helpers, never a shared database.
func pairSeed(t *testing.T) *companyFixture {
	t.Helper()
	if os.Getenv("TABMAIL_PAIR_REVIEW") != "1" {
		t.Skip("explicit owned pair review only")
	}
	if !strings.Contains(os.Getenv("TABMAIL_TEST_DB_DSN"), "127.0.0.1:55447/") {
		t.Fatal("review-owned cluster required")
	}
	f := seedCompany(t)
	_, e := f.pool.Exec(context.Background(), `CREATE FUNCTION pair_never_reactivate() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NOT OLD.is_active AND NEW.is_active THEN RAISE EXCEPTION 'review prohibits reactivation'; END IF; RETURN NEW; END $$; CREATE TRIGGER pair_never_reactivate BEFORE UPDATE ON users FOR EACH ROW EXECUTE FUNCTION pair_never_reactivate()`)
	must(t, e)
	return f
}
func pairSnapshot(t *testing.T, f *companyFixture) string {
	t.Helper()
	rows, e := f.pool.Query(context.Background(), `SELECT tablename FROM pg_tables WHERE schemaname='public' ORDER BY tablename`)
	must(t, e)
	names := []string{}
	for rows.Next() {
		var n string
		must(t, rows.Scan(&n))
		names = append(names, n)
	}
	must(t, rows.Err())
	rows.Close()
	var out strings.Builder
	for _, n := range names {
		var s string
		must(t, f.pool.QueryRow(context.Background(), `SELECT COALESCE(jsonb_agg(v ORDER BY v::text),'[]')::text FROM (SELECT to_jsonb(x) v FROM `+pgx.Identifier{n}.Sanitize()+` x) q`).Scan(&s))
		fmt.Fprintf(&out, "%s:%s\n", n, s)
	}
	return out.String()
}

type pairReply struct {
	status int
	raw    []byte
	err    error
}

func pairPost(url, token, tenant, path string, body any) pairReply {
	b, e := json.Marshal(body)
	if e != nil {
		return pairReply{err: e}
	}
	r, e := http.NewRequest("POST", url+path, bytes.NewReader(b))
	if e != nil {
		return pairReply{err: e}
	}
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	if tenant != "" {
		r.Header.Set("X-Tenant-ID", tenant)
	}
	resp, e := (&http.Client{Timeout: 8 * time.Second}).Do(r)
	if e != nil {
		return pairReply{err: e}
	}
	defer resp.Body.Close()
	raw, e := io.ReadAll(resp.Body)
	return pairReply{resp.StatusCode, raw, e}
}
func pairCheck(t *testing.T, r pairReply, want int) *company.OffboardingPlan {
	t.Helper()
	must(t, r.err)
	if r.status != want {
		t.Fatalf("HTTP %d want %d: %s", r.status, want, r.raw)
	}
	t.Logf("real HTTP %d", r.status)
	if want != 200 {
		return nil
	}
	var v struct {
		Data company.OffboardingPlan `json:"data"`
	}
	must(t, json.Unmarshal(r.raw, &v))
	if v.Data.ID == uuid.Nil {
		t.Fatal("missing persisted plan")
	}
	return &v.Data
}
func pairPath(id uuid.UUID, preview bool) string {
	s := "/api/v1/company/employees/" + id.String() + "/offboard"
	if preview {
		s += "/preview"
	}
	return s
}
func pairBody(id uuid.UUID) any {
	return map[string]any{"successor_user_id": id, "options": company.OffboardingOptions{Drafts: "seal"}, "reason": "Independent bounded pair review"}
}
func pairServer(t *testing.T, f *companyFixture) *httptest.Server {
	s := httptest.NewServer(companyRouter(t, f, testutil.NewMemoryObjectStore(), nil))
	t.Cleanup(s.Close)
	return s
}
func pairSQL(t *testing.T, f *companyFixture, q string, args ...any) {
	t.Helper()
	_, e := f.pool.Exec(context.Background(), q, args...)
	must(t, e)
}
func pairUnchanged(t *testing.T, f *companyFixture, s string) {
	t.Helper()
	if pairSnapshot(t, f) != s {
		t.Fatal("denial/replay changed public database tables")
	}
}
func pairForeign(t *testing.T, f *companyFixture) *models.User {
	tenant := &models.Tenant{Name: "Review foreign tenant", PlanID: f.tenant.PlanID}
	must(t, f.st.CreateTenant(context.Background(), tenant))
	u := &models.User{TenantID: tenant.ID, Email: "review@foreign.test", DisplayName: "Review foreign", Role: models.RoleSuperAdmin, IsActive: true, PasswordHash: "synthetic"}
	must(t, f.st.CreateUser(context.Background(), u))
	return u
}
func TestPairIndependentOldPeerSelf(t *testing.T) {
	for _, mode := range []string{"peer", "self"} {
		t.Run(mode, func(t *testing.T) {
			f := pairSeed(t)
			s := pairServer(t, f)
			id := f.admin.ID
			want := 400
			if mode == "peer" {
				id = f.other.ID
				pairSQL(t, f, `UPDATE users SET role='admin' WHERE id=$1`, id)
				want = 403
			}
			before := pairSnapshot(t, f)
			if os.Getenv("TABMAIL_PAIR_BASELINE") == "1" {
				want = 200
			}
			p := pairCheck(t, pairPost(s.URL, r3Token(t, f.admin), "", pairPath(f.employee.ID, true), pairBody(id)), want)
			if want == 200 {
				if p.State != "preview" || pairSnapshot(t, f) == before {
					t.Fatal("old admission not persisted")
				}
			} else {
				pairUnchanged(t, f, before)
			}
		})
	}
}
func TestPairIndependentPreviewPolicy(t *testing.T) {
	for _, mode := range []string{"frozen", "peer-target", "super-target", "caller-target", "same-subject", "inactive-successor", "super-successor", "foreign-target", "foreign-successor", "ordinary-caller", "inactive-caller", "ordinary-selected-foreign", "super-selected-user", "super-selected-admin", "super-selected-super"} {
		t.Run(mode, func(t *testing.T) {
			f := pairSeed(t)
			x := pairForeign(t, f)
			s := pairServer(t, f)
			target, successor, caller := f.employee.ID, f.other.ID, f.admin
			selected := ""
			want := 200
			switch mode {
			case "frozen":
				pairSQL(t, f, `UPDATE users SET is_active=false WHERE id=$1`, target)
			case "peer-target", "super-target":
				role := "admin"
				if mode == "super-target" {
					role = "super_admin"
				}
				pairSQL(t, f, `UPDATE users SET role=$2 WHERE id=$1`, target, role)
				want = 403
			case "caller-target":
				target = caller.ID
				want = 400
			case "same-subject":
				successor = target
				want = 400
			case "inactive-successor":
				pairSQL(t, f, `UPDATE users SET is_active=false WHERE id=$1`, successor)
				want = 400
			case "super-successor":
				pairSQL(t, f, `UPDATE users SET role='super_admin' WHERE id=$1`, successor)
				want = 403
			case "foreign-target":
				target = x.ID
				want = 404
			case "foreign-successor":
				successor = x.ID
				want = 400
			case "ordinary-caller":
				pairSQL(t, f, `UPDATE users SET role='user' WHERE id=$1`, caller.ID)
				want = 403
			case "inactive-caller":
				pairSQL(t, f, `UPDATE users SET is_active=false WHERE id=$1`, caller.ID)
				want = 401
			case "ordinary-selected-foreign":
				selected = x.TenantID.String()
				successor = x.ID
				want = 400
			case "super-selected-user", "super-selected-admin", "super-selected-super":
				caller = x
				selected = f.tenant.ID.String()
				role := "user"
				if mode == "super-selected-admin" {
					role = "admin"
				}
				if mode == "super-selected-super" {
					role = "super_admin"
				}
				pairSQL(t, f, `UPDATE users SET role=$2 WHERE id=$1`, successor, role)
			}
			before := pairSnapshot(t, f)
			p := pairCheck(t, pairPost(s.URL, r3Token(t, caller), selected, pairPath(target, true), pairBody(successor)), want)
			if want != 200 {
				pairUnchanged(t, f, before)
				return
			}
			if mode == "frozen" {
				u, e := f.st.GetUser(context.Background(), target)
				must(t, e)
				if u.IsActive {
					t.Fatal("frozen preview reactivated target")
				}
			}
			pairCheck(t, pairPost(s.URL, r3Token(t, caller), selected, pairPath(target, false), map[string]any{"plan_id": p.ID}), 200)
			u, e := f.st.GetUser(context.Background(), target)
			must(t, e)
			if u.IsActive {
				t.Fatal("execute left target active")
			}
			before = pairSnapshot(t, f)
			pairCheck(t, pairPost(s.URL, r3Token(t, caller), selected, pairPath(target, false), map[string]any{"plan_id": p.ID}), 200)
			pairUnchanged(t, f, before)
		})
	}
}
func TestPairIndependentStaleExecute(t *testing.T) {
	for _, mode := range []string{"successor-role", "successor-freeze", "target-role", "target-freeze", "caller-role", "caller-freeze", "caller-tenant", "successor-tenant-fk", "target-tenant-fk", "asset", "expiry", "plan-creator", "plan-target", "synthetic-old-self-plan", "super-successor-role", "super-target-role"} {
		t.Run(mode, func(t *testing.T) {
			f := pairSeed(t)
			x := pairForeign(t, f)
			s := pairServer(t, f)
			token := r3Token(t, f.admin)
			selected := ""
			if mode == "super-successor-role" || mode == "super-target-role" {
				token = r3Token(t, x)
				selected = f.tenant.ID.String()
			}
			p := pairCheck(t, pairPost(s.URL, token, selected, pairPath(f.employee.ID, true), pairBody(f.other.ID)), 200)
			want := 409
			target := f.employee.ID
			switch mode {
			case "successor-role":
				pairSQL(t, f, `UPDATE users SET role='admin' WHERE id=$1`, f.other.ID)
				want = 403
			case "successor-freeze":
				pairSQL(t, f, `UPDATE users SET is_active=false WHERE id=$1`, f.other.ID)
				want = 400
			case "target-role":
				pairSQL(t, f, `UPDATE users SET role='admin' WHERE id=$1`, target)
				want = 403
			case "target-freeze":
				pairSQL(t, f, `UPDATE users SET is_active=false WHERE id=$1`, target)
			case "caller-role":
				pairSQL(t, f, `UPDATE users SET role='user' WHERE id=$1`, f.admin.ID)
				want = 403
			case "caller-freeze":
				pairSQL(t, f, `UPDATE users SET is_active=false WHERE id=$1`, f.admin.ID)
				want = 401
			case "caller-tenant":
				pairSQL(t, f, `UPDATE users SET tenant_id=$2 WHERE id=$1`, f.admin.ID, x.TenantID)
				// Authentication resolves the current company; the old plan is
				// absent in that scope. Ordinary X-Tenant-ID cannot restore it.
				selected = f.tenant.ID.String()
				want = 404
			case "synthetic-old-self-plan":
				pairSQL(t, f, `UPDATE employee_offboarding_plans SET successor_id=$2 WHERE id=$1`, p.ID, f.admin.ID)
				want = 400
			case "super-successor-role":
				pairSQL(t, f, `UPDATE users SET role='admin' WHERE id=$1`, f.other.ID)
			case "super-target-role":
				pairSQL(t, f, `UPDATE users SET role='admin' WHERE id=$1`, target)
			case "successor-tenant-fk", "target-tenant-fk":
				id := f.other.ID
				if mode == "target-tenant-fk" {
					id = target
				}
				before := pairSnapshot(t, f)
				_, e := f.pool.Exec(context.Background(), `UPDATE users SET tenant_id=$2 WHERE id=$1`, id, x.TenantID)
				var pe *pgconn.PgError
				if !errors.As(e, &pe) || pe.Code != "23503" {
					t.Fatalf("relocation not fenced: %v", e)
				}
				pairUnchanged(t, f, before)
				want = 200
			case "asset":
				pairSQL(t, f, `UPDATE mailboxes SET lifecycle_revision=lifecycle_revision+1 WHERE id=$1`, f.personal.ID)
			case "expiry":
				pairSQL(t, f, `UPDATE employee_offboarding_plans SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, p.ID)
			case "plan-creator":
				pairSQL(t, f, `UPDATE users SET role='admin' WHERE id=$1`, f.other.ID)
				token = r3Token(t, f.other)
				want = 404
			case "plan-target":
				target = f.other.ID
				want = 404
			}
			before := pairSnapshot(t, f)
			pairCheck(t, pairPost(s.URL, token, selected, pairPath(target, false), map[string]any{"plan_id": p.ID}), want)
			if want != 200 {
				pairUnchanged(t, f, before)
			}
		})
	}
}
func TestPairIndependentReplayCompletion(t *testing.T) {
	for _, mode := range []string{"same", "successor-role", "successor-freeze", "target-role", "target-session", "successor-session", "unverified-receipt", "second-plan"} {
		t.Run(mode, func(t *testing.T) {
			f := pairSeed(t)
			s := pairServer(t, f)
			token := r3Token(t, f.admin)
			p := pairCheck(t, pairPost(s.URL, token, "", pairPath(f.employee.ID, true), pairBody(f.other.ID)), 200)
			var p2 *company.OffboardingPlan
			if mode == "second-plan" {
				p2 = pairCheck(t, pairPost(s.URL, token, "", pairPath(f.employee.ID, true), pairBody(f.other.ID)), 200)
			}
			first := pairCheck(t, pairPost(s.URL, token, "", pairPath(f.employee.ID, false), map[string]any{"plan_id": p.ID}), 200)
			want := 409
			switch mode {
			case "same":
				want = 200
			case "successor-role":
				pairSQL(t, f, `UPDATE users SET role='admin' WHERE id=$1`, f.other.ID)
				want = 403
			case "successor-freeze":
				pairSQL(t, f, `UPDATE users SET is_active=false WHERE id=$1`, f.other.ID)
				want = 400
			case "target-role":
				pairSQL(t, f, `UPDATE users SET role='admin' WHERE id=$1`, f.employee.ID)
				want = 403
			case "target-session":
				pairSQL(t, f, `UPDATE users SET session_version=session_version+1 WHERE id=$1`, f.employee.ID)
			case "successor-session":
				pairSQL(t, f, `UPDATE users SET session_version=session_version+1 WHERE id=$1`, f.other.ID)
			case "unverified-receipt":
				pairSQL(t, f, `UPDATE employee_offboarding_plans SET fingerprint='unverified' WHERE id=$1`, p.ID)
			case "second-plan":
				p = p2
			}
			before := pairSnapshot(t, f)
			r := pairCheck(t, pairPost(s.URL, token, "", pairPath(f.employee.ID, false), map[string]any{"plan_id": p.ID}), want)
			pairUnchanged(t, f, before)
			if want == 200 && !r.ExecutedAt.Equal(*first.ExecutedAt) {
				t.Fatal("receipt changed")
			}
			if mode == "same" {
				pairCheck(t, pairPost(s.URL, token, "", pairPath(f.employee.ID, true), pairBody(f.other.ID)), 409)
				pairUnchanged(t, f, before)
			}
		})
	}
}
func TestPairIndependentCompatibility(t *testing.T) {
	for _, mode := range []string{"peer", "self", "inactive", "foreign", "frozen-target"} {
		t.Run(mode, func(t *testing.T) {
			f := pairSeed(t)
			id := f.other.ID
			want := app.KindForbidden
			switch mode {
			case "peer":
				pairSQL(t, f, `UPDATE users SET role='admin' WHERE id=$1`, id)
			case "self":
				id = f.admin.ID
				want = app.KindBadRequest
			case "inactive":
				pairSQL(t, f, `UPDATE users SET is_active=false WHERE id=$1`, id)
				want = app.KindBadRequest
			case "foreign":
				id = pairForeign(t, f).ID
				want = app.KindBadRequest
			case "frozen-target":
				pairSQL(t, f, `UPDATE users SET is_active=false WHERE id=$1`, f.employee.ID)
				want = app.KindConflict
			}
			before := pairSnapshot(t, f)
			e := f.st.OffboardEmployee(context.Background(), f.a, f.employee.ID, id, "Independent compatibility review")
			var ae *app.Error
			if !errors.As(e, &ae) || ae.Kind != want {
				t.Fatalf("compatibility error %v want %v", e, want)
			}
			pairUnchanged(t, f, before)
		})
	}
}

func pairWaitBlocked(t *testing.T, f *companyFixture, pid int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var yes bool
		must(t, f.pool.QueryRow(context.Background(), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))`, pid).Scan(&yes))
		if yes {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("no real PostgreSQL wait edge")
}
func TestPairIndependentLockWait(t *testing.T) {
	for _, stage := range []string{"preview", "execute", "replay"} {
		for _, subject := range []string{"successor", "cross-tenant-super-caller"} {
			t.Run(stage+"/"+subject, func(t *testing.T) {
				f := pairSeed(t)
				caller := f.admin
				selected := ""
				if subject == "cross-tenant-super-caller" {
					caller = pairForeign(t, f)
					selected = f.tenant.ID.String()
				}
				s := pairServer(t, f)
				token := r3Token(t, caller)
				path := pairPath(f.employee.ID, true)
				body := pairBody(f.other.ID)
				if stage != "preview" {
					p := pairCheck(t, pairPost(s.URL, token, selected, path, body), 200)
					path = pairPath(f.employee.ID, false)
					body = map[string]any{"plan_id": p.ID}
					if stage == "replay" {
						pairCheck(t, pairPost(s.URL, token, selected, path, body), 200)
					}
				}
				tx, e := f.pool.Begin(context.Background())
				must(t, e)
				defer tx.Rollback(context.Background())
				var pid int
				must(t, tx.QueryRow(context.Background(), `SELECT pg_backend_pid()`).Scan(&pid))
				_, e = tx.Exec(context.Background(), `SELECT id FROM tenants WHERE id=$1 FOR UPDATE`, f.tenant.ID)
				must(t, e)
				ch := make(chan pairReply, 1)
				go func() { ch <- pairPost(s.URL, token, selected, path, body) }()
				pairWaitBlocked(t, f, pid)
				id := f.other.ID
				want := 403
				role := "admin"
				if subject == "cross-tenant-super-caller" {
					id = caller.ID
					role = "user"
				}
				_, e = tx.Exec(context.Background(), `UPDATE users SET role=$2 WHERE id=$1`, id, role)
				must(t, e)
				must(t, tx.Commit(context.Background()))
				before := pairSnapshot(t, f)
				select {
				case r := <-ch:
					pairCheck(t, r, want)
				case <-time.After(8 * time.Second):
					t.Fatal("HTTP did not finish after lock release")
				}
				pairUnchanged(t, f, before)
			})
		}
	}
}
func TestPairIndependentConcurrentExecute(t *testing.T) {
	f := pairSeed(t)
	s := pairServer(t, f)
	token := r3Token(t, f.admin)
	p := pairCheck(t, pairPost(s.URL, token, "", pairPath(f.employee.ID, true), pairBody(f.other.ID)), 200)
	ch := make(chan pairReply, 4)
	start := make(chan struct{})
	for i := 0; i < 4; i++ {
		go func() {
			<-start
			ch <- pairPost(s.URL, token, "", pairPath(f.employee.ID, false), map[string]any{"plan_id": p.ID})
		}()
	}
	close(start)
	var stamp time.Time
	for i := 0; i < 4; i++ {
		r := pairCheck(t, <-ch, 200)
		if i == 0 {
			stamp = *r.ExecutedAt
		} else if !stamp.Equal(*r.ExecutedAt) {
			t.Fatal("concurrent receipts differ")
		}
	}
	var audits, plans int
	must(t, f.pool.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM audit_log WHERE action='employee.offboard'),(SELECT count(*) FROM employee_offboarding_plans WHERE state='executed')`).Scan(&audits, &plans))
	if audits != 1 || plans != 1 {
		t.Fatalf("effects audits=%d plans=%d", audits, plans)
	}
	u, e := f.st.GetUser(context.Background(), f.employee.ID)
	must(t, e)
	if u.IsActive || u.SessionVersion != f.employee.SessionVersion+1 {
		t.Fatal("activity/session effect repeated")
	}
}

func TestPairIndependentLocksHeldThroughCommit(t *testing.T) {
	f := pairSeed(t)
	s := pairServer(t, f)
	token := r3Token(t, f.admin)
	p := pairCheck(t, pairPost(s.URL, token, "", pairPath(f.employee.ID, true), pairBody(f.other.ID)), 200)
	ctx := context.Background()
	block, e := f.pool.Begin(ctx)
	must(t, e)
	defer block.Rollback(ctx)
	var pid int
	must(t, block.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid))
	_, e = block.Exec(ctx, `LOCK TABLE audit_log IN ACCESS EXCLUSIVE MODE`)
	must(t, e)
	ch := make(chan pairReply, 1)
	go func() {
		ch <- pairPost(s.URL, token, "", pairPath(f.employee.ID, false), map[string]any{"plan_id": p.ID})
	}()
	pairWaitBlocked(t, f, pid)
	// The audited mutation has already qualified the three subjects. Each
	// subject fence must still prevent changes while the transaction waits.
	for _, id := range []uuid.UUID{f.admin.ID, f.employee.ID, f.other.ID} {
		tx, err := f.pool.Begin(ctx)
		must(t, err)
		_, err = tx.Exec(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE NOWAIT`, id)
		var pe *pgconn.PgError
		if !errors.As(err, &pe) || pe.Code != "55P03" {
			_ = tx.Rollback(ctx)
			t.Fatalf("subject fence not held: %v", err)
		}
		must(t, tx.Rollback(ctx))
	}
	must(t, block.Commit(ctx))
	pairCheck(t, <-ch, 200)
	// Once committed, the successor can change; the receipt must then deny.
	pairSQL(t, f, `UPDATE users SET role='admin' WHERE id=$1`, f.other.ID)
	before := pairSnapshot(t, f)
	pairCheck(t, pairPost(s.URL, token, "", pairPath(f.employee.ID, false), map[string]any{"plan_id": p.ID}), 403)
	pairUnchanged(t, f, before)
}
func TestPairIndependentShippingUI(t *testing.T) {
	for _, mode := range []string{"frozen", "successor-role", "target-freeze", "selected-super"} {
		t.Run(mode, func(t *testing.T) {
			f := pairSeed(t)
			caller := f.admin
			selected := ""
			if mode == "selected-super" {
				caller = pairForeign(t, f)
				selected = f.tenant.ID.String()
			}
			if mode == "frozen" {
				pairSQL(t, f, `UPDATE users SET is_active=false WHERE id=$1`, f.employee.ID)
			}
			s := pairServer(t, f)
			packet := map[string]any{"url": s.URL, "token": r3Token(t, caller), "user": map[string]any{"id": caller.ID, "tenant_id": caller.TenantID, "role": caller.Role, "email": caller.Email, "display_name": caller.DisplayName}, "selected": selected, "target": f.employee.ID, "successor": f.other.ID, "mode": mode}
			b, e := json.Marshal(packet)
			must(t, e)
			path := filepath.Join(t.TempDir(), "fixture.json")
			must(t, os.WriteFile(path, b, 0600))
			mutationDone := make(chan error, 1)
			stopMutation := make(chan struct{})
			if mode == "successor-role" || mode == "target-freeze" {
				go func() {
					for {
						select {
						case <-stopMutation:
							return
						case <-time.After(10 * time.Millisecond):
						}
						if _, err := os.Stat(path + ".mutate"); err != nil {
							continue
						}
						query, id := `UPDATE users SET role='admin' WHERE id=$1`, f.other.ID
						if mode == "target-freeze" {
							query, id = `UPDATE users SET is_active=false WHERE id=$1`, f.employee.ID
						}
						_, err := f.pool.Exec(context.Background(), query, id)
						if err == nil {
							err = os.WriteFile(path+".done", []byte("mutation committed"), 0600)
						}
						mutationDone <- err
						return
					}
				}()
			}
			cmd := exec.Command("npm", "exec", "--", "vitest", "run", "--config", "vitest.pair-review.config.ts")
			cmd.Dir = "../../../web"
			cmd.Env = append(os.Environ(), "TABMAIL_PAIR_PACKET="+path)
			out, e := cmd.CombinedOutput()
			close(stopMutation)
			t.Log(string(out))
			must(t, e)
			if mode == "successor-role" || mode == "target-freeze" {
				must(t, <-mutationDone)
			}
			u, e := f.st.GetUser(context.Background(), f.employee.ID)
			must(t, e)
			if mode == "frozen" || mode == "selected-super" || mode == "target-freeze" {
				if u.IsActive {
					t.Fatal("target must remain inactive")
				}
			} else if !u.IsActive {
				t.Fatal("denied UI execute mutated target")
			}
			var n int
			must(t, f.pool.QueryRow(context.Background(), `SELECT count(*) FROM employee_offboarding_plans WHERE state='executed'`).Scan(&n))
			want := 0
			if mode == "frozen" || mode == "selected-super" {
				want = 1
			}
			if n != want {
				t.Fatal("UI completion mismatch")
			}
		})
	}
}
