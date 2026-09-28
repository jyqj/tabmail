package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/app"
	"tabmail/internal/company"
	"tabmail/internal/models"
	"tabmail/internal/store"
)

// These checks exercise the production ledger/transaction, not SMTP acceptance
// or object-store durability. Every fixture owns a fresh disposable database.
func r5IngressTarget(f *companyFixture, mailbox *models.Mailbox) store.IngressTarget {
	return store.IngressTarget{MailboxID: mailbox.ID, TenantID: f.tenant.ID, ZoneID: f.zone.ID, Address: mailbox.FullAddress}
}

func r5IngressJob(recipients ...string) *models.IngestJob {
	id := uuid.New()
	return &models.IngestJob{ID: id, Source: "smtp", RemoteIP: "127.0.0.1", MailFrom: "sender@fixture.invalid", Recipients: recipients, RawObjectKey: "ingress-" + id.String() + ".eml", Metadata: []byte(`{}`)}
}

func r5ClaimIngress(t *testing.T, f *companyFixture) (*store.IngressClaim, *models.Message) {
	t.Helper()
	ctx := context.Background()
	j := r5IngressJob(f.personal.FullAddress)
	must(t, f.st.CreateIngress(ctx, j, []store.IngressTarget{r5IngressTarget(f, f.personal)}, company.Hash("x"), 1))
	c, err := f.st.ClaimIngress(ctx)
	must(t, err)
	if c == nil || c.Job.ID != j.ID || c.Token == uuid.Nil {
		t.Fatal("fixture receipt was not claimed")
	}
	m := &models.Message{TenantID: f.tenant.ID, MailboxID: f.personal.ID, ZoneID: f.zone.ID, Sender: j.MailFrom, Recipients: j.Recipients, Subject: "transaction boundary fixture", RawObjectKey: j.RawObjectKey, Size: 1, HeadersJSON: []byte(`{}`)}
	return c, m
}

