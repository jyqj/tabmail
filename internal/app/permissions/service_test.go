package permissionsapp

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/models"
)

type permTestStore struct {
	profiles   map[uuid.UUID]*models.PermissionProfile
	users      map[uuid.UUID]*models.User
	zones      map[uuid.UUID]*models.DomainZone
	overrides  map[uuid.UUID]*models.UserPermissionOverride
	deleted    []uuid.UUID
	listScope  []*uuid.UUID
	getZoneErr error
	audits     []*models.AuditEntry
}

func newPermTestStore() *permTestStore {
	return &permTestStore{
		profiles:  map[uuid.UUID]*models.PermissionProfile{},
		users:     map[uuid.UUID]*models.User{},
		zones:     map[uuid.UUID]*models.DomainZone{},
		overrides: map[uuid.UUID]*models.UserPermissionOverride{},
	}
}

func (s *permTestStore) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

func (s *permTestStore) InsertAudit(_ context.Context, entry *models.AuditEntry) error {
	cp := *entry
	s.audits = append(s.audits, &cp)
	return nil
}

func (s *permTestStore) ListPermissionProfiles(_ context.Context, tenantID *uuid.UUID) ([]*models.PermissionProfile, error) {
	s.listScope = append(s.listScope, tenantID)
	out := make([]*models.PermissionProfile, 0, len(s.profiles))
	for _, p := range s.profiles {
		if tenantID != nil && p.TenantID != nil && *p.TenantID != *tenantID {
			continue
		}
		cp := *p
		out = append(out, &cp)
	}
	return out, nil
}

func (s *permTestStore) CreatePermissionProfile(_ context.Context, p *models.PermissionProfile) error {
	cp := *p
	s.profiles[cp.ID] = &cp
	return nil
}

func (s *permTestStore) GetPermissionProfile(_ context.Context, id uuid.UUID) (*models.PermissionProfile, error) {
	p, found := s.profiles[id]
	if !found {
		return nil, nil
	}
	cp := *p
	return &cp, nil
}

func (s *permTestStore) UpdatePermissionProfile(_ context.Context, p *models.PermissionProfile) error {
	if _, found := s.profiles[p.ID]; !found {
		return errors.New("profile not found")
	}
	cp := *p
	s.profiles[cp.ID] = &cp
	return nil
}

func (s *permTestStore) DeletePermissionProfile(_ context.Context, id uuid.UUID, _ *uuid.UUID) error {
	delete(s.profiles, id)
	s.deleted = append(s.deleted, id)
	return nil
}

func (s *permTestStore) GetUser(_ context.Context, id uuid.UUID) (*models.User, error) {
	u, found := s.users[id]
	if !found {
		return nil, nil
	}
	cp := *u
	return &cp, nil
}

func (s *permTestStore) EffectivePermission(_ context.Context, userID uuid.UUID) (*models.EffectivePermission, error) {
	if _, found := s.users[userID]; !found {
		return nil, errors.New("user not found")
	}
	return &models.EffectivePermission{CanSend: true, MaxMailboxes: 5}, nil
}

func (s *permTestStore) UpsertUserPermissionOverride(_ context.Context, o *models.UserPermissionOverride) error {
	cp := *o
	s.overrides[cp.UserID] = &cp
	return nil
}

func (s *permTestStore) DeleteUserPermissionOverride(_ context.Context, userID uuid.UUID) error {
	delete(s.overrides, userID)
	return nil
}

func (s *permTestStore) GetZone(_ context.Context, id uuid.UUID) (*models.DomainZone, error) {
	if s.getZoneErr != nil {
		return nil, s.getZoneErr
	}
	z, found := s.zones[id]
	if !found {
		return nil, nil
	}
	cp := *z
	return &cp, nil
}

func newTestService(store *permTestStore) *Service {
	return NewService(store, nil, zerolog.Nop())
}

func requireKind(t *testing.T, err error, want app.ErrorKind) {
	t.Helper()
	appErr, found := app.As(err)
	if !found {
		t.Fatalf("expected an application error, got %v", err)
	}
	if appErr.Kind != want {
		t.Fatalf("expected kind %q, got %q (%s)", want, appErr.Kind, appErr.Message)
	}
}

func seedTenantUser(store *permTestStore, tenantID uuid.UUID) *models.User {
	user := &models.User{ID: uuid.New(), TenantID: tenantID, Email: "user@example.com", IsActive: true}
	store.users[user.ID] = user
	return user
}

func seedZone(store *permTestStore, tenantID uuid.UUID) *models.DomainZone {
	zone := &models.DomainZone{ID: uuid.New(), TenantID: tenantID, Domain: "mail.test"}
	store.zones[zone.ID] = zone
	return zone
}

