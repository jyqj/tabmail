package submissions

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"tabmail/internal/company"
	"tabmail/internal/models"
)

// restrictedJob returns a fully populated job and an independent snapshot of
// every field redaction is allowed to touch, so tests can prove the input was
// not mutated.
func restrictedJob(f submissionFixture) (*models.OutboundJob, models.OutboundJob) {
	job := &models.OutboundJob{
		ID: uuid.New(), TenantID: f.tenant.ID, ZoneID: f.zone.ID, MailFrom: f.mb.FullAddress,
		UserID: &f.owner.ID, SenderUserID: &f.owner.ID, SenderMailboxID: &f.mb.ID,
		State: models.OutboundSent, Subject: "s", TextBody: "secret", HTMLBody: "<b>secret</b>",
		To: []string{"to@example.test"}, CC: []string{"cc@example.test"}, BCC: []string{"bcc@example.test"},
		RcptTo:           []string{"to@example.test", "cc@example.test", "bcc@example.test"},
		DeliveryToken:    ptrUUID(uuid.New()),
		LastError:        "542 policy: bcc@example.test refused",
		SMTPResponse:     "550 bounce detail",
		InFlightDomain:   "pending.test",
		DeliveredDomains: []string{"private-bcc.example.test"},
		HeadersJSON:      []byte(`{"X-Test":"1"}`),
		AttachmentIDs:    []uuid.UUID{uuid.New()},
	}
	return job, *job
}

func assertJobUnchanged(t *testing.T, job, before *models.OutboundJob) {
	t.Helper()
	if job.TextBody != before.TextBody || job.HTMLBody != before.HTMLBody ||
		job.LastError != before.LastError || job.SMTPResponse != before.SMTPResponse ||
		job.InFlightDomain != before.InFlightDomain || job.ContentRedacted != before.ContentRedacted {
		t.Fatalf("input job was mutated by redaction: %+v", job)
	}
	if job.DeliveryToken == nil {
		t.Fatalf("input job lost its own fields: %+v", job)
	}
	if len(job.BCC) != len(before.BCC) || len(job.RcptTo) != len(before.RcptTo) ||
		len(job.To) != len(before.To) || len(job.CC) != len(before.CC) {
		t.Fatalf("input job address lists were resized: %+v", job)
	}
	if len(before.RcptTo) > 0 && len(job.RcptTo) > 0 && &job.RcptTo[0] != &before.RcptTo[0] {
		t.Fatal("input RcptTo backing array must not be reallocated")
	}
}

func TestRedactOutboundJobViewRestrictedHidesBCCAndProtocol(t *testing.T) {
	f := newSubmissionFixture(t)
	job, before := restrictedJob(f)

	view := RedactOutboundJobView(job, false)
	if view == nil || !view.ContentRedacted {
		t.Fatalf("expected redacted view, got %+v", view)
	}
	if view.TextBody != "" || view.HTMLBody != "" || view.BCC != nil || view.HeadersJSON != nil || view.AttachmentIDs != nil {
		t.Fatalf("redacted view leaked content: %+v", view)
	}
	if view.DeliveryToken != nil || view.RawMIME != nil {
		t.Fatal("redacted view must strip delivery token and raw MIME")
	}
	for _, addr := range []string{"bcc@example.test"} {
		for _, rcpt := range view.RcptTo {
			if rcpt == addr {
				t.Fatalf("redacted RcptTo leaked BCC address %q: %v", addr, view.RcptTo)
			}
		}
	}
	if len(view.RcptTo) != 2 || view.RcptTo[0] != "to@example.test" || view.RcptTo[1] != "cc@example.test" {
		t.Fatalf("redacted RcptTo must be To+CC, got %v", view.RcptTo)
	}
	if view.LastError != RestrictedJobError || view.SMTPResponse != RestrictedProtocolResponse {
		t.Fatalf("redacted protocol copy wrong: %q / %q", view.LastError, view.SMTPResponse)
	}
	if view.InFlightDomain != "" || len(view.DeliveredDomains) != 0 {
		t.Fatal("redacted view must hide in-flight and historical BCC domains")
	}
	assertJobUnchanged(t, job, &before)
}

