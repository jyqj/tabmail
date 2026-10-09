package postgres_test

import (
	"context"
	"testing"
	"time"

	"tabmail/internal/app"
	"tabmail/internal/models"
)

// The audit INSERT references tenants. Its parent protection must precede both
// user and mailbox locks, not merely be moved before the final audit statement.
func TestR5SentMutationParentLockPrecedesActor(t *testing.T) {
	f := seedCompany(t)
	job := archJob(t, f, models.OutboundSent)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	hold, err := f.pool.Begin(ctx)
	must(t, err)
	defer hold.Rollback(context.Background())
	_, err = hold.Exec(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, f.employee.ID)
	must(t, err)
	done := make(chan error, 1)
	go func() { done <- f.st.MutateArchivedMail(ctx, f.u, f.personal.ID, job.ID, 1, "archive") }()
	r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "users")

	probe, err := f.pool.Begin(ctx)
	must(t, err)
	defer probe.Rollback(context.Background())
	_, err = probe.Exec(ctx, `SELECT id FROM mailboxes WHERE id=$1 FOR UPDATE NOWAIT`, f.personal.ID)
	must(t, err) // No mailbox lock before the actor fence.
	_, err = probe.Exec(ctx, `SELECT id FROM tenants WHERE id=$1 FOR UPDATE NOWAIT`, f.tenant.ID)
	r5RequireLockConflict(t, err) // Parent key already protected while user waits.
	must(t, probe.Rollback(ctx))
	must(t, hold.Rollback(ctx))
	r5AwaitOperation(t, ctx, done)
}

func TestR5SentIndependentMutationsShareParent(t *testing.T) {
	f := seedCompany(t)
	first := archJob(t, f, models.OutboundSent)
	second := archJob(t, f, models.OutboundSent)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	hold, err := f.pool.Begin(ctx)
	must(t, err)
	defer hold.Rollback(context.Background())
	_, err = hold.Exec(ctx, `SELECT asset_id FROM sent_mail_items WHERE asset_id=$1 FOR UPDATE`, first.ID)
	must(t, err)
	done := make(chan error, 1)
	go func() { done <- f.st.MutateArchivedMail(ctx, f.u, f.personal.ID, first.ID, 1, "archive") }()
	r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "sent_mail_items")

	probe, err := f.pool.Begin(ctx)
	must(t, err)
	defer probe.Rollback(context.Background())
	_, err = probe.Exec(ctx, `SELECT id FROM tenants WHERE id=$1 FOR KEY SHARE NOWAIT`, f.tenant.ID)
	must(t, err) // Normal independent writers share the same parent key.
	must(t, probe.Rollback(ctx))
	otherCtx, stop := context.WithTimeout(ctx, 4*time.Second)
	defer stop()
	must(t, f.st.MutateArchivedMail(otherCtx, f.u, f.personal.ID, second.ID, 1, "archive"))
	var revision int64
	must(t, f.pool.QueryRow(ctx, `SELECT revision FROM sent_mail_items WHERE asset_id=$1`, second.ID).Scan(&revision))
	if revision != 2 {
		t.Fatal("independent item did not finish while the first item waited")
	}
	must(t, hold.Rollback(ctx))
	r5AwaitOperation(t, ctx, done)
}

func TestR5SentMutationReloadsActorAfterParentWait(t *testing.T) {
	f := seedCompany(t)
	job := archJob(t, f, models.OutboundSent)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	before := r5SentState(t, f, job.ID)
	hold, err := f.pool.Begin(ctx)
	must(t, err)
	defer hold.Rollback(context.Background())
	_, err = hold.Exec(ctx, `SELECT id FROM tenants WHERE id=$1 FOR UPDATE`, f.tenant.ID)
	must(t, err)
	done := make(chan error, 1)
	go func() { done <- f.st.MutateArchivedMail(ctx, f.u, f.personal.ID, job.ID, 1, "archive") }()
	r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "tenants")
	// The waiting command must not already hold its user/mailbox children.
	_, err = hold.Exec(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE NOWAIT`, f.employee.ID)
	must(t, err)
	_, err = hold.Exec(ctx, `SELECT id FROM mailboxes WHERE id=$1 FOR UPDATE NOWAIT`, f.personal.ID)
	must(t, err)
	_, err = hold.Exec(ctx, `UPDATE users SET is_active=false WHERE id=$1`, f.employee.ID)
	must(t, err)
	must(t, hold.Commit(ctx))
	value, ok := app.As(app.FromAuthz(r5ConcurrentResult(t, ctx, done)))
	if !ok || value.Kind != app.KindForbidden {
		t.Fatal("stale active actor was accepted after the parent wait")
	}
	if r5SentState(t, f, job.ID) != before {
		t.Fatal("rejected actor left a sent mutation or audit/event writes")
	}
}
