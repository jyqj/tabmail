package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
)

// Real commands, real PostgreSQL. A controller holds only the existing
// per-uploader advisory key; it does not replace authorization or the INSERT.
func r5HoldUploadBudget(t *testing.T, f *companyFixture, ctx context.Context, user uuid.UUID) pgx.Tx {
	t.Helper()
	tx, err := f.pool.Begin(ctx)
	must(t, err)
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	_, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "attachment-budget:"+f.tenant.ID.String()+":"+user.String())
	must(t, err)
	return tx
}

func r5Reserve(ctx context.Context, f *companyFixture, actor authz.Actor, mailbox uuid.UUID, size int64) error {
	_, err := f.st.ReserveMailAttachment(ctx, actor, company.Attachment{MailboxID: mailbox, Filename: "reserve-boundary.txt", Size: size})
	return err
}

func TestR5ReservationOrdersMailboxRevocation(t *testing.T) {
	f := seedCompany(t)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	must(t, grantCurrent(f.st, ctx, f.a, models.MailboxGrant{TenantID: f.tenant.ID, MailboxID: f.shared.ID, UserID: f.employee.ID, CanRead: true, CanSend: true}))
	gate := r5HoldUploadBudget(t, f, ctx, f.employee.ID)
	reserved := make(chan error, 1)
	go func() { reserved <- r5Reserve(ctx, f, f.u, f.shared.ID, 3) }()
	writer := r5WaitBlockedBy(t, f, ctx, gate.Conn().PgConn().PID(), "pg_advisory_xact_lock")
	revoked := make(chan error, 1)
	go func() {
		revoked <- grantCurrent(f.st, ctx, f.a, models.MailboxGrant{TenantID: f.tenant.ID, MailboxID: f.shared.ID, UserID: f.employee.ID, CanRead: true})
	}()
	completed := false
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
observe:
	for {
		select {
		case err := <-revoked:
			must(t, err)
			completed = true
			break observe
		default:
		}
		var blocked bool
		must(t, f.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND state='active' AND $1::int=ANY(pg_blocking_pids(pid)) AND query LIKE '%UPDATE mailboxes SET lifecycle_revision%')`, int32(writer)).Scan(&blocked))
		if blocked {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("neither revocation completion nor mailbox dependency observed")
		case <-ticker.C:
		}
	}
	must(t, gate.Rollback(ctx))
	reserveErr := r5ConcurrentResult(t, ctx, reserved)
	if !completed {
		must(t, r5ConcurrentResult(t, ctx, revoked))
	}
	var count int
	must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM mail_attachments WHERE tenant_id=$1 AND user_id=$2`, f.tenant.ID, f.employee.ID).Scan(&count))
	if completed && reserveErr == nil {
		t.Fatal("R5_RESERVE_DEFECT_REVOKED: reservation committed after completed mailbox revoke using earlier authorization")
	}
	if completed {
		r5AttachmentRejection(t, reserveErr)
		if count != 0 {
			t.Fatal("rejected reservation persisted")
		}
	} else {
		must(t, reserveErr)
		if count != 1 {
			t.Fatal("serialized reservation missing")
		}
	}
	nextErr := r5Reserve(ctx, f, f.u, f.shared.ID, 3)
	v, ok := app.As(nextErr)
	if !ok || v.Kind != app.KindForbidden {
		t.Fatalf("new reservation after revoke not forbidden: %v", nextErr)
	}
	t.Logf("revoke_completed_before_budget_release=%v; reservations=%d", completed, count)
}

