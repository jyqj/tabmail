package postgres_test

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"strings"
	"sync"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/config"
	"tabmail/internal/models"
	"tabmail/internal/outbound"
	"tabmail/internal/store/postgres"
	"tabmail/internal/testpg"
	"testing"
	"time"
)

type companyFixture struct {
	st                     *postgres.PgStore
	pool                   *pgxpool.Pool
	tenant                 *models.Tenant
	admin, employee, other *models.User
	a, u                   authz.Actor
	zone                   *models.DomainZone
	personal, shared       *models.Mailbox
}

func seedCompany(t *testing.T) *companyFixture {
	t.Helper()
	ctx := context.Background()
	st, pool, _ := testpg.NewPostgres(t)
	f := &companyFixture{st: st, pool: pool}
	f.tenant = &models.Tenant{Name: "Company", PlanID: uuid.MustParse("00000000-0000-0000-0000-000000000002")}
	must(t, st.CreateTenant(ctx, f.tenant))
	f.admin = &models.User{TenantID: f.tenant.ID, Email: "admin@contact.test", DisplayName: "Administrator", Role: models.RoleAdmin, IsActive: true, PasswordHash: "not-a-production-password"}
	must(t, st.CreateUser(ctx, f.admin))
	f.a = authz.Actor{Type: authz.PrincipalUser, ID: f.admin.ID, TenantID: f.tenant.ID, Role: models.RoleAdmin, IsAdmin: true}
	f.zone = &models.DomainZone{TenantID: f.tenant.ID, Domain: "company.test", IsVerified: true, MXVerified: true}
	must(t, st.CreateZone(ctx, f.zone))
	_, e := st.ConfigureCompany(ctx, f.a, company.Settings{Name: "Example Company", PrimaryZoneID: f.zone.ID})
	must(t, e)
	for i, email := range []string{"employee@contact.test", "successor@contact.test"} {
		local := strings.Split(email, "@")[0]
		hash := company.Hash("invitation-" + local)
		_, e := st.InviteEmployee(ctx, f.a, company.InvitationInput{Email: email, LocalPart: local, DisplayName: local}, hash)
		must(t, e)
		must(t, st.ActivateEmployee(ctx, hash, "test-only-password-hash"))
		u, e := st.GetUserByEmail(ctx, email)
		must(t, e)
		if i == 0 {
			f.employee = u
			f.u = authz.Actor{Type: authz.PrincipalUser, ID: u.ID, TenantID: f.tenant.ID, Role: models.RoleUser}
		} else {
			f.other = u
		}
	}
	f.personal, e = st.GetMailboxByAddress(ctx, "employee@company.test")
	must(t, e)
	f.shared, e = st.CreateWorkMailbox(ctx, f.a, company.MailboxInput{LocalPart: "support", Kind: "shared"})
	must(t, e)
	return f
}
func TestR3EmployeeActivationSingleUseAndPrivateProvisioning(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()
	hash := company.Hash("new-hire")
	inv, e := f.st.InviteEmployee(ctx, f.a, company.InvitationInput{Email: "new@contact.test", LocalPart: "new", DisplayName: "New"}, hash)
	must(t, e)
	if inv.Address != "new@company.test" {
		t.Fatal(inv)
	}
	start := make(chan struct{})
	errs := make(chan error, 8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); <-start; errs <- f.st.ActivateEmployee(ctx, hash, "test-hash") }()
	}
	close(start)
	wg.Wait()
	close(errs)
	success := 0
	for e := range errs {
		if e == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("activation successes=%d", success)
	}
	u, e := f.st.GetUserByEmail(ctx, "new@contact.test")
	must(t, e)
	mb, e := f.st.GetMailboxByAddress(ctx, inv.Address)
	must(t, e)
	if u.Role != models.RoleUser || mb.Kind != "personal" || mb.AccessMode != models.AccessToken || mb.OwnerUserID == nil || *mb.OwnerUserID != u.ID {
		t.Fatal("activation did not create protected employee mailbox")
	}
	p, e := f.st.EffectivePermission(ctx, u.ID)
	must(t, e)
	if !p.CanSend || p.CanCreateDomains || p.CanCreateRoutes || p.CanCreateAPIKeys {
		t.Fatalf("unsafe company employee defaults: %+v", p)
	}
}
func TestR3EmployeeInvitationRevocationExpiryAndAuditRollback(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()
	hash := company.Hash("rollback")
	inv, e := f.st.InviteEmployee(ctx, f.a, company.InvitationInput{Email: "rollback@contact.test", LocalPart: "rollback", DisplayName: "Rollback"}, hash)
	must(t, e)
	_, e = f.pool.Exec(ctx, `ALTER TABLE audit_log ADD CONSTRAINT injected_audit_failure CHECK(action<>'employee.activate') NOT VALID`)
	must(t, e)
	if f.st.ActivateEmployee(ctx, hash, "hash") == nil {
		t.Fatal("activation survived required audit failure")
	}
	u, e := f.st.GetUserByEmail(ctx, inv.Email)
	must(t, e)
	if u != nil {
		t.Fatal("user persisted despite audit rollback")
	}
	mb, e := f.st.GetMailboxByAddress(ctx, inv.Address)
	must(t, e)
	if mb != nil {
		t.Fatal("mailbox persisted despite audit rollback")
	}
	_, e = f.pool.Exec(ctx, `ALTER TABLE audit_log DROP CONSTRAINT injected_audit_failure`)
	must(t, e)
	must(t, f.st.RevokeEmployeeInvitation(ctx, f.a, inv.ID))
	if f.st.ActivateEmployee(ctx, hash, "hash") == nil {
		t.Fatal("revoked invite activated")
	}
	_, e = f.st.InviteEmployee(ctx, f.u, company.InvitationInput{Email: "evil@contact.test", LocalPart: "evil", DisplayName: "Evil"}, company.Hash("evil"))
	if e == nil {
		t.Fatal("employee issued invitation")
	}
	hash = company.Hash("expired")
	inv, e = f.st.InviteEmployee(ctx, f.a, company.InvitationInput{Email: "expired@contact.test", LocalPart: "expired", DisplayName: "Expired"}, hash)
	must(t, e)
	_, e = f.pool.Exec(ctx, `UPDATE employee_invitations SET expires_at=now()-interval '1 second' WHERE id=$1`, inv.ID)
	must(t, e)
	if f.st.ActivateEmployee(ctx, hash, "hash") == nil {
		t.Fatal("expired invite activated")
	}
}
func TestR3SharedMailboxRightsAndGrantAuditRollback(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()
	g := models.MailboxGrant{MailboxID: f.shared.ID, UserID: f.employee.ID, CanRead: true}
	must(t, f.st.SetWorkGrant(ctx, f.a, g))
	v, e := f.st.GetWorkMailbox(ctx, f.u, f.shared.ID)
	must(t, e)
	if !v.CanRead || v.CanSend || v.CanOrganize {
		t.Fatal("read grant expanded")
	}
	if e = f.st.MutateWorkMessage(ctx, f.u, f.shared.ID, uuid.New(), "trash"); e == nil {
		t.Fatal("reader could organize")
	}
	_, e = f.pool.Exec(ctx, `ALTER TABLE audit_log ADD CONSTRAINT fail_grant CHECK(action<>'mailbox.grant') NOT VALID`)
	must(t, e)
	g.CanSend = true
	if f.st.SetWorkGrant(ctx, f.a, g) == nil {
		t.Fatal("grant survived audit failure")
	}
	v, e = f.st.GetWorkMailbox(ctx, f.u, f.shared.ID)
	must(t, e)
	if v.CanSend {
		t.Fatal("grant committed despite rollback")
	}
	_, e = f.pool.Exec(ctx, `ALTER TABLE audit_log DROP CONSTRAINT fail_grant`)
	must(t, e)
	foreign := f.u
	foreign.TenantID = uuid.New()
	if _, e = f.st.GetWorkMailbox(ctx, foreign, f.shared.ID); e == nil {
		t.Fatal("cross-company mailbox lookup succeeded")
	}
	g.CanRead = false
	g.CanSend = false
	must(t, f.st.SetWorkGrant(ctx, f.a, g))
	if _, e = f.st.GetWorkMailbox(ctx, f.u, f.shared.ID); e == nil {
		t.Fatal("revoked mailbox discoverable")
	}
	adminView, e := f.st.GetWorkMailbox(ctx, f.a, f.personal.ID)
	must(t, e)
	if adminView.CanRead || adminView.CanSend {
		t.Fatal("admin role implied employee content or send-as rights")
	}
}
func TestR3OffboardingAndOptimisticHandover(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()
	v, e := f.st.GetWorkMailbox(ctx, f.a, f.personal.ID)
	must(t, e)
	if f.st.TransferWorkMailbox(ctx, f.a, f.personal.ID, f.other.ID, v.Revision+1, "Documented handover") == nil {
		t.Fatal("stale handover accepted")
	}
	must(t, f.st.TransferWorkMailbox(ctx, f.a, f.personal.ID, f.other.ID, v.Revision, "Documented handover"))
	if _, e = f.st.GetWorkMailbox(ctx, f.u, f.personal.ID); e == nil {
		t.Fatal("former owner retained mailbox access")
	}
	must(t, f.st.SetWorkGrant(ctx, f.a, models.MailboxGrant{MailboxID: f.shared.ID, UserID: f.employee.ID, CanRead: true, CanSend: true}))
	before := f.employee.SessionVersion
	must(t, f.st.OffboardEmployee(ctx, f.a, f.employee.ID, f.other.ID, "Employee departure and mailbox handover"))
	u, e := f.st.GetUser(ctx, f.employee.ID)
	must(t, e)
	if u.IsActive || u.SessionVersion <= before {
		t.Fatal("offboarding did not invalidate session")
	}
	g, e := f.st.GetMailboxGrant(ctx, f.tenant.ID, f.shared.ID, f.employee.ID)
	must(t, e)
	if g != nil {
		t.Fatal("offboarding retained grants")
	}
	if _, e = f.st.ListWorkMailboxes(ctx, f.u); e == nil {
		t.Fatal("inactive employee accessed workbench")
	}
	if f.st.OffboardEmployee(ctx, f.u, f.admin.ID, f.other.ID, "Malicious employee attempt") == nil {
		t.Fatal("employee offboarded admin")
	}
}
// Mailbox administration follows the member hierarchy: an administrator can
// reassign or regrant a mailbox only when they manage its owner (or own it
// themselves). Personal mailboxes owned by a peer administrator are protected.
func TestMailboxAdministrationFollowsMemberHierarchy(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()
	peer := &models.User{TenantID: f.tenant.ID, Email: "peer-admin@contact.test", DisplayName: "Peer Admin", Role: models.RoleAdmin, IsActive: true, PasswordHash: "not-a-production-password"}
	must(t, f.st.CreateUser(ctx, peer))
	peerMailbox, e := f.st.CreateWorkMailbox(ctx, f.a, company.MailboxInput{LocalPart: "peer", Kind: "personal", OwnerUserID: &peer.ID})
	must(t, e)
	super := &models.User{TenantID: f.tenant.ID, Email: "super@contact.test", DisplayName: "Super Admin", Role: models.RoleSuperAdmin, IsActive: true, PasswordHash: "not-a-production-password"}
	must(t, f.st.CreateUser(ctx, super))
	superActor := authz.Actor{Type: authz.PrincipalUser, ID: super.ID, TenantID: f.tenant.ID, Role: models.RoleSuperAdmin, IsSuperAdmin: true}

	v, e := f.st.GetWorkMailbox(ctx, f.a, peerMailbox.ID)
	must(t, e)
	if f.st.TransferWorkMailbox(ctx, f.a, peerMailbox.ID, f.other.ID, v.Revision, "Attempt over peer admin") == nil {
		t.Fatal("admin handed over a peer administrator's mailbox")
	}
	if f.st.SetWorkGrant(ctx, f.a, models.MailboxGrant{MailboxID: peerMailbox.ID, UserID: f.employee.ID, CanRead: true}) == nil {
		t.Fatal("admin regranted a peer administrator's mailbox")
	}
	must(t, f.st.TransferWorkMailbox(ctx, superActor, peerMailbox.ID, f.other.ID, v.Revision, "Super admin mediated handover"))

	own, e := f.st.CreateWorkMailbox(ctx, f.a, company.MailboxInput{LocalPart: "admin-own", Kind: "personal", OwnerUserID: &f.admin.ID})
	must(t, e)
	ov, e := f.st.GetWorkMailbox(ctx, f.a, own.ID)
	must(t, e)
	must(t, f.st.TransferWorkMailbox(ctx, f.a, own.ID, f.other.ID, ov.Revision, "Administrator own mailbox handover"))

	// Managing employee mailboxes stays within a company administrator's scope.
	must(t, f.st.SetWorkGrant(ctx, f.a, models.MailboxGrant{MailboxID: f.personal.ID, UserID: f.other.ID, CanRead: true}))
}
func TestR3SoftDeleteRestoreRetentionAndEvents(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()
	must(t, f.st.SetWorkGrant(ctx, f.a, models.MailboxGrant{MailboxID: f.shared.ID, UserID: f.employee.ID, CanRead: true, CanOrganize: true}))
	past := time.Now().Add(-time.Hour)
	m := &models.Message{TenantID: f.tenant.ID, MailboxID: f.shared.ID, ZoneID: f.zone.ID, Sender: "sender@client.test", Recipients: []string{f.shared.FullAddress}, Subject: "Quarterly proposal", RawObjectKey: "test-original", ExpiresAt: &past}
	must(t, f.st.CreateMessage(ctx, m))
	n, _, e := f.st.DeleteExpiredMessagesReturningKeys(ctx, time.Now(), 100)
	must(t, e)
	if n != 0 {
		t.Fatal("shared permanent mailbox inherited temporary TTL")
	}
	rows, total, e := f.st.ListWorkMessages(ctx, f.u, f.shared.ID, "inbox", "proposal", models.Page{})
	must(t, e)
	if total != 1 || len(rows) != 1 {
		t.Fatal("search missing result")
	}
	must(t, f.st.MutateWorkMessage(ctx, f.u, f.shared.ID, m.ID, "trash"))
	_, total, e = f.st.ListWorkMessages(ctx, f.u, f.shared.ID, "inbox", "", models.Page{})
	must(t, e)
	if total != 0 {
		t.Fatal("trashed mail remains in inbox")
	}
	rows, total, e = f.st.ListWorkMessages(ctx, f.u, f.shared.ID, "trash", "", models.Page{})
	must(t, e)
	if total != 1 || rows[0].DeletedAt == nil {
		t.Fatal("trash metadata missing")
	}
	must(t, f.st.MutateWorkMessage(ctx, f.u, f.shared.ID, m.ID, "restore"))
	got, e := f.st.GetMessage(ctx, m.ID)
	must(t, e)
	if got.DeletedAt != nil || got.PurgeAfter != nil {
		t.Fatal("restoration lost lifecycle")
	}
	must(t, f.st.MutateWorkMessage(ctx, f.u, f.shared.ID, m.ID, "trash"))
	_, e = f.pool.Exec(ctx, `UPDATE messages SET purge_after=now()-interval '1 second' WHERE id=$1`, m.ID)
	must(t, e)
	n, _, e = f.st.DeleteExpiredMessagesReturningKeys(ctx, time.Now(), 100)
	must(t, e)
	if n != 1 {
		t.Fatal("trash did not expire after recovery window")
	}
	events, _, e := f.st.ListMailboxEvents(ctx, f.tenant.ID, f.shared.ID, 0, 100)
	must(t, e)
	if len(events) < 4 {
		t.Fatalf("durable notifications missing: %d", len(events))
	}
}
func TestR3DraftRevisionAndAttachmentAuthorization(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()
	att, e := f.st.ReserveMailAttachment(ctx, f.u, company.Attachment{MailboxID: f.personal.ID, Filename: "proposal.pdf", Size: 10})
	must(t, e)
	must(t, f.st.FinishMailAttachment(ctx, f.u, att.ID, strings.Repeat("a", 64)))
	draft, e := f.st.SaveMailDraft(ctx, f.u, company.Draft{MailboxID: f.personal.ID, Payload: company.DraftPayload{Subject: "Proposal", AttachmentIDs: []uuid.UUID{att.ID}}})
	must(t, e)
	old := *draft
	draft.Payload.Subject = "Updated"
	draft, e = f.st.SaveMailDraft(ctx, f.u, *draft)
	must(t, e)
	if _, e = f.st.SaveMailDraft(ctx, f.u, old); e == nil {
		t.Fatal("stale draft overwrote update")
	}
	if f.st.DeleteMailDraft(ctx, f.u, draft.ID, old.Revision) == nil {
		t.Fatal("stale draft deletion accepted")
	}
	other := authz.Actor{Type: authz.PrincipalUser, ID: f.other.ID, TenantID: f.tenant.ID}
	if _, e = f.st.GetWorkAttachment(ctx, other, att.ID); e == nil {
		t.Fatal("unpublished attachment leaked")
	}
	must(t, f.st.DeleteMailDraft(ctx, f.u, draft.ID, draft.Revision))
}
func templateDraft() company.TemplateDraft {
	return company.TemplateDraft{Subject: "Hello {{.customer}}", TextBody: "From {{.employee_name}} at {{.company_name}}", HTMLBody: "<p>Hello {{.customer}}</p>", Variables: []company.Variable{{Name: "customer", Type: "text", Required: true, MaxLength: 100}}}
}
func TestR3TemplateImmutableVersionsAndCurrentGrants(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()
	tpl, e := f.st.SaveMailTemplate(ctx, f.a, company.Template{Name: "Welcome", Draft: templateDraft()})
	must(t, e)
	v, e := f.st.PublishMailTemplate(ctx, f.a, tpl.ID, tpl.Revision)
	must(t, e)
	if _, _, _, e = f.st.TemplateForSend(ctx, f.tenant.ID, &f.employee.ID, nil, f.personal.ID, v.ID); e == nil {
		t.Fatal("ungranted template usable")
	}
	must(t, f.st.SetTemplateGrant(ctx, f.a, company.TemplateGrant{TemplateID: tpl.ID, MailboxID: f.personal.ID, UserID: f.employee.ID}, true))
	_, employee, name, e := f.st.TemplateForSend(ctx, f.tenant.ID, &f.employee.ID, nil, f.personal.ID, v.ID)
	must(t, e)
	if employee != "employee" || name != "Example Company" {
		t.Fatal("identity variables missing")
	}
	if _, e = f.pool.Exec(ctx, `UPDATE mail_template_versions SET snapshot='{}' WHERE id=$1`, v.ID); e == nil {
		t.Fatal("published version mutable")
	}
	tpl.Draft.TextBody = "Changed"
	tpl.Revision++
	next, e := f.st.SaveMailTemplate(ctx, f.a, *tpl)
	must(t, e)
	must(t, f.st.SetMailTemplateRetired(ctx, f.a, next.ID, next.Revision, true))
	if _, _, _, e = f.st.TemplateForSend(ctx, f.tenant.ID, &f.employee.ID, nil, f.personal.ID, v.ID); e == nil {
		t.Fatal("retired template usable")
	}
}
func TestR3SubmissionIdempotencyTemplateOnlyAndCodec(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()
	tpl, e := f.st.SaveMailTemplate(ctx, f.a, company.Template{Name: "Reply", Draft: templateDraft()})
	must(t, e)
	ver, e := f.st.PublishMailTemplate(ctx, f.a, tpl.ID, tpl.Revision)
	must(t, e)
	must(t, f.st.SetWorkGrant(ctx, f.a, models.MailboxGrant{MailboxID: f.shared.ID, UserID: f.employee.ID, CanSend: true, TemplateOnly: true}))
	must(t, f.st.SetTemplateGrant(ctx, f.a, company.TemplateGrant{TemplateID: tpl.ID, MailboxID: f.shared.ID, UserID: f.employee.ID}, true))
	svc := outbound.NewService(config.Outbound{Enabled: true, Mode: "relay", MaxRetries: 5}, f.st, zerolog.Nop())
	req := outbound.SendRequest{TenantID: f.tenant.ID, UserID: &f.employee.ID, SenderMailboxID: &f.shared.ID, ZoneID: f.zone.ID, From: f.shared.FullAddress, To: []string{"client@client.test"}, Subject: "Spoofed", TextBody: "Cannot bypass template", Headers: map[string]string{"References": "<thread@test>", "In-Reply-To": "<parent@test>"}, IdempotencyKey: "test-send"}
	if _, e = svc.Submit(ctx, req); e == nil {
		t.Fatal("template-only accepted freeform")
	}
	req.TemplateVersionID = &ver.ID
	req.TemplateVars = map[string]string{"customer": "Client"}
	ids := make(chan uuid.UUID, 8)
	errs := make(chan error, 8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			j, e := svc.Submit(ctx, req)
			errs <- e
			if e == nil {
				ids <- j.ID
			}
		}()
	}
	wg.Wait()
	close(ids)
	close(errs)
	for e := range errs {
		must(t, e)
	}
	var id uuid.UUID
	for v := range ids {
		if id != uuid.Nil && id != v {
			t.Fatal("duplicate business job")
		}
		id = v
	}
	j, e := f.st.GetOutboundJob(ctx, id)
	must(t, e)
	if j.Subject != "Hello Client" || !j.RecipientLedger || j.TemplateVersionID == nil {
		t.Fatalf("template/provenance codec failed: %+v", j)
	}
	must(t, svc.ValidateJobAuthorization(ctx, j))
	req.TemplateVars = map[string]string{"customer": "Changed"}
	if _, e = svc.Submit(ctx, req); e == nil {
		t.Fatal("same key accepted different request")
	}
	recipients, e := f.st.ListOutboundRecipients(ctx, f.tenant.ID, id)
	must(t, e)
	if len(recipients) != 1 {
		t.Fatal("recipient ledger not created")
	}
	jobs, e := f.st.ClaimOutboundJobs(ctx, time.Now(), 1)
	must(t, e)
	if len(jobs) != 1 {
		t.Fatal("missing claim")
	}
	must(t, svc.ValidateJobAuthorization(ctx, jobs[0]))
	started, e := f.st.BeginOutboundRecipient(ctx, id, jobs[0].DeliveryToken, recipients[0].Address)
	must(t, e)
	if !started {
		t.Fatal("not started")
	}
	_, e = f.pool.Exec(ctx, `UPDATE outbound_jobs SET lease_until=now()-interval '1 second' WHERE id=$1`, id)
	must(t, e)
	jobs, e = f.st.ClaimOutboundJobs(ctx, time.Now(), 1)
	must(t, e)
	if len(jobs) != 0 {
		t.Fatal("uncertain recipient automatically replayed")
	}
	j, e = f.st.GetOutboundJob(ctx, id)
	must(t, e)
	if j.State != models.OutboundFailed || j.InFlightDomain == "" {
		t.Fatal("uncertainty lost")
	}
}
func TestR3MessageMutationAuditRollback(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()
	m := &models.Message{TenantID: f.tenant.ID, MailboxID: f.personal.ID, ZoneID: f.zone.ID, Recipients: []string{f.personal.FullAddress}, Subject: "keep"}
	must(t, f.st.CreateMessage(ctx, m))
	_, e := f.pool.Exec(ctx, `ALTER TABLE audit_log ADD CONSTRAINT fail_trash CHECK(action<>'message.trash')`)
	must(t, e)
	if e = f.st.MutateWorkMessage(ctx, f.u, f.personal.ID, m.ID, "trash"); e == nil {
		t.Fatal("trash succeeded despite audit failure")
	}
	got, e := f.st.GetMessage(ctx, m.ID)
	must(t, e)
	if got.DeletedAt != nil {
		t.Fatal("failed operation changed mailbox")
	}
	var ae *app.Error
	_ = errors.As(e, &ae)
}

