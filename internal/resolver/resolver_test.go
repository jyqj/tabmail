package resolver

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"tabmail/internal/models"
	"tabmail/internal/policy"
	"tabmail/internal/testutil"
)

func TestResolverCheckMatchesRouteWithoutCreatingMailbox(t *testing.T) {
	st := seededResolverStore()
	rv := New(st, policy.NamingFull, true)

	res, err := rv.Check(context.Background(), "user@sub.mail.test")
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if res == nil || res.Route == nil {
		t.Fatalf("expected route match, got %#v", res)
	}
	if res.Mailbox != nil || res.Created {
		t.Fatalf("expected no mailbox creation on Check, got %#v", res)
	}
	count, err := st.CountAllMailboxes(context.Background())
	if err != nil {
		t.Fatalf("count mailboxes: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no mailbox persisted, got %d", count)
	}
}

func TestResolverResolveAutoCreatesMailboxUsingParentZoneAndWildcardRoute(t *testing.T) {
	st := seededResolverStore()
	rv := New(st, policy.NamingFull, true)

	res, err := rv.Resolve(context.Background(), "user@sub.mail.test")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if res == nil || res.Zone == nil || res.Route == nil || res.Mailbox == nil {
		t.Fatalf("unexpected resolver result: %#v", res)
	}
	if !res.Created {
		t.Fatal("expected mailbox to be auto-created")
	}
	if res.Zone.Domain != "mail.test" {
		t.Fatalf("expected parent zone mail.test, got %q", res.Zone.Domain)
	}
	if res.Mailbox.FullAddress != "user@sub.mail.test" {
		t.Fatalf("unexpected mailbox address: %#v", res.Mailbox)
	}
	if res.Mailbox.RouteID == nil || *res.Mailbox.RouteID != res.Route.ID {
		t.Fatalf("expected route id to be attached: %#v", res.Mailbox)
	}
}

func TestResolverResolveMatchesSequenceRoute(t *testing.T) {
	st := testutil.NewFakeStore()
	planID := uuid.New()
	tenantID := uuid.New()
	zoneID := uuid.New()
	st.SeedPlan(&models.Plan{
		ID:                    planID,
		Name:                  "starter",
		MaxDomains:            5,
		MaxMailboxesPerDomain: 50,
		MaxMessagesPerMailbox: 200,
		MaxMessageBytes:       1024 * 1024,
		RetentionHours:        24,
		RPMLimit:              60,
		DailyQuota:            100,
	})
	st.SeedTenant(&models.Tenant{
		ID:      tenantID,
		Name:    "tenant-a",
		PlanID:  planID,
		IsSuper: false,
	})
	st.SeedZone(&models.DomainZone{
		ID:         zoneID,
		TenantID:   tenantID,
		Domain:     "mail.test",
		IsVerified: true,
		MXVerified: true,
		CreatedAt:  time.Now(),
	})
	start, end := 1, 20
	st.SeedRoute(&models.DomainRoute{
		ID:                uuid.New(),
		ZoneID:            zoneID,
		RouteType:         models.RouteSequence,
		MatchValue:        "box-{n}.mail.test",
		RangeStart:        &start,
		RangeEnd:          &end,
		AutoCreateMailbox: true,
		AccessModeDefault: models.AccessToken,
		CreatedAt:         time.Now(),
	})

	rv := New(st, policy.NamingFull, true)
	res, err := rv.Resolve(context.Background(), "hello@box-7.mail.test")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if res == nil || res.Route == nil || res.Mailbox == nil {
		t.Fatalf("unexpected resolver result: %#v", res)
	}
	if res.Route.RouteType != models.RouteSequence {
		t.Fatalf("expected sequence route, got %#v", res.Route)
	}
	if res.Mailbox.AccessMode != models.AccessToken {
		t.Fatalf("expected access mode inherited from route, got %#v", res.Mailbox)
	}
}

