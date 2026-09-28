package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"tabmail/internal/company"
	"tabmail/internal/models"
)

// Observe an actual waiter, not an arbitrary sleep that assumes a query ran.
// Each test owns a fresh database; this never reads another company's sessions.
func r5WaitBlockedBy(t *testing.T, f *companyFixture, ctx context.Context, blocker uint32, query string) uint32 {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var pid uint32
		err := f.pool.QueryRow(ctx, `SELECT pid FROM pg_stat_activity WHERE datname=current_database() AND state='active' AND $1::int=ANY(pg_blocking_pids(pid)) AND query LIKE $2 LIMIT 1`, int32(blocker), "%"+query+"%").Scan(&pid)
		if err == nil {
			return pid
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("observing lock waiter: %v", err)
		}
		select {
		case <-ctx.Done():
			t.Fatal("expected lock waiter was not observed")
		case <-ticker.C:
		}
	}
}
func r5RequireLockConflict(t *testing.T, err error) {
	t.Helper()
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "55P03" {
		t.Fatalf("expected NOWAIT lock conflict, got %v", err)
	}
}
func r5AwaitOperation(t *testing.T, ctx context.Context, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		must(t, err)
	case <-ctx.Done():
		t.Fatal("operation did not finish after blocker release")
	}
}
func r5Draft(t *testing.T, f *companyFixture) *company.Draft {
	t.Helper()
	d, e := f.st.SaveMailDraft(context.Background(), f.u, company.Draft{MailboxID: f.personal.ID, Payload: company.DraftPayload{Subject: "lock baseline"}})
	must(t, e)
	return d
}

func TestR5LockMapExistingDraftCASAvoidsTenantLock(t *testing.T) {
	f := seedCompany(t)
	d := r5Draft(t, f)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	hold, e := f.pool.Begin(ctx)
	must(t, e)
	defer hold.Rollback(context.Background())
	_, e = hold.Exec(ctx, `SELECT id FROM tenants WHERE id=$1 FOR UPDATE`, f.tenant.ID)
	must(t, e)
	d.Payload.Subject = "updated without parent key changes"
	saved, e := f.st.SaveMailDraft(ctx, f.u, *d)
	must(t, e)
	if saved.Revision != d.Revision+1 {
		t.Fatal("existing draft CAS did not commit")
	}
	t.Log("observed: existing draft update completed while tenant FOR UPDATE remained held; not a claim about INSERT/FK paths")
}

func TestR5LockMapDraftAuthorizationWaitsForMailboxFence(t *testing.T) {
	f := seedCompany(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	hold, e := f.pool.Begin(ctx)
	must(t, e)
	defer hold.Rollback(context.Background())
	_, e = hold.Exec(ctx, `SELECT id FROM mailboxes WHERE id=$1 FOR UPDATE`, f.personal.ID)
	must(t, e)
	done := make(chan error, 1)
	go func() {
		_, err := f.st.SaveMailDraft(ctx, f.u, company.Draft{MailboxID: f.personal.ID, Payload: company.DraftPayload{Subject: "new draft FK wait"}})
		done <- err
	}()
	r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "SELECT id FROM mailboxes")
	must(t, hold.Rollback(ctx))
	r5AwaitOperation(t, ctx, done)
	t.Log("observed: draft save waits on the mailbox fence before reading grant authority; original FK-only wait is superseded by the explicit authorization fence")
}

