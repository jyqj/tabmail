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
	"tabmail/internal/company"
	"tabmail/internal/models"
)

// All controllers operate on a fresh testpg database. They hold real rows and
// observe the database clock; no production query or lease result is mocked.
func r5IndexFixture(t *testing.T, f *companyFixture, existingDocument bool) (company.MailIndexJob, company.ParsedMessage) {
	t.Helper()
	ctx := context.Background()
	m := &models.Message{TenantID: f.tenant.ID, MailboxID: f.personal.ID, ZoneID: f.zone.ID, Sender: "sender@index.test", Recipients: []string{f.personal.FullAddress}, Subject: "index boundary", RawObjectKey: "r5-index-" + uuid.NewString()}
	must(t, f.st.CreateMessage(ctx, m))
	d := company.ParsedMessage{MessageID: m.ID, SourceKey: m.RawObjectKey, SourceSHA256: company.Hash("source"), ParserVersion: 1, TextBody: "before", BodyAccess: "full"}
	if existingDocument {
		must(t, f.st.SaveParsedMessage(ctx, f.u, f.personal.ID, d))
	}
	jobs, err := f.st.ClaimMailIndexJobs(ctx, 1)
	must(t, err)
	if len(jobs) != 1 || jobs[0].MessageID != m.ID {
		t.Fatal("fixture did not claim exact message")
	}
	return jobs[0], d
}
func r5IndexExpiry(t *testing.T, f *companyFixture, ctx context.Context, j company.MailIndexJob) time.Time {
	t.Helper()
	var deadline time.Time
	must(t, f.pool.QueryRow(ctx, `UPDATE mail_index_jobs SET lease_until=clock_timestamp()+interval '2 seconds' WHERE message_id=$1 RETURNING lease_until`, j.MessageID).Scan(&deadline))
	return deadline
}
func r5IndexHold(t *testing.T, f *companyFixture, ctx context.Context, table string, id uuid.UUID) pgx.Tx {
	t.Helper()
	if table != "mail_index_jobs" && table != "mail_documents" && table != "messages" {
		t.Fatal("invalid test row")
	}
	column := "message_id"
	if table == "messages" {
		column = "id"
	}
	tx, err := f.pool.Begin(ctx)
	must(t, err)
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	_, err = tx.Exec(ctx, `SELECT `+column+` FROM `+table+` WHERE `+column+`=$1 FOR UPDATE`, id)
	must(t, err)
	return tx
}
func r5IndexCrossDeadline(t *testing.T, f *companyFixture, ctx context.Context, pid uint32, deadline time.Time) {
	t.Helper()
	var started bool
	must(t, f.pool.QueryRow(ctx, `SELECT xact_start<$2 FROM pg_stat_activity WHERE datname=current_database() AND pid=$1`, int32(pid), deadline).Scan(&started))
	if !started {
		t.Fatal("fixture did not start operation before lease deadline")
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var expired bool
		must(t, f.pool.QueryRow(ctx, `SELECT clock_timestamp()>=$1::timestamptz`, deadline).Scan(&expired))
		if expired {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("database clock did not cross deadline")
		case <-ticker.C:
		}
	}
}
func r5IndexUnchanged(t *testing.T, f *companyFixture, j company.MailIndexJob) {
	t.Helper()
	var state, body string
	var token uuid.UUID
	must(t, f.pool.QueryRow(context.Background(), `SELECT j.state,j.lease_token,d.text_body FROM mail_index_jobs j JOIN mail_documents d USING(message_id) WHERE j.message_id=$1`, j.MessageID).Scan(&state, &token, &body))
	if state != "processing" || token != j.Token || body != "before" {
		t.Fatalf("expired command persisted effects: state=%s token_changed=%v body_changed=%v", state, token != j.Token, body != "before")
	}
}
func r5IndexDeadlineCase(t *testing.T, table string, fail bool, marker string) {
	t.Helper()
	f := seedCompany(t)
	j, d := r5IndexFixture(t, f, true)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	deadline := r5IndexExpiry(t, f, ctx, j)
	hold := r5IndexHold(t, f, ctx, table, j.MessageID)
	d.TextBody = "expired writer"
	done := make(chan error, 1)
	go func() {
		if fail {
			done <- f.st.FailMailIndexJob(ctx, j, "PRIVATE parser details")
		} else {
			done <- f.st.CompleteMailIndexJob(ctx, j, d)
		}
	}()
	pid := r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), table)
	r5IndexCrossDeadline(t, f, ctx, pid, deadline)
	must(t, hold.Rollback(ctx))
	err := r5ConcurrentResult(t, ctx, done)
	if err == nil {
		t.Fatal("R5_INDEX_DEFECT_" + marker + ": expired waiting worker committed")
	}
	v, ok := app.As(err)
	if !ok || v.Kind != app.KindConflict {
		t.Fatalf("expected classified lease conflict, got %v", err)
	}
	r5IndexUnchanged(t, f, j)
	// Losing the old lease must leave a recoverable job, not force ready/failed.
	next, err := f.st.ClaimMailIndexJobs(ctx, 1)
	must(t, err)
	if len(next) != 1 || next[0].Token == j.Token {
		t.Fatal("expired work not reclaimable with a fresh token")
	}
	d.TextBody = "fresh worker"
	must(t, f.st.CompleteMailIndexJob(ctx, next[0], d))
	if err = f.st.FailMailIndexJob(ctx, j, "old failure after recovery"); err == nil {
		t.Fatal("old token changed recovered result")
	}
	t.Log("expired command rejected without effects; fresh token completed and old failure rejected")
}
func TestR5IndexCompleteRechecksAfterJobWait(t *testing.T) {
	r5IndexDeadlineCase(t, "mail_index_jobs", false, "COMPLETE_WAIT")
}
func TestR5IndexFailRechecksAfterJobWait(t *testing.T) {
	r5IndexDeadlineCase(t, "mail_index_jobs", true, "FAIL_WAIT")
}
func TestR5IndexCompleteRechecksAfterDocumentWait(t *testing.T) {
	r5IndexDeadlineCase(t, "mail_documents", false, "DOCUMENT_WAIT")
}

