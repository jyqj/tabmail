package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
)

// The sent-content surface reuses the submission metadata scope verbatim:
// submitter and current readers of the sender mailbox may read what was sent;
// everyone else — including administrators — gets the same 404 collapse, so
// existence is not disclosed and content authority never derives from an
// administrative role.
func TestP4SubmissionContentAndAttachmentsScope(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()
	actor := func(u *models.User) authz.Actor {
		return authz.Actor{Type: authz.PrincipalUser, ID: u.ID, TenantID: f.tenant.ID, Role: models.RoleUser}
	}
	mkAttachment := func(name string) *company.Attachment {
		t.Helper()
		a, e := f.st.ReserveMailAttachment(ctx, f.u, company.Attachment{MailboxID: f.personal.ID, Filename: name, Size: 3})
		must(t, e)
		must(t, f.st.FinishMailAttachment(ctx, f.u, a.ID, company.Hash("abc")))
		return a
	}
	pinned := mkAttachment("report.csv")
	pinnedUploading := mkAttachment("draft.bin")
	unlinked := mkAttachment("other.txt")
	job := &models.OutboundJob{
		TenantID: f.tenant.ID, ZoneID: f.zone.ID,
		UserID: &f.employee.ID, SenderUserID: &f.employee.ID, SenderMailboxID: &f.personal.ID,
		MailFrom: f.personal.FullAddress,
		RcptTo:   []string{"dest@client.test"}, To: []string{"dest@client.test"}, CC: []string{"cc@client.test"}, BCC: []string{"blind@client.test"},
		Subject: "quarterly", TextBody: "see attached", HTMLBody: "<p>see attached</p>",
		HeadersJSON:   json.RawMessage(`{"X-Tag":"ok","Bcc":"hidden@client.test","Bad Name":"x"}`),
		AttachmentIDs: []uuid.UUID{pinned.ID, pinnedUploading.ID}, State: models.OutboundSent,
	}
	must(t, f.st.CreateOutboundJob(ctx, job))

	// Submitter reads the full sent content. BCC and blocked/invalid custom
	// header names never surface; the wire-safe subset does.
	c, e := f.st.GetSubmissionContent(ctx, f.u, job.ID)
	must(t, e)
	if c.Subject != "quarterly" || c.TextBody != "see attached" || c.HTMLBody != "<p>see attached</p>" || c.MailFrom != f.personal.FullAddress {
		t.Fatalf("content projection wrong: %+v", c)
	}
	if len(c.To) != 1 || c.To[0] != "dest@client.test" || len(c.CC) != 1 || c.CC[0] != "cc@client.test" {
		t.Fatalf("structural recipients wrong: %+v", c)
	}
	if c.ContentRedacted {
		t.Fatal("in-scope human viewer must not be redacted")
	}
	if len(c.Headers) != 1 || c.Headers["X-Tag"] != "ok" {
		t.Fatalf("header display filter wrong: %v", c.Headers)
	}

	files, e := f.st.ListSubmissionAttachments(ctx, f.u, job.ID)
	must(t, e)
	if len(files) != 2 {
		t.Fatalf("expected the two pinned attachments, got %d", len(files))
	}
	// Ordering is by attachment id (a random UUID), so compare as a set.
	names := map[string]bool{files[0].Filename: true, files[1].Filename: true}
	if !names["draft.bin"] || !names["report.csv"] {
		t.Fatalf("attachment join wrong: %+v", files)
	}
	// ObjectKey/SHA256 are json:"-": the HTTP projection can never leak them
	// (asserted in the API integration test); the download lookup consumes
	// them store-side.

	// A current read grant on the sender mailbox confers content authority;
	// revoking it removes the authority even though the submission is history.
	must(t, grantCurrent(f.st, ctx, f.a, models.MailboxGrant{MailboxID: f.personal.ID, UserID: f.other.ID, CanRead: true}))
	grantee := actor(f.other)
	if _, e = f.st.GetSubmissionContent(ctx, grantee, job.ID); e != nil {
		t.Fatal("current mailbox reader denied sent content", e)
	}
	if _, e = f.st.GetSubmissionAttachment(ctx, grantee, job.ID, pinned.ID); e != nil {
		t.Fatal("current mailbox reader denied sent attachment", e)
	}
	must(t, grantCurrent(f.st, ctx, f.a, models.MailboxGrant{MailboxID: f.personal.ID, UserID: f.other.ID, CanRead: false}))
	if _, e = f.st.GetSubmissionContent(ctx, grantee, job.ID); e == nil {
		t.Fatal("revoked reader still sees sent content")
	}

	// Administrative roles confer no bypass.
	if _, e = f.st.GetSubmissionContent(ctx, f.a, job.ID); e == nil {
		t.Fatal("admin bypassed the content scope")
	}

	// Cross-tenant actors are collapsed to the same not-found.
	foreign := f.u
	foreign.TenantID = uuid.New()
	if _, e = f.st.GetSubmissionContent(ctx, foreign, job.ID); e == nil {
		t.Fatal("cross-tenant content read succeeded")
	}

	// Download lookups: the attachment must be pinned to this job and ready.
	dl, e := f.st.GetSubmissionAttachment(ctx, f.u, job.ID, pinned.ID)
	must(t, e)
	if dl.ObjectKey == "" || dl.SHA256 == "" || dl.State != "ready" || dl.Filename != "report.csv" {
		t.Fatalf("download lookup wrong: %+v", dl)
	}
	if _, e = f.st.GetSubmissionAttachment(ctx, f.u, job.ID, unlinked.ID); e == nil {
		t.Fatal("unpinned attachment downloadable through the job")
	}
	if _, e = f.st.GetSubmissionAttachment(ctx, f.u, job.ID, uuid.New()); e == nil {
		t.Fatal("unknown attachment downloadable")
	}
	_, e = f.pool.Exec(ctx, `UPDATE mail_attachments SET state='uploading' WHERE id=$1`, pinnedUploading.ID)
	must(t, e)
	if _, e = f.st.GetSubmissionAttachment(ctx, f.u, job.ID, pinnedUploading.ID); e == nil {
		t.Fatal("unfinished attachment downloadable")
	}
	if _, e = f.st.ListSubmissionAttachments(ctx, foreign, job.ID); e == nil {
		t.Fatal("cross-tenant attachment listing succeeded")
	}

	var notFound *app.Error
	_, e = f.st.GetSubmissionAttachment(ctx, f.u, job.ID, pinnedUploading.ID)
	if !errors.As(e, &notFound) || notFound.Kind != app.KindNotFound {
		t.Fatalf("expected not-found app error, got %v", e)
	}
}
