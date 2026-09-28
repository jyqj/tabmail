package postgres_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"tabmail/internal/app"
	"tabmail/internal/company"
	"tabmail/internal/models"
)

// These tests call the real completion command against a disposable PostgreSQL
// database. Controllers only hold rows to select an interleaving; they never
// replace the production authorization, update, or transaction implementation.
func r5UploadingAttachment(t *testing.T, f *companyFixture, mailbox uuid.UUID) *company.Attachment {
	t.Helper()
	a, err := f.st.ReserveMailAttachment(context.Background(), f.u, company.Attachment{MailboxID: mailbox, Filename: "finish-boundary.txt", Size: 3})
	must(t, err)
	return a
}

func r5HoldAttachment(t *testing.T, f *companyFixture, ctx context.Context, id uuid.UUID) pgx.Tx {
	t.Helper()
	tx, err := f.pool.Begin(ctx)
	must(t, err)
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	_, err = tx.Exec(ctx, `SELECT id FROM mail_attachments WHERE id=$1 FOR UPDATE`, id)
	must(t, err)
	return tx
}

func r5AttachmentRejection(t *testing.T, err error) {
	t.Helper()
	v, ok := app.As(err)
	if !ok || (v.Kind != app.KindNotFound && v.Kind != app.KindConflict && v.Kind != app.KindForbidden) {
		t.Fatalf("expected a classified unavailable-upload error, got %v", err)
	}
}

func TestR5AttachmentFinishRejectsConcurrentRemoval(t *testing.T) {
	f := seedCompany(t)
	a := r5UploadingAttachment(t, f, f.personal.ID)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	hold := r5HoldAttachment(t, f, ctx, a.ID)
	done := make(chan error, 1)
	go func() { done <- f.st.FinishMailAttachment(ctx, f.u, a.ID, company.Hash("abc")) }()
	r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "mail_attachments")
	_, err := hold.Exec(ctx, `DELETE FROM mail_attachments WHERE id=$1`, a.ID)
	must(t, err)
	must(t, hold.Commit(ctx))
	err = r5ConcurrentResult(t, ctx, done)
	var count int
	must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM mail_attachments WHERE id=$1`, a.ID).Scan(&count))
	if count != 0 {
		t.Fatal("controller did not remove the reservation")
	}
	if err == nil {
		t.Fatal("R5_FINISH_DEFECT_REMOVED: completion reported success after its reservation was deleted")
	}
	r5AttachmentRejection(t, err)
}

func TestR5AttachmentFinishRejectsExpiryAfterWait(t *testing.T) {
	f := seedCompany(t)
	a := r5UploadingAttachment(t, f, f.personal.ID)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	hold := r5HoldAttachment(t, f, ctx, a.ID)
	done := make(chan error, 1)
	go func() { done <- f.st.FinishMailAttachment(ctx, f.u, a.ID, company.Hash("abc")) }()
	r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "mail_attachments")
	// No wall-clock sleep: the controller changes the deadline while completion
	// is blocked, so the statement following lock acquisition must recheck it.
	_, err := hold.Exec(ctx, `UPDATE mail_attachments SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, a.ID)
	must(t, err)
	must(t, hold.Commit(ctx))
	err = r5ConcurrentResult(t, ctx, done)
	var state, digest string
	must(t, f.pool.QueryRow(ctx, `SELECT state,COALESCE(sha256,'') FROM mail_attachments WHERE id=$1`, a.ID).Scan(&state, &digest))
	if err == nil || state != "uploading" || digest != "" {
		t.Fatal("R5_FINISH_DEFECT_EXPIRED: completion published an expired reservation after waiting")
	}
	r5AttachmentRejection(t, err)
}

