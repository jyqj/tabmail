package postgres_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/config"
	"tabmail/internal/ingest"
	"tabmail/internal/models"
	"tabmail/internal/policy"
	"tabmail/internal/resolver"
	"tabmail/internal/testpg"
	"tabmail/internal/testutil"
)

func TestIngressInspectionAndReviewedRetry(t *testing.T) {
	st, pool, _ := testpg.NewPostgres(t)
	ctx := context.Background()
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	tenant := &models.Tenant{Name: "Inspection", PlanID: uuid.MustParse("00000000-0000-0000-0000-000000000002")}
	check(st.CreateTenant(ctx, tenant))
	zone := &models.DomainZone{TenantID: tenant.ID, Domain: "inspection.test", IsVerified: true, MXVerified: true}
	check(st.CreateZone(ctx, zone))
	box := &models.Mailbox{TenantID: tenant.ID, ZoneID: zone.ID, LocalPart: "alice", ResolvedDomain: zone.Domain, FullAddress: "alice@inspection.test", AccessMode: models.AccessAPIKey}
	check(st.CreateMailbox(ctx, box))
	svc := ingest.NewService(st, testutil.NewMemoryObjectStore(), resolver.New(st, policy.NamingFull, false), nil, nil, models.SMTPPolicy{DefaultAccept: true, DefaultStore: true}, 24, nil, config.Ingest{Durable: true}, zerolog.Nop())
	_, err := svc.Accept(ctx, ingest.Envelope{Source: "smtp", Recipients: []string{box.FullAddress}}, []byte("Subject: test\r\n\r\nbody"), nil)
	check(err)
	claim, err := st.ClaimIngress(ctx)
	check(err)
	id := claim.Job.ID
	snapshot, err := st.InspectIngress(ctx, id)
	check(err)
	if snapshot.CanRetry || snapshot.RetryBlockReason != "not_held" || len(snapshot.Targets) != 1 || snapshot.ExpectedBytes == nil {
		t.Fatalf("bad processing snapshot %+v", snapshot)
	}
	check(st.FailIngressTarget(ctx, claim, box.ID, "storage unavailable"))
	check(st.FinishIngress(ctx, claim, 1, time.Now()))
	snapshot, err = st.InspectIngress(ctx, id)
	check(err)
	if !snapshot.CanRetry || snapshot.Targets[0].State != "held" {
		t.Fatalf("bad held snapshot %+v", snapshot)
	}
	encoded, err := json.Marshal(snapshot)
	check(err)
	for _, forbidden := range []string{"raw_object_key", "raw_sha256", "claim_token", "password"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("inspection leaks %s", forbidden)
		}
	}

	// Two reviewers read the same snapshot; only one may mutate it.
	var wg sync.WaitGroup
	outcomes := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			outcomes <- st.RetryReviewedIngress(ctx, id, "test-operator", "repair confirmed", snapshot.UpdatedAt)
		}()
	}
	wg.Wait()
	close(outcomes)
	success, denied := 0, 0
	for err := range outcomes {
		if err == nil {
			success++
		} else {
			denied++
		}
	}
	if success != 1 || denied != 1 {
		t.Fatalf("retry race %d/%d", success, denied)
	}
	var audit int
	check(pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action='ingest.retry'`).Scan(&audit))
	if audit != 1 {
		t.Fatalf("audit count %d", audit)
	}

	// Even if a new worker holds the receipt again, the old review is obsolete.
	claim, err = st.ClaimIngress(ctx)
	check(err)
	check(st.FinishIngress(ctx, claim, 1, time.Now()))
	if err = st.RetryReviewedIngress(ctx, id, "test-operator", "old review", snapshot.UpdatedAt); err == nil {
		t.Fatal("stale review replayed a new recovery cycle")
	}
	current, err := st.InspectIngress(ctx, id)
	check(err)
	if !current.CanRetry {
		t.Fatal("new held cycle missing")
	}
	_, err = pool.Exec(ctx, `CREATE FUNCTION fail_review_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='ingest.retry' THEN RAISE EXCEPTION 'audit unavailable'; END IF; RETURN NEW; END $$; CREATE TRIGGER review_audit_fault BEFORE INSERT ON audit_log FOR EACH ROW EXECUTE FUNCTION fail_review_audit()`)
	check(err)
	if err = st.RetryReviewedIngress(ctx, id, "test-operator", "new review", current.UpdatedAt); err == nil {
		t.Fatal("unaudited review accepted")
	}
	after, err := st.InspectIngress(ctx, id)
	check(err)
	if !after.CanRetry || !after.UpdatedAt.Equal(current.UpdatedAt) {
		t.Fatal("failed audit changed the inspected revision")
	}
	_, err = pool.Exec(ctx, `DROP TRIGGER review_audit_fault ON audit_log`)
	check(err)
	check(st.RetryReviewedIngress(ctx, id, "test-operator", "repaired audit", current.UpdatedAt))

	legacy := &models.IngestJob{ID: uuid.New(), Source: "smtp", Recipients: []string{box.FullAddress}, RawObjectKey: "legacy.eml", State: "dead"}
	check(st.CreateIngestJob(ctx, legacy))
	old, err := st.InspectIngress(ctx, legacy.ID)
	check(err)
	if old.CanRetry || old.RetryBlockReason != "legacy_receipt" || len(old.Targets) != 0 || old.ExpectedBytes != nil {
		t.Fatalf("legacy affordance %+v", old)
	}
	if _, err = st.InspectIngress(ctx, uuid.New()); err == nil {
		t.Fatal("unknown receipt accepted")
	}
	if err = st.RetryReviewedIngress(ctx, id, "operator", "reason", time.Time{}); err == nil {
		t.Fatal("empty revision accepted")
	}

	// Coherent snapshots across concurrent atomic job/target transitions.
	// A reader must never observe state=dead with pending targets or vice versa.
	failures := make(chan error, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 30; i++ {
			state, target := "dead", "held"
			if i%2 == 0 {
				state, target = "retry", "pending"
			}
			tx, e := pool.Begin(ctx)
			if e != nil {
				failures <- e
				return
			}
			_, e = tx.Exec(ctx, `UPDATE ingest_jobs SET state=$2 WHERE id=$1`, id, state)
			if e == nil {
				_, e = tx.Exec(ctx, `UPDATE ingress_targets SET state=$2 WHERE job_id=$1`, id, target)
			}
			if e == nil {
				e = tx.Commit(ctx)
			} else {
				_ = tx.Rollback(ctx)
			}
			if e != nil {
				failures <- e
				return
			}
		}
	}()
	for i := 0; i < 30; i++ {
		v, e := st.InspectIngress(ctx, id)
		check(e)
		if (v.State == "dead") != (v.Targets[0].State == "held") {
			t.Fatalf("torn snapshot %+v", v)
		}
	}
	wg.Wait()
	close(failures)
	for e := range failures {
		check(e)
	}
}