func TestR5ReservationWaitsForMailboxBeforeBudget(t *testing.T) {
	f := seedCompany(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	hold, err := f.pool.Begin(ctx)
	must(t, err)
	defer hold.Rollback(context.Background())
	_, err = hold.Exec(ctx, `SELECT id FROM mailboxes WHERE id=$1 FOR UPDATE`, f.personal.ID)
	must(t, err)
	done := make(chan error, 1)
	go func() { done <- r5Reserve(ctx, f, f.u, f.personal.ID, 3) }()
	r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "")
	probe, err := f.pool.Begin(ctx)
	must(t, err)
	defer probe.Rollback(context.Background())
	var available bool
	must(t, probe.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(hashtextextended($1,0))`, "attachment-budget:"+f.tenant.ID.String()+":"+f.employee.ID.String()).Scan(&available))
	must(t, probe.Rollback(ctx))
	must(t, hold.Rollback(ctx))
	must(t, r5ConcurrentResult(t, ctx, done))
	if !available {
		t.Fatal("R5_RESERVE_DEFECT_ORDER: waiting mailbox reservation held uploader budget before authorization fence")
	}
}

func TestR5ReservationConcurrentBudgetIsAtomic(t *testing.T) {
	f := seedCompany(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	const item int64 = 20 * 1024 * 1024
	for i := 0; i < 9; i++ {
		must(t, r5Reserve(ctx, f, f.u, f.personal.ID, item))
	}
	gate := r5HoldUploadBudget(t, f, ctx, f.employee.ID)
	done := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { done <- r5Reserve(ctx, f, f.u, f.personal.ID, item) }()
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var count int
		must(t, f.pool.QueryRow(ctx, `WITH RECURSIVE blocked(pid) AS (
 SELECT pid FROM pg_stat_activity WHERE datname=current_database() AND $1::int=ANY(pg_blocking_pids(pid))
 UNION SELECT a.pid FROM pg_stat_activity a JOIN blocked b ON b.pid=ANY(pg_blocking_pids(a.pid)) WHERE a.datname=current_database()
 ) SELECT count(*) FROM pg_stat_activity a JOIN blocked b USING(pid) WHERE a.state='active' AND a.query LIKE '%pg_advisory_xact_lock%'`, int32(gate.Conn().PgConn().PID())).Scan(&count))
		if count == 2 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("both reservations did not wait on uploader budget")
		case <-ticker.C:
		}
	}
	must(t, gate.Rollback(ctx))
	successes, conflicts := 0, 0
	for i := 0; i < 2; i++ {
		err := r5ConcurrentResult(t, ctx, done)
		if err == nil {
			successes++
		} else if v, ok := app.As(err); ok && v.Kind == app.KindConflict {
			conflicts++
		} else {
			t.Fatalf("unexpected reservation result: %v", err)
		}
	}
	var count int
	var bytes int64
	must(t, f.pool.QueryRow(ctx, `SELECT count(*),COALESCE(sum(size),0) FROM mail_attachments WHERE tenant_id=$1 AND user_id=$2`, f.tenant.ID, f.employee.ID).Scan(&count, &bytes))
	if successes != 1 || conflicts != 1 || count != 10 || bytes != 200*1024*1024 {
		t.Fatalf("budget not atomic: success=%d conflict=%d count=%d bytes=%d", successes, conflicts, count, bytes)
	}
}

func TestR5ReservationOtherUploaderRemainsIndependent(t *testing.T) {
	f := seedCompany(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, id := range []uuid.UUID{f.employee.ID, f.other.ID} {
		must(t, grantCurrent(f.st, ctx, f.a, models.MailboxGrant{TenantID: f.tenant.ID, MailboxID: f.shared.ID, UserID: id, CanRead: true, CanSend: true}))
	}
	gate := r5HoldUploadBudget(t, f, ctx, f.employee.ID)
	done := make(chan error, 1)
	go func() { done <- r5Reserve(ctx, f, f.u, f.shared.ID, 3) }()
	r5WaitBlockedBy(t, f, ctx, gate.Conn().PgConn().PID(), "pg_advisory_xact_lock")
	other := authz.Actor{Type: authz.PrincipalUser, ID: f.other.ID, TenantID: f.tenant.ID, Role: models.RoleUser}
	// Same mailbox and tenant, different uploader. Must complete while gate held.
	must(t, r5Reserve(ctx, f, other, f.shared.ID, 3))
	must(t, gate.Rollback(ctx))
	must(t, r5ConcurrentResult(t, ctx, done))
}

func TestR5ReservationCancellationReleasesLocksAndBudget(t *testing.T) {
	f := seedCompany(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	gate := r5HoldUploadBudget(t, f, ctx, f.employee.ID)
	work, cancelWork := context.WithCancel(ctx)
	defer cancelWork()
	done := make(chan error, 1)
	go func() { done <- r5Reserve(work, f, f.u, f.personal.ID, 3) }()
	r5WaitBlockedBy(t, f, ctx, gate.Conn().PgConn().PID(), "pg_advisory_xact_lock")
	cancelWork()
	if err := r5ConcurrentResult(t, ctx, done); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	must(t, gate.Rollback(ctx))
	var n int
	must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM mail_attachments WHERE user_id=$1`, f.employee.ID).Scan(&n))
	if n != 0 {
		t.Fatal("cancelled reservation persisted")
	}
	must(t, r5Reserve(ctx, f, f.u, f.personal.ID, 3))
}
