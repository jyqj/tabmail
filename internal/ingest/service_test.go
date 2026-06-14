package ingest

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/config"
	"tabmail/internal/hooks"
	"tabmail/internal/models"
	"tabmail/internal/policy"
	"tabmail/internal/rawobject"
	"tabmail/internal/realtime"
	"tabmail/internal/resolver"
	"tabmail/internal/testutil"
)

func TestServiceDurableAcceptAndProcess(t *testing.T) {
	st := testutil.NewFakeStore()
	obj := testutil.NewMemoryObjectStore()

	planID := uuid.New()
	tenantID := uuid.New()
	zoneID := uuid.New()
	st.SeedPlan(&models.Plan{
		ID:                    planID,
		Name:                  "test",
		MaxDomains:            10,
		MaxMailboxesPerDomain: 100,
		MaxMessagesPerMailbox: 1000,
		MaxMessageBytes:       1024 * 1024,
		RetentionHours:        24,
		RPMLimit:              1000,
		DailyQuota:            1000,
	})
	st.SeedTenant(&models.Tenant{ID: tenantID, Name: "tenant-a", PlanID: planID})
	st.SeedZone(&models.DomainZone{
		ID:         zoneID,
		TenantID:   tenantID,
		Domain:     "mail.test",
		IsVerified: true,
		MXVerified: true,
		TXTRecord:  "tabmail-verify=test",
	})
	st.SeedRoute(&models.DomainRoute{
		ID:                uuid.New(),
		ZoneID:            zoneID,
		RouteType:         models.RouteExact,
		MatchValue:        "mail.test",
		AutoCreateMailbox: true,
		AccessModeDefault: models.AccessPublic,
	})

	resolverSvc := resolver.New(st, policy.NamingFull, true)
	svc := NewService(
		st,
		obj,
		resolverSvc,
		realtime.NewHub(10, st),
		hooks.New(hooks.Config{}, zerolog.Nop()),
		models.SMTPPolicy{DefaultAccept: true, DefaultStore: true},
		24,
		nil,
		config.Ingest{Durable: true, BatchSize: 10},
		zerolog.Nop(),
	)

	res, err := svc.Accept(context.Background(), Envelope{
		Source:     "smtp",
		MailFrom:   "sender@example.org",
		Recipients: []string{"user@mail.test"},
	}, []byte("Subject: durable\r\n\r\nhello"))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Queued {
		t.Fatalf("expected durable accept to queue, got %#v", res)
	}

	mb, err := st.GetMailboxByAddress(context.Background(), "user@mail.test")
	if err != nil {
		t.Fatal(err)
	}
	if mb != nil {
		t.Fatalf("expected mailbox not materialized before worker, got %#v", mb)
	}

	if err := svc.processBatch(context.Background()); err != nil {
		t.Fatal(err)
	}

	mb, err = st.GetMailboxByAddress(context.Background(), "user@mail.test")
	if err != nil {
		t.Fatal(err)
	}
	if mb == nil {
		t.Fatal("expected mailbox after ingest processing")
	}
	msgs, total, err := st.ListMessages(context.Background(), mb.ID, models.Page{Page: 1, PerPage: 10})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(msgs) != 1 {
		t.Fatalf("expected 1 message after ingest processing, total=%d len=%d", total, len(msgs))
	}
	if msgs[0].Subject != "durable" {
		t.Fatalf("unexpected subject: %#v", msgs[0].Subject)
	}
	if msgs[0].ReceivedAt.Before(time.Now().Add(-time.Minute)) {
		t.Fatalf("unexpected received_at: %#v", msgs[0].ReceivedAt)
	}
	if obj.Count() != 1 {
		t.Fatalf("expected delivered message to retain raw object, got %d objects", obj.Count())
	}
	if ok, err := obj.Exists(context.Background(), msgs[0].RawObjectKey); err != nil || !ok {
		t.Fatalf("expected delivered raw object to exist, ok=%v err=%v", ok, err)
	}
}

