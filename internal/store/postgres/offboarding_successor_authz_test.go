package postgres_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"tabmail/internal/app"
	"tabmail/internal/company"
	"tabmail/internal/models"
	"tabmail/internal/testutil"
)

func successorAuthzSeed(t *testing.T) *companyFixture {
	t.Helper()
	if os.Getenv("TABMAIL_SUCCESSOR_AUTHZ_OWNED") != "1" {
		t.Skip("opt-in owned PostgreSQL/HTTP successor authorization probe")
	}
	if os.Getenv("TABMAIL_TEST_DB_DSN") == "" {
		t.Fatal("owned PostgreSQL DSN required")
	}
	f := seedCompany(t)
	_, err := f.pool.Exec(context.Background(), `CREATE FUNCTION successor_no_reactivation() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF OLD.is_active=false AND NEW.is_active=true THEN RAISE EXCEPTION 'reactivation forbidden'; END IF; RETURN NEW; END $$; CREATE TRIGGER successor_no_reactivation BEFORE UPDATE ON users FOR EACH ROW EXECUTE FUNCTION successor_no_reactivation()`)
	must(t, err)
	return f
}

// All requests use the shipping router through an actual loopback socket.
func successorAuthzHTTP(t *testing.T, f *companyFixture, token, tenant, path string, body any, want int) []byte {
	t.Helper()
	server := httptest.NewServer(companyRouter(t, f, testutil.NewMemoryObjectStore(), nil))
	defer server.Close()
	raw, err := json.Marshal(body)
	must(t, err)
	req, err := http.NewRequest("POST", server.URL+path, bytes.NewReader(raw))
	must(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	if tenant != "" {
		req.Header.Set("X-Tenant-ID", tenant)
	}
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	must(t, err)
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	must(t, err)
	t.Logf("direct HTTP status=%d expected=%d", resp.StatusCode, want)
	if resp.StatusCode != want {
		t.Errorf("POST %s: got %d want %d: %s", path, resp.StatusCode, want, data)
	}
	return data
}
func successorAuthzPath(target uuid.UUID, command string) string {
	path := "/api/v1/company/employees/" + target.String() + "/offboard"
	if command != "" {
		path += "/" + command
	}
	return path
}
func successorAuthzBody(id uuid.UUID) any {
	return map[string]any{"successor_user_id": id, "reason": "Owned synthetic successor authorization regression", "options": company.OffboardingOptions{Drafts: "seal"}}
}
func successorAuthzUnchanged(t *testing.T, f *companyFixture, before string) {
	t.Helper()
	if after := r5OffboardingState(t, f.pool); after != before {
		t.Error("denial mutated owned database state")
	}
}
func TestSuccessorAuthzOwnedDirectHTTPPeerAndSelf(t *testing.T) {
	for _, mode := range []string{"peer", "caller-self"} {
		t.Run(mode, func(t *testing.T) {
			f := successorAuthzSeed(t)
			id, status := f.admin.ID, 400
			if mode == "peer" {
				_, err := f.pool.Exec(context.Background(), `UPDATE users SET role='admin' WHERE id=$1`, f.other.ID)
				must(t, err)
				id, status = f.other.ID, 403
			}
			before := r5OffboardingState(t, f.pool)
			successorAuthzHTTP(t, f, r3Token(t, f.admin), "", successorAuthzPath(f.employee.ID, "preview"), successorAuthzBody(id), status)
			successorAuthzUnchanged(t, f, before)
		})
	}
}

func TestSuccessorAuthzOwnedPreviewMatrix(t *testing.T) {
	for _, mode := range []string{"user", "frozen-target", "admin", "super-admin", "inactive", "foreign", "target-self", "nil", "peer-target", "foreign-target", "ordinary-foreign-selection", "super-selected-user", "super-selected-admin", "super-selected-super", "super-selected-foreign", "super-caller-self"} {
		t.Run(mode, func(t *testing.T) {
			f := successorAuthzSeed(t)
			ctx := context.Background()
			target, successor, caller := f.employee.ID, f.other.ID, f.admin
			selected, want := "", 200
			foreign := &models.Tenant{Name: "Owned foreign company", PlanID: f.tenant.PlanID}
			must(t, f.st.CreateTenant(ctx, foreign))
			external := &models.User{TenantID: foreign.ID, Email: "synthetic@foreign.test", DisplayName: "Synthetic foreign", Role: models.RoleUser, IsActive: true, PasswordHash: "synthetic"}
			must(t, f.st.CreateUser(ctx, external))
			switch mode {
			case "frozen-target":
				_, err := f.pool.Exec(ctx, `UPDATE users SET is_active=false WHERE id=$1`, target)
				must(t, err)
			case "admin", "super-admin", "peer-target":
				role := models.RoleAdmin
				if mode == "super-admin" {
					role = models.RoleSuperAdmin
				}
				id := successor
				if mode == "peer-target" {
					id = target
				}
				_, err := f.pool.Exec(ctx, `UPDATE users SET role=$2 WHERE id=$1`, id, role)
				must(t, err)
				want = 403
			case "inactive":
				_, err := f.pool.Exec(ctx, `UPDATE users SET is_active=false WHERE id=$1`, successor)
				must(t, err)
				want = 400
			case "foreign":
				successor = external.ID
				want = 400
			case "target-self":
				successor = target
				want = 400
			case "nil":
				successor = uuid.Nil
				want = 400
			case "foreign-target":
				target = external.ID
				want = 404
			case "ordinary-foreign-selection":
				selected = foreign.ID.String()
				successor = external.ID
				want = 400
			case "super-caller-self":
				_, err := f.pool.Exec(ctx, `UPDATE users SET role='super_admin' WHERE id=$1`, caller.ID)
				must(t, err)
				caller.Role = models.RoleSuperAdmin
				successor = caller.ID
				want = 400
			case "super-selected-user", "super-selected-admin", "super-selected-super", "super-selected-foreign":
				external.Role = models.RoleSuperAdmin
				_, err := f.pool.Exec(ctx, `UPDATE users SET role='super_admin' WHERE id=$1`, external.ID)
				must(t, err)
				caller = external
				selected = f.tenant.ID.String()
				if mode == "super-selected-foreign" {
					successor = external.ID
					want = 400
				}
				if mode == "super-selected-admin" || mode == "super-selected-super" {
					role := models.RoleAdmin
					if mode == "super-selected-super" {
						role = models.RoleSuperAdmin
					}
					_, err := f.pool.Exec(ctx, `UPDATE users SET role=$2 WHERE id=$1`, successor, role)
					must(t, err)
				}
			}
			before := r5OffboardingState(t, f.pool)
			raw := successorAuthzHTTP(t, f, r3Token(t, caller), selected, successorAuthzPath(target, "preview"), successorAuthzBody(successor), want)
			if want != 200 {
				successorAuthzUnchanged(t, f, before)
				return
			}
			var result struct {
				Data company.OffboardingPlan `json:"data"`
			}
			must(t, json.Unmarshal(raw, &result))
			if result.Data.ID == uuid.Nil {
				t.Fatal("missing real plan")
			}
			// Preserve frozen-target execution, super-admin selection and qualified replay.
			raw = successorAuthzHTTP(t, f, r3Token(t, caller), selected, successorAuthzPath(target, ""), map[string]any{"plan_id": result.Data.ID}, 200)
			var receipt struct {
				Data company.OffboardingPlan `json:"data"`
			}
			must(t, json.Unmarshal(raw, &receipt))
			if receipt.Data.State != "executed" {
				t.Fatal("missing completion")
			}
			var active bool
			var owner uuid.UUID
			var completed int
			must(t, f.pool.QueryRow(ctx, `SELECT is_active FROM users WHERE id=$1`, target).Scan(&active))
			must(t, f.pool.QueryRow(ctx, `SELECT owner_user_id FROM mailboxes WHERE id=$1`, f.personal.ID).Scan(&owner))
			must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM employee_offboarding_plans WHERE target_id=$1 AND state='executed'`, target).Scan(&completed))
			if active || owner != successor || completed != 1 {
				t.Fatal("execution did not preserve frozen target and exact single custody transfer")
			}

			before = r5OffboardingState(t, f.pool)
			successorAuthzHTTP(t, f, r3Token(t, caller), selected, successorAuthzPath(target, ""), map[string]any{"plan_id": result.Data.ID}, 200)
			successorAuthzUnchanged(t, f, before)
			successorAuthzHTTP(t, f, r3Token(t, caller), selected, successorAuthzPath(target, "preview"), successorAuthzBody(successor), 409)
			successorAuthzUnchanged(t, f, before)
		})
	}
}

func TestSuccessorAuthzOwnedCurrentState(t *testing.T) {
	for _, phase := range []string{"execute", "replay"} {
		for _, change := range []string{"role-admin", "role-super", "inactive", "allowed-role-change", "caller-demoted", "caller-inactive", "caller-tenant", "target-frozen", "assets-changed"} {
			t.Run(phase+"/"+change, func(t *testing.T) {
				f := successorAuthzSeed(t)
				ctx := context.Background()
				token := r3Token(t, f.admin)
				if change == "allowed-role-change" {
					_, err := f.pool.Exec(ctx, `UPDATE users SET role='super_admin' WHERE id=$1`, f.admin.ID)
					must(t, err)
					f.admin.Role = models.RoleSuperAdmin
					token = r3Token(t, f.admin)
				}
				raw := successorAuthzHTTP(t, f, token, "", successorAuthzPath(f.employee.ID, "preview"), successorAuthzBody(f.other.ID), 200)
				var p struct {
					Data company.OffboardingPlan `json:"data"`
				}
				must(t, json.Unmarshal(raw, &p))
				body := map[string]any{"plan_id": p.Data.ID}
				if phase == "replay" {
					successorAuthzHTTP(t, f, token, "", successorAuthzPath(f.employee.ID, ""), body, 200)
				}
				want := 403
				var err error
				switch change {
				case "role-admin":
					_, err = f.pool.Exec(ctx, `UPDATE users SET role='admin' WHERE id=$1`, f.other.ID)
				case "role-super":
					_, err = f.pool.Exec(ctx, `UPDATE users SET role='super_admin' WHERE id=$1`, f.other.ID)
				case "inactive":
					_, err = f.pool.Exec(ctx, `UPDATE users SET is_active=false WHERE id=$1`, f.other.ID)
					want = 400
				case "caller-tenant":
					foreign := &models.Tenant{Name: "Owned caller relocation", PlanID: f.tenant.PlanID}
					must(t, f.st.CreateTenant(ctx, foreign))
					_, err = f.pool.Exec(ctx, `UPDATE users SET tenant_id=$2 WHERE id=$1`, f.admin.ID, foreign.ID)
					want = 404

				case "allowed-role-change":
					_, err = f.pool.Exec(ctx, `UPDATE users SET role='admin' WHERE id=$1`, f.other.ID)
					want = 409
				case "caller-demoted":
					_, err = f.pool.Exec(ctx, `UPDATE users SET role='user' WHERE id=$1`, f.admin.ID)
				case "caller-inactive":
					_, err = f.pool.Exec(ctx, `UPDATE users SET is_active=false WHERE id=$1`, f.admin.ID)
					want = 401
				case "target-frozen":
					_, err = f.pool.Exec(ctx, `UPDATE users SET is_active=false,session_version=session_version+1 WHERE id=$1`, f.employee.ID)
					want = 409
				case "assets-changed":
					_, err = f.pool.Exec(ctx, `UPDATE mailboxes SET lifecycle_revision=lifecycle_revision+1 WHERE id=$1`, f.personal.ID)
					want = 409
					// Executed receipt epochs deliberately do not track subsequent asset changes.
					if phase == "replay" {
						want = 200
					}
				}
				must(t, err)
				before := r5OffboardingState(t, f.pool)
				successorAuthzHTTP(t, f, token, "", successorAuthzPath(f.employee.ID, ""), body, want)
				successorAuthzUnchanged(t, f, before)
			})
		}
	}
}

func TestSuccessorAuthzOwnedAfterTenantWait(t *testing.T) {
	for _, phase := range []string{"preview", "execute", "replay"} {
		t.Run(phase, func(t *testing.T) {
			f := successorAuthzSeed(t)
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			actor := r5OffboardingActor(t, f)
			var plan *company.OffboardingPlan
			var err error
			if phase != "preview" {
				plan, err = f.st.PreviewOffboarding(ctx, actor, f.employee.ID, f.other.ID, company.OffboardingOptions{Drafts: "seal"}, "Owned wait qualification")
				must(t, err)
				if phase == "replay" {
					_, err = f.st.ExecuteOffboarding(ctx, actor, f.employee.ID, plan.ID)
					must(t, err)
				}
			}
			gate, err := f.pool.Begin(ctx)
			must(t, err)
			defer gate.Rollback(context.Background())
			_, err = gate.Exec(ctx, `SELECT id FROM tenants WHERE id=$1 FOR UPDATE`, f.tenant.ID)
			must(t, err)
			type result struct {
				plan *company.OffboardingPlan
				err  error
			}
			done := make(chan result, 1)
			go func() {
				var p *company.OffboardingPlan
				var e error
				if phase == "preview" {
					p, e = f.st.PreviewOffboarding(ctx, actor, f.employee.ID, f.other.ID, company.OffboardingOptions{Drafts: "seal"}, "Owned wait qualification")
				} else {
					p, e = f.st.ExecuteOffboarding(ctx, actor, f.employee.ID, plan.ID)
				}
				done <- result{p, e}
			}()
			// Observe a real wait edge, then change the recipient before releasing it.
			r5OffboardingWait(t, f.pool, ctx, gate.Conn().PgConn().PID())
			_, err = gate.Exec(ctx, `UPDATE users SET role='admin' WHERE id=$1`, f.other.ID)
			must(t, err)
			must(t, gate.Commit(ctx))
			before := r5OffboardingState(t, f.pool)
			got := <-done
			r5OffboardingReject(t, got.plan, got.err, app.KindForbidden)
			successorAuthzUnchanged(t, f, before)
		})
	}
}

func TestSuccessorAuthzOwnedTenantFence(t *testing.T) {
	f := successorAuthzSeed(t)
	ctx := context.Background()
	// A recipient without mailbox custody isolates the persisted plan FK.
	recipient := &models.User{TenantID: f.tenant.ID, Email: "no-custody@owned.test", Role: models.RoleUser, IsActive: true, PasswordHash: "synthetic"}
	must(t, f.st.CreateUser(ctx, recipient))
	foreign := &models.Tenant{Name: "Owned relocation destination", PlanID: f.tenant.PlanID}
	must(t, f.st.CreateTenant(ctx, foreign))
	p, err := f.st.PreviewOffboarding(ctx, r5OffboardingActor(t, f), f.employee.ID, recipient.ID, company.OffboardingOptions{Drafts: "seal"}, "Owned tenant fence proof")
	must(t, err)
	for _, phase := range []string{"execute", "replay"} {
		before := r5OffboardingState(t, f.pool)
		_, err = f.pool.Exec(ctx, `UPDATE users SET tenant_id=$2 WHERE id=$1`, recipient.ID, foreign.ID)
		var pg *pgconn.PgError
		if !errors.As(err, &pg) || pg.Code != "23503" || (pg.TableName != "employee_offboarding_plans" && !(phase == "replay" && pg.ConstraintName == "mailbox_owner_same_tenant")) {
			t.Fatalf("%s relocation escaped persisted same-tenant constraints: %v", phase, err)
		}
		successorAuthzUnchanged(t, f, before)
		successorAuthzHTTP(t, f, r3Token(t, f.admin), "", successorAuthzPath(f.employee.ID, ""), map[string]any{"plan_id": p.ID}, 200)
		if phase == "replay" {
			successorAuthzUnchanged(t, f, before)
		}
	}
}

func TestSuccessorAuthzOwnedSuccessorFenceThroughCommit(t *testing.T) {
	for _, phase := range []string{"preview", "execute"} {
		t.Run(phase, func(t *testing.T) {
			f := successorAuthzSeed(t)
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			actor := r5OffboardingActor(t, f)
			var p *company.OffboardingPlan
			var err error
			if phase == "execute" {
				p, err = f.st.PreviewOffboarding(ctx, actor, f.employee.ID, f.other.ID, company.OffboardingOptions{Drafts: "seal"}, "Owned successor fence")
				must(t, err)
			}
			gate, err := f.pool.Begin(ctx)
			must(t, err)
			defer gate.Rollback(context.Background())
			_, err = gate.Exec(ctx, `LOCK TABLE audit_log IN SHARE MODE`)
			must(t, err)
			done := make(chan error, 1)
			go func() {
				var e error
				if phase == "preview" {
					_, e = f.st.PreviewOffboarding(ctx, actor, f.employee.ID, f.other.ID, company.OffboardingOptions{Drafts: "seal"}, "Owned successor fence")
				} else {
					_, e = f.st.ExecuteOffboarding(ctx, actor, f.employee.ID, p.ID)
				}
				done <- e
			}()
			// The audit wait follows subject qualification and asset effects. NOWAIT
			// proves the successor remains locked for role/activity/tenant writes.
			r5OffboardingWait(t, f.pool, ctx, gate.Conn().PgConn().PID())
			contender, err := f.pool.Begin(ctx)
			must(t, err)
			_, err = contender.Exec(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE NOWAIT`, f.other.ID)
			var pg *pgconn.PgError
			if !errors.As(err, &pg) || pg.Code != "55P03" {
				t.Errorf("successor fence lost before commit: %v", err)
			}
			must(t, contender.Rollback(ctx))
			must(t, gate.Commit(ctx))
			must(t, <-done)
			// Locks are released after commit, so a subsequent role change can proceed.
			_, err = f.pool.Exec(ctx, `UPDATE users SET role='admin' WHERE id=$1`, f.other.ID)
			must(t, err)
		})
	}
}

