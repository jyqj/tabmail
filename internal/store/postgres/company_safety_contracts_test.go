package postgres_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/app"
	"tabmail/internal/company"
	"tabmail/internal/models"
	"tabmail/internal/testutil"
)

func deleteEntry(f *companyFixture) models.AuditEntry {
	return models.AuditEntry{TenantID: &f.tenant.ID, Actor: f.a.AuditLabel(), Details: json.RawMessage(`{"reason":"remove unused configuration"}`)}
}

func TestDomainDeletePreservesFormerPrimaryAssets(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()
	draft, err := f.st.SaveMailDraft(ctx, f.u, company.Draft{MailboxID: f.personal.ID, Payload: company.DraftPayload{Subject: "keep me"}})
	must(t, err)
	newer := &models.DomainZone{TenantID: f.tenant.ID, Domain: "new-company.test", IsVerified: true, MXVerified: true}
	must(t, f.st.CreateZone(ctx, newer))
	settings, err := f.st.GetCompanySettings(ctx, f.tenant.ID)
	must(t, err)
	settings.PrimaryZoneID = newer.ID
	_, err = f.st.ConfigureCompany(ctx, f.a, *settings)
	must(t, err)
	wantAppKind(t, f.st.DeleteZone(ctx, f.zone.ID, deleteEntry(f)), app.KindConflict)
	// The invariant is also enforced below every HTTP/service entry point.
	if _, err = f.pool.Exec(ctx, `DELETE FROM domain_zones WHERE id=$1`, f.zone.ID); err == nil {
		t.Fatal("direct SQL cascaded a former primary domain's mailboxes")
	}
	saved, err := f.st.GetMailDraft(ctx, f.u, draft.ID)
	must(t, err)
	if saved.Payload.Subject != "keep me" {
		t.Fatal("draft lost after refused deletion")
	}
	mailbox, err := f.st.GetMailboxByAddress(ctx, f.personal.FullAddress)
	must(t, err)
	if mailbox == nil {
		t.Fatal("mailbox lost after refused deletion")
	}
	var audits int
	must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action='domain.delete' AND resource_id=$1`, f.zone.ID).Scan(&audits))
	if audits != 0 {
		t.Fatal("rejected deletion left a success audit")
	}
}

func TestDomainDeleteTenantScopeAndAtomicAudit(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()
	zone := &models.DomainZone{TenantID: f.tenant.ID, Domain: "unused.test"}
	must(t, f.st.CreateZone(ctx, zone))
	wrong := deleteEntry(f)
	other := &models.Tenant{Name: "Other", PlanID: f.tenant.PlanID}
	must(t, f.st.CreateTenant(ctx, other))
	wrong.TenantID = &other.ID
	wantAppKind(t, f.st.DeleteZone(ctx, zone.ID, wrong), app.KindNotFound)
	_, err := f.pool.Exec(ctx, `ALTER TABLE audit_log ADD CONSTRAINT fail_domain_audit CHECK(action<>'domain.delete') NOT VALID`)
	must(t, err)
	if f.st.DeleteZone(ctx, zone.ID, deleteEntry(f)) == nil {
		t.Fatal("deletion survived required audit failure")
	}
	kept, err := f.st.GetZone(ctx, zone.ID)
	must(t, err)
	if kept == nil {
		t.Fatal("audit failure did not roll back domain deletion")
	}
	_, err = f.pool.Exec(ctx, `ALTER TABLE audit_log DROP CONSTRAINT fail_domain_audit`)
	must(t, err)
	must(t, f.st.DeleteZone(ctx, zone.ID, deleteEntry(f)))
	kept, err = f.st.GetZone(ctx, zone.ID)
	must(t, err)
	if kept != nil {
		t.Fatal("unused domain was not deleted")
	}
	var count int
	must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action='domain.delete' AND resource_id=$1 AND details->>'domain'='unused.test'`, zone.ID).Scan(&count))
	if count != 1 {
		t.Fatalf("deletion audits = %d", count)
	}
}

func TestDomainDeleteProtectsDurableIngressProvenance(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()
	zone := &models.DomainZone{TenantID: f.tenant.ID, Domain: "recovery.test"}
	must(t, f.st.CreateZone(ctx, zone))
	job := uuid.New()
	_, err := f.pool.Exec(ctx, `INSERT INTO ingest_jobs(id,source,recipients,raw_object_key) VALUES($1,'smtp',ARRAY['old@recovery.test'],'retained.eml')`, job)
	must(t, err)
	_, err = f.pool.Exec(ctx, `INSERT INTO ingest_recipient_outcomes(job_id,mailbox_id,tenant_id,zone_id,address,state) VALUES($1,$2,$3,$4,'old@recovery.test','held')`, job, uuid.New(), f.tenant.ID, zone.ID)
	must(t, err)
	wantAppKind(t, f.st.DeleteZone(ctx, zone.ID, deleteEntry(f)), app.KindConflict)
}

