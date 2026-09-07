package ingest

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"tabmail/internal/config"
	"tabmail/internal/models"
	"tabmail/internal/policy"
	"tabmail/internal/resolver"
	"tabmail/internal/store"
	"tabmail/internal/store/postgres"
	"tabmail/internal/testpg"
	"tabmail/internal/testutil"
)

type recoveryFixture struct {
	st     *postgres.PgStore
	pool   *pgxpool.Pool
	obj    *testutil.MemoryObjectStore
	svc    *Service
	tenant *models.Tenant
	boxes  []*models.Mailbox
	dsn    string
}

func newRecoveryFixture(t *testing.T) *recoveryFixture {
	t.Helper()
	st, pool, dsn := testpg.NewPostgres(t)
	ctx := context.Background()
	tenant := &models.Tenant{Name: "Inbound", PlanID: uuid.MustParse("00000000-0000-0000-0000-000000000002")}
	check(t, st.CreateTenant(ctx, tenant))
	zone := &models.DomainZone{TenantID: tenant.ID, Domain: "company.test", IsVerified: true, MXVerified: true}
	check(t, st.CreateZone(ctx, zone))
	boxes := []*models.Mailbox{}
	for _, local := range []string{"alice", "bob"} {
		m := &models.Mailbox{TenantID: tenant.ID, ZoneID: zone.ID, LocalPart: local, ResolvedDomain: zone.Domain, FullAddress: local + "@" + zone.Domain, AccessMode: models.AccessAPIKey}
		check(t, st.CreateMailbox(ctx, m))
		boxes = append(boxes, m)
	}
	obj := testutil.NewMemoryObjectStore()
	svc := NewService(st, obj, resolver.New(st, policy.NamingFull, false), nil, nil, models.SMTPPolicy{DefaultAccept: true, DefaultStore: true}, 24, nil, config.Ingest{Durable: true, BatchSize: 10, MaxRetries: 2}, zerolog.Nop())
	return &recoveryFixture{st, pool, obj, svc, tenant, boxes, dsn}
}
func check(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func (f *recoveryFixture) accept(t *testing.T, boxes ...*models.Mailbox) *models.IngestJob {
	t.Helper()
	addresses := []string{}
	for _, m := range boxes {
		addresses = append(addresses, m.FullAddress)
	}
	res, err := f.svc.Accept(context.Background(), Envelope{Source: "smtp", MailFrom: "sender@example.test", Recipients: addresses}, []byte("Subject: recovery\r\n\r\nprivate body"), nil)
	check(t, err)
	if !res.Queued {
		t.Fatal("not acknowledged")
	}
	jobs, _, err := f.st.ListIngestJobs(context.Background(), models.Page{Page: 1, PerPage: 100}, "pending", "", "")
	check(t, err)
	if len(jobs) == 0 {
		t.Fatal("no receipt")
	}
	return jobs[0]
}
func (f *recoveryFixture) state(t *testing.T, id uuid.UUID) string {
	t.Helper()
	var state string
	check(t, f.pool.QueryRow(context.Background(), `SELECT state FROM ingest_jobs WHERE id=$1`, id).Scan(&state))
	return state
}
func (f *recoveryFixture) ready(t *testing.T, id uuid.UUID) {
	t.Helper()
	_, err := f.pool.Exec(context.Background(), `UPDATE ingest_jobs SET next_attempt_at=now()-interval '1 second' WHERE id=$1`, id)
	check(t, err)
}
func (f *recoveryFixture) count(t *testing.T, table string) int {
	t.Helper()
	var n int
	check(t, f.pool.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&n))
	return n
}

