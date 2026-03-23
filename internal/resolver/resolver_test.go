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

func TestResolverResolveReturnsExistingMailbox(t *testing.T) {
	st := seededResolverStore()
	existing := &models.Mailbox{
		ID:             uuid.New(),
		TenantID:       uuid.New(),
		ZoneID:         uuid.New(),
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
