package postgres_test

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"strings"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
	"tabmail/internal/testutil"
	"testing"
)

func TestArchitectureConsoleUsesTenantAndCanonicalAuthority(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()
	want, e := f.st.GetWorkMailbox(ctx, f.u, f.personal.ID)
	must(t, e)
	got, e := f.st.ExplainMailboxAccess(ctx, f.a, f.personal.ID, f.employee.ID)
	must(t, e)
	if got.Source != "owner" || got.CanRead != want.CanRead || got.CanSend != want.CanSend || got.CanOrganize != want.CanOrganize {
		t.Fatalf("explanation differs from authority: %+v %+v", got, want)
	}
	adminView, e := f.st.ExplainMailboxAccess(ctx, f.a, f.personal.ID, f.admin.ID)
	must(t, e)
	if adminView.CanRead || adminView.CanSend {
		t.Fatal("management grants content", adminView)
	}
	if _, e = f.st.ExplainMailboxAccess(ctx, f.u, f.personal.ID, f.other.ID); e == nil {
		t.Fatal("employee queried another employee's authority")
	}
	if _, e = f.st.CompanyOverview(ctx, f.u); e == nil {
		t.Fatal("employee queried administration")
	}
	if _, _, e = f.st.ListCompanyAudit(ctx, f.u, models.Page{}); e == nil {
		t.Fatal("employee queried audit")
	}
	a := f.a
	a.TenantID = uuid.New()
	if _, e = f.st.CompanyOverview(ctx, a); e == nil {
		t.Fatal("spoofed tenant accepted")
	}
	_, e = f.pool.Exec(ctx, `INSERT INTO audit_log(tenant_id,actor,action,details) VALUES($1,'fixture','employee.private_test',$2)`, f.tenant.ID, []byte(`{"reason":"documented review","text_body":"PRIVATE_AUDIT_PAYLOAD"}`))
	must(t, e)
	events, n, e := f.st.ListCompanyAudit(ctx, f.a, models.Page{PerPage: 100})
	must(t, e)
	raw, e := json.Marshal(events)
	must(t, e)
	if n == 0 || strings.Contains(string(raw), "PRIVATE_AUDIT_PAYLOAD") {
		t.Fatal("audit projection leaked arbitrary details", string(raw))
	}
	overview, e := f.st.CompanyOverview(ctx, f.a)
	must(t, e)
	if overview.ActiveEmployees != 3 || overview.Mailboxes != 3 {
		t.Fatalf("bad company overview %+v", overview)
	}
}
func TestArchitectureAdministrativeOutboxFailureRollsBack(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()
	mb, e := f.st.GetWorkMailbox(ctx, f.a, f.shared.ID)
	must(t, e)
	var before int
	must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_log`).Scan(&before))
	_, e = f.pool.Exec(ctx, `ALTER TABLE outbox_events ADD CONSTRAINT no_new_company_events CHECK(event_type<>'company.admin.changed') NOT VALID`)
	must(t, e)
	e = f.st.SetWorkGrant(ctx, f.a, models.MailboxGrant{MailboxID: f.shared.ID, UserID: f.employee.ID, CanRead: true}, mb.Revision)
	if e == nil {
		t.Fatal("required outbox failure ignored")
	}
	after, e := f.st.GetWorkMailbox(ctx, f.a, f.shared.ID)
	must(t, e)
	rights, e := f.st.GetWorkMailbox(ctx, f.u, f.shared.ID)
	must(t, e)
	var audits int
	must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_log`).Scan(&audits))
	if after.Revision != mb.Revision || rights.CanRead || audits != before {
		t.Fatal("partial administration committed")
	}
}
func TestArchitectureExpiredArchiveCannotBeReopenedViaWorkspace(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()
	j := archJob(t, f, models.OutboundSent)
	file := company.Attachment{ID: uuid.New(), MailboxID: f.personal.ID, Filename: "sent.txt", ContentType: "text/plain", Size: 4, SHA256: company.Hash("test"), ObjectKey: "immutable-test-object"}
	v, e := f.st.ReserveMailAttachment(ctx, f.u, file)
	must(t, e)
	must(t, f.st.FinishMailAttachment(ctx, f.u, v.ID, company.Hash("test")))
	_, e = f.pool.Exec(ctx, `INSERT INTO outbound_attachments(tenant_id,job_id,attachment_id) VALUES($1,$2,$3)`, f.tenant.ID, j.ID, v.ID)
	must(t, e)
	if _, e = f.st.GetWorkAttachment(ctx, f.u, v.ID); e != nil {
		t.Fatal(e)
	}
	_, e = f.pool.Exec(ctx, `UPDATE sent_mail_items SET expires_at=now()-interval '1 second' WHERE asset_id=$1`, j.ID)
	must(t, e)
	if _, e = f.st.GetSubmissionAttachment(ctx, f.u, j.ID, v.ID); e == nil {
		t.Fatal("expired sent attachment readable")
	}
	if _, e = f.st.GetWorkAttachment(ctx, f.u, v.ID); e == nil {
		t.Fatal("workspace bypassed sent retention")
	}
	// A worker can still use the held job's attachment; expiration is not loss
	// of recovery evidence, and cannot cause a second send.
	pins, e := f.st.OutboundAttachments(ctx, j.ID)
	must(t, e)
	if len(pins) != 1 {
		t.Fatal("worker evidence lost")
	}
}
func TestArchitectureNewHTTPViewsRespectContentAndAdminBoundaries(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()
	j := archJob(t, f, models.OutboundSent)
	h := companyRouter(t, f, testutil.NewMemoryObjectStore(), nil)
	user, admin := r3Token(t, f.employee), r3Token(t, f.admin)
	box := "/api/v1/company/mailboxes/" + f.personal.ID.String()
	r3HTTP(t, h, user, "GET", box+"/sent", nil, 200)
	r3HTTP(t, h, admin, "GET", box+"/sent", nil, 403)
	r3HTTP(t, h, user, "GET", "/api/v1/company/overview", nil, 403)
	r3HTTP(t, h, admin, "GET", "/api/v1/company/overview", nil, 200)
	r3HTTP(t, h, user, "GET", "/api/v1/company/audit", nil, 403)
	r3HTTP(t, h, admin, "GET", "/api/v1/company/audit", nil, 200)
	r3HTTP(t, h, admin, "GET", box+"/access/"+f.employee.ID.String(), nil, 200)
	r3HTTP(t, h, user, "GET", box+"/access/"+f.employee.ID.String(), nil, 403)
	d, e := f.st.SaveMailDraft(ctx, f.u, company.Draft{MailboxID: f.personal.ID, Payload: company.DraftPayload{Subject: "private"}})
	must(t, e)
	list := r3HTTP(t, h, user, "GET", "/api/v1/company/drafts?page=1&per_page=1", nil, 200)
	var paged struct {
		Data []company.Draft `json:"data"`
		Meta struct {
			Total int `json:"total"`
		} `json:"meta"`
	}
	must(t, json.Unmarshal(list.Body.Bytes(), &paged))
	if paged.Meta.Total != 1 || len(paged.Data) != 1 {
		t.Fatal(list.Body.String())
	}
	r3HTTP(t, h, user, "GET", "/api/v1/company/drafts/"+d.ID.String(), nil, 200)
	r3HTTP(t, h, admin, "GET", "/api/v1/company/drafts/"+d.ID.String(), nil, 404)
	r3HTTP(t, h, user, "POST", box+"/sent/"+j.ID.String()+"/actions", map[string]any{"action": "archive", "revision": 1}, 200)
	r3HTTP(t, h, user, "POST", box+"/sent/"+j.ID.String()+"/actions", map[string]any{"action": "trash", "revision": 1}, 409)
	// A cached admin actor still has no content authority.
	other := authz.Actor{Type: authz.PrincipalUser, ID: f.other.ID, TenantID: f.tenant.ID}
	if _, e = f.st.GetSubmissionContent(ctx, other, j.ID); e == nil {
		t.Fatal("ungranted read")
	}
}