func TestDomainDeleteConcurrentMailboxInsertCannotCascade(t *testing.T) {
	f := seedCompany(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	zone := &models.DomainZone{TenantID: f.tenant.ID, Domain: "race.test"}
	must(t, f.st.CreateZone(ctx, zone))
	tx, err := f.pool.Begin(ctx)
	must(t, err)
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `INSERT INTO mailboxes(tenant_id,zone_id,local_part,resolved_domain,full_address,access_mode,mailbox_kind) VALUES($1,$2,'new','race.test','new@race.test','token','shared')`, f.tenant.ID, zone.ID)
	must(t, err)
	done := make(chan error, 1)
	go func() { done <- f.st.DeleteZone(ctx, zone.ID, deleteEntry(f)) }()
	must(t, tx.Commit(ctx))
	wantAppKind(t, <-done, app.KindConflict)
	mailbox, err := f.st.GetMailboxByAddress(ctx, "new@race.test")
	must(t, err)
	if mailbox == nil {
		t.Fatal("concurrent insert lost to domain cascade")
	}
}

func TestMailboxGrantStaleSnapshotCannotRestoreRevokedPermission(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()
	snapshot, err := f.st.ListWorkGrants(ctx, f.a, f.shared.ID)
	must(t, err)
	g := models.MailboxGrant{MailboxID: f.shared.ID, UserID: f.employee.ID, CanRead: true, CanSend: true}
	must(t, f.st.SetWorkGrant(ctx, f.a, g, snapshot.Revision))
	beforeRevoke, err := f.st.ListWorkGrants(ctx, f.a, f.shared.ID)
	must(t, err)
	must(t, f.st.SetWorkGrant(ctx, f.a, models.MailboxGrant{MailboxID: f.shared.ID, UserID: f.employee.ID}, beforeRevoke.Revision))
	wantAppKind(t, f.st.SetWorkGrant(ctx, f.a, g, beforeRevoke.Revision), app.KindConflict)
	latest, err := f.st.ListWorkGrants(ctx, f.a, f.shared.ID)
	must(t, err)
	if len(latest.Grants) != 0 || latest.Revision != beforeRevoke.Revision+1 {
		t.Fatalf("stale grant resurrected rights: %+v", latest)
	}
	var audits int
	must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action='mailbox.grant' AND resource_id=$1`, f.shared.ID).Scan(&audits))
	if audits != 2 {
		t.Fatalf("stale write emitted a success audit: %d", audits)
	}
}

func TestMailboxGrantAndPolicyShareOneCAS(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()
	snapshot, err := f.st.ListWorkGrants(ctx, f.a, f.shared.ID)
	must(t, err)
	errs := make(chan error, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		errs <- f.st.SetWorkGrant(ctx, f.a, models.MailboxGrant{MailboxID: f.shared.ID, UserID: f.employee.ID, CanRead: true}, snapshot.Revision)
	}()
	go func() {
		defer wg.Done()
		<-start
		policy := "disabled"
		errs <- f.st.SetWorkMailboxSendPolicy(ctx, f.a, f.shared.ID, &policy, snapshot.Revision)
	}()
	close(start)
	wg.Wait()
	close(errs)
	success, conflict := 0, 0
	for err := range errs {
		if err == nil {
			success++
		} else if e, ok := app.As(err); ok && e.Kind == app.KindConflict {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("success=%d conflict=%d", success, conflict)
	}
	view, err := f.st.ListWorkGrants(ctx, f.a, f.shared.ID)
	must(t, err)
	if view.Revision != snapshot.Revision+1 {
		t.Fatal("CAS advanced more than once")
	}
}

func TestMailboxAdministrationHTTPRequiresObservedVersion(t *testing.T) {
	f := seedCompany(t)
	h := companyRouter(t, f, testutil.NewMemoryObjectStore(), newPolicyService(t, f))
	admin := r3Token(t, f.admin)
	path := "/api/v1/company/mailboxes/" + f.shared.ID.String() + "/grants"
	snapshot := r3Data[company.MailboxGrantSnapshot](t, r3HTTP(t, h, admin, "GET", path, nil, 200))
	body := map[string]any{"user_id": f.employee.ID, "can_read": true}
	r3HTTP(t, h, admin, "PUT", path, body, 400)
	body["revision"] = snapshot.Revision
	r3HTTP(t, h, admin, "PUT", path, body, 200)
	r3HTTP(t, h, admin, "PUT", path, body, 409)
	policyPath := "/api/v1/company/mailboxes/" + f.shared.ID.String() + "/send-policy"
	r3HTTP(t, h, admin, "PUT", policyPath, map[string]any{"send_policy": "disabled"}, 400)
	r3HTTP(t, h, admin, "PUT", policyPath, map[string]any{"send_policy": "disabled", "revision": snapshot.Revision}, 409)
}

func TestTemplateUsableListAndSendRequireSameAdminGrant(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()
	access, err := f.st.GetWorkMailbox(ctx, f.a, f.personal.ID)
	must(t, err)
	must(t, f.st.SetWorkGrant(ctx, f.a, models.MailboxGrant{MailboxID: f.personal.ID, UserID: f.admin.ID, CanSend: true}, access.Revision))
	tpl, err := f.st.SaveMailTemplate(ctx, f.a, company.Template{Name: "Admin authored", Draft: templateDraft()})
	must(t, err)
	version, err := f.st.PublishMailTemplate(ctx, f.a, tpl.ID, tpl.Revision)
	must(t, err)
	available, err := f.st.ListUsableTemplates(ctx, f.a, f.personal.ID)
	must(t, err)
	if len(available) != 0 {
		t.Fatal("management right leaked into sendable template list")
	}
	if _, _, _, err = f.st.TemplateForSend(ctx, f.tenant.ID, &f.admin.ID, nil, f.personal.ID, version.ID); err == nil {
		t.Fatal("ungranted admin template send allowed")
	}
	grant := company.TemplateGrant{TemplateID: tpl.ID, MailboxID: f.personal.ID, UserID: f.admin.ID}
	must(t, f.st.SetTemplateGrant(ctx, f.a, grant, true))
	available, err = f.st.ListUsableTemplates(ctx, f.a, f.personal.ID)
	must(t, err)
	if len(available) != 1 || available[0].ID != version.ID {
		t.Fatal("explicitly granted template absent")
	}
	_, _, _, err = f.st.TemplateForSend(ctx, f.tenant.ID, &f.admin.ID, nil, f.personal.ID, version.ID)
	must(t, err)
	must(t, f.st.SetTemplateGrant(ctx, f.a, grant, false))
	available, err = f.st.ListUsableTemplates(ctx, f.a, f.personal.ID)
	must(t, err)
	if len(available) != 0 {
		t.Fatal("revoked template still listed")
	}
}

func TestPinnedTemplateDraftRemainsUsableAfterNewPublication(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()
	tpl, err := f.st.SaveMailTemplate(ctx, f.a, company.Template{Name: "Pinned", Draft: templateDraft()})
	must(t, err)
	v1, err := f.st.PublishMailTemplate(ctx, f.a, tpl.ID, tpl.Revision)
	must(t, err)
	must(t, f.st.SetTemplateGrant(ctx, f.a, company.TemplateGrant{TemplateID: tpl.ID, MailboxID: f.personal.ID, UserID: f.employee.ID}, true))
	saved, err := f.st.SaveMailDraft(ctx, f.u, company.Draft{MailboxID: f.personal.ID, Payload: company.DraftPayload{TemplateVersionID: &v1.ID, TemplateVars: map[string]string{"customer": "Old customer"}}})
	must(t, err)
	// Publish increments template revision; reload it rather than assuming one.
	all, err := f.st.ListMailTemplates(ctx, f.a)
	must(t, err)
	next := all[0]
	next.Draft.Subject = "New {{.customer}}"
	updated, err := f.st.SaveMailTemplate(ctx, f.a, next)
	must(t, err)
	v2, err := f.st.PublishMailTemplate(ctx, f.a, updated.ID, updated.Revision)
	must(t, err)
	listed, err := f.st.ListUsableTemplates(ctx, f.u, f.personal.ID)
	must(t, err)
	if len(listed) != 1 || listed[0].ID != v2.ID {
		t.Fatal("picker did not select new version")
	}
	restored, err := f.st.GetMailDraft(ctx, f.u, saved.ID)
	must(t, err)
	if restored.TemplateVersion == nil || restored.TemplateVersion.Status != company.TemplateVersionUsable || restored.TemplateVersion.Snapshot == nil || restored.TemplateVersion.Snapshot.Subject != v1.Snapshot.Subject {
		t.Fatal("old draft lost authorized immutable snapshot")
	}
	_, _, _, err = f.st.TemplateForSend(ctx, f.tenant.ID, &f.employee.ID, nil, f.personal.ID, v1.ID)
	must(t, err)
}
