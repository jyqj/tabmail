package postgres_test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
	"testing"
	"time"
)

func archJob(t *testing.T, f *companyFixture, state models.OutboundState) *models.OutboundJob {
	t.Helper()
	j := &models.OutboundJob{TenantID: f.tenant.ID, ZoneID: f.zone.ID, UserID: &f.employee.ID, SenderUserID: &f.employee.ID, SenderMailboxID: &f.personal.ID, MailFrom: f.personal.FullAddress, RcptTo: []string{"client@recipient.test"}, To: []string{"client@recipient.test"}, Subject: "archived subject", TextBody: "employee permanent message", State: state}
	must(t, f.st.CreateOutboundJob(context.Background(), j))
	return j
}
func TestArchitectureSentAssetsOutliveQueueAndRemainPrivate(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()
	j := archJob(t, f, models.OutboundSent)
	_, e := f.pool.Exec(ctx, `DELETE FROM outbound_jobs WHERE id=$1`, j.ID)
	must(t, e)
	c, e := f.st.GetSubmissionContent(ctx, f.u, j.ID)
	must(t, e)
	if c.TextBody != "employee permanent message" {
		t.Fatalf("archive changed: %+v", c)
	}
	list, n, e := f.st.ListArchivedMail(ctx, f.u, f.personal.ID, "sent", "permanent", models.Page{Page: 1, PerPage: 30})
	must(t, e)
	if n != 1 || len(list) != 1 || list[0].DeliveryAvailable {
		t.Fatalf("queue-owned sent folder: %+v %d", list, n)
	}
	if _, e = f.st.GetSubmissionContent(ctx, f.a, j.ID); e == nil {
		t.Fatal("management right granted content")
	}
	other := authz.Actor{Type: authz.PrincipalUser, ID: f.other.ID, TenantID: f.tenant.ID}
	if _, e = f.st.GetSubmissionContent(ctx, other, j.ID); e == nil {
		t.Fatal("ungranted employee read archive")
	}
	if _, e = f.pool.Exec(ctx, `UPDATE sent_mail_assets SET text_body='changed' WHERE id=$1`, j.ID); e == nil {
		t.Fatal("immutable asset mutated")
	}
	must(t, f.st.MutateArchivedMail(ctx, f.u, f.personal.ID, j.ID, 1, "archive"))
	if e = f.st.MutateArchivedMail(ctx, f.u, f.personal.ID, j.ID, 1, "trash"); e == nil {
		t.Fatal("stale sent revision accepted")
	}
	_, n, e = f.st.ListArchivedMail(ctx, f.u, f.personal.ID, "archive", "", models.Page{})
	must(t, e)
	if n != 1 {
		t.Fatal(n)
	}
	must(t, f.st.MutateArchivedMail(ctx, f.u, f.personal.ID, j.ID, 2, "trash"))
	must(t, f.st.MutateArchivedMail(ctx, f.u, f.personal.ID, j.ID, 3, "restore"))
}
func TestArchitectureSentAssetCreationRollsBackWithJob(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()
	_, e := f.pool.Exec(ctx, `ALTER TABLE mailbox_event_log ADD CONSTRAINT reject_sent CHECK(event_type<>'sent.created')`)
	must(t, e)
	j := &models.OutboundJob{TenantID: f.tenant.ID, ZoneID: f.zone.ID, UserID: &f.employee.ID, SenderUserID: &f.employee.ID, SenderMailboxID: &f.personal.ID, MailFrom: f.personal.FullAddress, To: []string{"a@client.test"}, RcptTo: []string{"a@client.test"}, State: models.OutboundPending}
	if e = f.st.CreateOutboundJob(ctx, j); e == nil {
		t.Fatal("enqueue ignored required asset event failure")
	}
	var n int
	must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM sent_mail_assets`).Scan(&n))
	if n != 0 {
		t.Fatal("partial sent asset")
	}
}
func TestArchitectureDraftCreationReplayAndTombstone(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()
	input := company.Draft{ID: uuid.New(), MailboxID: f.personal.ID, Payload: company.DraftPayload{Subject: "initial", TextBody: "keep"}}
	first, e := f.st.SaveMailDraft(ctx, f.u, input)
	must(t, e)
	replay, e := f.st.SaveMailDraft(ctx, f.u, input)
	must(t, e)
	if first.ID != replay.ID || replay.Revision != 1 {
		t.Fatal("creation replay created another revision")
	}
	changed := input
	changed.Payload.Subject = "different"
	if _, e = f.st.SaveMailDraft(ctx, f.u, changed); e == nil {
		t.Fatal("same creation ID accepted different input")
	}
	must(t, f.st.DeleteMailDraft(ctx, f.u, first.ID, first.Revision))
	if _, e = f.st.SaveMailDraft(ctx, f.u, input); e == nil {
		t.Fatal("consumed/deleted UUID resurrected")
	}
}
func TestArchitectureDraftAuthorizationBeforePagination(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()
	// A large newer set of inaccessible shared drafts must not hide the older
	// own-mailbox draft. Inserts are test data, not a production ACL bypass.
	own, e := f.st.SaveMailDraft(ctx, f.u, company.Draft{MailboxID: f.personal.ID, Payload: company.DraftPayload{Subject: "reachable"}})
	must(t, e)
	_, e = f.pool.Exec(ctx, `INSERT INTO mail_drafts(id,tenant_id,user_id,mailbox_id,payload,updated_at) SELECT gen_random_uuid(),$1,$2,$3,'{}',now()+interval '1 hour' FROM generate_series(1,205)`, f.tenant.ID, f.employee.ID, f.shared.ID)
	must(t, e)
	v, total, e := f.st.ListMailDraftPage(ctx, f.u, models.Page{Page: 1, PerPage: 30})
	must(t, e)
	if total != 1 || len(v) != 1 || v[0].ID != own.ID {
		t.Fatalf("ACL applied after pagination: %d %+v", total, v)
	}
	var hidden uuid.UUID
	must(t, f.pool.QueryRow(ctx, `SELECT id FROM mail_drafts WHERE mailbox_id=$1 LIMIT 1`, f.shared.ID).Scan(&hidden))
	if _, e = f.st.GetMailDraft(ctx, f.u, hidden); e == nil {
		t.Fatal("single draft bypassed current send permission")
	}
}
func TestArchitectureOffboardingPreviewFencesChangesAndReplay(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()
	draft, e := f.st.SaveMailDraft(ctx, f.u, company.Draft{MailboxID: f.personal.ID, Payload: company.DraftPayload{Subject: "private draft"}})
	must(t, e)
	p, e := f.st.PreviewOffboarding(ctx, f.a, f.employee.ID, f.other.ID, company.OffboardingOptions{Drafts: "seal"}, "Employee departure with private drafts")
	must(t, e)
	draft.Payload.Subject = "changed after preview"
	_, e = f.st.SaveMailDraft(ctx, f.u, *draft)
	must(t, e)
	if _, e = f.st.ExecuteOffboarding(ctx, f.a, f.employee.ID, p.ID); e == nil {
		t.Fatal("stale preview executed")
	}
	p, e = f.st.PreviewOffboarding(ctx, f.a, f.employee.ID, f.other.ID, company.OffboardingOptions{Drafts: "seal"}, "Employee departure with private drafts")
	must(t, e)
	if p.Impact.Drafts != 1 || p.Impact.TransferableDrafts != 1 {
		t.Fatalf("incorrect preview %+v", p)
	}
	raw, _ := json.Marshal(p)
	if string(raw) == "" {
		t.Fatal("empty preview")
	}
	done, e := f.st.ExecuteOffboarding(ctx, f.a, f.employee.ID, p.ID)
	must(t, e)
	again, e := f.st.ExecuteOffboarding(ctx, f.a, f.employee.ID, p.ID)
	must(t, e)
	if done.State != "executed" || again.ExecutedAt == nil || !done.ExecutedAt.Equal(*again.ExecutedAt) {
		t.Fatal("offboarding replay changed receipt")
	}
	var sealed bool
	var owner uuid.UUID
	must(t, f.pool.QueryRow(ctx, `SELECT sealed_at IS NOT NULL,user_id FROM mail_drafts WHERE id=$1`, draft.ID).Scan(&sealed, &owner))
	if !sealed || owner != f.employee.ID {
		t.Fatal("private draft transferred without consent")
	}
	other := authz.Actor{Type: authz.PrincipalUser, ID: f.other.ID, TenantID: f.tenant.ID}
	if _, e = f.st.GetMailDraft(ctx, other, draft.ID); e == nil {
		t.Fatal("successor read sealed private draft")
	}
	var version int64
	must(t, f.pool.QueryRow(ctx, `SELECT session_version FROM users WHERE id=$1`, f.employee.ID).Scan(&version))
	if version != f.employee.SessionVersion+1 {
		t.Fatal("replay repeated session revocation")
	}
}
func TestArchitectureOffboardingDispositionAndQueueEvidence(t *testing.T) {
	for _, mode := range []string{"transfer_owned", "discard"} {
		t.Run(mode, func(t *testing.T) {
			f := seedCompany(t)
			ctx := context.Background()
			d, e := f.st.SaveMailDraft(ctx, f.u, company.Draft{MailboxID: f.personal.ID, Payload: company.DraftPayload{Subject: "handover"}})
			must(t, e)
			pending := archJob(t, f, models.OutboundPending)
			active := archJob(t, f, models.OutboundProcessing)
			uncertain := archJob(t, f, models.OutboundRetry)
			_, e = f.pool.Exec(ctx, `UPDATE outbound_jobs SET in_flight_domain='recipient.test' WHERE id=$1`, uncertain.ID)
			must(t, e)
			p, e := f.st.PreviewOffboarding(ctx, f.a, f.employee.ID, f.other.ID, company.OffboardingOptions{Drafts: mode}, "Explicit documented employee disposition")
			must(t, e)
			if p.Impact.Queued != 1 || p.Impact.InFlight != 1 || p.Impact.Uncertain != 1 {
				t.Fatalf("queue preview %+v", p.Impact)
			}
			_, e = f.st.ExecuteOffboarding(ctx, f.a, f.employee.ID, p.ID)
			must(t, e)
			for id, want := range map[uuid.UUID]models.OutboundState{pending.ID: models.OutboundCancelled, active.ID: models.OutboundProcessing, uncertain.ID: models.OutboundRetry} {
				j, e := f.st.GetOutboundJob(ctx, id)
				must(t, e)
				if j.State != want {
					t.Fatalf("%s: %s != %s", id, j.State, want)
				}
			}
			var n int
			must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM mail_drafts WHERE id=$1`, d.ID).Scan(&n))
			if mode == "discard" {
				if n != 0 {
					t.Fatal("explicit discard not performed")
				}
			} else {
				other := authz.Actor{Type: authz.PrincipalUser, ID: f.other.ID, TenantID: f.tenant.ID}
				v, e := f.st.GetMailDraft(ctx, other, d.ID)
				must(t, e)
				if v.Payload.Subject != "handover" || v.Revision != d.Revision+1 {
					t.Fatal("draft not handed over")
				}
			}
			// A racing enqueue that reaches the DB after offboarding must fail even
			// when a caller retained an old employee principal.
			j := &models.OutboundJob{TenantID: f.tenant.ID, ZoneID: f.zone.ID, UserID: &f.employee.ID, SenderUserID: &f.employee.ID, SenderMailboxID: &f.personal.ID, MailFrom: f.personal.FullAddress, RcptTo: []string{"x@client.test"}, State: models.OutboundPending}
			if e = f.st.CreateOutboundJob(ctx, j); e == nil {
				t.Fatal("inactive sender enqueued after offboarding")
			}
		})
	}
}
func TestArchitectureIndexLeaseSearchAndConversation(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		m := &models.Message{TenantID: f.tenant.ID, MailboxID: f.personal.ID, ZoneID: f.zone.ID, Sender: "a@client.test", Recipients: []string{f.personal.FullAddress}, Subject: fmt.Sprintf("message %d", i), RawObjectKey: fmt.Sprintf("raw-%d", i)}
		must(t, f.st.CreateMessage(ctx, m))
	}
	jobs, e := f.st.ClaimMailIndexJobs(ctx, 2)
	must(t, e)
	if len(jobs) != 2 {
		t.Fatalf("jobs %+v", jobs)
	}
	d := company.ParsedMessage{MessageID: jobs[0].MessageID, SourceKey: jobs[0].SourceKey, SourceSHA256: company.Hash("raw"), ParserVersion: 1, TextBody: "unique body needle", Parts: []company.ParsedAttachment{}, ThreadKey: company.Hash("thread")}
	wrong := jobs[0]
	wrong.Token = uuid.New()
	if e = f.st.CompleteMailIndexJob(ctx, wrong, d); e == nil {
		t.Fatal("stale worker committed")
	}
	must(t, f.st.CompleteMailIndexJob(ctx, jobs[0], d))
	d.MessageID = jobs[1].MessageID
	d.SourceKey = jobs[1].SourceKey
	must(t, f.st.CompleteMailIndexJob(ctx, jobs[1], d))
	docs, n, e := f.st.ListWorkMessages(ctx, f.u, f.personal.ID, "inbox", "unique body needle", models.Page{})
	must(t, e)
	if n != 2 || len(docs) != 2 {
		t.Fatal("body search not indexed", n)
	}
	conv, n, e := f.st.ListMessageConversation(ctx, f.u, f.personal.ID, jobs[0].MessageID, models.Page{})
	must(t, e)
	if n != 2 || len(conv) != 2 {
		t.Fatal("thread not scoped/assembled", n)
	}
	status, e := f.st.ContentIndexStatus(ctx, f.u, f.personal.ID)
	must(t, e)
	if status.Total != 2 || status.Indexed != 2 {
		t.Fatalf("status %+v", status)
	}
	_, e = f.pool.Exec(ctx, `UPDATE messages SET raw_object_key='new-source' WHERE id=$1`, jobs[0].MessageID)
	must(t, e)
	if e = f.st.CompleteMailIndexJob(ctx, jobs[0], d); e == nil {
		t.Fatal("obsolete raw source reindexed")
	}
	v, e := f.st.GetParsedMessage(ctx, f.u, f.personal.ID, jobs[0].MessageID)
	must(t, e)
	if v != nil {
		t.Fatal("obsolete parsed content returned")
	}
	if _, e = f.st.GetParsedMessage(ctx, f.a, f.personal.ID, jobs[1].MessageID); e == nil {
		t.Fatal("admin index lookup disclosed content")
	}
}
func TestArchitectureOffboardingExpiryAndAuditRollback(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()
	p, e := f.st.PreviewOffboarding(ctx, f.a, f.employee.ID, f.other.ID, company.OffboardingOptions{Drafts: "seal"}, "Documented private asset preservation")
	must(t, e)
	_, e = f.pool.Exec(ctx, `UPDATE employee_offboarding_plans SET expires_at=$2 WHERE id=$1`, p.ID, time.Now().Add(-time.Minute))
	must(t, e)
	if _, e = f.st.ExecuteOffboarding(ctx, f.a, f.employee.ID, p.ID); e == nil {
		t.Fatal("expired preview executed")
	}
	p, e = f.st.PreviewOffboarding(ctx, f.a, f.employee.ID, f.other.ID, company.OffboardingOptions{Drafts: "seal"}, "Documented private asset preservation")
	must(t, e)
	_, e = f.pool.Exec(ctx, `ALTER TABLE audit_log ADD CONSTRAINT reject_offboard CHECK(action<>'employee.offboard')`)
	must(t, e)
	if _, e = f.st.ExecuteOffboarding(ctx, f.a, f.employee.ID, p.ID); e == nil {
		t.Fatal("audit failure ignored")
	}
	var active bool
	must(t, f.pool.QueryRow(ctx, `SELECT is_active FROM users WHERE id=$1`, f.employee.ID).Scan(&active))
	if !active {
		t.Fatal("failed audit still disabled employee")
	}
}