func TestRedactOutboundJobViewAllowedKeepsContent(t *testing.T) {
	f := newSubmissionFixture(t)
	job, before := restrictedJob(f)

	view := RedactOutboundJobView(job, true)
	if view == nil || view.ContentRedacted {
		t.Fatalf("expected unredacted view, got %+v", view)
	}
	if view.TextBody != "secret" || view.HTMLBody != "<b>secret</b>" || len(view.BCC) != 1 ||
		view.LastError != before.LastError || view.SMTPResponse != before.SMTPResponse {
		t.Fatalf("owner view lost content: %+v", view)
	}
	if view.DeliveryToken != nil || view.RawMIME != nil {
		t.Fatal("delivery token and raw MIME must be stripped even for the owner view")
	}
	if !view.DeliveryUncertain {
		t.Fatal("in-flight domain with non-processing state must surface delivery uncertainty")
	}
	assertJobUnchanged(t, job, &before)
}

func TestRedactionViewsOwnTheirMutableSlices(t *testing.T) {
	f := newSubmissionFixture(t)
	job, _ := restrictedJob(f)
	view := RedactOutboundJobView(job, true)
	view.To[0] = "changed@example.test"
	view.BCC[0] = "changed@example.test"
	view.HeadersJSON[0] = 'x'
	view.DeliveredDomains[0] = "changed.test"
	if job.To[0] == view.To[0] || job.BCC[0] == view.BCC[0] || job.HeadersJSON[0] == 'x' || job.DeliveredDomains[0] == view.DeliveredDomains[0] {
		t.Fatal("view mutation changed source job")
	}
	rows := []company.Recipient{{Address: "to@example.test", State: "accepted"}}
	filtered := FilterRecipientsForJobView(view, rows)
	filtered[0].State = "uncertain"
	if rows[0].State != "accepted" {
		t.Fatal("view mutated source recipient evidence")
	}
}

func TestRedactOutboundJobViewNilFailsClosed(t *testing.T) {
	if view := RedactOutboundJobView(nil, true); view != nil {
		t.Fatalf("nil job must yield nil view, got %+v", view)
	}
	if view := RedactOutboundJobView(nil, false); view != nil {
		t.Fatalf("nil job must yield nil view, got %+v", view)
	}
}

func TestRedactOutboundAttemptView(t *testing.T) {
	before := models.OutboundAttempt{
		ID: uuid.New(), JobID: uuid.New(), Attempt: 1, SMTPCode: 550,
		RemoteHost: "mx.private-bcc.example.test",
		Error:      "550 5.1.1 user unknown (bcc@example.test)", SMTPResponse: "550 5.1.1 brute detail",
	}

	restricted := RedactOutboundAttemptView(&before, false)
	if restricted.RemoteHost != "" || restricted.Error != RestrictedAttemptError || restricted.SMTPResponse != RestrictedProtocolResponse {
		t.Fatalf("restricted attempt leaked diagnostics: %+v", restricted)
	}
	if restricted.SMTPCode != 550 || restricted.Attempt != 1 {
		t.Fatalf("restricted attempt must keep status fields: %+v", restricted)
	}
	if before.Error != "550 5.1.1 user unknown (bcc@example.test)" || before.SMTPResponse != "550 5.1.1 brute detail" {
		t.Fatalf("input attempt was mutated: %+v", before)
	}

	allowed := RedactOutboundAttemptView(&before, true)
	if allowed.Error != before.Error || allowed.SMTPResponse != before.SMTPResponse {
		t.Fatalf("allowed attempt must be verbatim: %+v", allowed)
	}

	if RedactOutboundAttemptView(nil, true) != nil || RedactOutboundAttemptView(nil, false) != nil {
		t.Fatal("nil attempt must yield nil")
	}

	empty := RedactOutboundAttemptView(&models.OutboundAttempt{}, false)
	if empty.Error != "" || empty.SMTPResponse != "" {
		t.Fatalf("empty diagnostics must stay empty, got %+v", empty)
	}
}