func TestSuccessorAuthzOwnedPersistedCallerSelfAndLegacy(t *testing.T) {
	f := successorAuthzSeed(t)
	ctx := context.Background()
	a := r5OffboardingActor(t, f)
	p, err := f.st.PreviewOffboarding(ctx, a, f.employee.ID, f.other.ID, company.OffboardingOptions{Drafts: "seal"}, "Owned old self plan")
	must(t, err)
	// Model a pre-fix caller-self plan in the owned database. Subject validation
	// must deny it before fingerprint comparison or returning an executed receipt.
	_, err = f.pool.Exec(ctx, `UPDATE employee_offboarding_plans SET successor_id=$2 WHERE id=$1`, p.ID, f.admin.ID)
	must(t, err)
	before := r5OffboardingState(t, f.pool)
	successorAuthzHTTP(t, f, r3Token(t, f.admin), "", successorAuthzPath(f.employee.ID, ""), map[string]any{"plan_id": p.ID}, 400)
	successorAuthzUnchanged(t, f, before)
	_, err = f.pool.Exec(ctx, `UPDATE users SET role='admin' WHERE id=$1`, f.other.ID)
	must(t, err)
	before = r5OffboardingState(t, f.pool)
	err = f.st.OffboardEmployee(ctx, a, f.employee.ID, f.other.ID, "Owned compatibility denial")
	r5OffboardingReject(t, nil, err, app.KindForbidden)
	successorAuthzUnchanged(t, f, before)
	err = f.st.OffboardEmployee(ctx, a, f.employee.ID, f.admin.ID, "Owned compatibility self denial")
	r5OffboardingReject(t, nil, err, app.KindBadRequest)
	successorAuthzUnchanged(t, f, before)
}

