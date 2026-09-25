package postgres_test

import (
	"context"
	"github.com/google/uuid"
	"tabmail/internal/models"
	"tabmail/internal/testutil"
	"testing"
)

func TestArchitectureIndexCrashBudgetAndFailureFence(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()
	m := &models.Message{TenantID: f.tenant.ID, MailboxID: f.personal.ID, ZoneID: f.zone.ID, Sender: "sender@test", Recipients: []string{f.personal.FullAddress}, RawObjectKey: "index-crash-source"}
	must(t, f.st.CreateMessage(ctx, m))
	jobs, e := f.st.ClaimMailIndexJobs(ctx, 1)
	must(t, e)
	if len(jobs) != 1 {
		t.Fatal(jobs)
	}
	_, e = f.pool.Exec(ctx, `UPDATE mail_index_jobs SET attempts=5,lease_until=now()-interval '1 second' WHERE message_id=$1`, m.ID)
	must(t, e)
	if e = f.st.FailMailIndexJob(ctx, jobs[0], "PRIVATE MIME ERROR"); e == nil {
		t.Fatal("expired failure writer accepted")
	}
	next, e := f.st.ClaimMailIndexJobs(ctx, 1)
	must(t, e)
	if len(next) != 0 {
		t.Fatal("crash budget was ignored", next)
	}
	var state string
	must(t, f.pool.QueryRow(ctx, `SELECT state FROM mail_index_jobs WHERE message_id=$1`, m.ID).Scan(&state))
	if state != "failed" {
		t.Fatal(state)
	}
	if _, e = f.st.RetryFailedMailIndex(ctx, f.u, "Operator reviewed source storage"); e == nil {
		t.Fatal("employee requeued admin indexes")
	}
	if _, e = f.st.RetryFailedMailIndex(ctx, f.a, " "); e == nil {
		t.Fatal("blank recovery reason")
	}
	n, e := f.st.RetryFailedMailIndex(ctx, f.a, "Operator reviewed source storage")
	must(t, e)
	if n != 1 {
		t.Fatal(n)
	}
	next, e = f.st.ClaimMailIndexJobs(ctx, 1)
	must(t, e)
	if len(next) != 1 || next[0].Token == jobs[0].Token {
		t.Fatal("no fresh fenced lease", next)
	}
	if e = f.st.FailMailIndexJob(ctx, jobs[0], "old worker"); e == nil {
		t.Fatal("old token changed recovered job")
	}
	n, e = f.st.RetryFailedMailIndex(ctx, f.a, "Review without stealing a live lease")
	must(t, e)
	if n != 0 {
		t.Fatal("recovery stole processing work")
	}
}

func TestArchitectureIndexRecoveryIsBoundedTenantScopedAndAtomic(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()
	_, e := f.pool.Exec(ctx, `INSERT INTO messages(id,tenant_id,mailbox_id,zone_id,sender,recipients,raw_object_key)
 SELECT gen_random_uuid(),$1,$2,$3,'test sender',ARRAY['test recipient'],'fixture-'||n::text FROM generate_series(1,103) n`, f.tenant.ID, f.personal.ID, f.zone.ID)
	must(t, e)
	foreign := &models.Tenant{Name: "Other company", PlanID: f.tenant.PlanID}
	must(t, f.st.CreateTenant(ctx, foreign))
	zone := &models.DomainZone{TenantID: foreign.ID, Domain: "other-company.test"}
	must(t, f.st.CreateZone(ctx, zone))
	// Foreign metadata may never be included in an administrator's batch.
	foreignBox := uuid.New()
	foreignMessage := uuid.New()
	_, e = f.pool.Exec(ctx, `INSERT INTO mailboxes(id,tenant_id,zone_id,local_part,resolved_domain,full_address,access_mode) VALUES($1,$2,$3,'other','other-company.test','other@other-company.test','api_key')`, foreignBox, foreign.ID, zone.ID)
	must(t, e)
	_, e = f.pool.Exec(ctx, `INSERT INTO messages(id,tenant_id,mailbox_id,zone_id,sender,recipients,raw_object_key) VALUES($1,$2,$3,$4,'private foreign sender',ARRAY['private recipient'],'foreign-source')`, foreignMessage, foreign.ID, foreignBox, zone.ID)
	must(t, e)
	_, e = f.pool.Exec(ctx, `UPDATE mail_index_jobs SET state='failed',attempts=5`)
	must(t, e)
	n, e := f.st.RetryFailedMailIndex(ctx, f.a, "First bounded storage recovery batch")
	must(t, e)
	if n != 100 {
		t.Fatal("unbounded recovery", n)
	}
	var foreignState string
	must(t, f.pool.QueryRow(ctx, `SELECT state FROM mail_index_jobs WHERE message_id=$1`, foreignMessage).Scan(&foreignState))
	if foreignState != "failed" {
		t.Fatal("cross-tenant recovery")
	}
	// A required outbox failure must roll back every retried job and audit.
	var before int
	must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action='company.index_retry'`).Scan(&before))
	_, e = f.pool.Exec(ctx, `ALTER TABLE outbox_events ADD CONSTRAINT reject_index_recovery CHECK(event_type<>'company.admin.changed') NOT VALID`)
	must(t, e)
	if n, e = f.st.RetryFailedMailIndex(ctx, f.a, "Injected outbox failure must roll back"); e == nil || n != 0 {
		t.Fatal("false successful recovery", n, e)
	}
	var pending, after int
	must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM mail_index_jobs WHERE tenant_id=$1 AND state='pending'`, f.tenant.ID).Scan(&pending))
	must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action='company.index_retry'`).Scan(&after))
	if pending != 100 || before != after {
		t.Fatal("partial recovery committed", pending, before, after)
	}
}

func TestArchitectureIndexRecoveryHTTPRequiresAdministratorAndReason(t *testing.T) {
	f := seedCompany(t)
	h := companyRouter(t, f, testutil.NewMemoryObjectStore(), nil)
	endpoint := "/api/v1/company/index/retry"
	r3HTTP(t, h, r3Token(t, f.employee), "POST", endpoint, map[string]any{"reason": "Employee cannot run recovery"}, 403)
	r3HTTP(t, h, r3Token(t, f.admin), "POST", endpoint, map[string]any{"reason": ""}, 400)
	r3HTTP(t, h, r3Token(t, f.admin), "POST", endpoint, map[string]any{"reason": "No failed jobs remains a valid audited operation"}, 200)
}