func TestFilterRecipientsForJobView(t *testing.T) {
	f := newSubmissionFixture(t)
	job, _ := restrictedJob(f)
	rows := []company.Recipient{
		{Address: "to@example.test", State: "sent", Diagnostic: "250 ok"},
		{Address: "cc@example.test", State: "failed", Diagnostic: "450 temporary"},
		{Address: "bcc@example.test", State: "failed", Diagnostic: "550 5.1.1 bcc refused"},
	}
	snapshot := append([]company.Recipient{}, rows...)

	restricted := RedactOutboundJobView(job, false)
	filtered := FilterRecipientsForJobView(restricted, rows)
	if len(filtered) != 2 {
		t.Fatalf("restricted filter must keep only To+CC rows, got %+v", filtered)
	}
	for _, row := range filtered {
		if row.Address == "bcc@example.test" {
			t.Fatalf("BCC row leaked through restricted filter: %+v", filtered)
		}
		if row.Diagnostic != RestrictedRecipientDiagnostic {
			t.Fatalf("restricted row must carry placeholder diagnostic, got %q", row.Diagnostic)
		}
	}
	for i := range rows {
		if rows[i] != snapshot[i] {
			t.Fatalf("input rows were mutated: %+v", rows)
		}
	}

	allowed := RedactOutboundJobView(job, true)
	if got := FilterRecipientsForJobView(allowed, rows); len(got) != 3 || got[2].Diagnostic != "550 5.1.1 bcc refused" {
		t.Fatalf("allowed view must return rows untouched, got %+v", got)
	}

	if got := FilterRecipientsForJobView(nil, rows); got == nil || len(got) != 0 {
		t.Fatalf("nil view must fail closed with no rows, got %+v", got)
	}
	if len(rows) != 3 {
		t.Fatalf("nil-view filter must not consume rows, got %d", len(rows))
	}
}

// TestRedactOutboundAttemptsUsesContentAllowed pins that the attempt
// diagnostic view is driven by the single ContentAllowed decision, with no
// handler-side copy rules.
func TestRedactOutboundAttemptsUsesContentAllowed(t *testing.T) {
	f := newSubmissionFixture(t)
	job, _ := restrictedJob(f)
	attempts := []*models.OutboundAttempt{
		{ID: uuid.New(), JobID: job.ID, Attempt: 1, Error: "550 refused", SMTPResponse: "550 detail"},
	}

	if err := f.st.CreateOutboundJob(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	ownerViews, err := f.svc.RedactOutboundAttempts(context.Background(), userActor(f.owner), job, attempts)
	if err != nil {
		t.Fatal(err)
	}
	if len(ownerViews) != 1 || ownerViews[0].Error != "550 refused" || ownerViews[0].SMTPResponse != "550 detail" {
		t.Fatalf("owner must see attempt diagnostics verbatim, got %+v", ownerViews)
	}

	otherViews, err := f.svc.RedactOutboundAttempts(context.Background(), userActor(f.other), job, attempts)
	if err != nil {
		t.Fatal(err)
	}
	if len(otherViews) != 1 || otherViews[0].Error != RestrictedAttemptError || otherViews[0].SMTPResponse != RestrictedProtocolResponse {
		t.Fatalf("unauthorized viewer must get placeholder copy, got %+v", otherViews)
	}
	if attempts[0].Error != "550 refused" || attempts[0].SMTPResponse != "550 detail" {
		t.Fatalf("input attempts were mutated: %+v", attempts[0])
	}
}