func TestR3AttachmentCleanupRespectsLiveDraftReferences(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()
	a, e := f.st.ReserveMailAttachment(ctx, f.u, company.Attachment{MailboxID: f.personal.ID, Filename: "retained.txt", Size: 3})
	must(t, e)
	must(t, f.st.FinishMailAttachment(ctx, f.u, a.ID, company.Hash("abc")))
	d, e := f.st.SaveMailDraft(ctx, f.u, company.Draft{MailboxID: f.personal.ID, Payload: company.DraftPayload{AttachmentIDs: []uuid.UUID{a.ID}}})
	must(t, e)
	_, e = f.pool.Exec(ctx, `UPDATE mail_attachments SET expires_at=now()-interval '1 day' WHERE id=$1`, a.ID)
	must(t, e)
	must(t, f.st.SweepCompanyMetadata(ctx))
	if _, e = f.st.GetWorkAttachment(ctx, f.u, a.ID); e != nil {
		t.Fatal("live draft attachment collected", e)
	}
	must(t, f.st.DeleteMailDraft(ctx, f.u, d.ID, d.Revision))
	must(t, f.st.SweepCompanyMetadata(ctx))
	var count int
	must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM mail_attachments WHERE id=$1`, a.ID).Scan(&count))
	if count != 0 {
		t.Fatal("expired orphan retained")
	}
	stats, e := f.st.CompanyMetrics(ctx)
	must(t, e)
	if len(stats) != 6 {
		t.Fatal("operational metrics missing")
	}
}
