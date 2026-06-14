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

	svc.ProcessBatch(context.Background())

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

	svc.ProcessBatch(context.Background())
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

	svc.ProcessBatch(context.Background())
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

// retryBackoff's exponential-with-jitter formula is now asserted table-driven
// in internal/workqueue (TestExponentialBackoff_IngestFormula), which drives
// the same ExponentialBackoff policy ingest's worker uses. The legacy
// TestRetryBackoffUsesPrecisePowersOfTwo lived here only to lock the local
// retryBackoff free function, which the refactor deleted.

func intPtr(v int) *int { return &v }

// TestResolveRetentionPureFunction locks the mailbox > route > tenant > fallback
// precedence without touching the store. Pre-P5 this read EffectiveConfig from
// the store; now it consumes the tenantConfigs cache value the caller supplies,
// so a single delivery no longer triggers a second EffectiveConfig round-trip.
// Each level is *int (nil = unset). The "non-nil tenant 0" case pins the legacy
// behavior: a configured retention of 0 (immediate expiry) is honored, not
// skipped to fallback — matching the pre-P5 EffectiveConfig semantics.
func TestResolveRetentionPureFunction(t *testing.T) {
	cases := []struct {
		name                          string
		mailbox, route, tenant        *int
		fallback, want                int
	}{
		{"mailbox wins over everything", intPtr(99), intPtr(48), intPtr(24), 12, 99},
		{"route wins when mailbox unset", nil, intPtr(48), intPtr(24), 12, 48},
		{"tenant wins when mailbox+route unset", nil, nil, intPtr(24), 12, 24},
		{"explicit fallback when all unset", nil, nil, nil, 72, 72},
		{"default 24 when nothing set at all", nil, nil, nil, 0, 24},
		{"non-nil tenant 0 honored as immediate expiry (legacy)", nil, nil, intPtr(0), 72, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveRetention(tc.mailbox, tc.route, tc.tenant, tc.fallback); got != tc.want {
				t.Fatalf("resolveRetention(%v,%v,%v,%d) = %d, want %d",
					tc.mailbox, tc.route, tc.tenant, tc.fallback, got, tc.want)
			}
		})
	}
}

// seedExistingMailboxStore builds a tenant+zone+route plus a pre-existing
// mailbox so deliver's reuse condition (Mailbox != nil && !Created) is met for
// the given recipient. The seeded mailbox has NO RetentionHoursOverride so the
// only signal that reuse took effect is the override carried by a caller-supplied
// Result (see TestAcceptWithResolvedReusesRCPTResult).
func seedExistingMailboxStore(t *testing.T, recipient string) (*testutil.FakeStore, *testutil.MemoryObjectStore, *models.Mailbox) {
	t.Helper()
	st := testutil.NewFakeStore()
	obj := testutil.NewMemoryObjectStore()

	planID := uuid.New()
	tenantID := uuid.New()
	zoneID := uuid.New()
	st.SeedPlan(&models.Plan{
		ID: planID, Name: "reuse-test", MaxDomains: 10, MaxMailboxesPerDomain: 100,
		MaxMessagesPerMailbox: 1000, MaxMessageBytes: 1024 * 1024, RetentionHours: 24,
		RPMLimit: 1000, DailyQuota: 1000,
	})
	st.SeedTenant(&models.Tenant{ID: tenantID, Name: "tenant-a", PlanID: planID})
	st.SeedZone(&models.DomainZone{ID: zoneID, TenantID: tenantID, Domain: "mail.test", IsVerified: true, MXVerified: true})
	st.SeedRoute(&models.DomainRoute{ID: uuid.New(), ZoneID: zoneID, RouteType: models.RouteExact, MatchValue: "mail.test", AutoCreateMailbox: true, AccessModeDefault: models.AccessPublic})

	mb := &models.Mailbox{
		ID:             uuid.New(),
		TenantID:       tenantID,
		ZoneID:         zoneID,
		LocalPart:      "user",
		ResolvedDomain: "mail.test",
		FullAddress:    recipient,
		AccessMode:     models.AccessPublic,
		CreatedAt:      time.Now(),
	}
	st.SeedMailbox(mb)
	return st, obj, mb
}

