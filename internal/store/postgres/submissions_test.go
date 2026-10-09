package postgres_test

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
)

// Ordinary receipt DTOs are strict status/full-ledger aggregates. Mailbox
// content and attachments require their separate current-read/live endpoints.
func assertSubmissionReceiptDTO(t *testing.T, v company.Submission, private ...string) {
	t.Helper()
	raw, e := json.Marshal(v)
	must(t, e)
	for _, value := range private {
		if value != "" && strings.Contains(string(raw), value) {
			t.Fatalf("receipt leaked private value %q", value)
		}
	}
	var wire map[string]json.RawMessage
	must(t, json.Unmarshal(raw, &wire))
	allowed := map[string]bool{"id": true, "tenant_id": true, "state": true, "status": true, "progress": true, "created_at": true, "updated_at": true, "attempt_count": true, "next_retry": true, "delivery_uncertain": true, "capabilities": true}
	for name := range wire {
		if !allowed[name] {
			t.Fatalf("receipt field outside strict whitelist: %s", name)
		}
	}
	for _, spec := range []struct {
		name    string
		allowed map[string]bool
	}{{"progress", map[string]bool{"completeness": true, "counts": true}}, {"capabilities", map[string]bool{"view_content": true, "retry": true, "retry_block_reason": true}}} {
		if raw, ok := wire[spec.name]; ok {
			var fields map[string]json.RawMessage
			must(t, json.Unmarshal(raw, &fields))
			for name := range fields {
				if !spec.allowed[name] {
					t.Fatalf("%s.%s outside receipt whitelist", spec.name, name)
				}
			}
			if spec.name == "progress" {
				if counts, ok := fields["counts"]; ok {
					var aggregate map[string]json.RawMessage
					must(t, json.Unmarshal(counts, &aggregate))
					for name := range aggregate {
						switch name {
						case "total", "accepted", "pending", "temporary", "permanent", "uncertain":
						default:
							t.Fatalf("progress.counts.%s outside receipt whitelist", name)
						}
					}
				}
			}
		}
	}
}

