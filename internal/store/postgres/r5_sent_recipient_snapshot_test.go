package postgres_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
	"tabmail/internal/store"
)

func r5RecipientSnapshotFixture(t *testing.T) *companyFixture {
	t.Helper()
	if os.Getenv("TABMAIL_TEST_DB_DSN") == "" {
		t.Fatal("sent-recipient snapshot acceptance requires owned TABMAIL_TEST_DB_DSN")
	}
	return seedCompany(t)
}

func r5RecipientSnapshotJob(t *testing.T, f *companyFixture, bcc []string) *models.OutboundJob {
	t.Helper()
	j := &models.OutboundJob{TenantID: f.tenant.ID, ZoneID: f.zone.ID, UserID: &f.employee.ID, SenderUserID: &f.employee.ID, SenderMailboxID: &f.personal.ID, MailFrom: f.personal.FullAddress, To: []string{"visible@recipient.test"}, CC: []string{"copy@recipient.test"}, BCC: bcc, RcptTo: append([]string{"visible@recipient.test", "copy@recipient.test"}, bcc...), Subject: "durable snapshot", TextBody: "immutable snapshot body", HeadersJSON: json.RawMessage(`{"X-Tag":"safe","Bcc":"header-must-stay-hidden@recipient.test"}`), State: models.OutboundSent}
	must(t, f.st.CreateOutboundJob(context.Background(), j))
	return j
}