// TestAcceptWithResolvedReusesRCPTResult proves the SMTP-session-reuse fast
// path: when WithResolved supplies a Reusable Result, deliverResolved skips
// resolver.Resolve entirely and uses the supplied Mailbox — including its
// RetentionHoursOverride, which the store-seeded mailbox deliberately does not
// carry. So ExpiresAt reflects the supplied override (99h) iff reuse happened,
// and the tenant default (24h) iff it did not.
func TestAcceptWithResolvedReusesRCPTResult(t *testing.T) {
	const recipient = "user@mail.test"

	st, obj, seededMB := seedExistingMailboxStore(t, recipient)
	resolverSvc := resolver.New(st, policy.NamingFull, true)
	svc := NewService(
		st, obj, resolverSvc, realtime.NewHub(10, st), hooks.New(hooks.Config{}, zerolog.Nop()),
		models.SMTPPolicy{DefaultAccept: true, DefaultStore: true}, 24, nil,
		config.Ingest{Durable: false, BatchSize: 10}, zerolog.Nop(),
	)

	raw := []byte("Subject: reuse\r\n\r\nhello")

	// --- Control: no WithResolved -> fresh Resolve -> seeded mailbox (no override)
	// -> ExpiresAt ~ now + tenant 24h.
	res1, err := svc.Accept(context.Background(), Envelope{
		Source: "smtp", MailFrom: "sender@example.org", Recipients: []string{recipient},
	}, raw)
	if err != nil {
		t.Fatal(err)
	}
	if res1.Delivered != 1 {
		t.Fatalf("control: expected 1 delivered, got %d", res1.Delivered)
	}

	// --- Reuse: WithResolved supplies a Result whose Mailbox carries override=99.
	// The supplied Mailbox is a copy of the seeded one with a fresh override; if
	// deliverResolved reuses it verbatim, the stored message's ExpiresAt will be
	// ~now + 99h. If it re-resolves, it reads the seeded (override-less) mailbox
	// and ExpiresAt is ~now + 24h.
	reusedMB := *seededMB
	reusedMB.RetentionHoursOverride = intPtr(99)
	supplied := &resolver.Result{
		Zone: &models.DomainZone{ID: seededMB.ZoneID, TenantID: seededMB.TenantID, Domain: "mail.test", IsVerified: true, MXVerified: true},
		Mailbox: &reusedMB,
	}

	res2, err := svc.Accept(context.Background(), Envelope{
		Source: "smtp", MailFrom: "sender@example.org", Recipients: []string{recipient},
	}, raw, WithResolved(recipient, supplied))
	if err != nil {
		t.Fatal(err)
	}
	if res2.Delivered != 1 {
		t.Fatalf("reuse: expected 1 delivered, got %d", res2.Delivered)
	}

	msgs, total, err := st.ListMessages(context.Background(), seededMB.ID, models.Page{Page: 1, PerPage: 10})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(msgs) != 2 {
		t.Fatalf("expected 2 messages (control + reuse), got total=%d len=%d", total, len(msgs))
	}
	// The reuse message is the second (later ReceivedAt). Assert its ExpiresAt is
	// ~99h from now, not 24h — the only way that happens is deliverResolved using
	// the supplied Mailbox.RetentionHoursOverride rather than re-resolving.
	var reuseMsg *models.Message
	for i := range msgs {
		if reuseMsg == nil || msgs[i].ReceivedAt.After(reuseMsg.ReceivedAt) {
			reuseMsg = msgs[i]
		}
	}
	delta := reuseMsg.ExpiresAt.Sub(reuseMsg.ReceivedAt)
	const expectH = 99
	if delta < (expectH-1)*time.Hour || delta > (expectH+1)*time.Hour {
		t.Fatalf("reuse: expected ExpiresAt-ReceivedAt ~%dh (reuse), got %v", expectH, delta)
	}
}

// TestAcceptWithResolvedDoesNotReuseAutoCreateResult locks the safety rail: a
// caller-supplied Result with Mailbox==nil (the shape Check returns for an
// auto-create route that has not materialized) MUST NOT short-circuit Resolve.
// deliverResolved must fall back to Resolve, which then auto-creates the mailbox
// for a recipient that does not yet exist.
func TestAcceptWithResolvedDoesNotReuseAutoCreateResult(t *testing.T) {
	const recipient = "fresh@mail.test" // not seeded -> auto-create path

	st := testutil.NewFakeStore()
	obj := testutil.NewMemoryObjectStore()
	planID := uuid.New()
	tenantID := uuid.New()
	zoneID := uuid.New()
	st.SeedPlan(&models.Plan{
		ID: planID, Name: "autocreate-test", MaxDomains: 10, MaxMailboxesPerDomain: 100,
		MaxMessagesPerMailbox: 1000, MaxMessageBytes: 1024 * 1024, RetentionHours: 24,
		RPMLimit: 1000, DailyQuota: 1000,
	})
	st.SeedTenant(&models.Tenant{ID: tenantID, Name: "tenant-a", PlanID: planID})
	st.SeedZone(&models.DomainZone{ID: zoneID, TenantID: tenantID, Domain: "mail.test", IsVerified: true, MXVerified: true})
	st.SeedRoute(&models.DomainRoute{ID: uuid.New(), ZoneID: zoneID, RouteType: models.RouteExact, MatchValue: "mail.test", AutoCreateMailbox: true, AccessModeDefault: models.AccessPublic})

	resolverSvc := resolver.New(st, policy.NamingFull, true)
	svc := NewService(
		st, obj, resolverSvc, realtime.NewHub(10, st), hooks.New(hooks.Config{}, zerolog.Nop()),
		models.SMTPPolicy{DefaultAccept: true, DefaultStore: true}, 24, nil,
		config.Ingest{Durable: false, BatchSize: 10}, zerolog.Nop(),
	)

	// Supplied Result mirrors what Check returns for an auto-create route: Zone +
	// Route set, Mailbox nil. Reusable() is false, so deliverResolved must ignore
	// it and run Resolve, which materializes the mailbox.
	zone := &models.DomainZone{ID: zoneID, TenantID: tenantID, Domain: "mail.test", IsVerified: true, MXVerified: true}
	supplied := &resolver.Result{Zone: zone, Mailbox: nil} // Mailbox nil -> not Reusable

	res, err := svc.Accept(context.Background(), Envelope{
		Source: "smtp", MailFrom: "sender@example.org", Recipients: []string{recipient},
	}, []byte("Subject: autocreate\r\n\r\nhello"), WithResolved(recipient, supplied))
	if err != nil {
		t.Fatal(err)
	}
	if res.Delivered != 1 {
		t.Fatalf("expected auto-create recipient delivered, got %d", res.Delivered)
	}
	mb, err := st.GetMailboxByAddress(context.Background(), recipient)
	if err != nil {
		t.Fatal(err)
	}
	if mb == nil {
		t.Fatal("expected mailbox auto-created despite non-reusable WithResolved")
	}
}

