package postgres_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/models"
)

// Regression for the submissionScope tenant-conjunct override: rows inserted
// into ANOTHER tenant's outbound_jobs that reference this tenant's member ids
// (user_id, or the sender_user_id / sender_mailbox_id provenance chain) must
// never surface in the member's submission list or detail views.
func TestSubmissionScopeRejectsCrossTenantRows(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()
	employeeActor := authz.Actor{Type: authz.PrincipalUser, ID: f.employee.ID, TenantID: f.tenant.ID, Role: models.RoleUser}

	// Company B with its own zone, so its jobs satisfy every foreign key while
	// their owner/provenance columns point back into company A.
	tenantB := &models.Tenant{Name: "Other Co", PlanID: uuid.MustParse("00000000-0000-0000-0000-000000000002")}
	must(t, f.st.CreateTenant(ctx, tenantB))
	zoneB := &models.DomainZone{TenantID: tenantB.ID, Domain: "other.test", IsVerified: true, MXVerified: true}
	must(t, f.st.CreateZone(ctx, zoneB))

	_, e := f.pool.Exec(ctx, `INSERT INTO outbound_jobs(tenant_id,zone_id,user_id,sender_user_id,mail_from,rcpt_to,to_addrs,subject,state,recipient_ledger)
		VALUES($1,$2,$3,$3,'boss@other.test','{x@client.test}','{x@client.test}','cross-tenant user_id','pending',true)`,
		tenantB.ID, zoneB.ID, f.employee.ID)
	must(t, e)
	_, e = f.pool.Exec(ctx, `INSERT INTO outbound_jobs(tenant_id,zone_id,sender_user_id,sender_mailbox_id,mail_from,rcpt_to,to_addrs,subject,state,recipient_ledger)
		VALUES($1,$2,$3,$4,'boss@other.test','{y@client.test}','{y@client.test}','cross-tenant mailbox chain','pending',true)`,
		tenantB.ID, zoneB.ID, f.employee.ID, f.personal.ID)
	if e == nil {
		t.Fatal("new cross-company employee enqueue bypassed database fence")
	}
	// Still exercise READ isolation for pre-migration malformed provenance.
	// Only privileged fixture SQL backfills a legacy row; production enqueue
	// remains fenced and no trigger or authorization check is disabled.
	var historicalID uuid.UUID
	must(t, f.pool.QueryRow(ctx, `INSERT INTO outbound_jobs(tenant_id,zone_id,sender_mailbox_id,mail_from,rcpt_to,to_addrs,subject,state,recipient_ledger)
        VALUES($1,$2,$3,'boss@other.test','{y@client.test}','{y@client.test}','cross-tenant mailbox chain','pending',true) RETURNING id`,
		tenantB.ID, zoneB.ID, f.personal.ID).Scan(&historicalID))
	_, e = f.pool.Exec(ctx, `UPDATE outbound_jobs SET sender_user_id=$2 WHERE id=$1`, historicalID, f.employee.ID)
	must(t, e)

	items, total, e := f.st.ListSubmissions(ctx, employeeActor, models.Page{})
	must(t, e)
	if total != 0 || len(items) != 0 {
		t.Fatalf("cross-tenant rows leaked into the employee listing: total=%d", total)
	}
	var leakedID uuid.UUID
	must(t, f.pool.QueryRow(ctx, `SELECT id FROM outbound_jobs WHERE tenant_id=$1 AND subject='cross-tenant user_id'`, tenantB.ID).Scan(&leakedID))
	if _, e = f.st.GetSubmission(ctx, employeeActor, leakedID); e == nil {
		t.Fatal("cross-tenant submission readable by id")
	}
	if appErr, ok := app.As(e); !ok || appErr.Kind != app.KindNotFound {
		t.Fatalf("cross-tenant submission error kind wrong: %v", e)
	}

	// Same-tenant ownership still resolves after the fix.
	j := &models.OutboundJob{TenantID: f.tenant.ID, ZoneID: f.zone.ID, UserID: &f.employee.ID, SenderUserID: &f.employee.ID, SenderMailboxID: &f.personal.ID, MailFrom: f.personal.FullAddress, RcptTo: []string{"z@client.test"}, To: []string{"z@client.test"}, Subject: "own tenant", State: models.OutboundSent, RecipientLedger: true}
	must(t, f.st.CreateOutboundJob(ctx, j))
	items, total, e = f.st.ListSubmissions(ctx, employeeActor, models.Page{})
	must(t, e)
	if total != 1 || len(items) != 1 || items[0].ID != j.ID {
		t.Fatalf("own-tenant submission lost: total=%d", total)
	}
}

// API-key principals are refused on the interactive company surface before the
// scope predicate runs (companyReadTx requires an interactive user), so their
// isolation is asserted at the predicate level in
// TestSubmissionScopeKeepsTenantConjunct. This documents the gate.
func TestSubmissionListRejectsAPIKeyPrincipal(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()
	keyActor := authz.Actor{Type: authz.PrincipalAPIKey, ID: uuid.New(), TenantID: f.tenant.ID, TenantWide: true}
	if _, _, e := f.st.ListSubmissions(ctx, keyActor, models.Page{}); e == nil {
		t.Fatal("api key principal listed interactive submissions")
	}
}
