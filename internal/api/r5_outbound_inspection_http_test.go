package api_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"tabmail/internal/models"
	"tabmail/internal/testpg"
)

const r5InspectionCanary = "INTERNAL-SECRET-CANARY-NOT-CONTENT"
const r5InspectionReason = "Controlled outbound inspection verification"

type r5InspectionHTTPResult struct {
	status int
	body   []byte
	err    error
}

func r5InspectionHTTPRequest(ctx context.Context, f *testpg.R5HTTPFixture, token string, tenant, job uuid.UUID, reason string) r5InspectionHTTPResult {
	raw, err := json.Marshal(map[string]string{"reason": reason})
	if err != nil {
		return r5InspectionHTTPResult{err: err}
	}
	req, err := http.NewRequestWithContext(ctx, "POST", f.Server.URL+"/api/v1/company/outbound/"+job.String()+"/inspect", strings.NewReader(string(raw)))
	if err != nil {
		return r5InspectionHTTPResult{err: err}
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.HasPrefix(token, "tb_") {
		req.Header.Set("X-API-Key", token)
	} else {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if tenant != uuid.Nil {
		req.Header.Set("X-Tenant-ID", tenant.String())
	}
	res, err := f.Server.Client().Do(req)
	if err != nil {
		return r5InspectionHTTPResult{err: err}
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 2<<20))
	return r5InspectionHTTPResult{res.StatusCode, body, err}
}
func r5InspectionHTTPCall(t *testing.T, f *testpg.R5HTTPFixture, token string, tenant, job uuid.UUID, reason string, want int) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	got := r5InspectionHTTPRequest(ctx, f, token, tenant, job, reason)
	if got.err != nil {
		t.Fatal(got.err)
	}
	if got.status != want {
		t.Fatalf("inspection HTTP status=%d want=%d", got.status, want)
	}
	if want >= 400 {
		var v map[string]json.RawMessage
		if err := json.Unmarshal(got.body, &v); err != nil {
			t.Fatal(err)
		}
		if raw, ok := v["data"]; ok && string(raw) != "null" {
			t.Fatal("error response returned inspection payload")
		}
		if strings.Contains(string(got.body), r5InspectionCanary) || strings.Contains(string(got.body), "inspection-body") {
			t.Fatal("error response leaked content")
		}
	}
	return got.body
}
func r5InspectionHTTPJob(t *testing.T, f *testpg.R5HTTPFixture, index int) *models.OutboundJob {
	t.Helper()
	c := f.Companies[index]
	token := uuid.New()
	now := time.Now().UTC()
	headers, _ := json.Marshal(map[string]string{"From": c.Shared.FullAddress, "Subject": "inspection-subject", "Date": "Thu, 01 Oct 2026 10:00:00 +0000", "Content-Type": "text/plain; charset=utf-8", "X-Credential": r5InspectionCanary, "Authorization": r5InspectionCanary, "Bcc": r5InspectionCanary, "DKIM-Signature": r5InspectionCanary})
	j := &models.OutboundJob{TenantID: c.Tenant.ID, ZoneID: c.Zone.ID, UserID: &c.Users["sender"].ID, SenderUserID: &c.Users["sender"].ID, SenderMailboxID: &c.Shared.ID, MailFrom: c.Shared.FullAddress, RcptTo: []string{"visible@fixture.test", "private@fixture.test"}, To: []string{"visible@fixture.test"}, BCC: []string{"private@fixture.test"}, Subject: "inspection-subject", TextBody: "inspection-body", HTMLBody: "<p>inspection-body</p>", HeadersJSON: headers, State: models.OutboundSent, DeliveryToken: &token, ClaimedAt: &now, LeaseUntil: &now, LastError: r5InspectionCanary, SMTPResponse: r5InspectionCanary}
	if err := f.Store.CreateOutboundJob(context.Background(), j); err != nil {
		t.Fatal(err)
	}
	_, err := f.Pool.Exec(context.Background(), `UPDATE outbound_recipients SET state=CASE WHEN address=$2 THEN 'accepted' ELSE 'permanent' END,smtp_code=CASE WHEN address=$2 THEN 250 ELSE 550 END,diagnostic='5.1.1 '||$3 WHERE job_id=$1`, j.ID, j.To[0], r5InspectionCanary)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.Pool.Exec(context.Background(), `UPDATE outbound_jobs SET delivery_token=$2,claimed_at=clock_timestamp(),lease_until=clock_timestamp()+interval '1 hour' WHERE id=$1`, j.ID, token)
	if err != nil {
		t.Fatal(err)
	}
	current, err := f.Store.GetOutboundJob(context.Background(), j.ID)
	if err != nil {
		t.Fatal(err)
	}
	return current
}
func r5InspectionHTTPSuper(t *testing.T, f *testpg.R5HTTPFixture) string {
	t.Helper()
	u, err := f.Store.GetUser(context.Background(), f.Companies[0].Users["reader"].ID)
	if err != nil {
		t.Fatal(err)
	}
	u.Role = models.RoleSuperAdmin
	if err = f.Store.UpdateUser(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	f.RefreshJWT(t, 0, "reader")
	return f.JWT(0, "reader")
}
func r5InspectionHTTPKey(t *testing.T, f *testpg.R5HTTPFixture, owner *uuid.UUID) string {
	t.Helper()
	raw := "tb_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	sum := sha256.Sum256([]byte(raw))
	k := &models.TenantAPIKey{TenantID: f.Companies[0].Tenant.ID, KeyHash: hex.EncodeToString(sum[:]), KeyPrefix: raw[:12], Label: "inspection fixture", Scopes: []string{"send:read", "send:write"}, OwnerUserID: owner}
	if err := f.Store.CreateAPIKey(context.Background(), k); err != nil {
		t.Fatal(err)
	}
	return raw
}
func r5InspectionHTTPCounts(t *testing.T, f *testpg.R5HTTPFixture) (int, int) {
	t.Helper()
	var a, o int
	if err := f.Pool.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM audit_log WHERE action='outbound.break_glass'),(SELECT count(*) FROM outbox_events WHERE event_type='company.admin.changed' AND payload->'metadata'->>'action'='outbound.break_glass')`).Scan(&a, &o); err != nil {
		t.Fatal(err)
	}
	return a, o
}

// This regression is intentionally against the shipping router/handler, not an
// injected app service. Before P2-060 it returns custom headers and raw SMTP
// diagnostics and commits no company.admin.changed outbox row.
func TestR5OutboundInspectionHTTPProjectionAndAtomicAudit(t *testing.T) {
	f := testpg.NewR5HTTPFixture(t)
	j := r5InspectionHTTPJob(t, f, 0)
	token := r5InspectionHTTPSuper(t, f)
	a, o := r5InspectionHTTPCounts(t, f)
	raw := r5InspectionHTTPCall(t, f, token, j.TenantID, j.ID, r5InspectionReason, 200)
	if strings.Contains(string(raw), r5InspectionCanary) {
		t.Fatal("inspection leaked internal secret canary")
	}
	var v struct {
		Data struct {
			Job struct {
				Status string   `json:"status"`
				BCC    []string `json:"bcc"`
				Text   string   `json:"text_body"`
			}
			Recipients []struct {
				Kind     string `json:"kind"`
				Enhanced string `json:"enhanced_code"`
			}
		}
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	if v.Data.Job.Status != "partially_accepted" || len(v.Data.Job.BCC) != 1 || v.Data.Job.Text != "inspection-body" || len(v.Data.Recipients) != 2 {
		t.Fatal("inspection did not use complete ledger and controlled content DTO")
	}
	for _, r := range v.Data.Recipients {
		if r.Kind == "" || r.Enhanced != "5.1.1" {
			t.Fatal("recipient lacks safe category/code projection")
		}
	}
	aa, oo := r5InspectionHTTPCounts(t, f)
	if aa != a+1 || oo != o+1 {
		t.Fatal("inspection audit and outbox were not committed together")
	}
}
func TestR5OutboundInspectionHTTPSevenPrincipalsAndSelectedCompany(t *testing.T) {
	f := testpg.NewR5HTTPFixture(t)
	j := r5InspectionHTTPJob(t, f, 0)
	a, o := r5InspectionHTTPCounts(t, f)
	for _, tc := range []struct {
		name, token string
		want        int
	}{{"reader", f.JWT(0, "reader"), 403}, {"organizer", f.JWT(0, "organizer"), 403}, {"historical-sender", f.JWT(0, "sender"), 403}, {"tenant-admin", f.JWT(0, "admin"), 403}, {"frozen", f.JWT(0, "frozen"), 401}, {"user-owned-key", r5InspectionHTTPKey(t, f, &f.Companies[0].Admin.ID), 403}, {"ownerless-key", r5InspectionHTTPKey(t, f, nil), 403}} {
		t.Run(tc.name, func(t *testing.T) {
			r5InspectionHTTPCall(t, f, tc.token, j.TenantID, j.ID, r5InspectionReason, tc.want)
		})
	}
	aa, oo := r5InspectionHTTPCounts(t, f)
	if aa != a || oo != o {
		t.Fatal("denied principals caused audit effects")
	}
	token := r5InspectionHTTPSuper(t, f)
	other := r5InspectionHTTPJob(t, f, 1)
	r5InspectionHTTPCall(t, f, token, other.TenantID, other.ID, r5InspectionReason, 200)
	r5InspectionHTTPCall(t, f, token, j.TenantID, other.ID, r5InspectionReason, 404)
	r5InspectionHTTPCall(t, f, token, j.TenantID, uuid.New(), r5InspectionReason, 404)
	for _, reason := range []string{"short", strings.Repeat("x", 1001)} {
		r5InspectionHTTPCall(t, f, token, j.TenantID, j.ID, reason, 400)
	}
}
func TestR5OutboundInspectionHTTPAuditAndOutboxFaultNoPayload(t *testing.T) {
	for _, table := range []string{"audit_log", "outbox_events"} {
		t.Run(table, func(t *testing.T) {
			f := testpg.NewR5HTTPFixture(t)
			j := r5InspectionHTTPJob(t, f, 0)
			token := r5InspectionHTTPSuper(t, f)
			a, o := r5InspectionHTTPCounts(t, f)
			_, err := f.Pool.Exec(context.Background(), `CREATE FUNCTION r5_inspection_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'controlled inspection fault'; END $$; CREATE TRIGGER r5_inspection_fault BEFORE INSERT ON `+table+` FOR EACH ROW EXECUTE FUNCTION r5_inspection_fault()`)
			if err != nil {
				t.Fatal(err)
			}
			r5InspectionHTTPCall(t, f, token, j.TenantID, j.ID, r5InspectionReason, 500)
			aa, oo := r5InspectionHTTPCounts(t, f)
			if aa != a || oo != o {
				t.Fatal("fault leaked committed audit/outbox effects")
			}
			current, err := f.Store.GetOutboundJob(context.Background(), j.ID)
			if err != nil {
				t.Fatal(err)
			}
			if current.State != j.State || !current.UpdatedAt.Equal(j.UpdatedAt) {
				t.Fatal("inspection fault mutated job")
			}
		})
	}
}

func r5InspectionHTTPWait(t *testing.T, f *testpg.R5HTTPFixture, ctx context.Context, blocker uint32, table string) {
	t.Helper()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		var pid int
		err := f.Pool.QueryRow(ctx, `SELECT pid FROM pg_stat_activity WHERE datname=current_database() AND state='active' AND $1::int=ANY(pg_blocking_pids(pid)) AND query LIKE $2 LIMIT 1`, int32(blocker), "%"+table+"%").Scan(&pid)
		if err == nil {
			return
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Fatal(err)
		}
		select {
		case <-ctx.Done():
			t.Fatal("HTTP inspection lock waiter not observed")
		case <-tick.C:
		}
	}
}
func TestR5OutboundInspectionHTTPFreshActorAfterLockWait(t *testing.T) {
	for _, change := range []string{"demote", "freeze", "epoch"} {
		t.Run(change, func(t *testing.T) {
			f := testpg.NewR5HTTPFixture(t)
			j := r5InspectionHTTPJob(t, f, 0)
			token := r5InspectionHTTPSuper(t, f)
			a, o := r5InspectionHTTPCounts(t, f)
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			hold, err := f.Pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer hold.Rollback(context.Background())
			uid := f.Companies[0].Users["reader"].ID
			_, err = hold.Exec(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, uid)
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan r5InspectionHTTPResult, 1)
			go func() { done <- r5InspectionHTTPRequest(ctx, f, token, j.TenantID, j.ID, r5InspectionReason) }()
			r5InspectionHTTPWait(t, f, ctx, hold.Conn().PgConn().PID(), "users")
			switch change {
			case "demote":
				_, err = hold.Exec(ctx, `UPDATE users SET role='admin' WHERE id=$1`, uid)
			case "freeze":
				_, err = hold.Exec(ctx, `UPDATE users SET is_active=false WHERE id=$1`, uid)
			case "epoch":
				_, err = hold.Exec(ctx, `UPDATE users SET session_version=session_version+1 WHERE id=$1`, uid)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = hold.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case got := <-done:
				if got.err != nil {
					t.Fatal(got.err)
				}
				if got.status != 403 {
					t.Fatalf("stale HTTP actor status=%d want=403", got.status)
				}
				if strings.Contains(string(got.body), "inspection-body") {
					t.Fatal("revoked HTTP actor received content")
				}
			case <-ctx.Done():
				t.Fatal("inspection HTTP lock waiter did not complete")
			}
			aa, oo := r5InspectionHTTPCounts(t, f)
			if aa != a || oo != o {
				t.Fatal("revoked HTTP actor left audit effects")
			}
		})
	}
}
func TestR5OutboundInspectionHTTPBusySnapshotNoPayload(t *testing.T) {
	for _, table := range []string{"outbound_jobs", "outbound_recipients"} {
		t.Run(table, func(t *testing.T) {
			f := testpg.NewR5HTTPFixture(t)
			j := r5InspectionHTTPJob(t, f, 0)
			token := r5InspectionHTTPSuper(t, f)
			a, o := r5InspectionHTTPCounts(t, f)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			hold, err := f.Pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer hold.Rollback(context.Background())
			q := `SELECT id FROM outbound_jobs WHERE id=$1 FOR UPDATE`
			if table == "outbound_recipients" {
				q = `SELECT address FROM outbound_recipients WHERE job_id=$1 FOR UPDATE`
			}
			if _, err = hold.Exec(ctx, q, j.ID); err != nil {
				t.Fatal(err)
			}
			got := r5InspectionHTTPRequest(ctx, f, token, j.TenantID, j.ID, r5InspectionReason)
			if got.err != nil {
				t.Fatal(got.err)
			}
			if got.status != 409 {
				t.Fatalf("busy inspection HTTP status=%d want=409", got.status)
			}
			if strings.Contains(string(got.body), "inspection-body") {
				t.Fatal("busy inspection leaked content")
			}
			aa, oo := r5InspectionHTTPCounts(t, f)
			if aa != a || oo != o {
				t.Fatal("busy inspection left audit effects")
			}
		})
	}
}
