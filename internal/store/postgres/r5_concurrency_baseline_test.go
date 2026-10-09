package postgres_test

// Secure-behavior regressions promoted after reproducing the R4 failures.
// The only injected triggers pause an
// otherwise unmodified production command in this test's disposable database.
// SQLSTATE 40P01, setup failure and a context timeout are never interchangeable.
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"tabmail/internal/app"
	"tabmail/internal/company"
	"tabmail/internal/models"
)

func r5SQLState(err error) string {
	if err == nil {
		return "ok"
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return pg.Code
	}
	if v, ok := app.As(err); ok {
		return "app:" + string(v.Kind)
	}
	return "unclassified_error"
}
func r5ConcurrentResult(t *testing.T, ctx context.Context, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		t.Fatal("concurrent command did not finish within deadline")
		return ctx.Err()
	}
}
func r5PauseCommand(t *testing.T, f *companyFixture, ctx context.Context, table, event, level string) pgx.Tx {
	t.Helper()
	// All identifiers come from this source, not HTTP or environment input.
	if (table != "mail_attachments" && table != "outbound_jobs") || (event != "UPDATE" && event != "INSERT") || (level != "STATEMENT" && level != "ROW") {
		t.Fatal("invalid test barrier definition")
	}
	const key int64 = 713502801
	_, err := f.pool.Exec(ctx, fmt.Sprintf(`CREATE FUNCTION r5_pause_command() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_advisory_xact_lock(%d::bigint); RETURN NEW; END $$;
 CREATE TRIGGER zz_r5_pause_command BEFORE %s ON %s FOR EACH %s EXECUTE FUNCTION r5_pause_command()`, key, event, table, level))
	must(t, err)
	gate, err := f.pool.Begin(ctx)
	must(t, err)
	t.Cleanup(func() { _ = gate.Rollback(context.Background()) })
	_, err = gate.Exec(ctx, `SELECT pg_advisory_xact_lock($1::bigint)`, key)
	must(t, err)
	return gate
}
func r5TransferFixture(t *testing.T, f *companyFixture) (*company.Attachment, *company.Draft, *company.OffboardingPlan, *models.OutboundJob) {
	t.Helper()
	ctx := context.Background()
	a, err := f.st.ReserveMailAttachment(ctx, f.u, company.Attachment{MailboxID: f.personal.ID, Filename: "controlled.txt", Size: 3})
	must(t, err)
	must(t, f.st.FinishMailAttachment(ctx, f.u, a.ID, company.Hash("abc")))
	d, err := f.st.SaveMailDraft(ctx, f.u, company.Draft{MailboxID: f.personal.ID, Payload: company.DraftPayload{Subject: "controlled handover", AttachmentIDs: []uuid.UUID{a.ID}}})
	must(t, err)
	p, err := f.st.PreviewOffboarding(ctx, f.a, f.employee.ID, f.other.ID, company.OffboardingOptions{Drafts: "transfer_owned"}, "Controlled concurrency validation")
	must(t, err)
	j := &models.OutboundJob{ID: uuid.New(), TenantID: f.tenant.ID, ZoneID: f.zone.ID, UserID: &f.employee.ID, SenderUserID: &f.employee.ID, SenderMailboxID: &f.personal.ID, MailFrom: f.personal.FullAddress, RcptTo: []string{"recipient@concurrency.test"}, To: []string{"recipient@concurrency.test"}, AttachmentIDs: []uuid.UUID{a.ID}, Subject: "controlled enqueue", State: models.OutboundPending}
	return a, d, p, j
}
func r5VerifyTransferAtomicity(t *testing.T, f *companyFixture, a *company.Attachment, d *company.Draft, p *company.OffboardingPlan, j *models.OutboundJob, offboard, enqueue error) {
	t.Helper()
	ctx := context.Background()
	var state string
	var active bool
	var owner, author, custodian uuid.UUID
	var jobs, assets, pins int
	must(t, f.pool.QueryRow(ctx, `SELECT state FROM employee_offboarding_plans WHERE id=$1`, p.ID).Scan(&state))
	must(t, f.pool.QueryRow(ctx, `SELECT is_active FROM users WHERE id=$1`, f.employee.ID).Scan(&active))
	must(t, f.pool.QueryRow(ctx, `SELECT owner_user_id FROM mailboxes WHERE id=$1`, f.personal.ID).Scan(&owner))
	must(t, f.pool.QueryRow(ctx, `SELECT user_id FROM mail_drafts WHERE id=$1`, d.ID).Scan(&author))
	must(t, f.pool.QueryRow(ctx, `SELECT user_id FROM mail_attachments WHERE id=$1`, a.ID).Scan(&custodian))
	must(t, f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM outbound_jobs WHERE id=$1),(SELECT count(*) FROM sent_mail_assets WHERE id=$1),(SELECT count(*) FROM sent_asset_attachments WHERE asset_id=$1)`, j.ID).Scan(&jobs, &assets, &pins))
	wantOwner := f.employee.ID
	if offboard == nil {
		wantOwner = f.other.ID
		if state != "executed" || active {
			t.Fatal("successful disposition not complete")
		}
	} else if state != "preview" || !active {
		t.Fatal("failed disposition left partial user/plan effects")
	}
	if owner != wantOwner || author != wantOwner || custodian != wantOwner {
		t.Fatal("non-atomic mailbox/draft/attachment disposition")
	}
	wantJobs := 0
	if enqueue == nil {
		wantJobs = 1
	}
	if jobs != wantJobs || assets != wantJobs || pins != wantJobs {
		t.Fatal("enqueue rollback broke job/archive/attachment atomicity")
	}
	t.Logf("atomicity: plan=%s old_active=%v job=%d archive=%d pin=%d; enqueue=%s offboard=%s", state, active, jobs, assets, pins, r5SQLState(enqueue), r5SQLState(offboard))
}
func TestR5ConcurrencyR01EnqueueOffboardNoAttachmentCycle(t *testing.T) {
	f := seedCompany(t)
	a, d, p, j := r5TransferFixture(t, f)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	gate := r5PauseCommand(t, f, ctx, "mail_attachments", "UPDATE", "STATEMENT")
	off := make(chan error, 1)
	go func() { _, err := f.st.ExecuteOffboarding(ctx, f.a, f.employee.ID, p.ID); off <- err }()
	offPID := r5WaitBlockedBy(t, f, ctx, gate.Conn().PgConn().PID(), "UPDATE mail_attachments f")
	enq := make(chan error, 1)
	go func() { enq <- f.st.CreateOutboundJob(ctx, j) }()
	enqPID := r5WaitBlockedBy(t, f, ctx, offPID, "")
	must(t, gate.Rollback(ctx))
	e1, e2 := r5ConcurrentResult(t, ctx, off), r5ConcurrentResult(t, ctx, enq)
	r5VerifyTransferAtomicity(t, f, a, d, p, j, e1, e2)
	t.Logf("production command pids: offboard=%d enqueue=%d; controller gate released before resolution", offPID, enqPID)
	if r5SQLState(e1) == "40P01" || r5SQLState(e2) == "40P01" {
		t.Fatal("R5_CONCURRENCY_R01: real enqueue and transfer-owned disposition formed an attachment/user deadlock")
	}
	if e1 != nil {
		t.Fatalf("unexpected non-deadlock offboarding failure: %v", e1)
	}
	if v, ok := app.As(e2); !ok || v.Kind != app.KindForbidden {
		t.Fatalf("expected inactive-sender refusal after offboarding, got %v", e2)
	}
}
func TestR5ConcurrencyR03EnqueueOffboardNoTenantFKCycle(t *testing.T) {
	f := seedCompany(t)
	a, d, p, j := r5TransferFixture(t, f)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	// zz sorts after the production fence_employee_enqueue BEFORE ROW trigger:
	// sender SHARE is held, while the FK's tenant KEY SHARE is still ahead.
	gate := r5PauseCommand(t, f, ctx, "outbound_jobs", "INSERT", "ROW")
	enq := make(chan error, 1)
	go func() { enq <- f.st.CreateOutboundJob(ctx, j) }()
	enqPID := r5WaitBlockedBy(t, f, ctx, gate.Conn().PgConn().PID(), "INSERT INTO outbound_jobs")
	off := make(chan error, 1)
	go func() { _, err := f.st.ExecuteOffboarding(ctx, f.a, f.employee.ID, p.ID); off <- err }()
	offPID := r5WaitBlockedBy(t, f, ctx, enqPID, "")
	must(t, gate.Rollback(ctx))
	e1, e2 := r5ConcurrentResult(t, ctx, off), r5ConcurrentResult(t, ctx, enq)
	r5VerifyTransferAtomicity(t, f, a, d, p, j, e1, e2)
	t.Logf("production command pids: offboard=%d enqueue=%d; controller gate released before either command resolves", offPID, enqPID)
	if r5SQLState(e1) == "40P01" || r5SQLState(e2) == "40P01" {
		t.Fatal("R5_CONCURRENCY_R03: sender-share enqueue and tenant-first offboarding deadlocked through implicit tenant FK")
	}
	// The pending submission legitimately changes the preview fingerprint;
	// a fresh preview, not blind retry, is required after it commits.
	if e2 != nil {
		t.Fatalf("unexpected enqueue failure: %v", e2)
	}
	if v, ok := app.As(e1); !ok || v.Kind != app.KindConflict {
		t.Fatalf("expected stale-preview conflict after new submission, got %v", e1)
	}
}
func TestR5ConcurrencyR02DraftCannotCommitAfterCompletedRevoke(t *testing.T) {
	f := seedCompany(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	must(t, grantCurrent(f.st, ctx, f.a, models.MailboxGrant{TenantID: f.tenant.ID, MailboxID: f.shared.ID, UserID: f.employee.ID, CanRead: true, CanSend: true}))
	d, err := f.st.SaveMailDraft(ctx, f.u, company.Draft{MailboxID: f.shared.ID, Payload: company.DraftPayload{Subject: "before revoke"}})
	must(t, err)
	hold, err := f.pool.Begin(ctx)
	must(t, err)
	defer hold.Rollback(context.Background())
	_, err = hold.Exec(ctx, `SELECT id FROM mail_drafts WHERE id=$1 FOR UPDATE`, d.ID)
	must(t, err)
	change := *d
	change.Payload.Subject = "written after completed revoke"
	write := make(chan error, 1)
	go func() { _, err := f.st.SaveMailDraft(ctx, f.u, change); write <- err }()
	writer := r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "UPDATE mail_drafts SET mailbox_id")
	revoke := make(chan error, 1)
	go func() {
		revoke <- grantCurrent(f.st, ctx, f.a, models.MailboxGrant{TenantID: f.tenant.ID, MailboxID: f.shared.ID, UserID: f.employee.ID, CanRead: true})
	}()
	// Either observe the revoker waiting on the actual writer, or its completed
	// transaction. This accepts a safe serialization, not just one lucky race.
	completed := false
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
observe:
	for {
		select {
		case e := <-revoke:
			must(t, e)
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
			t.Fatal("neither revoke completion nor writer lock dependency observed")
		case <-ticker.C:
		}
	}
	must(t, hold.Rollback(ctx))
	writeErr := r5ConcurrentResult(t, ctx, write)
	if !completed {
		must(t, r5ConcurrentResult(t, ctx, revoke))
	}
	access, err := f.st.GetWorkMailbox(ctx, f.u, f.shared.ID)
	must(t, err)
	if access.CanSend {
		t.Fatal("fixture revoke did not commit")
	}
	var raw []byte
	var revision int
	must(t, f.pool.QueryRow(ctx, `SELECT payload,revision FROM mail_drafts WHERE id=$1`, d.ID).Scan(&raw, &revision))
	var payload company.DraftPayload
	must(t, json.Unmarshal(raw, &payload))
	t.Logf("writer=%d revoke_completed_before_writer_release=%v revision=%d write=%s", writer, completed, revision, r5SQLState(writeErr))
	if completed && writeErr == nil && revision == d.Revision+1 && payload.Subject == change.Payload.Subject {
		t.Fatal("R5_CONCURRENCY_R02: draft write committed after completed grant revocation while using the earlier authorization read")
	}
	if !completed && (writeErr != nil || revision != d.Revision+1 || payload.Subject != change.Payload.Subject) {
		t.Fatal("serialized draft did not commit before revoke")
	}
	if completed && (writeErr == nil || revision != d.Revision || payload.Subject != d.Payload.Subject) {
		t.Fatal("rejected draft changed persisted state")
	}
	change.Revision = revision
	if _, e := f.st.SaveMailDraft(ctx, f.u, change); e == nil {
		t.Fatal("a new save after revoke was authorized")
	}
}