func TestIngressPartialFailureRecoversWithoutDuplicate(t *testing.T) {
	f := newRecoveryFixture(t)
	ctx := context.Background()
	j := f.accept(t, f.boxes...)
	// PostgreSQL failure occurs AFTER mailbox/quota updates inside the transaction.
	_, err := f.pool.Exec(ctx, `CREATE FUNCTION reject_bob() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NEW.recipients[1]='bob@company.test' THEN RAISE EXCEPTION 'injected metadata storage failure'; END IF; RETURN NEW; END $$;
 CREATE TRIGGER injected_failure BEFORE INSERT ON messages FOR EACH ROW EXECUTE FUNCTION reject_bob()`)
	check(t, err)
	check(t, f.svc.processBatch(ctx))
	if state := f.state(t, j.ID); state != "retry" {
		t.Fatalf("state %s", state)
	}
	if n := f.count(t, "messages"); n != 1 {
		t.Fatalf("messages=%d", n)
	}
	targets, err := f.st.ListIngressTargets(ctx, j.ID)
	check(t, err)
	if targets[0].State != "delivered" || targets[1].State != "pending" || targets[1].LastError == "" {
		t.Fatalf("%+v", targets)
	}
	var used int
	check(t, f.pool.QueryRow(ctx, `SELECT used FROM ingress_daily_usage WHERE tenant_id=$1`, f.tenant.ID).Scan(&used))
	if used != 1 {
		t.Fatalf("quota leaked %d", used)
	}
	_, err = f.pool.Exec(ctx, `DROP TRIGGER injected_failure ON messages`)
	check(t, err)
	f.ready(t, j.ID)
	check(t, f.svc.processBatch(ctx))
	if f.state(t, j.ID) != "done" {
		t.Fatal("not done")
	}
	for _, table := range []string{"messages", "outbox_events"} {
		if n := f.count(t, table); n != 2 {
			t.Fatalf("%s=%d", table, n)
		}
	}
	targets, err = f.st.ListIngressTargets(ctx, j.ID)
	check(t, err)
	if targets[0].Attempts != 1 || targets[1].Attempts != 2 {
		t.Fatalf("replayed successful recipient: %+v", targets)
	}
	if exists, err := f.obj.Exists(ctx, j.RawObjectKey); err != nil || !exists {
		t.Fatal("lost raw")
	}
}

func TestIngressHeldBytesSurviveRetentionAndAuditedRetry(t *testing.T) {
	f := newRecoveryFixture(t)
	ctx := context.Background()
	j := f.accept(t, f.boxes[0])
	f.svc.maxRetries = 1
	// Missing configuration / changed policy is not permission to discard DATA.
	f.svc.defaultPolicy.DefaultStore = false
	check(t, f.svc.processBatch(ctx))
	if f.state(t, j.ID) != "dead" {
		t.Fatal("not held")
	}
	_, err := f.pool.Exec(ctx, `UPDATE ingest_jobs SET updated_at=now()-interval '30 days' WHERE id=$1`, j.ID)
	check(t, err)
	n, _, err := f.st.PurgeOldIngestJobs(ctx, time.Now().Add(-7*24*time.Hour), 100)
	check(t, err)
	if n != 0 {
		t.Fatal("held receipt purged")
	}
	refs, err := f.st.CountRawObjectReferences(ctx, j.RawObjectKey)
	check(t, err)
	if refs == 0 {
		t.Fatal("held bytes unpinned")
	}
	check(t, f.st.RetryIngress(ctx, j.ID, "user:platform-admin", "storage policy repaired"))
	f.svc.defaultPolicy.DefaultStore = true
	f.svc.policyExpiresAt = time.Time{}
	check(t, f.svc.processBatch(ctx))
	if f.state(t, j.ID) != "done" {
		t.Fatal("retry did not deliver")
	}
	if err = f.st.RetryIngress(ctx, j.ID, "admin", "try again"); err == nil {
		t.Fatal("completed receipt replay allowed")
	}
	if f.count(t, "messages") != 1 {
		t.Fatal("duplicate")
	}
}

func TestIngressExpiredWorkerCannotCommitOrFinalize(t *testing.T) {
	f := newRecoveryFixture(t)
	ctx := context.Background()
	j := f.accept(t, f.boxes[0])
	old, err := f.st.ClaimIngress(ctx)
	check(t, err)
	_, err = f.pool.Exec(ctx, `UPDATE ingest_jobs SET lease_until=now()-interval '1 second' WHERE id=$1`, j.ID)
	check(t, err)
	fresh, err := f.st.ClaimIngress(ctx)
	check(t, err)
	if fresh.Token == old.Token {
		t.Fatal("claim not fenced")
	}
	if err = f.st.FinishIngress(ctx, old, 2, time.Now()); !errors.Is(err, store.ErrIngressClaim) {
		t.Fatalf("old finalize: %v", err)
	}
	if err = f.st.FailIngressTarget(ctx, old, f.boxes[0].ID, "stale"); !errors.Is(err, store.ErrIngressClaim) {
		t.Fatalf("old failure: %v", err)
	}
	check(t, f.svc.processReceipt(ctx, f.st, fresh))
	if f.state(t, j.ID) != "done" || f.count(t, "messages") != 1 {
		t.Fatal("recovery failed")
	}
}

