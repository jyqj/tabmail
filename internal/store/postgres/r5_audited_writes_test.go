package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
)

// Each case uses an independent testpg database and the production command.
// SQL locks only arrange a known wait; no sleeps or production fault hooks.
var r5AuditedKinds = []string{"message", "template-save", "template-retire", "template-revoke", "template-create"}

type r5AuditedCase struct {
	actor    authz.Actor
	lockSQL  string
	lockArgs []any
	run      func(context.Context) error
}

func r5AuditedFixture(t *testing.T, f *companyFixture, kind string) r5AuditedCase {
	t.Helper()
	ctx := context.Background()
	if kind == "message" {
		m := &models.Message{TenantID: f.tenant.ID, MailboxID: f.personal.ID, ZoneID: f.zone.ID, Sender: "fixture@sender.test", Recipients: []string{f.personal.FullAddress}, Subject: "audited boundary", RawObjectKey: "audit-" + uuid.NewString()}
		must(t, f.st.CreateMessage(ctx, m))
		return r5AuditedCase{f.u, `SELECT id FROM messages WHERE id=$1 FOR UPDATE`, []any{m.ID}, func(ctx context.Context) error {
			return f.st.MutateWorkMessage(ctx, f.u, f.personal.ID, m.ID, "archive")
		}}
	}
	if kind == "template-create" {
		return r5AuditedCase{f.a, `LOCK TABLE mail_templates IN SHARE MODE`, nil, func(ctx context.Context) error {
			_, e := f.st.SaveMailTemplate(ctx, f.a, company.Template{Name: "New audited template", Draft: templateDraft()})
			return e
		}}
	}
	tpl, e := f.st.SaveMailTemplate(ctx, f.a, company.Template{Name: "Audited template " + uuid.NewString(), Draft: templateDraft()})
	must(t, e)
	c := r5AuditedCase{actor: f.a, lockSQL: `SELECT id FROM mail_templates WHERE id=$1 FOR UPDATE`, lockArgs: []any{tpl.ID}}
	switch kind {
	case "template-save":
		tpl.Name = "Changed " + tpl.Name
		c.run = func(ctx context.Context) error { _, e := f.st.SaveMailTemplate(ctx, f.a, *tpl); return e }
	case "template-retire":
		c.run = func(ctx context.Context) error {
			return f.st.SetMailTemplateRetired(ctx, f.a, tpl.ID, tpl.Revision, true)
		}
	case "template-revoke":
		v, e := f.st.PublishMailTemplate(ctx, f.a, tpl.ID, tpl.Revision)
		must(t, e)
		tpl.Revision++
		c.lockSQL = `SELECT id FROM mail_template_versions WHERE id=$1 FOR UPDATE`
		c.lockArgs = []any{v.ID}
		c.run = func(ctx context.Context) error {
			return f.st.RevokeMailTemplateVersion(ctx, f.a, tpl.ID, v.Version, tpl.Revision)
		}
	default:
		t.Fatalf("unknown audited case %q", kind)
	}
	return c
}

func r5AuditedSnapshot(t *testing.T, f *companyFixture) string {
	t.Helper()
	var snapshot string
	must(t, f.pool.QueryRow(context.Background(), `SELECT jsonb_build_object(
 'messages',(SELECT jsonb_agg(to_jsonb(m) ORDER BY id) FROM messages m),
 'personal',(SELECT jsonb_agg(to_jsonb(s) ORDER BY mailbox_id,message_id,user_id) FROM message_user_states s),
 'templates',(SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM mail_templates t),
 'versions',(SELECT jsonb_agg(to_jsonb(v) ORDER BY id) FROM mail_template_versions v),
 'audit',(SELECT count(*) FROM audit_log), 'outbox',(SELECT count(*) FROM outbox_events),
 'events',(SELECT count(*) FROM mailbox_event_log))::text`).Scan(&snapshot))
	return snapshot
}
func r5AuditedHold(t *testing.T, f *companyFixture, ctx context.Context, c r5AuditedCase) pgx.Tx {
	t.Helper()
	tx, e := f.pool.Begin(ctx)
	must(t, e)
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	_, e = tx.Exec(ctx, c.lockSQL, c.lockArgs...)
	must(t, e)
	return tx
}
func r5AuditedForbidden(t *testing.T, e error) {
	t.Helper()
	v, ok := app.As(app.FromAuthz(e))
	if !ok || v.Kind != app.KindForbidden {
		t.Fatalf("expected current authority denial, got %v", e)
	}
}

