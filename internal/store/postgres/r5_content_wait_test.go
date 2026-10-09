package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/app"
	"tabmail/internal/company"
	"tabmail/internal/models"
)

// Each case uses testpg's disposable database. Clock crossing is observed in
// PostgreSQL after pg_blocking_pids proves the intended lock wait; never guessed
// with sleep. These tests must not be reported as passed when DSN is absent.
func r5SentDeadline(t *testing.T, f *companyFixture, id uuid.UUID, kind string) time.Time {
	t.Helper()
	var deadline time.Time
	sql := `UPDATE sent_mail_items SET expires_at=clock_timestamp()+interval '2 seconds' WHERE asset_id=$1 RETURNING expires_at`
	if kind == "purge" {
		sql = `UPDATE sent_mail_items SET deleted_at=clock_timestamp(),purge_after=clock_timestamp()+interval '2 seconds' WHERE asset_id=$1 RETURNING purge_after`
	}
	if kind == "mailbox" {
		sql = `UPDATE mailboxes SET expires_at=clock_timestamp()+interval '2 seconds' WHERE id=$1 RETURNING expires_at`
		id = f.personal.ID
	}
	must(t, f.pool.QueryRow(context.Background(), sql, id).Scan(&deadline))
	return deadline
}
func r5AwaitSentDeadline(t *testing.T, f *companyFixture, ctx context.Context, writer uint32, deadline time.Time) {
	t.Helper()
	var beganBefore bool
	must(t, f.pool.QueryRow(ctx, `SELECT xact_start<$2 FROM pg_stat_activity WHERE datname=current_database() AND pid=$1`, int32(writer), deadline).Scan(&beganBefore))
	if !beganBefore {
		t.Fatal("fixture did not begin the operation before its deadline")
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
			t.Fatal("database clock did not reach deadline")
		case <-ticker.C:
		}
	}
}
func r5SentState(t *testing.T, f *companyFixture, id uuid.UUID) string {
	t.Helper()
	var state string
	must(t, f.pool.QueryRow(context.Background(), `SELECT jsonb_build_object('item',to_jsonb(i),'audit',(SELECT count(*) FROM audit_log),'outbox',(SELECT count(*) FROM outbox_events),'events',(SELECT count(*) FROM mailbox_event_log))::text FROM sent_mail_items i WHERE asset_id=$1`, id).Scan(&state))
	return state
}
func r5RequireSentConflict(t *testing.T, err error) {
	t.Helper()
	value, ok := app.As(err)
	if !ok || value.Kind != app.KindConflict {
		t.Fatalf("expected unavailable sent item conflict, got %v", err)
	}
}

func TestR5SentMutationRejectsExpiryAfterItemWait(t *testing.T) {
	r5ParallelFreshDB(t)
	for _, kind := range []string{"expires", "purge"} {
		t.Run(kind, func(t *testing.T) {
			f := seedCompany(t)
			job := archJob(t, f, models.OutboundSent)
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			deadline := r5SentDeadline(t, f, job.ID, kind)
			before := r5SentState(t, f, job.ID)
			hold, err := f.pool.Begin(ctx)
			must(t, err)
			defer hold.Rollback(context.Background())
			_, err = hold.Exec(ctx, `SELECT asset_id FROM sent_mail_items WHERE asset_id=$1 FOR UPDATE`, job.ID)
			must(t, err)
			done := make(chan error, 1)
			action := "archive"
			if kind == "purge" {
				action = "restore"
			}
			go func() { done <- f.st.MutateArchivedMail(ctx, f.u, f.personal.ID, job.ID, 1, action) }()
			writer := r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "sent_mail_items")
			r5AwaitSentDeadline(t, f, ctx, writer, deadline)
			must(t, hold.Rollback(ctx))
			r5RequireSentConflict(t, r5ConcurrentResult(t, ctx, done))
			if r5SentState(t, f, job.ID) != before {
				t.Fatal("expired operation changed item, audit or events")
			}
		})
	}
}

func TestR5SentMutationRollsBackExpiryDuringAuditWait(t *testing.T) {
	r5ParallelFreshDB(t)
	for _, kind := range []string{"expires", "purge", "mailbox"} {
		t.Run(kind, func(t *testing.T) {
			f := seedCompany(t)
			job := archJob(t, f, models.OutboundSent)
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			deadline := r5SentDeadline(t, f, job.ID, kind)
			before := r5SentState(t, f, job.ID)
			hold, err := f.pool.Begin(ctx)
			must(t, err)
			defer hold.Rollback(context.Background())
			_, err = hold.Exec(ctx, `LOCK TABLE audit_log IN SHARE MODE`)
			must(t, err)
			action := "archive"
			if kind == "purge" {
				action = "restore"
			}
			done := make(chan error, 1)
			go func() { done <- f.st.MutateArchivedMail(ctx, f.u, f.personal.ID, job.ID, 1, action) }()
			writer := r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "audit_log")
			r5AwaitSentDeadline(t, f, ctx, writer, deadline)
			must(t, hold.Rollback(ctx))
			r5RequireSentConflict(t, r5ConcurrentResult(t, ctx, done))
			if r5SentState(t, f, job.ID) != before {
				t.Fatal("post-audit expiry left a partial mutation or cleared original purge bound")
			}
		})
	}
}