func TestIngressDeliveredTombstoneSurvivesMessageDeletion(t *testing.T) {
	f := newRecoveryFixture(t)
	ctx := context.Background()
	j := f.accept(t, f.boxes[0])
	c, err := f.st.ClaimIngress(ctx)
	check(t, err)
	targets, err := f.st.ListIngressTargets(ctx, j.ID)
	check(t, err)
	check(t, f.svc.deliverTarget(ctx, f.st, c, targets[0], []byte("body"), "subject", nil))
	targets, err = f.st.ListIngressTargets(ctx, j.ID)
	check(t, err)
	check(t, f.st.DeleteMessage(ctx, *targets[0].MessageID))
	// Simulate crash after successful local commit but before receipt completion.
	_, err = f.pool.Exec(ctx, `UPDATE ingest_jobs SET lease_until=now()-interval '1 second' WHERE id=$1`, j.ID)
	check(t, err)
	check(t, f.svc.processBatch(ctx))
	if f.count(t, "messages") != 0 {
		t.Fatal("deleted message resurrected")
	}
	if f.count(t, "outbox_events") != 1 {
		t.Fatal("duplicate event")
	}
	if f.state(t, j.ID) != "done" {
		t.Fatal("not completed")
	}
}

func TestIngressDestinationReuseIsNotRerouted(t *testing.T) {
	f := newRecoveryFixture(t)
	ctx := context.Background()
	j := f.accept(t, f.boxes[0])
	old := f.boxes[0]
	check(t, f.st.DeleteMailbox(ctx, old.ID))
	replacement := *old
	replacement.ID = uuid.New()
	check(t, f.st.CreateMailbox(ctx, &replacement))
	f.svc.maxRetries = 1
	check(t, f.svc.processBatch(ctx))
	if f.state(t, j.ID) != "dead" || f.count(t, "messages") != 0 {
		t.Fatal("mail delivered into replacement identity")
	}
}

func TestIngressConcurrentClaimsAndQuota(t *testing.T) {
	f := newRecoveryFixture(t)
	ctx := context.Background()
	_, err := f.pool.Exec(ctx, `INSERT INTO tenant_overrides(tenant_id,daily_quota) VALUES($1,1)`, f.tenant.ID)
	check(t, err)
	for i := 0; i < 4; i++ {
		f.accept(t, f.boxes[i%2])
	}
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- f.svc.processBatch(ctx) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		check(t, err)
	}
	if f.count(t, "messages") != 1 || f.count(t, "outbox_events") != 1 {
		t.Fatal("concurrent quota exceeded")
	}
}

func TestIngressMissingRawRetainedForReview(t *testing.T) {
	f := newRecoveryFixture(t)
	ctx := context.Background()
	j := f.accept(t, f.boxes...)
	check(t, f.obj.Delete(ctx, j.RawObjectKey))
	f.svc.maxRetries = 1
	check(t, f.svc.processBatch(ctx))
	if f.state(t, j.ID) != "dead" {
		t.Fatal("missing object was completed")
	}
	targets, err := f.st.ListIngressTargets(ctx, j.ID)
	check(t, err)
	for _, target := range targets {
		if target.State != "held" || target.LastError == "" {
			t.Fatal("missing failure evidence")
		}
	}
	// Restoring raw bytes then explicitly retrying recovers the same receipt.
	check(t, f.obj.Put(ctx, j.RawObjectKey, bytes.NewBufferString("Subject: recovery\r\n\r\nprivate body"), 0))
	check(t, f.st.RetryIngress(ctx, j.ID, "admin", "restored original object"))
	check(t, f.svc.processBatch(ctx))
	if f.count(t, "messages") != 2 {
		t.Fatal("restored receipt not recovered")
	}
}