// BC03 is a NEW post-17 enqueue, never a fabricated legacy-unknown fixture.
func TestR5SentRecipientSnapshotCompleteOutlivesJobAndLedger(t *testing.T) {
	for _, bc := range []struct {
		name string
		bcc  []string
	}{{"nonempty", []string{"private@recipient.test"}}, {"known-empty", nil}} {
		t.Run(bc.name, func(t *testing.T) {
			f := r5RecipientSnapshotFixture(t)
			ctx := context.Background()
			j := r5RecipientSnapshotJob(t, f, bc.bcc)
			receipt, e := f.st.GetSubmission(ctx, f.u, j.ID)
			must(t, e)
			wire, e := json.Marshal(receipt)
			must(t, e)
			for _, private := range []string{"bcc", "private@recipient.test", "durable snapshot", "immutable snapshot body", "header-must-stay-hidden"} {
				if strings.Contains(string(wire), private) {
					t.Fatalf("ordinary receipt disclosed %q", private)
				}
			}
			_, e = f.pool.Exec(ctx, `DELETE FROM outbound_jobs WHERE id=$1`, j.ID)
			must(t, e)
			var jobs, ledger int
			must(t, f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM outbound_jobs WHERE id=$1),(SELECT count(*) FROM outbound_recipients WHERE job_id=$1)`, j.ID).Scan(&jobs, &ledger))
			if jobs != 0 || ledger != 0 {
				t.Fatal("job or ledger was not actually removed")
			}
			c, e := f.st.GetSubmissionContent(ctx, f.u, j.ID)
			must(t, e)
			if c.RecipientCompleteness != "complete" || c.BCC == nil || len(c.BCC) != len(bc.bcc) || c.TextBody != j.TextBody {
				t.Fatalf("durable snapshot lost: %+v", c)
			}
			if len(bc.bcc) > 0 && c.BCC[0] != bc.bcc[0] {
				t.Fatal("BCC changed after queue cleanup")
			}
			if len(c.Headers) != 1 || c.Headers["X-Tag"] != "safe" {
				t.Fatal("structured BCC contaminated wire-header filter")
			}
			var asset string
			must(t, f.pool.QueryRow(ctx, `SELECT to_jsonb(a)::text FROM sent_mail_assets a WHERE id=$1`, j.ID).Scan(&asset))
			if len(bc.bcc) > 0 && !strings.Contains(asset, bc.bcc[0]) {
				t.Fatal("original A07 durable-asset invariant not met")
			}
			before := asset
			_, e = f.st.GetSubmissionContent(ctx, f.u, j.ID)
			must(t, e)
			must(t, f.pool.QueryRow(ctx, `SELECT to_jsonb(a)::text FROM sent_mail_assets a WHERE id=$1`, j.ID).Scan(&asset))
			if asset != before {
				t.Fatal("GET mutated sent-recipient snapshot")
			}
		})
	}
}

func TestR5SentRecipientSnapshotImmutableAndAtomic(t *testing.T) {
	f := r5RecipientSnapshotFixture(t)
	ctx := context.Background()
	j := r5RecipientSnapshotJob(t, f, []string{"private@recipient.test"})
	for _, q := range []string{`UPDATE sent_mail_assets SET text_body='changed' WHERE id=$1`, `UPDATE sent_mail_assets SET to_addrs=ARRAY['changed@recipient.test'] WHERE id=$1`, `UPDATE sent_mail_assets SET cc_addrs='{}' WHERE id=$1`, `UPDATE sent_mail_assets SET bcc_addrs='{}' WHERE id=$1`, `UPDATE sent_mail_assets SET recipient_completeness='legacy_unknown',bcc_addrs=NULL,recipient_snapshot_version=0 WHERE id=$1`} {
		if _, e := f.pool.Exec(ctx, q, j.ID); e == nil {
			t.Fatal("immutable sent content changed", q)
		}
	}
	_, e := f.pool.Exec(ctx, `UPDATE sent_mail_assets SET bcc_addrs=bcc_addrs WHERE id=$1`, j.ID)
	must(t, e)
	_, e = f.pool.Exec(ctx, `ALTER TABLE sent_mail_assets ADD CONSTRAINT r5_reject_recipient_capture CHECK(recipient_completeness<>'complete') NOT VALID`)
	must(t, e)
	failed := &models.OutboundJob{ID: uuid.New(), TenantID: f.tenant.ID, ZoneID: f.zone.ID, UserID: &f.employee.ID, SenderUserID: &f.employee.ID, SenderMailboxID: &f.personal.ID, MailFrom: f.personal.FullAddress, To: []string{"failed@recipient.test"}, BCC: []string{"hidden@recipient.test"}, RcptTo: []string{"failed@recipient.test", "hidden@recipient.test"}, State: models.OutboundPending}
	draft, e := f.st.SaveMailDraft(ctx, f.u, company.Draft{MailboxID: f.personal.ID, Payload: company.DraftPayload{Subject: "capture rollback"}})
	must(t, e)
	attachment, e := f.st.ReserveMailAttachment(ctx, f.u, company.Attachment{MailboxID: f.personal.ID, Filename: "capture.txt", Size: 3})
	must(t, e)
	must(t, f.st.FinishMailAttachment(ctx, f.u, attachment.ID, company.Hash("abc")))
	failed.AttachmentIDs = []uuid.UUID{attachment.ID}
	quota := store.OutboundQuotaReservation{UserDaily: &store.OutboundUserDailyQuota{UserID: &f.employee.ID, Since: time.Now().Add(-time.Hour), Limit: 2}}
	consumption := store.DraftConsumption{TenantID: f.tenant.ID, UserID: f.employee.ID, ID: draft.ID, Revision: draft.Revision}
	if replayed, e := f.st.CreateOutboundJobConsumeDraft(ctx, failed, quota, consumption); e == nil || replayed {
		t.Fatal("enqueue succeeded without mandatory recipient capture")
	}
	retained, e := f.st.GetMailDraft(ctx, f.u, draft.ID)
	must(t, e)
	if retained.Revision != draft.Revision {
		t.Fatal("failed recipient capture consumed or changed draft")
	}
	var pins int
	must(t, f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM outbound_attachments WHERE job_id=$1)+(SELECT count(*) FROM sent_asset_attachments WHERE asset_id=$1)`, failed.ID).Scan(&pins))
	if pins != 0 {
		t.Fatal("failed capture retained attachment pins")
	}

	var remnants int
	must(t, f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM outbound_jobs WHERE id=$1)+(SELECT count(*) FROM outbound_recipients WHERE job_id=$1)+(SELECT count(*) FROM sent_mail_assets WHERE id=$1)+(SELECT count(*) FROM sent_mail_items WHERE asset_id=$1)+(SELECT count(*) FROM mailbox_event_log WHERE message_id=$1)`, failed.ID).Scan(&remnants))
	if remnants != 0 {
		t.Fatal("failed capture committed partial queue/asset/item/ledger/event")
	}
	_, e = f.pool.Exec(ctx, `ALTER TABLE sent_mail_assets DROP CONSTRAINT r5_reject_recipient_capture`)
	must(t, e)
	// The failed attempt did not consume the one remaining quota slot.
	replayed, e := f.st.CreateOutboundJobConsumeDraft(ctx, failed, quota, consumption)
	must(t, e)
	if replayed {
		t.Fatal("failed capture created a replay record")
	}
	complete, e := f.st.GetSubmissionContent(ctx, f.u, failed.ID)
	must(t, e)
	if complete.RecipientCompleteness != "complete" || len(complete.BCC) != 1 {
		t.Fatal("successful retry did not atomically capture BCC")
	}
}

func r5RecipientSnapshotRequireNotFound(t *testing.T, c *company.SubmissionContent, e error) {
	t.Helper()
	v, ok := app.As(app.FromAuthz(e))
	if c != nil || !ok || v.Kind != app.KindNotFound {
		t.Fatalf("unreadable sent-content result=%+v error=%v", c, e)
	}
}

func TestR5SentRecipientSnapshotUsesExistingCurrentReadAndLifecycle(t *testing.T) {
	for _, mode := range []string{"revoke", "admin", "cross-tenant", "item-expiry", "purge", "mailbox-expiry", "missing-item"} {
		t.Run(mode, func(t *testing.T) {
			f := r5RecipientSnapshotFixture(t)
			ctx := context.Background()
			j := r5RecipientSnapshotJob(t, f, []string{"private@recipient.test"})
			a := f.u
			switch mode {
			case "revoke":
				must(t, grantCurrent(f.st, ctx, f.a, models.MailboxGrant{MailboxID: f.personal.ID, UserID: f.other.ID, CanRead: true}))
				a = authz.Actor{Type: authz.PrincipalUser, ID: f.other.ID, TenantID: f.tenant.ID, Role: models.RoleUser}
				c, e := f.st.GetSubmissionContent(ctx, a, j.ID)
				must(t, e)
				if len(c.BCC) != 1 {
					t.Fatal("current reader denied structured BCC")
				}
				must(t, grantCurrent(f.st, ctx, f.a, models.MailboxGrant{MailboxID: f.personal.ID, UserID: f.other.ID, CanRead: false}))
			case "admin":
				a = f.a
			case "cross-tenant":
				foreignTenant := &models.Tenant{Name: "Foreign content reader", PlanID: f.tenant.PlanID}
				must(t, f.st.CreateTenant(ctx, foreignTenant))
				foreignUser := &models.User{TenantID: foreignTenant.ID, Email: "foreign-reader@recipient.test", PasswordHash: "test-only", Role: models.RoleUser, IsActive: true}
				must(t, f.st.CreateUser(ctx, foreignUser))
				a = authz.Actor{Type: authz.PrincipalUser, ID: foreignUser.ID, TenantID: foreignTenant.ID, Role: models.RoleUser}
			case "item-expiry":
				_, e := f.pool.Exec(ctx, `UPDATE sent_mail_items SET expires_at=clock_timestamp() WHERE asset_id=$1`, j.ID)
				must(t, e)
			case "purge":
				_, e := f.pool.Exec(ctx, `UPDATE sent_mail_items SET deleted_at=clock_timestamp(),purge_after=clock_timestamp() WHERE asset_id=$1`, j.ID)
				must(t, e)
			case "mailbox-expiry":
				_, e := f.pool.Exec(ctx, `UPDATE mailboxes SET expires_at=clock_timestamp() WHERE id=$1`, f.personal.ID)
				must(t, e)
			case "missing-item":
				_, e := f.pool.Exec(ctx, `DELETE FROM sent_mail_items WHERE asset_id=$1`, j.ID)
				must(t, e)
			}
			c, e := f.st.GetSubmissionContent(ctx, a, j.ID)
			r5RecipientSnapshotRequireNotFound(t, c, e)
			allowed, e := f.st.CanReadOutboundContent(ctx, a, j)
			must(t, e)
			if allowed {
				t.Fatal("parallel content policy revived unreadable asset")
			}
		})
	}
}

func TestR5SentRecipientSnapshotRejectsDeadlineAfterMailboxWait(t *testing.T) {
	f := r5RecipientSnapshotFixture(t)
	j := r5RecipientSnapshotJob(t, f, []string{"private@recipient.test"})
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	deadline := r5SentDeadline(t, f, j.ID, "expires")
	hold, e := f.pool.Begin(ctx)
	must(t, e)
	defer hold.Rollback(context.Background())
	_, e = hold.Exec(ctx, `SELECT id FROM mailboxes WHERE id=$1 FOR UPDATE`, f.personal.ID)
	must(t, e)
	type result struct {
		c *company.SubmissionContent
		e error
	}
	done := make(chan result, 1)
	go func() { c, e := f.st.GetSubmissionContent(ctx, f.u, j.ID); done <- result{c, e} }()
	waiter := r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "mailboxes")
	r5AwaitSentDeadline(t, f, ctx, waiter, deadline)
	must(t, hold.Rollback(ctx))
	select {
	case r := <-done:
		r5RecipientSnapshotRequireNotFound(t, r.c, r.e)
	case <-ctx.Done():
		t.Fatal("sent-content read did not finish")
	}
}