func TestSubmissionProjectionScopeAndDTO(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()
	employeeActor := authz.Actor{Type: authz.PrincipalUser, ID: f.employee.ID, TenantID: f.tenant.ID, Role: models.RoleUser}
	otherActor := authz.Actor{Type: authz.PrincipalUser, ID: f.other.ID, TenantID: f.tenant.ID, Role: models.RoleUser}
	adminActor := f.a

	// Own submission from the personal mailbox, with a real attachment.
	att, e := f.st.ReserveMailAttachment(ctx, f.u, company.Attachment{MailboxID: f.personal.ID, Filename: "notes.txt", Size: 12})
	must(t, e)
	must(t, f.st.FinishMailAttachment(ctx, f.u, att.ID, strings.Repeat("a", 64)))
	j1 := &models.OutboundJob{TenantID: f.tenant.ID, ZoneID: f.zone.ID, UserID: &f.employee.ID, SenderUserID: &f.employee.ID, SenderMailboxID: &f.personal.ID, MailFrom: f.personal.FullAddress, RcptTo: []string{"a@client.test", "b@client.test"}, To: []string{"a@client.test", "b@client.test"}, Subject: "quarterly numbers", State: models.OutboundSent, RecipientLedger: true, AttachmentIDs: []uuid.UUID{att.ID}}
	must(t, f.st.CreateOutboundJob(ctx, j1))
	_, e = f.pool.Exec(ctx, `INSERT INTO outbound_recipients(tenant_id,job_id,address,state) VALUES($1,$2,'a@client.test','accepted'),($1,$2,'b@client.test','permanent') ON CONFLICT (job_id,address) DO UPDATE SET state=EXCLUDED.state`, f.tenant.ID, j1.ID)
	must(t, e)

	// Shared-mailbox submission owned by another member.
	j2 := &models.OutboundJob{TenantID: f.tenant.ID, ZoneID: f.zone.ID, UserID: &f.other.ID, SenderUserID: &f.other.ID, SenderMailboxID: &f.shared.ID, MailFrom: f.shared.FullAddress, RcptTo: []string{"c@client.test"}, To: []string{"c@client.test"}, Subject: "shared reply", State: models.OutboundPending, RecipientLedger: true}
	must(t, f.st.CreateOutboundJob(ctx, j2))

	// Without any grant the employee sees only their own submission.
	items, total, e := f.st.ListSubmissions(ctx, employeeActor, models.Page{})
	must(t, e)
	if total != 1 || len(items) != 1 || items[0].ID != j1.ID {
		t.Fatalf("own-scope listing wrong: total=%d", total)
	}
	v := items[0]
	assertSubmissionReceiptDTO(t, v, j1.MailFrom, j1.Subject, j1.To[0], j1.To[1], att.ID.String())
	if v.TenantID == nil || *v.TenantID != f.tenant.ID || v.State != models.OutboundSent || v.Status != "partially_accepted" || v.Progress.Completeness != "known" || v.Progress.Counts == nil || *v.Progress.Counts != (company.OutboundReceiptCounts{Total: 2, Accepted: 1, Permanent: 1}) || v.DeliveryUncertain {
		t.Fatalf("complete ledger receipt wrong: %+v", v)
	}
	// The old sensitive expectations remain positive assertions through the
	// actual dedicated content/pin endpoints, not receipt fields.
	content, e := f.st.GetSubmissionContent(ctx, employeeActor, j1.ID)
	must(t, e)
	files, e := f.st.ListSubmissionAttachments(ctx, employeeActor, j1.ID)
	must(t, e)
	if content.MailFrom != f.personal.FullAddress || content.Subject != "quarterly numbers" || !reflect.DeepEqual(content.To, j1.To) || len(files) != 1 || files[0].ID != att.ID {
		t.Fatalf("separate current-readable content/pins wrong: %+v %+v", content, files)
	}

	// A read grant on the shared mailbox pulls its submissions into scope.
	must(t, grantCurrent(f.st, ctx, f.a, models.MailboxGrant{MailboxID: f.shared.ID, UserID: f.employee.ID, CanRead: true}))
	items, total, e = f.st.ListSubmissions(ctx, employeeActor, models.Page{})
	must(t, e)
	if total != 2 || len(items) != 2 {
		t.Fatalf("read-grant scope wrong: total=%d", total)
	}
	// And a pending ledgered job with no attempts yet is "submitted".
	for _, v := range items {
		assertSubmissionReceiptDTO(t, v, j1.Subject, j2.Subject, j1.To[0], j2.To[0])
		if v.ID == j2.ID && (v.Status != "submitted" || v.Progress.Completeness != "known" || v.Progress.Counts == nil || *v.Progress.Counts != (company.OutboundReceiptCounts{Total: 1, Pending: 1})) {
			t.Fatalf("pending full-ledger submission wrong: %+v", v)
		}
	}
	must(t, grantCurrent(f.st, ctx, f.a, models.MailboxGrant{MailboxID: f.shared.ID, UserID: f.employee.ID}))
	items, _, e = f.st.ListSubmissions(ctx, employeeActor, models.Page{})
	must(t, e)
	if len(items) != 1 {
		t.Fatal("revoked grant still exposed shared submissions")
	}

	// Administrators do not get a bypass: the admin view lives in recovery.
	if _, e = f.st.GetSubmission(ctx, adminActor, j1.ID); e == nil {
		t.Fatal("admin fetched an employee submission without content rights")
	}
	// Another member cannot fetch a foreign submission by id.
	if _, e = f.st.GetSubmission(ctx, otherActor, j1.ID); e == nil {
		t.Fatal("foreign submission readable by id")
	}
	if _, e = f.st.GetSubmission(ctx, employeeActor, uuid.New()); e == nil {
		t.Fatal("missing submission must be not-found")
	}
	if appErr, ok := app.As(e); !ok || appErr.Kind != app.KindNotFound {
		t.Fatalf("missing submission error kind wrong: %v", e)
	}

	// Detail projection matches the list projection.
	detail, e := f.st.GetSubmission(ctx, employeeActor, j1.ID)
	must(t, e)
	assertSubmissionReceiptDTO(t, *detail, j1.Subject, j1.MailFrom, j1.To[0], j1.To[1], att.ID.String())
	if !reflect.DeepEqual(*detail, v) {
		t.Fatalf("list/detail aggregate contradiction: list=%+v detail=%+v", v, detail)
	}
	unchanged, e := json.Marshal(detail)
	must(t, e)

	// Draft provenance is internal and must not change the ordinary wire DTO.
	draft, e := f.st.SaveMailDraft(ctx, f.u, company.Draft{MailboxID: f.personal.ID, Payload: company.DraftPayload{To: []string{"d@client.test"}, Subject: "dr"}, Revision: 0})
	must(t, e)
	_, e = f.pool.Exec(ctx, `UPDATE outbound_jobs SET draft_id=$2 WHERE id=$1`, j1.ID, draft.ID)
	must(t, e)
	detail, e = f.st.GetSubmission(ctx, employeeActor, j1.ID)
	must(t, e)
	assertSubmissionReceiptDTO(t, *detail, draft.ID.String(), j1.Subject, j1.MailFrom)
	after, e := json.Marshal(detail)
	must(t, e)
	if string(after) != string(unchanged) {
		t.Fatal("internal draft provenance changed the ordinary receipt")
	}
	must(t, grantCurrent(f.st, ctx, f.a, models.MailboxGrant{MailboxID: f.shared.ID, UserID: f.employee.ID, CanRead: true}))
	if _, e = f.st.GetSubmissionContent(ctx, employeeActor, j2.ID); e != nil {
		t.Fatal("current read grant denied dedicated content", e)
	}
	must(t, grantCurrent(f.st, ctx, f.a, models.MailboxGrant{MailboxID: f.shared.ID, UserID: f.employee.ID}))
	if _, e = f.st.GetSubmissionContent(ctx, employeeActor, j2.ID); e == nil {
		t.Fatal("receipt history bypassed revoked content read")
	}
}