func TestListProfilesScopesTenantAdminsToTheirTenant(t *testing.T) {
	store := newPermTestStore()
	tenant := &models.Tenant{ID: uuid.New()}
	svc := newTestService(store)

	if _, err := svc.ListProfiles(context.Background(), authz.Actor{IsAdmin: true}, tenant); err != nil {
		t.Fatalf("list as tenant admin: %v", err)
	}
	if _, err := svc.ListProfiles(context.Background(), authz.Actor{IsSuperAdmin: true}, tenant); err != nil {
		t.Fatalf("list as platform admin: %v", err)
	}

	if len(store.listScope) != 2 {
		t.Fatalf("expected two list calls, got %d", len(store.listScope))
	}
	if store.listScope[0] == nil || *store.listScope[0] != tenant.ID {
		t.Fatalf("expected the tenant admin to be scoped to its tenant, got %v", store.listScope[0])
	}
	if store.listScope[1] != nil {
		t.Fatalf("expected the platform admin to see every profile, got scope %v", store.listScope[1])
	}
}

func TestListProfilesRequiresATenantForNonAdmins(t *testing.T) {
	svc := newTestService(newPermTestStore())

	_, err := svc.ListProfiles(context.Background(), authz.Actor{IsAdmin: true}, nil)
	requireKind(t, err, app.KindForbidden)
}