// The isolated fixture has no earlier messages. Check every side effect of the
// delivery transaction, including the trigger-created index job and event.
func r5IngressEffects(t *testing.T, f *companyFixture, c *store.IngressClaim, want int) {
	t.Helper()
	ctx := context.Background()
	var messages, quota, mailboxCount, audits, outbox, indexes int
	must(t, f.pool.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM messages WHERE tenant_id=$1),
 (SELECT COALESCE(sum(used),0) FROM ingress_daily_usage WHERE tenant_id=$1),
 (SELECT message_count FROM mailboxes WHERE id=$2),
 (SELECT count(*) FROM audit_log WHERE tenant_id=$1 AND action='message.received'),
 (SELECT count(*) FROM outbox_events WHERE event_type='message.received'),
 (SELECT count(*) FROM mail_index_jobs WHERE tenant_id=$1)`, f.tenant.ID, f.personal.ID).Scan(&messages, &quota, &mailboxCount, &audits, &outbox, &indexes))
	for name, got := range map[string]int{"messages": messages, "quota": quota, "mailbox_count": mailboxCount, "audit": audits, "outbox": outbox, "index_jobs": indexes} {
		if got != want {
			t.Fatalf("delivery transaction %s=%d, want %d", name, got, want)
		}
	}
	targets, err := f.st.ListIngressTargets(ctx, c.Job.ID)
	must(t, err)
	if len(targets) != 1 {
		t.Fatal("fixed destination ledger changed")
	}
	target := targets[0]
	if want == 0 && (target.State != "pending" || target.MessageID != nil || target.Attempts != 0) {
		t.Fatal("rolled-back delivery left recipient progress")
	}
	if want == 1 && (target.State != "delivered" || target.MessageID == nil || target.Attempts != 1) {
		t.Fatal("successful delivery lost or duplicated its progress tombstone")
	}
	refs, err := f.st.CountRawObjectReferences(ctx, c.Job.RawObjectKey)
	must(t, err)
	if refs < 1 {
		t.Fatal("receipt lost its raw-object retention reference")
	}
}

func TestR5IngressLeaseExpiryAfterTenantWaitRollsBack(t *testing.T) {
	f := seedCompany(t)
	c, m := r5ClaimIngress(t, f)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	var deadline time.Time
	must(t, f.pool.QueryRow(ctx, `UPDATE ingest_jobs SET lease_until=clock_timestamp()+interval '2 seconds' WHERE id=$1 RETURNING lease_until`, c.Job.ID).Scan(&deadline))
	hold, err := f.pool.Begin(ctx)
	must(t, err)
	defer hold.Rollback(context.Background())
	_, err = hold.Exec(ctx, `SELECT id FROM tenants WHERE id=$1 FOR UPDATE`, f.tenant.ID)
	must(t, err)
	type result struct {
		delivered bool
		err       error
	}
	done := make(chan result, 1)
	go func() { ok, e := f.st.DeliverIngress(ctx, c, m, 100, 100); done <- result{ok, e} }()
	writer := r5WaitBlockedBy(t, f, ctx, hold.Conn().PgConn().PID(), "SELECT id FROM tenants")
	var startedBefore bool
	must(t, f.pool.QueryRow(ctx, `SELECT xact_start<$2 FROM pg_stat_activity WHERE datname=current_database() AND pid=$1`, int32(writer), deadline).Scan(&startedBefore))
	if !startedBefore {
		t.Fatal("fixture did not enter delivery before the lease deadline")
	}
	// Observe the database clock; the controller never changes a row after the
	// worker starts. This specifically checks revalidation after lock waiting.
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var expired bool
		must(t, f.pool.QueryRow(ctx, `SELECT clock_timestamp()>=$1::timestamptz`, deadline).Scan(&expired))
		if expired {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("database clock did not cross lease deadline")
		case <-ticker.C:
		}
	}
	must(t, hold.Rollback(ctx))
	select {
	case r := <-done:
		if r.delivered || !errors.Is(r.err, store.ErrIngressClaim) {
			t.Fatalf("expired worker must return the lease error without delivery: %v %v", r.delivered, r.err)
		}
	case <-ctx.Done():
		t.Fatal("delivery did not finish after releasing the tenant")
	}
	r5IngressEffects(t, f, c, 0)
	fresh, err := f.st.ClaimIngress(ctx)
	must(t, err)
	if fresh == nil || fresh.Token == c.Token {
		t.Fatal("expired receipt was not reclaimable with a new token")
	}
	ok, err := f.st.DeliverIngress(ctx, fresh, m, 100, 100)
	must(t, err)
	if !ok {
		t.Fatal("fresh worker did not deliver")
	}
	ok, err = f.st.DeliverIngress(ctx, fresh, m, 100, 100)
	must(t, err)
	if ok {
		t.Fatal("delivery replay inserted a second message")
	}
	r5IngressEffects(t, f, fresh, 1)
	t.Log("expired waiting worker rolled back quota/message/progress/audit/outbox/index; new token delivered exactly once")
}

func TestR5IngressAuditFailureRollsBackCheckpointAndQuota(t *testing.T) {
	f := seedCompany(t)
	c, m := r5ClaimIngress(t, f)
	ctx := context.Background()
	_, err := f.pool.Exec(ctx, `ALTER TABLE audit_log ADD CONSTRAINT r5_reject_received_audit CHECK(action<>'message.received') NOT VALID`)
	must(t, err)
	ok, err := f.st.DeliverIngress(ctx, c, m, 100, 100)
	if ok || r5SQLState(err) != "23514" {
		t.Fatalf("expected injected audit constraint failure, got %v %v", ok, err)
	}
	r5IngressEffects(t, f, c, 0)
	_, err = f.pool.Exec(ctx, `ALTER TABLE audit_log DROP CONSTRAINT r5_reject_received_audit`)
	must(t, err)
	ok, err = f.st.DeliverIngress(ctx, c, m, 100, 100)
	must(t, err)
	if !ok {
		t.Fatal("delivery could not recover after removing the audit fault")
	}
	r5IngressEffects(t, f, c, 1)
	t.Log("mandatory audit failure rolled back prior message, progress and quota writes; same valid claim recovered")
}

func TestR5IngressAcceptanceRollsBackEarlierTarget(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()
	j := r5IngressJob(f.personal.FullAddress, f.shared.FullAddress)
	targets := []store.IngressTarget{r5IngressTarget(f, f.personal), r5IngressTarget(f, f.shared)}
	targets[1].Address = "changed@fixture.invalid"
	err := f.st.CreateIngress(ctx, j, targets, company.Hash("x"), 1)
	v, ok := app.As(err)
	if !ok || v.Kind != app.KindBadRequest {
		t.Fatalf("changed target must reject acceptance: %v", err)
	}
	var jobs, outcomes int
	must(t, f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM ingest_jobs WHERE id=$1),(SELECT count(*) FROM ingest_recipient_outcomes WHERE job_id=$1)`, j.ID).Scan(&jobs, &outcomes))
	if jobs != 0 || outcomes != 0 {
		t.Fatal("later target rejection left a partial acceptance ledger")
	}
	targets[1] = r5IngressTarget(f, f.shared)
	must(t, f.st.CreateIngress(ctx, j, targets, company.Hash("x"), 1))
	stored, err := f.st.ListIngressTargets(ctx, j.ID)
	must(t, err)
	if len(stored) != 2 {
		t.Fatal("corrected acceptance did not retain both original mailbox IDs")
	}
	ids := map[uuid.UUID]bool{}
	for _, target := range stored {
		ids[target.MailboxID] = true
	}
	if !ids[f.personal.ID] || !ids[f.shared.ID] {
		t.Fatal("fixed destination identities changed")
	}
	t.Log("later target validation failure rolled back the receipt and earlier target; valid acceptance retained both IDs")
}