// The controller only holds a row lock: it does not modify the row after the
// worker starts. Observing the database clock cross the saved deadline catches
// stale transaction/statement-time checks even without a concurrent UPDATE.
func TestR5AttachmentFinishUsesTimeAfterLockWait(t *testing.T) {
	f := seedCompany(t)
	a := r5UploadingAttachment(t, f, f.personal.ID)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	var deadline time.Time
	must(t, f.pool.QueryRow(ctx, `UPDATE mail_attachments SET expires_at=clock_timestamp()+interval '2 seconds' WHERE id=$1 RETURNING expires_at`, a.ID).Scan(&deadline))
	hold := r5HoldAttachment(t, f, ctx, a.ID)
	done := make(chan error, 1)
	go func() { done <- f.st.FinishMailAttachment(ctx, f.u, a.ID, company.Hash("abc")) }()
	writer := r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "mail_attachments")
	var beganBefore bool
	must(t, f.pool.QueryRow(ctx, `SELECT xact_start<$2 FROM pg_stat_activity WHERE datname=current_database() AND pid=$1`, int32(writer), deadline).Scan(&beganBefore))
	if !beganBefore {
		t.Fatal("fixture failed to start the completion before its deadline")
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var expired bool
		must(t, f.pool.QueryRow(ctx, `SELECT clock_timestamp()>=$1::timestamptz`, deadline).Scan(&expired))
		if expired {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("database clock did not reach the fixture deadline")
		case <-ticker.C:
		}
	}
	must(t, hold.Rollback(ctx))
	err := r5ConcurrentResult(t, ctx, done)
	v, ok := app.As(err)
	if !ok || v.Kind != app.KindConflict {
		t.Fatalf("expired completion must conflict after waiting: %v", err)
	}
	var state, checksum string
	must(t, f.pool.QueryRow(ctx, `SELECT state,COALESCE(sha256,'') FROM mail_attachments WHERE id=$1`, a.ID).Scan(&state, &checksum))
	if state != "uploading" || checksum != "" {
		t.Fatal("deadline-crossing completion modified its reservation")
	}
}

func TestR5AttachmentFinishOnlyOneChecksumWins(t *testing.T) {
	f := seedCompany(t)
	a := r5UploadingAttachment(t, f, f.personal.ID)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	hold := r5HoldAttachment(t, f, ctx, a.ID)
	type outcome struct {
		digest string
		err    error
	}
	done := make(chan outcome, 2)
	for _, digest := range []string{company.Hash("abc"), company.Hash("xyz")} {
		digest := digest
		go func() { done <- outcome{digest, f.st.FinishMailAttachment(ctx, f.u, a.ID, digest)} }()
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var count int
		// The second updater can wait on the first updater's tuple lock, not
		// directly on our controller. Follow the real dependency chain.
		must(t, f.pool.QueryRow(ctx, `WITH RECURSIVE blocked(pid) AS (
 SELECT pid FROM pg_stat_activity WHERE datname=current_database() AND $1::int=ANY(pg_blocking_pids(pid))
 UNION
 SELECT a.pid FROM pg_stat_activity a JOIN blocked b ON b.pid=ANY(pg_blocking_pids(a.pid)) WHERE a.datname=current_database()
) SELECT count(*) FROM pg_stat_activity a JOIN blocked b USING(pid) WHERE a.state='active' AND a.query LIKE '%mail_attachments%'`, int32(hold.Conn().PgConn().PID())).Scan(&count))
		if count == 2 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("both completion commands did not reach the row barrier")
		case <-ticker.C:
		}
	}
	must(t, hold.Rollback(ctx))
	accepted := []string{}
	for i := 0; i < 2; i++ {
		select {
		case result := <-done:
			if result.err == nil {
				accepted = append(accepted, result.digest)
			} else {
				r5AttachmentRejection(t, result.err)
			}
		case <-ctx.Done():
			t.Fatal("completion did not finish after row release")
		}
	}
	var state, digest string
	must(t, f.pool.QueryRow(ctx, `SELECT state,COALESCE(sha256,'') FROM mail_attachments WHERE id=$1`, a.ID).Scan(&state, &digest))
	if len(accepted) != 1 {
		t.Fatal("R5_FINISH_DEFECT_REPLAY: two completions both succeeded and allowed checksum replacement")
	}
	if state != "ready" || digest != accepted[0] {
		t.Fatal("stored checksum does not belong to the successful completion")
	}
}