func TestIngressAuditFailureRollsBackAndCorruptionIsHeld(t *testing.T) {
	f := newRecoveryFixture(t)
	ctx := context.Background()
	j := f.accept(t, f.boxes[0])
	_, err := f.pool.Exec(ctx, `CREATE FUNCTION reject_inbound_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NEW.action='message.received' THEN RAISE EXCEPTION 'injected audit failure'; END IF; RETURN NEW; END $$;
 CREATE TRIGGER audit_fault BEFORE INSERT ON audit_log FOR EACH ROW EXECUTE FUNCTION reject_inbound_audit()`)
	check(t, err)
	check(t, f.svc.processBatch(ctx))
	if f.count(t, "messages") != 0 || f.count(t, "outbox_events") != 0 || f.count(t, "ingress_daily_usage") != 0 {
		t.Fatal("audit rollback not atomic")
	}
	_, err = f.pool.Exec(ctx, `DROP TRIGGER audit_fault ON audit_log`)
	check(t, err)
	// A read with the wrong bytes is also a recoverable error, never silently delivered.
	check(t, f.obj.Put(ctx, j.RawObjectKey, bytes.NewBufferString("corrupted body"), 0))
	f.ready(t, j.ID)
	check(t, f.svc.processBatch(ctx))
	if f.state(t, j.ID) != "dead" || f.count(t, "messages") != 0 {
		t.Fatal("corrupt spool accepted")
	}
	targets, err := f.st.ListIngressTargets(ctx, j.ID)
	check(t, err)
	if targets[0].LastError != "original object checksum mismatch; receipt held for recovery" {
		t.Fatalf("%+v", targets)
	}
}

func TestIngressLegacyQueuesCannotBypassLedger(t *testing.T) {
	f := newRecoveryFixture(t)
	ctx := context.Background()
	j := f.accept(t, f.boxes[0])
	legacy, err := f.st.ClaimIngestJobs(ctx, time.Now(), 100)
	check(t, err)
	if len(legacy) != 0 {
		t.Fatal("legacy worker claimed ledger receipt")
	}
	check(t, f.st.MarkIngestJobDone(ctx, j.ID))
	if f.state(t, j.ID) != "pending" {
		t.Fatal("legacy finalizer bypassed progress")
	}
	c, err := f.st.ClaimIngress(ctx)
	check(t, err)
	m := &models.Message{MailboxID: f.boxes[0].ID, TenantID: uuid.New(), ZoneID: f.boxes[0].ZoneID, RawObjectKey: j.RawObjectKey}
	if _, err = f.st.DeliverIngress(ctx, c, m, 100, 100); err == nil {
		t.Fatal("cross tenant progress accepted")
	}
	check(t, f.svc.processReceipt(ctx, f.st, c))
	if f.count(t, "messages") != 1 {
		t.Fatal("valid target not delivered")
	}
}

func TestIngressDuplicateRecipientAndDistinctSubmissionIdentity(t *testing.T) {
	f := newRecoveryFixture(t)
	ctx := context.Background()
	first := f.accept(t, f.boxes[0], f.boxes[0])
	second := f.accept(t, f.boxes[0])
	if first.RawObjectKey == second.RawObjectKey {
		t.Fatal("separate accepts shared a reclaimable raw key")
	}
	check(t, f.svc.processBatch(ctx))
	if f.count(t, "messages") != 2 {
		t.Fatal("same mailbox repeated or distinct receipt collapsed")
	}
	targets, err := f.st.ListIngressTargets(ctx, first.ID)
	check(t, err)
	if len(targets) != 1 {
		t.Fatal("not one local delivery per receipt/mailbox")
	}
	// Successful receipts can be retired normally, while each message pins its bytes.
	_, err = f.pool.Exec(ctx, `UPDATE ingest_jobs SET updated_at=now()-interval '30 days' WHERE state='done'`)
	check(t, err)
	n, _, err := f.st.PurgeOldIngestJobs(ctx, time.Now().Add(-7*24*time.Hour), 100)
	check(t, err)
	if n != 2 {
		t.Fatalf("done GC=%d", n)
	}
	refs, err := f.st.CountRawObjectReferences(ctx, first.RawObjectKey)
	check(t, err)
	if refs != 1 {
		t.Fatal("message raw reference lost after job cleanup")
	}
}