func TestR5IndexSourceLockPrecedesJob(t *testing.T) {
	f := seedCompany(t)
	j, d := r5IndexFixture(t, f, false)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	hold := r5IndexHold(t, f, ctx, "messages", j.MessageID)
	done := make(chan error, 1)
	go func() { done <- f.st.CompleteMailIndexJob(ctx, j, d) }()
	r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "")
	probe, err := f.pool.Begin(ctx)
	must(t, err)
	defer probe.Rollback(context.Background())
	_, probeErr := probe.Exec(ctx, `SELECT message_id FROM mail_index_jobs WHERE message_id=$1 FOR UPDATE NOWAIT`, j.MessageID)
	must(t, probe.Rollback(ctx))
	must(t, hold.Rollback(ctx))
	must(t, r5ConcurrentResult(t, ctx, done))
	if probeErr != nil {
		var pg *pgconn.PgError
		if !errors.As(probeErr, &pg) || pg.Code != "55P03" {
			t.Fatalf("unexpected lock probe error: %v", probeErr)
		}
		t.Fatal("R5_INDEX_DEFECT_SOURCE_ORDER: completion held child job while waiting for source parent")
	}
	t.Log("completion waited for source before owning job; parent/child ordering observed, not a global deadlock proof")
}

func TestR5IndexDocumentFailureRollsBackCompletion(t *testing.T) {
	f := seedCompany(t)
	j, d := r5IndexFixture(t, f, true)
	ctx := context.Background()
	_, err := f.pool.Exec(ctx, `ALTER TABLE mail_documents ADD CONSTRAINT r5_reject_document CHECK(text_body<>'reject-test') NOT VALID`)
	must(t, err)
	d.TextBody = "reject-test"
	err = f.st.CompleteMailIndexJob(ctx, j, d)
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "23514" {
		t.Fatalf("did not reach controlled document failure: %v", err)
	}
	r5IndexUnchanged(t, f, j)
	_, err = f.pool.Exec(ctx, `ALTER TABLE mail_documents DROP CONSTRAINT r5_reject_document`)
	must(t, err)
	d.TextBody = "recovered"
	must(t, f.st.CompleteMailIndexJob(ctx, j, d))
}
func TestR5IndexCancelledCompletionReleasesLocks(t *testing.T) {
	f := seedCompany(t)
	j, d := r5IndexFixture(t, f, true)
	observe, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	hold := r5IndexHold(t, f, observe, "mail_documents", j.MessageID)
	ctx, stop := context.WithCancel(observe)
	done := make(chan error, 1)
	go func() { done <- f.st.CompleteMailIndexJob(ctx, j, d) }()
	r5WaitBlockedBy(t, f, observe, hold.Conn().PgConn().PID(), "mail_documents")
	stop()
	if err := r5ConcurrentResult(t, observe, done); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation not propagated: %v", err)
	}
	must(t, hold.Rollback(observe))
	r5IndexUnchanged(t, f, j)
	must(t, f.st.CompleteMailIndexJob(observe, j, d))
}
func TestR5IndexRejectsChangedSourceAndStaleToken(t *testing.T) {
	f := seedCompany(t)
	j, d := r5IndexFixture(t, f, true)
	ctx := context.Background()
	_, err := f.pool.Exec(ctx, `UPDATE messages SET raw_object_key='replacement-source' WHERE id=$1`, j.MessageID)
	must(t, err)
	if err = f.st.CompleteMailIndexJob(ctx, j, d); err == nil {
		t.Fatal("old source completion accepted")
	}
	if err = f.st.FailMailIndexJob(ctx, j, "private old source"); err == nil {
		t.Fatal("old source failure accepted")
	}
	var count int
	must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM mail_documents WHERE message_id=$1`, j.MessageID).Scan(&count))
	if count != 0 {
		t.Fatal("invalidated source document restored")
	}
	jobs, err := f.st.ClaimMailIndexJobs(ctx, 1)
	must(t, err)
	if len(jobs) != 1 {
		t.Fatal("replacement not claimable")
	}
	d.SourceKey = "replacement-source"
	d.TextBody = "replacement"
	must(t, f.st.CompleteMailIndexJob(ctx, jobs[0], d))
}
