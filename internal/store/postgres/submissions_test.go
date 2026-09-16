package postgres_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
)

// The employee submission view is a projection of outbound jobs: scope is
// "submitted by me OR sent from a mailbox I can read", and the DTO carries
// only user-facing fields — queue internals (attempts, lease, SMTP response)
// never reach it.
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
	if v.MailboxID != f.personal.ID || v.MailFrom != f.personal.FullAddress || v.Subject != "quarterly numbers" {
		t.Fatalf("projection fields wrong: %+v", v)
	}
	if v.Status != "partially_accepted" {
		t.Fatalf("status mapping wrong: %s", v.Status)
	}
	if len(v.Recipients) != 2 || v.Recipients[0].State != "accepted" || v.Recipients[1].State != "permanent" {
		t.Fatalf("recipient ledger projection wrong: %+v", v.Recipients)
	}
	if v.AttachmentCount != 1 {
		t.Fatalf("attachment count wrong: %d", v.AttachmentCount)
	}
	if v.DraftConsumed || v.TemplateVersionID != nil {
		t.Fatalf("provenance fields wrong: %+v", v)
	}
	if v.DeliveryUncertain {
		t.Fatal("a plain sent job must not be flagged uncertain")
	}

	// A read grant on the shared mailbox pulls its submissions into scope.
	must(t, f.st.SetWorkGrant(ctx, f.a, models.MailboxGrant{MailboxID: f.shared.ID, UserID: f.employee.ID, CanRead: true}))
	items, total, e = f.st.ListSubmissions(ctx, employeeActor, models.Page{})
	must(t, e)
	if total != 2 || len(items) != 2 {
		t.Fatalf("read-grant scope wrong: total=%d", total)
	}
	// And a pending ledgered job with no attempts yet is "submitted".
	for _, v := range items {
		if v.ID == j2.ID && v.Status != "submitted" {
			t.Fatalf("pending submission status wrong: %s", v.Status)
		}
	}
	must(t, f.st.SetWorkGrant(ctx, f.a, models.MailboxGrant{MailboxID: f.shared.ID, UserID: f.employee.ID}))
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
	if detail.Status != "partially_accepted" || detail.AttachmentCount != 1 || len(detail.Recipients) != 2 {
		t.Fatalf("detail projection wrong: %+v", detail)
	}

	// Draft provenance marker is surfaced as draft_consumed.
	draft, e := f.st.SaveMailDraft(ctx, f.u, company.Draft{MailboxID: f.personal.ID, Payload: company.DraftPayload{To: []string{"d@client.test"}, Subject: "dr"}, Revision: 0})
	must(t, e)
	_, e = f.pool.Exec(ctx, `UPDATE outbound_jobs SET draft_id=$2 WHERE id=$1`, j1.ID, draft.ID)
	must(t, e)
	detail, e = f.st.GetSubmission(ctx, employeeActor, j1.ID)
	must(t, e)
	if !detail.DraftConsumed {
		t.Fatal("draft provenance not projected")
	}
}

// Uncertainty must surface to the employee: a job stuck in an ambiguous SMTP
// attempt maps to needs_attention, never to a clean terminal state.
func TestSubmissionUncertaintyMapsToNeedsAttention(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()
	employeeActor := authz.Actor{Type: authz.PrincipalUser, ID: f.employee.ID, TenantID: f.tenant.ID, Role: models.RoleUser}
	j := &models.OutboundJob{TenantID: f.tenant.ID, ZoneID: f.zone.ID, UserID: &f.employee.ID, SenderUserID: &f.employee.ID, SenderMailboxID: &f.personal.ID, MailFrom: f.personal.FullAddress, RcptTo: []string{"u@client.test"}, To: []string{"u@client.test"}, Subject: "uncertain", State: models.OutboundFailed, RecipientLedger: true}
	must(t, f.st.CreateOutboundJob(ctx, j))
	_, e := f.pool.Exec(ctx, `INSERT INTO outbound_recipients(tenant_id,job_id,address,state) VALUES($1,$2,'u@client.test','uncertain') ON CONFLICT (job_id,address) DO UPDATE SET state=EXCLUDED.state`, f.tenant.ID, j.ID)
	must(t, e)
	items, _, e := f.st.ListSubmissions(ctx, employeeActor, models.Page{})
	must(t, e)
	if len(items) != 1 || items[0].Status != "needs_attention" {
		t.Fatalf("uncertain submission status wrong: %+v", items)
	}
	// In-flight uncertainty without a ledger row is equally flagged.
	_, e = f.pool.Exec(ctx, `UPDATE outbound_recipients SET state='accepted' WHERE job_id=$1`, j.ID)
	must(t, e)
	_, e = f.pool.Exec(ctx, `UPDATE outbound_jobs SET in_flight_domain='client.test',state='failed' WHERE id=$1`, j.ID)
	must(t, e)
	items, _, e = f.st.ListSubmissions(ctx, employeeActor, models.Page{})
	must(t, e)
	if len(items) != 1 || items[0].Status != "needs_attention" || !items[0].DeliveryUncertain {
		t.Fatalf("in-flight uncertainty wrong: %+v", items)
	}
}