func TestResolverRoutePriorityPrefersExactThenSequenceThenWildcard(t *testing.T) {
	st := testutil.NewFakeStore()
	planID := uuid.New()
	tenantID := uuid.New()
	zoneID := uuid.New()
	now := time.Now()

	st.SeedPlan(&models.Plan{
		ID:                    planID,
		Name:                  "starter",
		MaxDomains:            5,
		MaxMailboxesPerDomain: 50,
		MaxMessagesPerMailbox: 200,
		MaxMessageBytes:       1024 * 1024,
		RetentionHours:        24,
		RPMLimit:              60,
		DailyQuota:            100,
	})
	st.SeedTenant(&models.Tenant{ID: tenantID, Name: "tenant-a", PlanID: planID})
	st.SeedZone(&models.DomainZone{
		ID:         zoneID,
		TenantID:   tenantID,
		Domain:     "mail.test",
		IsVerified: true,
		MXVerified: true,
		CreatedAt:  now,
	})

	startWide, endWide := 1, 100
	startNarrow, endNarrow := 10, 20

	st.SeedRoute(&models.DomainRoute{
		ID:                     uuid.New(),
		ZoneID:                 zoneID,
		RouteType:              models.RouteWildcard,
		MatchValue:             "*.mail.test",
		AutoCreateMailbox:      true,
		AccessModeDefault:      models.AccessPublic,
		RetentionHoursOverride: intPtr(72),
		CreatedAt:              now,
	})
	st.SeedRoute(&models.DomainRoute{
		ID:                     uuid.New(),
		ZoneID:                 zoneID,
		RouteType:              models.RouteSequence,
		MatchValue:             "box-{n}.mail.test",
		RangeStart:             &startWide,
		RangeEnd:               &endWide,
		AutoCreateMailbox:      true,
		AccessModeDefault:      models.AccessAPIKey,
		RetentionHoursOverride: intPtr(24),
		CreatedAt:              now.Add(time.Second),
	})
	st.SeedRoute(&models.DomainRoute{
		ID:                     uuid.New(),
		ZoneID:                 zoneID,
		RouteType:              models.RouteSequence,
		MatchValue:             "box-{n}.mail.test",
		RangeStart:             &startNarrow,
		RangeEnd:               &endNarrow,
		AutoCreateMailbox:      true,
		AccessModeDefault:      models.AccessToken,
		RetentionHoursOverride: intPtr(12),
		CreatedAt:              now.Add(2 * time.Second),
	})
	st.SeedRoute(&models.DomainRoute{
		ID:                     uuid.New(),
		ZoneID:                 zoneID,
		RouteType:              models.RouteExact,
		MatchValue:             "box-15.mail.test",
		AutoCreateMailbox:      true,
		AccessModeDefault:      models.AccessPublic,
		RetentionHoursOverride: intPtr(6),
		CreatedAt:              now.Add(3 * time.Second),
	})

	routes, err := st.ListRoutes(context.Background(), zoneID)
	if err != nil {
		t.Fatalf("list routes: %v", err)
	}

	best := matchRoute(routes, "hello", "box-15.mail.test", "mail.test")
	if best == nil || best.RouteType != models.RouteExact || best.MatchValue != "box-15.mail.test" {
		t.Fatalf("expected exact route to win, got %#v", best)
	}

	best = matchRoute(routes, "hello", "box-12.mail.test", "mail.test")
	if best == nil || best.RouteType != models.RouteSequence || best.AccessModeDefault != models.AccessToken {
		t.Fatalf("expected narrower sequence route to win, got %#v", best)
	}

	best = matchRoute(routes, "hello", "foo.mail.test", "mail.test")
	if best == nil || best.RouteType != models.RouteWildcard {
		t.Fatalf("expected wildcard route to win fallback, got %#v", best)
	}
}

func TestResolverResolveMatchesDeepWildcardRoute(t *testing.T) {
	st := testutil.NewFakeStore()
	planID := uuid.New()
	tenantID := uuid.New()
	zoneID := uuid.New()
	st.SeedPlan(&models.Plan{
		ID:                    planID,
		Name:                  "starter",
		MaxDomains:            5,
		MaxMailboxesPerDomain: 50,
		MaxMessagesPerMailbox: 200,
		MaxMessageBytes:       1024 * 1024,
		RetentionHours:        24,
		RPMLimit:              60,
		DailyQuota:            100,
	})
	st.SeedTenant(&models.Tenant{ID: tenantID, Name: "tenant-a", PlanID: planID})
	st.SeedZone(&models.DomainZone{
		ID:         zoneID,
		TenantID:   tenantID,
		Domain:     "mail.test",
		IsVerified: true,
		MXVerified: true,
		CreatedAt:  time.Now(),
	})
	st.SeedRoute(&models.DomainRoute{
		ID:                uuid.New(),
		ZoneID:            zoneID,
		RouteType:         models.RouteDeepWildcard,
		MatchValue:        "**.mail.test",
		AutoCreateMailbox: true,
		AccessModeDefault: models.AccessPublic,
		CreatedAt:         time.Now(),
	})

	rv := New(st, policy.NamingFull, true)
	res, err := rv.Resolve(context.Background(), "hello@two.deep.mail.test")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if res == nil || res.Route == nil || res.Route.RouteType != models.RouteDeepWildcard {
		t.Fatalf("expected deep wildcard route, got %#v", res)
	}
	if res.Mailbox == nil || res.Mailbox.FullAddress != "hello@two.deep.mail.test" {
		t.Fatalf("unexpected mailbox: %#v", res)
	}
}

func intPtr(v int) *int { return &v }