func TestSuccessorAuthzOwnedConcurrentExecute(t *testing.T) {
	f := successorAuthzSeed(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	actor := r5OffboardingActor(t, f)
	p, err := f.st.PreviewOffboarding(ctx, actor, f.employee.ID, f.other.ID, company.OffboardingOptions{Drafts: "seal"}, "Owned concurrent single disposition")
	must(t, err)
	type result struct {
		p   *company.OffboardingPlan
		err error
	}
	start := make(chan struct{})
	done := make(chan result, 4)
	for i := 0; i < 4; i++ {
		go func() {
			<-start
			p, e := f.st.ExecuteOffboarding(ctx, actor, f.employee.ID, p.ID)
			done <- result{p, e}
		}()
	}
	close(start)
	var executed time.Time
	for i := 0; i < 4; i++ {
		got := <-done
		must(t, got.err)
		if got.p == nil || got.p.ExecutedAt == nil || got.p.State != "executed" {
			t.Fatal("concurrent execute missing qualified receipt")
		}
		if i == 0 {
			executed = *got.p.ExecutedAt
		} else if !executed.Equal(*got.p.ExecutedAt) {
			t.Fatal("concurrent execute returned different disposition")
		}
	}
	var completed, audits int
	var version int64
	var owner uuid.UUID
	must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM employee_offboarding_plans WHERE state='executed'`).Scan(&completed))
	must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action='employee.offboard'`).Scan(&audits))
	must(t, f.pool.QueryRow(ctx, `SELECT session_version FROM users WHERE id=$1`, f.employee.ID).Scan(&version))
	must(t, f.pool.QueryRow(ctx, `SELECT owner_user_id FROM mailboxes WHERE id=$1`, f.personal.ID).Scan(&owner))
	if completed != 1 || audits != 1 || version != f.employee.SessionVersion+1 || owner != f.other.ID {
		t.Fatal("concurrent execution duplicated or lost disposition effects")
	}
}