func TestR5AttachmentFinishOrdersMailboxRevocation(t *testing.T) {
	f := seedCompany(t)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	must(t, grantCurrent(f.st, ctx, f.a, models.MailboxGrant{TenantID: f.tenant.ID, MailboxID: f.shared.ID, UserID: f.employee.ID, CanRead: true, CanSend: true}))
	a := r5UploadingAttachment(t, f, f.shared.ID)
	hold := r5HoldAttachment(t, f, ctx, a.ID)
	done := make(chan error, 1)
	go func() { done <- f.st.FinishMailAttachment(ctx, f.u, a.ID, company.Hash("abc")) }()
	writer := r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "mail_attachments")
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
			t.Fatal("neither revocation completion nor mailbox fence was observed")
		case <-ticker.C:
		}
	}
	must(t, hold.Rollback(ctx))
	err := r5ConcurrentResult(t, ctx, done)
	if !completed {
		must(t, r5ConcurrentResult(t, ctx, revoked))
	}
	var state string
	must(t, f.pool.QueryRow(ctx, `SELECT state FROM mail_attachments WHERE id=$1`, a.ID).Scan(&state))
	if completed && (err == nil || state != "uploading") {
		t.Fatal("R5_FINISH_DEFECT_REVOKED: upload became ready after completed mailbox revocation")
	}
	if !completed && (err != nil || state != "ready") {
		t.Fatal("upload did not complete before the serialized revocation")
	}
	access, err := f.st.GetWorkMailbox(ctx, f.u, f.shared.ID)
	must(t, err)
	if access.CanSend {
		t.Fatal("revocation did not commit")
	}
	// A different still-pending upload, not a repeat of the completed ID, checks
	// authorization after the revocation. Grant read remains intact.
	must(t, grantCurrent(f.st, ctx, f.a, models.MailboxGrant{TenantID: f.tenant.ID, MailboxID: f.shared.ID, UserID: f.employee.ID, CanRead: true, CanSend: true}))
	pending := r5UploadingAttachment(t, f, f.shared.ID)
	must(t, grantCurrent(f.st, ctx, f.a, models.MailboxGrant{TenantID: f.tenant.ID, MailboxID: f.shared.ID, UserID: f.employee.ID, CanRead: true}))
	v, ok := app.As(f.st.FinishMailAttachment(ctx, f.u, pending.ID, company.Hash("abc")))
	if !ok || v.Kind != app.KindForbidden {
		t.Fatal("new upload completion after revocation was not forbidden")
	}
	t.Logf("revocation_completed_before_finish_release=%v; state=%s", completed, state)
}

func TestR5AttachmentFinishCanonicalChecksumAndReplay(t *testing.T) {
	f := seedCompany(t)
	a := r5UploadingAttachment(t, f, f.personal.ID)
	ctx := context.Background()
	want := company.Hash("abc")
	must(t, f.st.FinishMailAttachment(ctx, f.u, a.ID, strings.ToUpper(want)))
	var state, checksum string
	must(t, f.pool.QueryRow(ctx, `SELECT state,sha256 FROM mail_attachments WHERE id=$1`, a.ID).Scan(&state, &checksum))
	if state != "ready" || checksum != want {
		t.Fatal("valid completion did not save a canonical SHA-256")
	}
	r5AttachmentRejection(t, f.st.FinishMailAttachment(ctx, f.u, a.ID, company.Hash("xyz")))
	must(t, f.pool.QueryRow(ctx, `SELECT sha256 FROM mail_attachments WHERE id=$1`, a.ID).Scan(&checksum))
	if checksum != want {
		t.Fatal("completed checksum was replaced by replay")
	}
}

func TestR5AttachmentFinishCancellationRollsBack(t *testing.T) {
	f := seedCompany(t)
	a := r5UploadingAttachment(t, f, f.personal.ID)
	ctx, stop := context.WithTimeout(context.Background(), 12*time.Second)
	defer stop()
	hold := r5HoldAttachment(t, f, ctx, a.ID)
	operation, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- f.st.FinishMailAttachment(operation, f.u, a.ID, company.Hash("abc")) }()
	r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "mail_attachments")
	cancel()
	err := r5ConcurrentResult(t, ctx, done)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("blocked completion did not honor cancellation: %v", err)
	}
	must(t, hold.Rollback(ctx))
	var state, checksum string
	must(t, f.pool.QueryRow(ctx, `SELECT state,COALESCE(sha256,'') FROM mail_attachments WHERE id=$1`, a.ID).Scan(&state, &checksum))
	if state != "uploading" || checksum != "" {
		t.Fatal("cancelled completion changed the reservation")
	}
	// A fresh command must be able to acquire every released lock and finish.
	must(t, f.st.FinishMailAttachment(ctx, f.u, a.ID, company.Hash("abc")))
}

func TestR5AttachmentFinishRejectsNonHexChecksum(t *testing.T) {
	f := seedCompany(t)
	a := r5UploadingAttachment(t, f, f.personal.ID)
	err := f.st.FinishMailAttachment(context.Background(), f.u, a.ID, strings.Repeat("z", 64))
	var state string
	must(t, f.pool.QueryRow(context.Background(), `SELECT state FROM mail_attachments WHERE id=$1`, a.ID).Scan(&state))
	if err == nil || state != "uploading" {
		t.Fatal("R5_FINISH_DEFECT_CHECKSUM: non-hex 64-byte checksum was accepted")
	}
	v, ok := app.As(err)
	if !ok || v.Kind != app.KindBadRequest {
		t.Fatalf("invalid checksum must be a bad request, got %v", err)
	}
}