func TestResolverResolveReturnsExistingMailbox(t *testing.T) {
	st := seededResolverStore()
	zone, err := st.GetZoneByDomain(context.Background(), "mail.test")
	if err != nil || zone == nil {
		t.Fatalf("seeded zone missing: %v", err)
	}
	existing := &models.Mailbox{
		ID:             uuid.New(),
		TenantID:       zone.TenantID,
		ZoneID:         zone.ID,
		LocalPart:      "user",
		ResolvedDomain: "sub.mail.test",
		FullAddress:    "user@sub.mail.test",
		AccessMode:     models.AccessPublic,
		CreatedAt:      time.Now(),
	}
	st.SeedMailbox(existing)

	rv := New(st, policy.NamingFull, true)
	res, err := rv.Resolve(context.Background(), "user@sub.mail.test")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if res == nil || res.Mailbox == nil {
		t.Fatalf("expected existing mailbox, got %#v", res)
	}
	if res.Created {
		t.Fatalf("expected existing mailbox path, got created result %#v", res)
	}
	if res.Mailbox.ID != existing.ID {
		t.Fatalf("unexpected mailbox returned: %#v", res.Mailbox)
	}
}

func TestResolverCheckIgnoresExistingMailboxFromDifferentZoneInLocalNamingMode(t *testing.T) {
	st := testutil.NewFakeStore()
	planID := uuid.New()
	tenantID := uuid.New()
	zoneAID := uuid.New()
	zoneBID := uuid.New()
	now := time.Now()

	st.SeedPlan(&models.Plan{
		ID:                    planID,
		Name:                  "starter",
		MaxDomains:            5,
		MaxMailboxesPerDomain: 50,
		MaxMessagesPerMailbox: 200,
		MaxMessageBytes:       1024 * 1024,
		RetentionHours:        24,
		RPMLimit:              60,
		DailyQuota:            100,
	})
	st.SeedTenant(&models.Tenant{ID: tenantID, Name: "tenant-a", PlanID: planID})
	st.SeedZone(&models.DomainZone{
		ID:         zoneAID,
		TenantID:   tenantID,
		Domain:     "a.mail.test",
		IsVerified: true,
		MXVerified: true,
		CreatedAt:  now,
	})
	st.SeedZone(&models.DomainZone{
		ID:         zoneBID,
		TenantID:   tenantID,
		Domain:     "b.mail.test",
		IsVerified: true,
		MXVerified: true,
		CreatedAt:  now,
	})
	st.SeedRoute(&models.DomainRoute{
		ID:                uuid.New(),
		ZoneID:            zoneBID,
		RouteType:         models.RouteExact,
		MatchValue:        "b.mail.test",
		AutoCreateMailbox: true,
		AccessModeDefault: models.AccessPublic,
		CreatedAt:         now,
	})
	existing := &models.Mailbox{
		ID:             uuid.New(),
		TenantID:       tenantID,
		ZoneID:         zoneAID,
		LocalPart:      "user",
		ResolvedDomain: "a.mail.test",
		FullAddress:    "user",
		AccessMode:     models.AccessPublic,
		CreatedAt:      now,
	}
	st.SeedMailbox(existing)

	rv := New(st, policy.NamingLocal, true)
	res, err := rv.Check(context.Background(), "user@b.mail.test")
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if res == nil || res.Zone == nil || res.Zone.ID != zoneBID {
		t.Fatalf("expected zone B result, got %#v", res)
	}
	if res.Mailbox != nil {
		t.Fatalf("must not return mailbox from zone A for zone B address: %#v", res.Mailbox)
	}
	if res.Route == nil || res.Route.ZoneID != zoneBID {
		t.Fatalf("expected zone B route after ignoring cross-zone mailbox, got %#v", res)
	}
}

func seededResolverStore() *testutil.FakeStore {
	st := testutil.NewFakeStore()
	planID := uuid.New()
	tenantID := uuid.New()
	zoneID := uuid.New()

	st.SeedPlan(&models.Plan{
		ID:                    planID,
		Name:                  "starter",
		MaxDomains:            5,
		MaxMailboxesPerDomain: 50,
		MaxMessagesPerMailbox: 200,
		MaxMessageBytes:       1024 * 1024,
		RetentionHours:        24,
		RPMLimit:              60,
		DailyQuota:            100,
	})
	st.SeedTenant(&models.Tenant{
		ID:      tenantID,
		Name:    "tenant-a",
		PlanID:  planID,
		IsSuper: false,
	})
	st.SeedZone(&models.DomainZone{
		ID:         zoneID,
		TenantID:   tenantID,
		Domain:     "mail.test",
		IsVerified: true,
		MXVerified: true,
		CreatedAt:  time.Now(),
	})
	st.SeedRoute(&models.DomainRoute{
		ID:                uuid.New(),
		ZoneID:            zoneID,
		RouteType:         models.RouteWildcard,
		MatchValue:        "*.mail.test",
		AutoCreateMailbox: true,
		AccessModeDefault: models.AccessPublic,
		CreatedAt:         time.Now(),
	})
	return st
}