func TestServiceDurableZeroDeliveryDeletesRawObject(t *testing.T) {
	st, obj, svc := newDurableCleanupService(t, 1024*1024, false)
	raw := []byte("Subject: route gone\r\n\r\nhello")
	key := rawobject.Key(raw)

	res, err := svc.Accept(context.Background(), Envelope{
		Source:     "smtp",
		MailFrom:   "sender@example.org",
		Recipients: []string{"user@mail.test"},
	}, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Queued {
		t.Fatalf("expected durable accept to queue, got %#v", res)
	}
	if ok, err := obj.Exists(context.Background(), key); err != nil || !ok {
		t.Fatalf("expected raw object queued before worker, ok=%v err=%v", ok, err)
	}

	if err := svc.processBatch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if ok, err := obj.Exists(context.Background(), key); err != nil || ok {
		t.Fatalf("expected zero-delivery raw object to be deleted, ok=%v err=%v", ok, err)
	}
	jobs, total, err := st.ListIngestJobs(context.Background(), models.Page{Page: 1, PerPage: 10}, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(jobs) != 1 || jobs[0].State != "done" {
		t.Fatalf("expected completed ingest job after zero-delivery cleanup, total=%d jobs=%#v", total, jobs)
	}
	refs, err := st.CountRawObjectReferences(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	if refs != 0 {
		t.Fatalf("expected no raw references after zero-delivery cleanup, got %d", refs)
	}
}

func TestServiceDurableSizeFailureDeletesRawObject(t *testing.T) {
	st, obj, svc := newDurableCleanupService(t, 8, true)
	raw := []byte("Subject: too big\r\n\r\nhello world")
	key := rawobject.Key(raw)

	res, err := svc.Accept(context.Background(), Envelope{
		Source:     "smtp",
		MailFrom:   "sender@example.org",
		Recipients: []string{"user@mail.test"},
	}, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Queued {
		t.Fatalf("expected durable accept to queue, got %#v", res)
	}

	if err := svc.processBatch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if ok, err := obj.Exists(context.Background(), key); err != nil || ok {
		t.Fatalf("expected size-failed raw object to be deleted, ok=%v err=%v", ok, err)
	}
	mb, err := st.GetMailboxByAddress(context.Background(), "user@mail.test")
	if err != nil {
		t.Fatal(err)
	}
	if mb != nil {
		_, total, err := st.ListMessages(context.Background(), mb.ID, models.Page{Page: 1, PerPage: 10})
		if err != nil {
			t.Fatal(err)
		}
		if total != 0 {
			t.Fatalf("expected no message after size failure, got %d", total)
		}
	}
}

func newDurableCleanupService(t *testing.T, maxMessageBytes int, seedRoute bool) (*testutil.FakeStore, *testutil.MemoryObjectStore, *Service) {
	t.Helper()
	st := testutil.NewFakeStore()
	obj := testutil.NewMemoryObjectStore()

	planID := uuid.New()
	tenantID := uuid.New()
	zoneID := uuid.New()
	st.SeedPlan(&models.Plan{
		ID:                    planID,
		Name:                  "cleanup-test",
		MaxDomains:            10,
		MaxMailboxesPerDomain: 100,
		MaxMessagesPerMailbox: 1000,
		MaxMessageBytes:       maxMessageBytes,
		RetentionHours:        24,
		RPMLimit:              1000,
		DailyQuota:            1000,
	})
	st.SeedTenant(&models.Tenant{ID: tenantID, Name: "tenant-a", PlanID: planID})
	st.SeedZone(&models.DomainZone{
		ID:         zoneID,
		TenantID:   tenantID,
		Domain:     "mail.test",
		IsVerified: true,
		MXVerified: true,
		TXTRecord:  "tabmail-verify=test",
	})
	if seedRoute {
		st.SeedRoute(&models.DomainRoute{
			ID:                uuid.New(),
			ZoneID:            zoneID,
			RouteType:         models.RouteExact,
			MatchValue:        "mail.test",
			AutoCreateMailbox: true,
			AccessModeDefault: models.AccessPublic,
		})
	}

	resolverSvc := resolver.New(st, policy.NamingFull, true)
	return st, obj, NewService(
		st,
		obj,
		resolverSvc,
		realtime.NewHub(10, st),
		hooks.New(hooks.Config{}, zerolog.Nop()),
		models.SMTPPolicy{DefaultAccept: true, DefaultStore: true},
		24,
		nil,
		config.Ingest{Durable: true, BatchSize: 10},
		zerolog.Nop(),
	)
}

func TestDeliverReturnsPerRecipientOutcomes(t *testing.T) {
	st := testutil.NewFakeStore()
	obj := testutil.NewMemoryObjectStore()

	planID := uuid.New()
	tenantID := uuid.New()
	okZoneID := uuid.New()
	badZoneID := uuid.New()
	st.SeedPlan(&models.Plan{
		ID: planID, Name: "outcome-test", MaxDomains: 10, MaxMailboxesPerDomain: 100,
		MaxMessagesPerMailbox: 1000, MaxMessageBytes: 1024 * 1024, RetentionHours: 24,
		RPMLimit: 1000, DailyQuota: 1000,
	})
	st.SeedTenant(&models.Tenant{ID: tenantID, Name: "tenant-a", PlanID: planID})
	// Verified zone — recipients here are delivered.
	st.SeedZone(&models.DomainZone{ID: okZoneID, TenantID: tenantID, Domain: "mail.test", IsVerified: true, MXVerified: true})
	st.SeedRoute(&models.DomainRoute{ID: uuid.New(), ZoneID: okZoneID, RouteType: models.RouteExact, MatchValue: "mail.test", AutoCreateMailbox: true, AccessModeDefault: models.AccessPublic})
	// Unverified zone — recipients here are rejected as zone_unverified.
	st.SeedZone(&models.DomainZone{ID: badZoneID, TenantID: tenantID, Domain: "bad.test", IsVerified: false, MXVerified: false})
	st.SeedRoute(&models.DomainRoute{ID: uuid.New(), ZoneID: badZoneID, RouteType: models.RouteExact, MatchValue: "bad.test", AutoCreateMailbox: true, AccessModeDefault: models.AccessPublic})

	svc := NewService(
		st, obj, resolver.New(st, policy.NamingFull, true),
		realtime.NewHub(10, st), hooks.New(hooks.Config{}, zerolog.Nop()),
		models.SMTPPolicy{DefaultAccept: true, DefaultStore: true}, 24, nil,
		config.Ingest{Durable: false, BatchSize: 10}, zerolog.Nop(),
	)

	outcomes, err := svc.deliver(context.Background(), Envelope{
		Source:     "smtp",
		MailFrom:   "sender@example.org",
		Recipients: []string{"good@mail.test", "blocked@bad.test", "nobody@nowhere.test"},
	}, []byte("Subject: outcomes\r\n\r\nhello"))
	if err != nil {
		t.Fatal(err)
	}
	if len(outcomes) != 3 {
		t.Fatalf("expected one outcome per recipient, got %d: %#v", len(outcomes), outcomes)
	}

	byAddr := map[string]RecipientOutcome{}
	for _, o := range outcomes {
		byAddr[o.Address] = o
	}
	if got := byAddr["good@mail.test"]; got.Status != RecipientDelivered || got.MessageID == "" {
		t.Errorf("good@mail.test: want delivered with message id, got %#v", got)
	}
	if got := byAddr["blocked@bad.test"]; got.Status != RecipientRejected || got.Reason != "zone_unverified" {
		t.Errorf("blocked@bad.test: want rejected/zone_unverified, got %#v", got)
	}
	if got := byAddr["nobody@nowhere.test"]; got.Status != RecipientRejected || got.Reason != "no_route" {
		t.Errorf("nobody@nowhere.test: want rejected/no_route, got %#v", got)
	}
	if deliveredCount(outcomes) != 1 {
		t.Fatalf("expected 1 delivered, got %d", deliveredCount(outcomes))
	}
}

func TestRetryBackoffUsesPrecisePowersOfTwo(t *testing.T) {
	cases := []struct {
		attempts int
		min      time.Duration
		max      time.Duration
	}{
		{attempts: 1, min: 1 * time.Second, max: 2 * time.Second},
		{attempts: 2, min: 2 * time.Second, max: 3 * time.Second},
		{attempts: 3, min: 4 * time.Second, max: 5 * time.Second},
		{attempts: 4, min: 8 * time.Second, max: 9 * time.Second},
	}
	for _, tc := range cases {
		got := retryBackoff(tc.attempts)
		if got < tc.min || got >= tc.max {
			t.Fatalf("attempt=%d expected duration in [%s,%s), got %s", tc.attempts, tc.min, tc.max, got)
		}
	}
}
