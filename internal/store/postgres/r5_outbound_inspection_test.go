package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
	"tabmail/internal/testpg"
)

const r5OutboundInspectionReason = "Controlled atomic outbound inspection"
const r5OutboundInspectionSecret = "PRIVATE-SYSTEM-SECRET-NEVER-DISCLOSE"

type r5OutboundInspectionResult struct {
	view *company.OutboundInspection
	err  error
}

func r5OutboundInspectionFixture(t *testing.T) (*testpg.R5Fixture, authz.Actor, *models.OutboundJob) {
	t.Helper()
	f := testpg.NewR5Fixture(t)
	c := f.Companies[0]
	u, err := f.Store.GetUser(context.Background(), c.Admin.ID)
	must(t, err)
	u.Role = models.RoleSuperAdmin
	must(t, f.Store.UpdateUser(context.Background(), u))
	version := u.SessionVersion
	a := authz.Actor{Type: authz.PrincipalUser, ID: u.ID, TenantID: c.Tenant.ID, Role: models.RoleSuperAdmin, IsSuperAdmin: true, SessionVersion: &version}
	raw, _ := json.Marshal(map[string]string{"From": c.Shared.FullAddress, "Content-Type": "text/plain", "X-Private": r5OutboundInspectionSecret, "Bcc": r5OutboundInspectionSecret})
	j := &models.OutboundJob{TenantID: c.Tenant.ID, ZoneID: c.Zone.ID, UserID: &c.Users["sender"].ID, SenderUserID: &c.Users["sender"].ID, SenderMailboxID: &c.Shared.ID, MailFrom: c.Shared.FullAddress, RcptTo: []string{"public@fixture.test", "hidden@fixture.test"}, To: []string{"public@fixture.test"}, BCC: []string{"hidden@fixture.test"}, Subject: "controlled-content", TextBody: "controlled-body", HeadersJSON: raw, State: models.OutboundSent, LastError: r5OutboundInspectionSecret, SMTPResponse: r5OutboundInspectionSecret}
	must(t, f.Store.CreateOutboundJob(context.Background(), j))
	_, err = f.Pool.Exec(context.Background(), `UPDATE outbound_recipients SET state=CASE WHEN address=$2 THEN 'accepted' ELSE 'permanent' END,smtp_code=550,diagnostic='5.1.1 '||$3 WHERE job_id=$1`, j.ID, j.To[0], r5OutboundInspectionSecret)
	must(t, err)
	current, err := f.Store.GetOutboundJob(context.Background(), j.ID)
	must(t, err)
	return f, a, current
}
func r5OutboundInspectionCounts(t *testing.T, f *testpg.R5Fixture) (int, int) {
	t.Helper()
	var a, o int
	must(t, f.Pool.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM audit_log WHERE action='outbound.break_glass'),(SELECT count(*) FROM outbox_events WHERE event_type='company.admin.changed' AND payload->'metadata'->>'action'='outbound.break_glass')`).Scan(&a, &o))
	return a, o
}
func r5OutboundInspectionWait(t *testing.T, f *testpg.R5Fixture, ctx context.Context, blocker uint32, table string) {
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
			t.Fatal("inspection lock waiter not observed")
		case <-tick.C:
		}
	}
}
func r5OutboundInspectionAwait(t *testing.T, ctx context.Context, done <-chan r5OutboundInspectionResult) r5OutboundInspectionResult {
	t.Helper()
	select {
	case r := <-done:
		return r
	case <-ctx.Done():
		t.Fatal("inspection did not finish within deadline")
		return r5OutboundInspectionResult{}
	}
}
func r5OutboundInspectionDeny(t *testing.T, r r5OutboundInspectionResult, kind app.ErrorKind) {
	t.Helper()
	if r.view != nil {
		t.Fatal("failed inspection returned payload")
	}
	e, ok := app.As(app.FromAuthz(r.err))
	if !ok || e.Kind != kind {
		t.Fatalf("inspection error=%v want=%s", r.err, kind)
	}
}

func TestR5OutboundInspectionPGCurrentAuthorityAfterWait(t *testing.T) {
	for _, mutation := range []string{"demote", "freeze", "epoch", "tenant-demotion"} {
		t.Run(mutation, func(t *testing.T) {
			f, a, j := r5OutboundInspectionFixture(t)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			aa, oo := r5OutboundInspectionCounts(t, f)
			hold, err := f.Pool.Begin(ctx)
			must(t, err)
			defer hold.Rollback(context.Background())
			_, err = hold.Exec(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, a.ID)
			must(t, err)
			done := make(chan r5OutboundInspectionResult, 1)
			go func() {
				v, e := f.Store.InspectOutboundRecovery(ctx, a, j.ID, r5OutboundInspectionReason)
				done <- r5OutboundInspectionResult{v, e}
			}()
			r5OutboundInspectionWait(t, f, ctx, hold.Conn().PgConn().PID(), "users")
			// Parent FK fence is already held before the user wait.
			probe, err := f.Pool.Begin(ctx)
			must(t, err)
			_, err = probe.Exec(ctx, `SELECT id FROM tenants WHERE id=$1 FOR UPDATE NOWAIT`, a.TenantID)
			r5RequireLockConflict(t, err)
			must(t, probe.Rollback(ctx))
			switch mutation {
			case "demote":
				_, err = hold.Exec(ctx, `UPDATE users SET role='admin' WHERE id=$1`, a.ID)
			case "freeze":
				_, err = hold.Exec(ctx, `UPDATE users SET is_active=false WHERE id=$1`, a.ID)
			case "epoch":
				_, err = hold.Exec(ctx, `UPDATE users SET session_version=session_version+1 WHERE id=$1`, a.ID)
			case "tenant-demotion":
				_, err = hold.Exec(ctx, `UPDATE users SET tenant_id=$2,role='user' WHERE id=$1`, a.ID, f.Companies[1].Tenant.ID)
			}
			must(t, err)
			must(t, hold.Commit(ctx))
			r5OutboundInspectionDeny(t, r5OutboundInspectionAwait(t, ctx, done), app.KindForbidden)
			x, y := r5OutboundInspectionCounts(t, f)
			if x != aa || y != oo {
				t.Fatal("revoked actor caused audit effects")
			}
		})
	}
}
func TestR5OutboundInspectionPGBusyJobAndLedger(t *testing.T) {
	for _, table := range []string{"outbound_jobs", "outbound_recipients"} {
		t.Run(table, func(t *testing.T) {
			f, a, j := r5OutboundInspectionFixture(t)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			hold, err := f.Pool.Begin(ctx)
			must(t, err)
			defer hold.Rollback(context.Background())
			query := `SELECT id FROM outbound_jobs WHERE id=$1 FOR UPDATE`
			if table == "outbound_recipients" {
				query = `SELECT address FROM outbound_recipients WHERE job_id=$1 FOR UPDATE`
			}
			_, err = hold.Exec(ctx, query, j.ID)
			must(t, err)
			v, e := f.Store.InspectOutboundRecovery(ctx, a, j.ID, r5OutboundInspectionReason)
			r5OutboundInspectionDeny(t, r5OutboundInspectionResult{v, e}, app.KindConflict)
			if ctx.Err() != nil {
				t.Fatal("busy inspection waited instead of NOWAIT")
			}
		})
	}
}
func TestR5OutboundInspectionPGLedgerAndJobSnapshotFence(t *testing.T) {
	f, a, j := r5OutboundInspectionFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	token := uuid.New()
	_, err := f.Pool.Exec(ctx, `UPDATE outbound_jobs SET state='processing',delivery_token=$2,lease_until=clock_timestamp()+interval '1 hour' WHERE id=$1`, j.ID, token)
	must(t, err)
	_, err = f.Pool.Exec(ctx, `UPDATE outbound_recipients SET state='pending' WHERE job_id=$1`, j.ID)
	must(t, err)
	hold, err := f.Pool.Begin(ctx)
	must(t, err)
	defer hold.Rollback(context.Background())
	_, err = hold.Exec(ctx, `LOCK TABLE audit_log IN SHARE MODE`)
	must(t, err)
	done := make(chan r5OutboundInspectionResult, 1)
	go func() {
		v, e := f.Store.InspectOutboundRecovery(ctx, a, j.ID, r5OutboundInspectionReason)
		done <- r5OutboundInspectionResult{v, e}
	}()
	r5OutboundInspectionWait(t, f, ctx, hold.Conn().PgConn().PID(), "audit_log")
	// Real pre-network worker transition is job->ledger and must remain fenced
	// while the inspection waits for its required audit write.
	writer := make(chan error, 1)
	go func() {
		ok, e := f.Store.BeginOutboundRecipient(ctx, j.ID, &token, j.To[0])
		if e == nil && !ok {
			e = errors.New("worker recipient not started")
		}
		writer <- e
	}()
	var inspectPID uint32
	must(t, f.Pool.QueryRow(ctx, `SELECT pid FROM pg_stat_activity WHERE datname=current_database() AND $1::int=ANY(pg_blocking_pids(pid)) AND query LIKE '%audit_log%' LIMIT 1`, int32(hold.Conn().PgConn().PID())).Scan(&inspectPID))
	r5OutboundInspectionWait(t, f, ctx, inspectPID, "outbound_jobs")
	must(t, hold.Rollback(ctx))
	r := r5OutboundInspectionAwait(t, ctx, done)
	must(t, r.err)
	if r.view.Job.State != models.OutboundProcessing || r.view.Job.Status != "sending" || len(r.view.Recipients) != 2 {
		t.Fatal("inspection snapshot incoherent")
	}
	for _, row := range r.view.Recipients {
		if row.State != "pending" {
			t.Fatal("inspection mixed post-worker ledger into pre-worker job")
		}
	}
	select {
	case err = <-writer:
		must(t, err)
	case <-ctx.Done():
		t.Fatal("worker not released after inspection commit")
	}
	next, err := f.Store.InspectOutboundRecovery(ctx, a, j.ID, r5OutboundInspectionReason)
	must(t, err)
	if next.Job.Status != "needs_attention" {
		t.Fatal("new uncertainty missing from next complete ledger")
	}
}
func TestR5OutboundInspectionPGFaultZeroPayloadAndEffects(t *testing.T) {
	for _, table := range []string{"audit_log", "outbox_events"} {
		t.Run(table, func(t *testing.T) {
			f, a, j := r5OutboundInspectionFixture(t)
			aa, oo := r5OutboundInspectionCounts(t, f)
			_, err := f.Pool.Exec(context.Background(), `CREATE FUNCTION r5_inspection_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'controlled inspection failure'; END $$; CREATE TRIGGER r5_inspection_fault BEFORE INSERT ON `+table+` FOR EACH ROW EXECUTE FUNCTION r5_inspection_fault()`)
			must(t, err)
			v, err := f.Store.InspectOutboundRecovery(context.Background(), a, j.ID, r5OutboundInspectionReason)
			if v != nil || err == nil {
				t.Fatal("failed required audit returned payload")
			}
			x, y := r5OutboundInspectionCounts(t, f)
			if x != aa || y != oo {
				t.Fatal("failed inspection left audit/outbox effects")
			}
			current, e := f.Store.GetOutboundJob(context.Background(), j.ID)
			must(t, e)
			if current.State != j.State || !current.UpdatedAt.Equal(j.UpdatedAt) {
				t.Fatal("inspection fault changed job")
			}
		})
	}
}
func TestR5OutboundInspectionPGSafeCompleteProjection(t *testing.T) {
	f, a, j := r5OutboundInspectionFixture(t)
	view, err := f.Store.InspectOutboundRecovery(context.Background(), a, j.ID, r5OutboundInspectionReason)
	must(t, err)
	raw, err := json.Marshal(view)
	must(t, err)
	if strings.Contains(string(raw), r5OutboundInspectionSecret) {
		t.Fatal("internal secret leaked")
	}
	if view.Job.Status != "partially_accepted" || len(view.Job.BCC) != 1 || len(view.Recipients) != 2 {
		t.Fatal("complete private recipient ledger lost")
	}
	for _, test := range []struct{ tenant, id uuid.UUID }{{f.Companies[1].Tenant.ID, j.ID}, {a.TenantID, uuid.New()}} {
		actor := a
		actor.TenantID = test.tenant
		v, e := f.Store.InspectOutboundRecovery(context.Background(), actor, test.id, r5OutboundInspectionReason)
		r5OutboundInspectionDeny(t, r5OutboundInspectionResult{v, e}, app.KindNotFound)
	}
	// A malformed/oversized legacy ledger is never truncated into all-accepted.
	for i := 0; i < 50; i++ {
		_, err = f.Pool.Exec(context.Background(), `INSERT INTO outbound_recipients(tenant_id,job_id,address,state) VALUES($1,$2,$3,'accepted')`, a.TenantID, j.ID, uuid.NewString()+"@fixture.test")
		must(t, err)
	}
	v, e := f.Store.InspectOutboundRecovery(context.Background(), a, j.ID, r5OutboundInspectionReason)
	r5OutboundInspectionDeny(t, r5OutboundInspectionResult{v, e}, app.KindConflict)
}
