package domainapp

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/authz"
	"tabmail/internal/models"
	"tabmail/internal/policy"
)

type domainTestStore struct {
	zones map[uuid.UUID]*models.DomainZone
}

func newDomainTestStore() *domainTestStore {
	return &domainTestStore{zones: map[uuid.UUID]*models.DomainZone{}}
}

func (s *domainTestStore) InsertAudit(context.Context, *models.AuditEntry) error    { return nil }
func (s *domainTestStore) GetUser(context.Context, uuid.UUID) (*models.User, error) { return nil, nil }
func (s *domainTestStore) GetAPIKey(context.Context, uuid.UUID) (*models.TenantAPIKey, error) {
	return nil, nil
}
func (s *domainTestStore) ListZones(_ context.Context, tenantID uuid.UUID) ([]*models.DomainZone, error) {
	out := make([]*models.DomainZone, 0, len(s.zones))
	for _, z := range s.zones {
		if z.TenantID == tenantID {
			cp := *z
			out = append(out, &cp)
		}
	}
	return out, nil
}
func (s *domainTestStore) ListAllZones(context.Context) ([]*models.DomainZone, error) {
	return nil, nil
}
func (s *domainTestStore) ListPublicZones(context.Context) ([]*models.DomainZone, error) {
	return nil, nil
}
func (s *domainTestStore) ListZonesByVisibilities(context.Context, []models.ResourceVisibility) ([]*models.DomainZone, error) {
	return nil, nil
}
func (s *domainTestStore) EffectiveConfig(context.Context, uuid.UUID) (*models.EffectiveConfig, error) {
	return &models.EffectiveConfig{MaxDomains: 100, MaxMailboxesPerDomain: 100}, nil
}
func (s *domainTestStore) CountZones(context.Context, uuid.UUID) (int, error) { return 0, nil }
func (s *domainTestStore) CreateZone(_ context.Context, z *models.DomainZone) error {
	if z.ID == uuid.Nil {
		z.ID = uuid.New()
	}
	cp := *z
	s.zones[z.ID] = &cp
	return nil
}
func (s *domainTestStore) DeleteZone(context.Context, uuid.UUID) error          { return nil }
func (s *domainTestStore) UpdateZone(context.Context, *models.DomainZone) error { return nil }
func (s *domainTestStore) GetZone(_ context.Context, id uuid.UUID) (*models.DomainZone, error) {
	if z := s.zones[id]; z != nil {
		cp := *z
		return &cp, nil
	}
	return nil, nil
}
func (s *domainTestStore) GetZoneByDomain(_ context.Context, domain string) (*models.DomainZone, error) {
	for _, z := range s.zones {
		if z.Domain == domain {
			cp := *z
			return &cp, nil
		}
	}
	return nil, nil
}
func (s *domainTestStore) ListRoutes(context.Context, uuid.UUID) ([]*models.DomainRoute, error) {
	return nil, nil
}
func (s *domainTestStore) CreateRoute(context.Context, *models.DomainRoute) error { return nil }
func (s *domainTestStore) GetRoute(context.Context, uuid.UUID) (*models.DomainRoute, error) {
	return nil, nil
}
func (s *domainTestStore) DeleteRoute(context.Context, uuid.UUID) error { return nil }
func (s *domainTestStore) CreateSendIdentity(context.Context, *models.SendIdentity) error {
	return nil
}
func (s *domainTestStore) ListSendIdentitiesByZone(context.Context, uuid.UUID) ([]*models.SendIdentity, error) {
	return nil, nil
}
func (s *domainTestStore) UpdateSendIdentitiesVerifiedByZone(context.Context, uuid.UUID, bool) error {
	return nil
}

func userActor(tenantID uuid.UUID, userID uuid.UUID) authz.Actor {
	return authz.Actor{Type: authz.PrincipalUser, ID: userID, TenantID: tenantID, Role: models.RoleUser}
}

func adminActor(tenantID uuid.UUID) authz.Actor {
	return authz.Actor{Type: authz.PrincipalUser, ID: uuid.New(), TenantID: tenantID, Role: models.RoleAdmin, IsAdmin: true}
}

func TestManagedZoneRejectsCrossTenantAccess(t *testing.T) {
	ctx := context.Background()
	st := newDomainTestStore()
	tenantA := &models.Tenant{ID: uuid.New(), Name: "tenant-a"}
	tenantB := &models.Tenant{ID: uuid.New(), Name: "tenant-b"}
	userA := uuid.New()
	zoneB := &models.DomainZone{ID: uuid.New(), TenantID: tenantB.ID, Domain: "b.example"}
	st.zones[zoneB.ID] = zoneB

	svc := NewService(st, nil, "mx.example", policy.NamingFull, "secret", zerolog.Nop())
	_, err := svc.ManagedZone(ctx, userActor(tenantA.ID, userA), zoneB.ID)
	if err == nil {
		t.Fatal("expected cross-tenant access to be rejected")
	}
}