// Two complete application commands: a mutation and a privileged actor freeze.
// The old code holds user SHARE, then waits for the tenant FK at audit/INSERT;
// freezing holds tenant UPDATE, then waits for that user. Do not swallow 40P01.
func TestR5AuditedWritesOrderMemberFreeze(t *testing.T) {
	for _, kind := range r5AuditedKinds {
		t.Run(kind, func(t *testing.T) {
			f := seedCompany(t)
			c := r5AuditedFixture(t, f, kind)
			supervisor := &models.User{TenantID: f.tenant.ID, Email: "supervisor@fixture.test", Role: models.RoleSuperAdmin, IsActive: true, PasswordHash: "test-only"}
			must(t, f.st.CreateUser(context.Background(), supervisor))
			admin := authz.Actor{Type: authz.PrincipalUser, ID: supervisor.ID, TenantID: f.tenant.ID, Role: models.RoleSuperAdmin, IsSuperAdmin: true, IsAdmin: true}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			hold := r5AuditedHold(t, f, ctx, c)
			done := make(chan error, 1)
			go func() { done <- c.run(ctx) }()
			writer := r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "")
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
				t.Fatalf("mutation/freeze must complete in order: mutation=%v freeze=%v", first, second)
			}
			before := r5AuditedSnapshot(t, f)
			r5AuditedForbidden(t, c.run(ctx))
			if after := r5AuditedSnapshot(t, f); after != before {
				t.Fatal("frozen actor left changes or audit records")
			}
		})
	}
}

func TestR5AuditedWritesParentPrecedesActor(t *testing.T) {
	for _, kind := range r5AuditedKinds {
		t.Run(kind, func(t *testing.T) {
			f := seedCompany(t)
			c := r5AuditedFixture(t, f, kind)
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
			r5RequireLockConflict(t, e)
			must(t, probe.Rollback(ctx))
			must(t, hold.Rollback(ctx))
			r5AwaitOperation(t, ctx, done)
		})
	}
}

