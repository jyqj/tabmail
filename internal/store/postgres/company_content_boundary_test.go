package postgres_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
)

func TestSubmissionAuthorLosesContentButKeepsReceiptAfterReadRevocation(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()
	must(t, f.st.SetWorkGrant(ctx, f.a, models.MailboxGrant{MailboxID: f.shared.ID, UserID: f.employee.ID, CanRead: true, CanSend: true}))
	attachment, e := f.st.ReserveMailAttachment(ctx, f.u, company.Attachment{MailboxID: f.shared.ID, Filename: "proof.txt", Size: 3})
	must(t, e)
	must(t, f.st.FinishMailAttachment(ctx, f.u, attachment.ID, company.Hash("abc")))
	job := &models.OutboundJob{TenantID: f.tenant.ID, ZoneID: f.zone.ID, UserID: &f.employee.ID, SenderUserID: &f.employee.ID, SenderMailboxID: &f.shared.ID, MailFrom: f.shared.FullAddress,
		RcptTo: []string{"client@example.test"}, To: []string{"client@example.test"}, Subject: "receipt", TextBody: "confidential", AttachmentIDs: []uuid.UUID{attachment.ID}, State: models.OutboundSent}
	must(t, f.st.CreateOutboundJob(ctx, job))
	_, e = f.st.GetSubmissionContent(ctx, f.u, job.ID)
	must(t, e)
	_, e = f.st.GetSubmissionAttachment(ctx, f.u, job.ID, attachment.ID)
	must(t, e)
	// Keep send-only: being the author or a current sender must not imply read.
	must(t, f.st.SetWorkGrant(ctx, f.a, models.MailboxGrant{MailboxID: f.shared.ID, UserID: f.employee.ID, CanSend: true}))
	_, e = f.st.GetSubmission(ctx, f.u, job.ID)
	must(t, e)
	_, total, e := f.st.ListSubmissions(ctx, f.u, models.Page{})
	must(t, e)
	if total != 1 {
		t.Fatal("revocation erased operation receipt")
	}
	checks := []func() error{
		func() error { _, e := f.st.GetSubmissionContent(ctx, f.u, job.ID); return e },
		func() error { _, e := f.st.ListSubmissionAttachments(ctx, f.u, job.ID); return e },
		func() error { _, e := f.st.GetSubmissionAttachment(ctx, f.u, job.ID, attachment.ID); return e },
	}
	for _, check := range checks {
		v, ok := app.As(check())
		if !ok || v.Kind != app.KindNotFound {
			t.Fatalf("revoked author content must be 404: %v", v)
		}
	}
	// Once pinned to a sent job, even the uploader reads the bytes only through
	// a current read right: the residual send-only grant stays closed.
	if _, e = f.st.GetWorkAttachment(ctx, f.u, attachment.ID); e == nil {
		t.Fatal("sent attachment bytes leaked to send-only uploader")
	}
	// The new mailbox reader inherits company history, without becoming its author.
	must(t, f.st.SetWorkGrant(ctx, f.a, models.MailboxGrant{MailboxID: f.shared.ID, UserID: f.other.ID, CanRead: true}))
	successor := authz.Actor{Type: authz.PrincipalUser, ID: f.other.ID, TenantID: f.tenant.ID, Role: models.RoleUser}
	_, e = f.st.GetSubmissionContent(ctx, successor, job.ID)
	must(t, e)
	_, e = f.st.GetSubmissionAttachment(ctx, successor, job.ID, attachment.ID)
	must(t, e)
	if _, e = f.st.GetSubmissionContent(ctx, f.a, job.ID); e == nil {
		t.Fatal("admin bypassed read grant")
	}
	_, e = f.pool.Exec(ctx, `UPDATE mailboxes SET expires_at=now()-interval '1 second' WHERE id=$1`, f.shared.ID)
	must(t, e)
	if _, e = f.st.GetSubmissionContent(ctx, successor, job.ID); e == nil {
		t.Fatal("expired mailbox exposed content")
	}
}

func TestCompanyMessageDetailMatchesPersonalListStateAndImmutableScope(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()
	for _, id := range []uuid.UUID{f.employee.ID, f.other.ID} {
		must(t, f.st.SetWorkGrant(ctx, f.a, models.MailboxGrant{MailboxID: f.shared.ID, UserID: id, CanRead: true}))
	}
	m := &models.Message{TenantID: f.tenant.ID, MailboxID: f.shared.ID, ZoneID: f.zone.ID, Sender: "client@example.test", Recipients: []string{f.shared.FullAddress}, Subject: "shared", RawObjectKey: "test/raw"}
	must(t, f.st.CreateMessage(ctx, m))
	must(t, f.st.MutateWorkMessage(ctx, f.u, f.shared.ID, m.ID, "seen"))
	must(t, f.st.MutateWorkMessage(ctx, f.u, f.shared.ID, m.ID, "starred"))
	other := authz.Actor{Type: authz.PrincipalUser, ID: f.other.ID, TenantID: f.tenant.ID, Role: models.RoleUser}
	for _, actor := range []authz.Actor{f.u, other} {
		list, _, e := f.st.ListWorkMessages(ctx, actor, f.shared.ID, "inbox", "", models.Page{})
		must(t, e)
		detail, e := f.st.GetWorkMessage(ctx, actor, f.shared.ID, m.ID)
		must(t, e)
		if len(list) != 1 || list[0].Seen != detail.Seen || list[0].Starred != detail.Starred {
			t.Fatal("list/detail user-state drift")
		}
		if detail.Seen != (actor.ID == f.employee.ID) || detail.Starred != (actor.ID == f.employee.ID) {
			t.Fatal("shared state leaked across members")
		}
	}
	if _, e := f.st.GetWorkMessage(ctx, f.u, f.personal.ID, m.ID); e == nil {
		t.Fatal("message id escaped mailbox boundary")
	}
	if _, e := f.st.GetWorkMessage(ctx, f.a, f.shared.ID, m.ID); e == nil {
		t.Fatal("admin read without grant")
	}
	foreign := f.u
	foreign.TenantID = uuid.New()
	if _, e := f.st.GetWorkMessage(ctx, foreign, f.shared.ID, m.ID); e == nil {
		t.Fatal("cross-tenant message detail")
	}
	must(t, f.st.SetWorkGrant(ctx, f.a, models.MailboxGrant{MailboxID: f.shared.ID, UserID: f.employee.ID}))
	if _, e := f.st.GetWorkMessage(ctx, f.u, f.shared.ID, m.ID); e == nil {
		t.Fatal("revoked message read")
	}
}
