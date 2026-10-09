package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
	"tabmail/internal/store"
)

var r5RecoveryParentKinds = []string{"inspect", "retry", "reconcile"}

// Only fixture data and controlled blockers use SQL. Commands below are the
// production PgStore entry points and run in fresh testpg databases.
func r5RecoveryParentFixture(t *testing.T, f *companyFixture, kind string) r5AuditedCase {
	t.Helper()
	ctx := context.Background()
	_, e := f.pool.Exec(ctx, `UPDATE users SET role='super_admin' WHERE id=$1`, f.admin.ID)
	must(t, e)
	a := f.a
	a.Role, a.IsSuperAdmin = models.RoleSuperAdmin, true
	c := r5AuditedCase{actor: a, lockSQL: `LOCK TABLE audit_log IN SHARE MODE`}
	const reason = "Controlled recovery parent-order verification"
	if kind == "reconcile" {
		j := &models.OutboundJob{TenantID: f.tenant.ID, ZoneID: f.zone.ID, UserID: &f.employee.ID, SenderUserID: &f.employee.ID, SenderMailboxID: &f.personal.ID, MailFrom: f.personal.FullAddress, RcptTo: []string{"uncertain@fixture.test"}, To: []string{"uncertain@fixture.test"}, Subject: "recovery lock fixture", TextBody: "synthetic", State: models.OutboundRetry}
		must(t, f.st.CreateOutboundJob(ctx, j))
		_, e = f.pool.Exec(ctx, `UPDATE outbound_jobs SET recipient_ledger=true,in_flight_domain='fixture.test' WHERE id=$1`, j.ID)
		must(t, e)
		_, e = f.pool.Exec(ctx, `INSERT INTO outbound_recipients(tenant_id,job_id,address,state) VALUES($1,$2,'uncertain@fixture.test','uncertain') ON CONFLICT(job_id,address) DO UPDATE SET state='uncertain'`, f.tenant.ID, j.ID)
		must(t, e)
		current, e := f.st.GetOutboundJob(ctx, j.ID)
		must(t, e)
		c.run = func(ctx context.Context) error {
			return f.st.ReconcileOutbound(ctx, a, j.ID, current.UpdatedAt, []company.Recipient{{Address: "uncertain@fixture.test", State: "accepted"}}, reason)
		}
		return c
	}
	j := r5IngressJob(f.personal.FullAddress)
	hash := company.Hash("recovery-parent-synthetic")
	must(t, f.st.CreateIngress(ctx, j, []store.IngressTarget{r5IngressTarget(f, f.personal)}, hash, 1))
	var version time.Time
	must(t, f.pool.QueryRow(ctx, `UPDATE ingest_jobs SET state='retry' WHERE id=$1 RETURNING updated_at`, j.ID).Scan(&version))
	_, e = f.pool.Exec(ctx, `UPDATE ingest_recipient_outcomes SET state='held',attempts=2,last_error='controlled fixture' WHERE job_id=$1`, j.ID)
	must(t, e)
	switch kind {
	case "inspect":
		c.run = func(ctx context.Context) error { _, e := f.st.InspectRecoveryReceipt(ctx, a, j.ID, reason); return e }
	case "retry":
		c.run = func(ctx context.Context) error {
			return f.st.RetryRecoveryReceipt(ctx, a, j.ID, version, []uuid.UUID{f.personal.ID}, reason, hash)
		}
	default:
		t.Fatalf("unknown recovery kind %q", kind)
	}
	return c
}

func r5RecoveryParentSnapshot(t *testing.T, f *companyFixture) string {
	t.Helper()
	var v string
	must(t, f.pool.QueryRow(context.Background(), `SELECT jsonb_build_object(
 'ingest',(SELECT jsonb_agg(to_jsonb(j) ORDER BY id) FROM ingest_jobs j),
 'targets',(SELECT jsonb_agg(to_jsonb(r) ORDER BY job_id,mailbox_id) FROM ingest_recipient_outcomes r),
 'outbound',(SELECT jsonb_agg(to_jsonb(j) ORDER BY id) FROM outbound_jobs j),
 'recipients',(SELECT jsonb_agg(to_jsonb(r) ORDER BY job_id,address) FROM outbound_recipients r),
 'audits',(SELECT count(*) FROM audit_log WHERE action LIKE 'ingress.%' OR action='outbound.reconcile'),
 'events',(SELECT count(*) FROM outbox_events WHERE event_type LIKE 'ingress.%' OR event_type='outbound.reconcile'))::text`).Scan(&v))
	return v
}