func TestManagedZoneAllowsTenantAdminOwnTenant(t *testing.T) {
	ctx := context.Background()
	st := newDomainTestStore()
	tenant := &models.Tenant{ID: uuid.New(), Name: "tenant"}
	zone := &models.DomainZone{ID: uuid.New(), TenantID: tenant.ID, Domain: "tenant.example"}
	st.zones[zone.ID] = zone

	svc := NewService(st, nil, "mx.example", policy.NamingFull, "secret", zerolog.Nop())
	got, err := svc.ManagedZone(ctx, adminActor(tenant.ID), zone.ID)
	if err != nil {
		t.Fatalf("tenant admin should manage own tenant zone: %v", err)
	}
	if got == nil || got.ID != zone.ID {
		t.Fatalf("unexpected zone: %#v", got)
	}
}

func TestListZonesFiltersByOwnerAndAllowlist(t *testing.T) {
	ctx := context.Background()
	st := newDomainTestStore()
	tenant := &models.Tenant{ID: uuid.New(), Name: "tenant"}
	owner := uuid.New()
	other := uuid.New()
	ownedAllowed := &models.DomainZone{ID: uuid.New(), TenantID: tenant.ID, OwnerUserID: &owner, Domain: "allowed.example"}
	ownedDenied := &models.DomainZone{ID: uuid.New(), TenantID: tenant.ID, OwnerUserID: &owner, Domain: "denied.example"}
	foreign := &models.DomainZone{ID: uuid.New(), TenantID: tenant.ID, OwnerUserID: &other, Domain: "other.example"}
	st.zones[ownedAllowed.ID] = ownedAllowed
	st.zones[ownedDenied.ID] = ownedDenied
	st.zones[foreign.ID] = foreign

	svc := NewService(st, nil, "mx.example", policy.NamingFull, "secret", zerolog.Nop())

	actor := userActor(tenant.ID, owner)
	actor.Permission = &models.EffectivePermission{AllowedZoneIDs: []uuid.UUID{ownedAllowed.ID, foreign.ID}}
	items, err := svc.ListZones(ctx, actor, tenant)
	if err != nil {
		t.Fatalf("list zones: %v", err)
	}
	if len(items) != 1 || items[0].ID != ownedAllowed.ID {
		t.Fatalf("expected only owned+allowed zone, got %#v", items)
	}

	// Admins see all tenant zones regardless of ownership or allowlist.
	adminItems, err := svc.ListZones(ctx, adminActor(tenant.ID), tenant)
	if err != nil {
		t.Fatalf("admin list zones: %v", err)
	}
	if len(adminItems) != 3 {
		t.Fatalf("expected admin to see all 3 zones, got %d", len(adminItems))
	}
}

func TestCreateRouteAuthorizedThroughSeam(t *testing.T) {
	ctx := context.Background()
	st := newDomainTestStore()
	tenant := &models.Tenant{ID: uuid.New(), Name: "tenant"}
	owner := uuid.New()
	zone := &models.DomainZone{ID: uuid.New(), TenantID: tenant.ID, OwnerUserID: &owner, Domain: "routes.example"}
	st.zones[zone.ID] = zone

	svc := NewService(st, nil, "mx.example", policy.NamingFull, "secret", zerolog.Nop())
	input := CreateRouteInput{RouteType: models.RouteExact, MatchValue: "routes.example"}

	// CanCreateRoutes=false is denied by ActionRouteManage.
	denied := userActor(tenant.ID, owner)
	denied.Permission = &models.EffectivePermission{CanCreateRoutes: false}
	if _, err := svc.CreateRoute(ctx, denied, zone.ID, input); err == nil || err.Error() != "route creation not allowed" {
		t.Fatalf("expected route creation denial, got: %v", err)
	}

	// Zone outside the allowlist is denied.
	restricted := userActor(tenant.ID, owner)
	restricted.Permission = &models.EffectivePermission{CanCreateRoutes: true, AllowedZoneIDs: []uuid.UUID{uuid.New()}}
	if _, err := svc.CreateRoute(ctx, restricted, zone.ID, input); err == nil || err.Error() != "zone not in allowed list" {
		t.Fatalf("expected allowlist denial, got: %v", err)
	}

	// Non-owner is denied.
	if _, err := svc.CreateRoute(ctx, userActor(tenant.ID, uuid.New()), zone.ID, input); err == nil || err.Error() != "not your domain" {
		t.Fatalf("expected ownership denial, got: %v", err)
	}

	// Owner with permission succeeds.
	allowed := userActor(tenant.ID, owner)
	allowed.Permission = &models.EffectivePermission{CanCreateRoutes: true}
	route, err := svc.CreateRoute(ctx, allowed, zone.ID, input)
	if err != nil {
		t.Fatalf("owner should create route: %v", err)
	}
	if route == nil || route.ZoneID != zone.ID {
		t.Fatalf("unexpected route: %#v", route)
	}
}