func TestR5SentMutationOrdersGrantRevocation(t *testing.T) {
	f := seedCompany(t)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	grant := models.MailboxGrant{TenantID: f.tenant.ID, MailboxID: f.shared.ID, UserID: f.employee.ID, CanRead: true, CanOrganize: true, CanSend: true}
	must(t, grantCurrent(f.st, ctx, f.a, grant))
	job := &models.OutboundJob{TenantID: f.tenant.ID, ZoneID: f.zone.ID, UserID: &f.employee.ID, SenderUserID: &f.employee.ID, SenderMailboxID: &f.shared.ID, MailFrom: f.shared.FullAddress, To: []string{"synthetic@recipient.test"}, RcptTo: []string{"synthetic@recipient.test"}, State: models.OutboundSent}
	must(t, f.st.CreateOutboundJob(ctx, job))
	hold, err := f.pool.Begin(ctx)
	must(t, err)
	defer hold.Rollback(context.Background())
	_, err = hold.Exec(ctx, `SELECT asset_id FROM sent_mail_items WHERE asset_id=$1 FOR UPDATE`, job.ID)
	must(t, err)
	done := make(chan error, 1)
	go func() { done <- f.st.MutateArchivedMail(ctx, f.u, f.shared.ID, job.ID, 1, "archive") }()
	writer := r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "sent_mail_items")
	probe, err := f.pool.Begin(ctx)
	must(t, err)
	defer probe.Rollback(context.Background())
	_, err = probe.Exec(ctx, `SELECT id FROM mailboxes WHERE id=$1 FOR UPDATE NOWAIT`, f.shared.ID)
	r5RequireLockConflict(t, err)
	must(t, probe.Rollback(ctx))
	grant.CanOrganize = false
	revoked := make(chan error, 1)
	go func() { revoked <- grantCurrent(f.st, ctx, f.a, grant) }()
	// Observe the actual revocation waiting behind this writer. A correctly
	// ordered parent-key fence queues it before its mailbox UPDATE; the old
	// implementation queues on the mailbox and then deadlocks during audit.
	r5WaitBlockedBy(t, f, ctx, writer, "")
	must(t, hold.Rollback(ctx))
	r5AwaitOperation(t, ctx, done)
	r5AwaitOperation(t, ctx, revoked)
	err = f.st.MutateArchivedMail(ctx, f.u, f.shared.ID, job.ID, 2, "unarchive")
	value, ok := app.As(err)
	if !ok || value.Kind != app.KindForbidden {
		t.Fatalf("completed revoke did not stop the next mutation: %v", err)
	}
}

func TestR5SentMutationCancellationAndAuditFailureRollBack(t *testing.T) {
	for _, kind := range []string{"cancel", "audit"} {
		t.Run(kind, func(t *testing.T) {
			f := seedCompany(t)
			job := archJob(t, f, models.OutboundSent)
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			before := r5SentState(t, f, job.ID)
			if kind == "audit" {
				_, err := f.pool.Exec(ctx, `ALTER TABLE audit_log ADD CONSTRAINT b01k_reject_sent CHECK(action<>'sent.archive')`)
				must(t, err)
				if err = f.st.MutateArchivedMail(ctx, f.u, f.personal.ID, job.ID, 1, "archive"); err == nil {
					t.Fatal("ignored required audit failure")
				}
			} else {
				hold, err := f.pool.Begin(ctx)
				must(t, err)
				defer hold.Rollback(context.Background())
				_, err = hold.Exec(ctx, `SELECT asset_id FROM sent_mail_items WHERE asset_id=$1 FOR UPDATE`, job.ID)
				must(t, err)
				runCtx, stop := context.WithCancel(ctx)
				defer stop()
				done := make(chan error, 1)
				go func() { done <- f.st.MutateArchivedMail(runCtx, f.u, f.personal.ID, job.ID, 1, "archive") }()
				r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "sent_mail_items")
				stop()
				if err := r5ConcurrentResult(t, ctx, done); !errors.Is(err, context.Canceled) {
					t.Fatalf("cancellation not propagated: %v", err)
				}
				must(t, hold.Rollback(ctx))
			}
			if r5SentState(t, f, job.ID) != before {
				t.Fatal("failed transaction changed sent item or evidence")
			}
			// A fresh operation proves both user and mailbox fences were released.
			if kind == "audit" {
				_, err := f.pool.Exec(ctx, `ALTER TABLE audit_log DROP CONSTRAINT b01k_reject_sent`)
				must(t, err)
			}
			must(t, f.st.MutateArchivedMail(ctx, f.u, f.personal.ID, job.ID, 1, "archive"))
		})
	}
}