func TestR5LockMapDraftUserFenceOrdersSuspension(t *testing.T) {
	f := seedCompany(t)
	d := r5Draft(t, f)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	hold, e := f.pool.Begin(ctx)
	must(t, e)
	defer hold.Rollback(context.Background())
	_, e = hold.Exec(ctx, `SELECT id FROM mail_drafts WHERE id=$1 FOR UPDATE`, d.ID)
	must(t, e)
	done := make(chan error, 1)
	d.Payload.Subject = "save before suspension"
	go func() { _, err := f.st.SaveMailDraft(ctx, f.u, *d); done <- err }()
	writer := r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "UPDATE mail_drafts SET mailbox_id")
	probe, e := f.pool.Begin(ctx)
	must(t, e)
	defer probe.Rollback(context.Background())
	_, e = probe.Exec(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE NOWAIT`, f.employee.ID)
	r5RequireLockConflict(t, e)
	must(t, probe.Rollback(ctx))
	stopped := make(chan error, 1)
	inactive := false
	go func() {
		_, err := f.st.UpdateUserGuarded(ctx, f.a, f.tenant.ID, f.employee.ID, models.UserAdminPatch{IsActive: &inactive})
		stopped <- err
	}()
	r5WaitBlockedBy(t, f, ctx, writer, "FOR UPDATE")
	must(t, hold.Rollback(ctx))
	r5AwaitOperation(t, ctx, done)
	r5AwaitOperation(t, ctx, stopped)
	if _, e = f.st.SaveMailDraft(ctx, f.u, *d); e == nil {
		t.Fatal("a new write after suspension was accepted")
	}
	t.Log("observed: draft writer held user SHARE; suspension waited, then subsequent old-actor write was denied")
}

func TestR5LockMapRefreshFamilyThenUserThenToken(t *testing.T) {
	f := seedCompany(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	root := rbNewRefresh(f.employee.ID)
	must(t, f.st.CreateRefreshToken(ctx, root))
	hold, e := f.pool.Begin(ctx)
	must(t, e)
	defer hold.Rollback(context.Background())
	_, e = hold.Exec(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, f.employee.ID)
	must(t, e)
	done := make(chan error, 1)
	go func() {
		ok, replay, err := f.st.RotateRefreshToken(ctx, root.TokenHash, rbNewRefresh(f.employee.ID))
		if err == nil && (!ok || replay) {
			err = fmt.Errorf("rotation did not succeed")
		}
		done <- err
	}()
	r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "FOR SHARE OF u")
	probe, e := f.pool.Begin(ctx)
	must(t, e)
	defer probe.Rollback(context.Background())
	_, e = probe.Exec(ctx, `SELECT id FROM refresh_tokens WHERE token_hash=$1 FOR UPDATE NOWAIT`, root.TokenHash)
	must(t, e)
	var familyAvailable bool
	e = probe.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(hashtextextended($1,0))`, "tabmail:refresh-family:"+root.FamilyID.String()).Scan(&familyAvailable)
	must(t, e)
	if familyAvailable {
		t.Fatal("family lock was not held before waiting for user")
	}
	must(t, probe.Rollback(ctx))
	must(t, hold.Rollback(ctx))
	r5AwaitOperation(t, ctx, done)
	t.Log("observed: family advisory held, user SHARE waiting, old token still NOWAIT-lockable; then rotation completed")
}

// Verify the repaired order without asserting unrelated paths are deadlock-free.
// Parent KEY SHARE and sender SHARE must precede any attachment pinning.
func TestR5LockMapEnqueueParentAndUserBeforeAttachments(t *testing.T) {
	f := seedCompany(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	a, e := f.st.ReserveMailAttachment(ctx, f.u, company.Attachment{MailboxID: f.personal.ID, Filename: "lock.txt", Size: 3})
	must(t, e)
	must(t, f.st.FinishMailAttachment(ctx, f.u, a.ID, company.Hash("abc")))
	hold, e := f.pool.Begin(ctx)
	must(t, e)
	defer hold.Rollback(context.Background())
	_, e = hold.Exec(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, f.employee.ID)
	must(t, e)
	job := &models.OutboundJob{TenantID: f.tenant.ID, ZoneID: f.zone.ID, UserID: &f.employee.ID, SenderUserID: &f.employee.ID, SenderMailboxID: &f.personal.ID, MailFrom: f.personal.FullAddress, RcptTo: []string{"recipient@lock.test"}, To: []string{"recipient@lock.test"}, AttachmentIDs: []uuid.UUID{a.ID}, Subject: "lock order only", State: models.OutboundPending}
	done := make(chan error, 1)
	go func() { done <- f.st.CreateOutboundJob(ctx, job) }()
	r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "SELECT is_active FROM users")
	probe, e := f.pool.Begin(ctx)
	must(t, e)
	defer probe.Rollback(context.Background())
	_, e = probe.Exec(ctx, `SELECT id FROM mail_attachments WHERE id=$1 FOR UPDATE NOWAIT`, a.ID)
	must(t, e) // No attachment lock while enqueue is waiting on its sender.
	_, e = probe.Exec(ctx, `SELECT id FROM tenants WHERE id=$1 FOR UPDATE NOWAIT`, f.tenant.ID)
	r5RequireLockConflict(t, e) // Parent key is already protected.
	must(t, probe.Rollback(ctx))
	must(t, hold.Rollback(ctx))
	r5AwaitOperation(t, ctx, done)
	t.Log("observed: parent key held before sender wait; attachment remains NOWAIT-lockable; enqueue succeeds after sender release")
}