func TestR5RecoveryAuditParentPrecedesActor(t *testing.T) {
	for _, kind := range r5RecoveryParentKinds {
		t.Run(kind, func(t *testing.T) {
			f := seedCompany(t)
			c := r5RecoveryParentFixture(t, f, kind)
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			hold, e := f.pool.Begin(ctx)
			must(t, e)
			defer hold.Rollback(context.Background())
			_, e = hold.Exec(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, c.actor.ID)
			must(t, e)
			done := make(chan error, 1)
			go func() { done <- c.run(ctx) }()
			r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "users")
			probe, e := f.pool.Begin(ctx)
			must(t, e)
			defer probe.Rollback(context.Background())
			_, e = probe.Exec(ctx, `SELECT id FROM tenants WHERE id=$1 FOR UPDATE NOWAIT`, f.tenant.ID)
			// Release all blockers and collect the command even on the red baseline.
			must(t, probe.Rollback(ctx))
			must(t, hold.Rollback(ctx))
			r5AwaitOperation(t, ctx, done)
			r5RequireLockConflict(t, e)
		})
	}
}

func TestR5RecoveryAuditOrdersMemberFreeze(t *testing.T) {
	for _, kind := range r5RecoveryParentKinds {
		t.Run(kind, func(t *testing.T) {
			f := seedCompany(t)
			c := r5RecoveryParentFixture(t, f, kind)
			supervisor := &models.User{TenantID: f.tenant.ID, Email: "recovery-supervisor@fixture.test", Role: models.RoleSuperAdmin, IsActive: true, PasswordHash: "test-only"}
			must(t, f.st.CreateUser(context.Background(), supervisor))
			admin := authz.Actor{Type: authz.PrincipalUser, ID: supervisor.ID, TenantID: f.tenant.ID, Role: models.RoleSuperAdmin, IsSuperAdmin: true, IsAdmin: true}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			hold := r5AuditedHold(t, f, ctx, c)
			done := make(chan error, 1)
			go func() { done <- c.run(ctx) }()
			writer := r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "audit_log")
			frozen := make(chan error, 1)
			inactive := false
			go func() {
				_, e := f.st.UpdateUserGuarded(ctx, admin, f.tenant.ID, c.actor.ID, models.UserAdminPatch{IsActive: &inactive})
				frozen <- e
			}()
			r5WaitBlockedBy(t, f, ctx, writer, "")
			must(t, hold.Rollback(ctx))
			first, second := r5ConcurrentResult(t, ctx, done), r5ConcurrentResult(t, ctx, frozen)
			for _, e := range []error{first, second} {
				var pg *pgconn.PgError
				if errors.As(e, &pg) {
					t.Logf("observed SQLSTATE=%s", pg.Code)
				}
			}
			if first != nil || second != nil {
				t.Fatalf("recovery/freeze must commit in order: recovery=%v freeze=%v", first, second)
			}
			before := r5RecoveryParentSnapshot(t, f)
			r5AuditedForbidden(t, c.run(ctx))
			if r5RecoveryParentSnapshot(t, f) != before {
				t.Fatal("frozen recovery actor left resource/audit/outbox effects")
			}
		})
	}
}

func TestR5RecoveryAuditReloadsActorAfterParentWait(t *testing.T) {
	for _, kind := range r5RecoveryParentKinds {
		t.Run(kind, func(t *testing.T) {
			f := seedCompany(t)
			c := r5RecoveryParentFixture(t, f, kind)
			before := r5RecoveryParentSnapshot(t, f)
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			hold, e := f.pool.Begin(ctx)
			must(t, e)
			defer hold.Rollback(context.Background())
			_, e = hold.Exec(ctx, `SELECT id FROM tenants WHERE id=$1 FOR UPDATE`, f.tenant.ID)
			must(t, e)
			done := make(chan error, 1)
			go func() { done <- c.run(ctx) }()
			r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "")
			_, e = hold.Exec(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE NOWAIT`, c.actor.ID)
			if e != nil {
				// Do not strand a command when the baseline retains actor SHARE.
				must(t, hold.Rollback(ctx))
				r5AwaitOperation(t, ctx, done)
				t.Fatalf("actor locked before waiting for audit parent: %v", e)
			}
			_, e = hold.Exec(ctx, `UPDATE users SET is_active=false WHERE id=$1`, c.actor.ID)
			must(t, e)
			must(t, hold.Commit(ctx))
			r5AuditedForbidden(t, r5ConcurrentResult(t, ctx, done))
			if r5RecoveryParentSnapshot(t, f) != before {
				t.Fatal("parent-wait authority denial failed to roll back recovery effects")
			}
		})
	}
}