func TestR5AuditedWritesReloadActorAfterParentWait(t *testing.T) {
	for _, kind := range r5AuditedKinds {
		t.Run(kind, func(t *testing.T) {
			f := seedCompany(t)
			c := r5AuditedFixture(t, f, kind)
			before := r5AuditedSnapshot(t, f)
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			hold, e := f.pool.Begin(ctx)
			must(t, e)
			defer hold.Rollback(context.Background())
			_, e = hold.Exec(ctx, `SELECT id FROM tenants WHERE id=$1 FOR UPDATE`, f.tenant.ID)
			must(t, e)
			done := make(chan error, 1)
			go func() { done <- c.run(ctx) }()
			// Observe either an early parent wait or the old late FK wait. The
			// NOWAIT user probe below distinguishes them without a timeout oracle.
			r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "")
			_, e = hold.Exec(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE NOWAIT`, c.actor.ID)
			must(t, e)
			_, e = hold.Exec(ctx, `UPDATE users SET is_active=false WHERE id=$1`, c.actor.ID)
			must(t, e)
			must(t, hold.Commit(ctx))
			r5AuditedForbidden(t, r5ConcurrentResult(t, ctx, done))
			if r5AuditedSnapshot(t, f) != before {
				t.Fatal("authority changed during wait but command left changes")
			}
		})
	}
}

func TestR5AuditedIndependentWritesShareParent(t *testing.T) {
	for _, kind := range r5AuditedKinds[:4] {
		t.Run(kind, func(t *testing.T) {
			f := seedCompany(t)
			first := r5AuditedFixture(t, f, kind)
			second := r5AuditedFixture(t, f, kind)
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			hold := r5AuditedHold(t, f, ctx, first)
			done := make(chan error, 1)
			go func() { done <- first.run(ctx) }()
			r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "")
			probe, e := f.pool.Begin(ctx)
			must(t, e)
			defer probe.Rollback(context.Background())
			_, e = probe.Exec(ctx, `SELECT id FROM tenants WHERE id=$1 FOR KEY SHARE NOWAIT`, f.tenant.ID)
			must(t, e)
			must(t, probe.Rollback(ctx))
			otherCtx, stop := context.WithTimeout(ctx, 4*time.Second)
			defer stop()
			must(t, second.run(otherCtx))
			must(t, hold.Rollback(ctx))
			r5AwaitOperation(t, ctx, done)
		})
	}
}

func TestR5AuditedWritesAuditFailureRollsBack(t *testing.T) {
	for _, kind := range r5AuditedKinds {
		t.Run(kind, func(t *testing.T) {
			f := seedCompany(t)
			c := r5AuditedFixture(t, f, kind)
			before := r5AuditedSnapshot(t, f)
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			_, e := f.pool.Exec(ctx, `ALTER TABLE audit_log ADD CONSTRAINT b01p_audit_failure CHECK(action NOT IN ('message.archive','template.save','template.retire','template.version.revoke')) NOT VALID`)
			must(t, e)
			if e = c.run(ctx); e == nil {
				t.Fatal("required audit failure was ignored")
			}
			if r5AuditedSnapshot(t, f) != before {
				t.Fatal("failed audit left data, revision or event writes")
			}
			_, e = f.pool.Exec(ctx, `ALTER TABLE audit_log DROP CONSTRAINT b01p_audit_failure`)
			must(t, e)
			must(t, c.run(ctx))
		})
	}
}

func TestR5MessageMutationReloadsGrantAfterMailboxWait(t *testing.T) {
	for _, action := range []string{"archive", "starred"} {
		t.Run(action, func(t *testing.T) {
			f := seedCompany(t)
			m, _, _ := r5ReadFixture(t, f)
			must(t, grantCurrent(f.st, context.Background(), f.a, models.MailboxGrant{TenantID: f.tenant.ID, MailboxID: f.shared.ID, UserID: f.employee.ID, CanRead: true, CanOrganize: true, CanSend: true}))
			before := r5AuditedSnapshot(t, f)
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			hold, e := f.pool.Begin(ctx)
			must(t, e)
			defer hold.Rollback(context.Background())
			_, e = hold.Exec(ctx, `SELECT id FROM mailboxes WHERE id=$1 FOR UPDATE`, f.shared.ID)
			must(t, e)
			done := make(chan error, 1)
			go func() { done <- f.st.MutateWorkMessage(ctx, f.u, f.shared.ID, m.ID, action) }()
			r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "mailboxes")
			_, e = hold.Exec(ctx, `UPDATE mailbox_grants SET can_read=false,can_organize=false WHERE mailbox_id=$1 AND user_id=$2`, f.shared.ID, f.employee.ID)
			must(t, e)
			_, e = hold.Exec(ctx, `UPDATE mailboxes SET lifecycle_revision=lifecycle_revision+1 WHERE id=$1`, f.shared.ID)
			must(t, e)
			must(t, hold.Commit(ctx))
			r5AuditedForbidden(t, r5ConcurrentResult(t, ctx, done))
			if r5AuditedSnapshot(t, f) != before {
				t.Fatal("revoked message operation changed content/state/events")
			}
		})
	}
}