// Uncertainty must surface to the employee: a job stuck in an ambiguous SMTP
// attempt maps to needs_attention, never to a clean terminal state.
func TestSubmissionUncertaintyMapsToNeedsAttention(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()
	employeeActor := authz.Actor{Type: authz.PrincipalUser, ID: f.employee.ID, TenantID: f.tenant.ID, Role: models.RoleUser}
	const privateSubject = "PRIVATE_UNCERTAINTY_SUBJECT_SENTINEL"
	const privateBody = "PRIVATE_UNCERTAINTY_BODY_SENTINEL"
	const privateHeader = "PRIVATE_UNCERTAINTY_HEADER_SENTINEL"
	const privateDiagnostic = "PRIVATE_UNCERTAINTY_DIAGNOSTIC_SENTINEL"
	// Schema keys/booleans/counts and documented outcome enums are public.
	// Check forbidden structure with the original strict nested whitelist,
	// then scan only DECODED string values for unambiguous private source
	// markers and addresses. Never substring-match a secret against JSON keys.
	assertPrivateProjection := func(v company.Submission) {
		t.Helper()
		assertSubmissionReceiptDTO(t, v)
		raw, e := json.Marshal(v)
		must(t, e)
		var wire any
		must(t, json.Unmarshal(raw, &wire))
		private := []string{privateSubject, privateBody, privateHeader, privateDiagnostic, f.personal.FullAddress, "u@client.test"}
		var visit func(any, string)
		visit = func(value any, path string) {
			switch value := value.(type) {
			case map[string]any:
				for name, child := range value {
					visit(child, path+"."+name)
				}
			case []any:
				for _, child := range value {
					visit(child, path+"[]")
				}
			case string:
				for _, secret := range private {
					if secret != "" && strings.Contains(value, secret) {
						t.Fatalf("receipt string value %s leaked private marker/address %q", path, secret)
					}
				}
			}
		}
		visit(wire, "receipt")
	}

	j := &models.OutboundJob{TenantID: f.tenant.ID, ZoneID: f.zone.ID, UserID: &f.employee.ID, SenderUserID: &f.employee.ID, SenderMailboxID: &f.personal.ID, MailFrom: f.personal.FullAddress, RcptTo: []string{"u@client.test"}, To: []string{"u@client.test"}, Subject: privateSubject, TextBody: privateBody, HTMLBody: "<p>" + privateBody + "</p>", HeadersJSON: json.RawMessage(`{"X-Private":"PRIVATE_UNCERTAINTY_HEADER_SENTINEL"}`), LastError: privateDiagnostic, SMTPResponse: privateDiagnostic, State: models.OutboundFailed, RecipientLedger: true}
	must(t, f.st.CreateOutboundJob(ctx, j))
	_, e := f.pool.Exec(ctx, `INSERT INTO outbound_recipients(tenant_id,job_id,address,state,diagnostic) VALUES($1,$2,'u@client.test','uncertain',$3) ON CONFLICT (job_id,address) DO UPDATE SET state=EXCLUDED.state,diagnostic=EXCLUDED.diagnostic`, f.tenant.ID, j.ID, privateDiagnostic)
	must(t, e)
	items, _, e := f.st.ListSubmissions(ctx, employeeActor, models.Page{})
	must(t, e)
	if len(items) != 1 || items[0].Status != "needs_attention" || !items[0].DeliveryUncertain || items[0].Progress.Completeness != "known" || items[0].Progress.Counts == nil || *items[0].Progress.Counts != (company.OutboundReceiptCounts{Total: 1, Uncertain: 1}) {
		t.Fatalf("uncertain submission status wrong: %+v", items)
	}
	assertPrivateProjection(items[0])
	// In-flight ambiguity remains flagged even if the ledger is accepted.
	_, e = f.pool.Exec(ctx, `UPDATE outbound_recipients SET state='accepted' WHERE job_id=$1`, j.ID)
	must(t, e)
	_, e = f.pool.Exec(ctx, `UPDATE outbound_jobs SET in_flight_domain='client.test',state='failed' WHERE id=$1`, j.ID)
	must(t, e)
	items, _, e = f.st.ListSubmissions(ctx, employeeActor, models.Page{})
	must(t, e)
	if len(items) != 1 || items[0].Status != "needs_attention" || !items[0].DeliveryUncertain || items[0].Progress.Completeness != "known" || items[0].Progress.Counts == nil || *items[0].Progress.Counts != (company.OutboundReceiptCounts{Total: 1, Accepted: 1}) {
		t.Fatalf("in-flight uncertainty wrong: %+v", items)
	}
	assertPrivateProjection(items[0])
}