func TestR5SentReadUsesTimeAfterIdentityWait(t *testing.T) {
	r5ParallelFreshDB(t)
	for _, endpoint := range []string{"content", "attachments", "list"} {
		t.Run(endpoint, func(t *testing.T) {
			f := seedCompany(t)
			job := archJob(t, f, models.OutboundSent)
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			deadline := r5SentDeadline(t, f, job.ID, "expires")
			hold, err := f.pool.Begin(ctx)
			must(t, err)
			defer hold.Rollback(context.Background())
			_, err = hold.Exec(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, f.employee.ID)
			must(t, err)
			done := make(chan error, 1)
			go func() {
				switch endpoint {
				case "content":
					v, e := f.st.GetSubmissionContent(ctx, f.u, job.ID)
					if v != nil || e == nil {
						done <- errors.New("expired sent content remained readable")
						return
					}
					value, ok := app.As(e)
					if !ok || value.Kind != app.KindNotFound {
						done <- e
						return
					}
				case "attachments":
					v, e := f.st.ListSubmissionAttachments(ctx, f.u, job.ID)
					if v != nil || e == nil {
						done <- errors.New("expired sent attachment metadata remained readable")
						return
					}
					value, ok := app.As(e)
					if !ok || value.Kind != app.KindNotFound {
						done <- e
						return
					}
				case "list":
					v, n, e := f.st.ListArchivedMail(ctx, f.u, f.personal.ID, "sent", "", models.Page{})
					if e != nil {
						done <- e
						return
					}
					if len(v) != 0 || n != 0 {
						done <- errors.New("expired item remained in list")
						return
					}
				}
				done <- nil
			}()
			reader := r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "users")
			r5AwaitSentDeadline(t, f, ctx, reader, deadline)
			must(t, hold.Rollback(ctx))
			r5AwaitOperation(t, ctx, done)
		})
	}
}

func TestR5ContentReadsFenceMailboxSnapshot(t *testing.T) {
	for _, operation := range []string{"message", "parsed", "save-parsed"} {
		t.Run(operation, func(t *testing.T) {
			f := seedCompany(t)
			message, _, _ := r5ReadFixture(t, f)
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			hold, err := f.pool.Begin(ctx)
			must(t, err)
			defer hold.Rollback(context.Background())
			_, err = hold.Exec(ctx, `SELECT id FROM mailboxes WHERE id=$1 FOR UPDATE`, f.shared.ID)
			must(t, err)
			done := make(chan error, 1)
			go func() {
				var e error
				switch operation {
				case "message":
					_, e = f.st.GetWorkMessage(ctx, f.u, f.shared.ID, message.ID)
				case "parsed":
					_, e = f.st.GetParsedMessage(ctx, f.u, f.shared.ID, message.ID)
				case "save-parsed":
					e = f.st.SaveParsedMessage(ctx, f.u, f.shared.ID, company.ParsedMessage{MessageID: message.ID, SourceKey: message.RawObjectKey, SourceSHA256: company.Hash("fixture"), ParserVersion: 1, TextBody: "must not be saved"})
				}
				done <- e
			}()
			r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "mailboxes")
			_, err = hold.Exec(ctx, `UPDATE mailboxes SET lifecycle_revision=lifecycle_revision+1 WHERE id=$1`, f.shared.ID)
			must(t, err)
			_, err = hold.Exec(ctx, `UPDATE mailbox_grants SET can_read=false WHERE mailbox_id=$1 AND user_id=$2`, f.shared.ID, f.employee.ID)
			must(t, err)
			must(t, hold.Commit(ctx))
			value, ok := app.As(r5ConcurrentResult(t, ctx, done))
			if !ok || (value.Kind != app.KindForbidden && value.Kind != app.KindNotFound) {
				t.Fatal("mailbox fence did not re-read revoked rights")
			}
			var body string
			must(t, f.pool.QueryRow(ctx, `SELECT text_body FROM mail_documents WHERE message_id=$1`, message.ID).Scan(&body))
			if body != "cached synthetic content" {
				t.Fatal("denied cache save overwrote the derived document")
			}
		})
	}
}