func TestCreateProfilePinsTenantAdminsToTheirOwnTenant(t *testing.T) {
	store := newPermTestStore()
	tenant := &models.Tenant{ID: uuid.New()}
	other := uuid.New()
	svc := newTestService(store)

	profile, err := svc.CreateProfile(context.Background(), authz.Actor{IsAdmin: true}, tenant, CreateProfileRequest{
		Name:     "team",
		TenantID: &other,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if profile.TenantID == nil || *profile.TenantID != tenant.ID {
		t.Fatalf("expected the profile to be pinned to the caller's tenant, got %v", profile.TenantID)
	}
	if profile.IsSystem {
		t.Fatal("expected a tenant profile, not a system one")
	}
}

func TestCreateProfileRejectsZonesFromAnotherTenant(t *testing.T) {
	store := newPermTestStore()
	tenant := &models.Tenant{ID: uuid.New()}
	foreignZone := seedZone(store, uuid.New())
	svc := newTestService(store)

	_, err := svc.CreateProfile(context.Background(), authz.Actor{IsAdmin: true}, tenant, CreateProfileRequest{
		Name:           "team",
		AllowedZoneIDs: []uuid.UUID{foreignZone.ID},
	})
	requireKind(t, err, app.KindBadRequest)
	if len(store.profiles) != 0 {
		t.Fatal("expected nothing to be stored when a zone is rejected")
	}
}

func TestCreateProfileRejectsZonesOnAGlobalProfile(t *testing.T) {
	store := newPermTestStore()
	zone := seedZone(store, uuid.New())
	svc := newTestService(store)

	_, err := svc.CreateProfile(context.Background(), authz.Actor{IsSuperAdmin: true}, nil, CreateProfileRequest{
		Name:           "global",
		AllowedZoneIDs: []uuid.UUID{zone.ID},
	})
	requireKind(t, err, app.KindBadRequest)
}

func TestCreateProfileRequiresAName(t *testing.T) {
	svc := newTestService(newPermTestStore())

	_, err := svc.CreateProfile(context.Background(), authz.Actor{IsSuperAdmin: true}, nil, CreateProfileRequest{})
	requireKind(t, err, app.KindBadRequest)
}

func TestUpdateProfileAppliesOnlyTheSuppliedFields(t *testing.T) {
	store := newPermTestStore()
	tenant := &models.Tenant{ID: uuid.New()}
	existing := &models.PermissionProfile{
		ID: uuid.New(), TenantID: &tenant.ID, Name: "team",
		Description: "keep me", MaxMailboxes: 3, CanSend: true,
	}
	store.profiles[existing.ID] = existing
	svc := newTestService(store)

	name := "renamed"
	quota := 9
	updated, err := svc.UpdateProfile(context.Background(), authz.Actor{IsAdmin: true}, tenant, existing.ID, UpdateProfileRequest{
		Name:         &name,
		MaxMailboxes: &quota,
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Name != "renamed" || updated.MaxMailboxes != 9 {
		t.Fatalf("expected the supplied fields to be applied, got %#v", updated)
	}
	if updated.Description != "keep me" || !updated.CanSend {
		t.Fatalf("expected untouched fields to survive, got %#v", updated)
	}
}

func TestUpdateProfileRefusesSystemProfiles(t *testing.T) {
	store := newPermTestStore()
	system := &models.PermissionProfile{ID: uuid.New(), Name: "default", IsSystem: true}
	store.profiles[system.ID] = system
	svc := newTestService(store)

	_, err := svc.UpdateProfile(context.Background(), authz.Actor{IsSuperAdmin: true}, nil, system.ID, UpdateProfileRequest{})
	requireKind(t, err, app.KindForbidden)
}

// A profile in another tenant must look missing rather than forbidden, so the
// endpoint never confirms that an id exists somewhere else.
func TestUpdateProfileHidesOtherTenantsProfiles(t *testing.T) {
	store := newPermTestStore()
	tenant := &models.Tenant{ID: uuid.New()}
	otherTenant := uuid.New()
	foreign := &models.PermissionProfile{ID: uuid.New(), TenantID: &otherTenant, Name: "theirs"}
	store.profiles[foreign.ID] = foreign
	svc := newTestService(store)

	_, err := svc.UpdateProfile(context.Background(), authz.Actor{IsAdmin: true}, tenant, foreign.ID, UpdateProfileRequest{})
	requireKind(t, err, app.KindNotFound)
}

func TestDeleteProfileRefusesSystemProfiles(t *testing.T) {
	store := newPermTestStore()
	system := &models.PermissionProfile{ID: uuid.New(), Name: "default", IsSystem: true}
	store.profiles[system.ID] = system
	svc := newTestService(store)

	err := svc.DeleteProfile(context.Background(), authz.Actor{IsSuperAdmin: true}, nil, system.ID)
	requireKind(t, err, app.KindForbidden)
	if len(store.deleted) != 0 {
		t.Fatalf("expected no delete to reach the store, got %v", store.deleted)
	}
}

func TestDeleteProfileRemovesATenantProfile(t *testing.T) {
	store := newPermTestStore()
	tenant := &models.Tenant{ID: uuid.New()}
	profile := &models.PermissionProfile{ID: uuid.New(), TenantID: &tenant.ID, Name: "team"}
	store.profiles[profile.ID] = profile
	svc := newTestService(store)

	if err := svc.DeleteProfile(context.Background(), authz.Actor{IsAdmin: true}, tenant, profile.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if len(store.deleted) != 1 || store.deleted[0] != profile.ID {
		t.Fatalf("expected the profile to be deleted, got %v", store.deleted)
	}
}

func TestUserPermissionHidesUsersFromOtherTenants(t *testing.T) {
	store := newPermTestStore()
	tenant := &models.Tenant{ID: uuid.New()}
	foreign := seedTenantUser(store, uuid.New())
	svc := newTestService(store)

	_, err := svc.UserPermission(context.Background(), tenant, foreign.ID)
	requireKind(t, err, app.KindNotFound)
}

func TestSetUserOverrideTakesTheUserFromThePath(t *testing.T) {
	store := newPermTestStore()
	tenant := &models.Tenant{ID: uuid.New()}
	user := seedTenantUser(store, tenant.ID)
	zone := seedZone(store, tenant.ID)
	svc := newTestService(store)

	canSend := false
	override, err := svc.SetUserOverride(context.Background(), tenant, user.ID, models.UserPermissionOverride{
		UserID:         uuid.New(), // a body that names somebody else must not win
		CanSend:        &canSend,
		AllowedZoneIDs: []uuid.UUID{zone.ID},
	})
	if err != nil {
		t.Fatalf("set override: %v", err)
	}
	if override.UserID != user.ID {
		t.Fatalf("expected the override to bind to the path user, got %s", override.UserID)
	}
	if stored := store.overrides[user.ID]; stored == nil || stored.CanSend == nil || *stored.CanSend {
		t.Fatalf("expected the override to be stored, got %#v", stored)
	}
}

func TestSetUserOverrideRejectsZonesFromAnotherTenant(t *testing.T) {
	store := newPermTestStore()
	tenant := &models.Tenant{ID: uuid.New()}
	user := seedTenantUser(store, tenant.ID)
	foreignZone := seedZone(store, uuid.New())
	svc := newTestService(store)

	_, err := svc.SetUserOverride(context.Background(), tenant, user.ID, models.UserPermissionOverride{
		AllowedZoneIDs: []uuid.UUID{foreignZone.ID},
	})
	requireKind(t, err, app.KindBadRequest)
	if len(store.overrides) != 0 {
		t.Fatal("expected nothing to be stored when a zone is rejected")
	}
}

func TestSetUserOverrideReportsStoreFailuresAsInternal(t *testing.T) {
	store := newPermTestStore()
	tenant := &models.Tenant{ID: uuid.New()}
	user := seedTenantUser(store, tenant.ID)
	zone := seedZone(store, tenant.ID)
	store.getZoneErr = errors.New("database is down")
	svc := newTestService(store)

	_, err := svc.SetUserOverride(context.Background(), tenant, user.ID, models.UserPermissionOverride{
		AllowedZoneIDs: []uuid.UUID{zone.ID},
	})
	requireKind(t, err, app.KindInternal)
}

func TestDeleteUserOverrideRequiresATenant(t *testing.T) {
	store := newPermTestStore()
	user := seedTenantUser(store, uuid.New())
	svc := newTestService(store)

	err := svc.DeleteUserOverride(context.Background(), nil, user.ID)
	requireKind(t, err, app.KindForbidden)
}

func TestOwnPermissionNeedsALoggedInUser(t *testing.T) {
	svc := newTestService(newPermTestStore())

	_, err := svc.OwnPermission(context.Background(), nil)
	requireKind(t, err, app.KindForbidden)
}

func TestOwnPermissionResolvesTheCaller(t *testing.T) {
	store := newPermTestStore()
	user := seedTenantUser(store, uuid.New())
	svc := newTestService(store)

	perm, err := svc.OwnPermission(context.Background(), user)
	if err != nil {
		t.Fatalf("own permission: %v", err)
	}
	if perm == nil || !perm.CanSend {
		t.Fatalf("expected the resolved permission, got %#v", perm)
	}
}
