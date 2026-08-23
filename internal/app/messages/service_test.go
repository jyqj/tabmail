package messageapp

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"tabmail/internal/app"
	"tabmail/internal/models"
	"tabmail/internal/policy"
	"tabmail/internal/testutil"
)

func TestResolveMailboxOwnerBoundAPIKeyRequiresExplicitAccess(t *testing.T) {
	ctx := context.Background()
	_, svc, tenant, mailbox := seededMessageService(t, models.AccessAPIKey, nil)
	keyID := uuid.New()
	ownerID := uuid.New()

	viewer := Viewer{
		Tenant:        tenant,
		AuthMode:      AuthModeAPIKey,
		PrincipalType: AuthModeAPIKey,
		PrincipalID:   &keyID,
		OwnerUserID:   &ownerID,
	}

	_, err := svc.ResolveMailbox(ctx, mailbox.FullAddress, viewer)
	if err == nil {
		t.Fatal("owner-bound API key without tenant-wide access should be denied")
	}
	if appErr, ok := app.As(err); !ok || appErr.Kind != app.KindForbidden {
		t.Fatalf("expected forbidden app error, got %T %v", err, err)
	}

	integrationKey := Viewer{
		Tenant:        tenant,
		AuthMode:      AuthModeAPIKey,
		PrincipalType: AuthModeAPIKey,
		PrincipalID:   ptrUUID(uuid.New()),
		TenantWide:    true,
	}
	if _, err := svc.ResolveMailbox(ctx, mailbox.FullAddress, integrationKey); err != nil {
		t.Fatalf("ownerless integration API key should remain tenant-wide: %v", err)
	}
}

func TestResolveMailboxOwnerBoundAPIKeyOwnerFallbackAndAllowedZones(t *testing.T) {
	ctx := context.Background()
	ownerID := uuid.New()
	_, svc, tenant, mailbox := seededMessageService(t, models.AccessAPIKey, &ownerID)
	keyID := uuid.New()

	viewer := Viewer{
		Tenant:        tenant,
		AuthMode:      AuthModeAPIKey,
		PrincipalType: AuthModeAPIKey,
		PrincipalID:   &keyID,
		OwnerUserID:   &ownerID,
	}
	if _, err := svc.ResolveMailboxForWrite(ctx, mailbox.FullAddress, viewer); err != nil {
		t.Fatalf("owner-bound API key should inherit zone owner fallback: %v", err)
	}

	otherZoneID := uuid.New()
	viewer.AllowedZoneIDs = []uuid.UUID{otherZoneID}
	_, err := svc.ResolveMailbox(ctx, mailbox.FullAddress, viewer)
	if err == nil {
		t.Fatal("allowed_zone_ids should narrow owner-bound API key access")
	}
	if appErr, ok := app.As(err); !ok || appErr.Kind != app.KindForbidden {
		t.Fatalf("expected forbidden app error, got %T %v", err, err)
	}
}

func seededMessageService(t *testing.T, accessMode models.AccessMode, ownerUserID *uuid.UUID) (*testutil.FakeStore, *Service, *models.Tenant, *models.Mailbox) {
	t.Helper()
	st := testutil.NewFakeStore()
	tenant := &models.Tenant{ID: uuid.New(), Name: "tenant"}
	st.SeedTenant(tenant)
	_, svc, _, mailbox := seededMessageServiceWithStore(t, st, tenant, accessMode, ownerUserID)
	return st, svc, tenant, mailbox
}

func seededMessageServiceWithStore(t *testing.T, st *testutil.FakeStore, tenant *models.Tenant, accessMode models.AccessMode, ownerUserID *uuid.UUID) (*testutil.FakeStore, *Service, *models.Tenant, *models.Mailbox) {
	t.Helper()
	zoneID := uuid.New()
	domain := zoneID.String() + ".mail.test"
	zone := &models.DomainZone{
		ID:          zoneID,
		TenantID:    tenant.ID,
		OwnerUserID: ownerUserID,
		Domain:      domain,
		IsVerified:  true,
		MXVerified:  true,
		CreatedAt:   time.Now(),
	}
	st.SeedZone(zone)
	mailbox := &models.Mailbox{
		ID:             uuid.New(),
		TenantID:       tenant.ID,
		ZoneID:         zoneID,
		LocalPart:      "inbox",
		ResolvedDomain: domain,
		FullAddress:    "inbox@" + domain,
		AccessMode:     accessMode,
		CreatedAt:      time.Now(),
	}
	st.SeedMailbox(mailbox)
	svc := NewService(st, testutil.NewMemoryObjectStore(), nil, nil, nil, policy.NamingFull, true, "mailbox-secret", zerolog.Nop())
	return st, svc, tenant, mailbox
}

func ptrUUID(id uuid.UUID) *uuid.UUID {
	return &id
}
