package sendidentitiesapp

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/models"
)

type identityTestStore struct {
	identities map[uuid.UUID]*models.SendIdentity
	zones      map[uuid.UUID]*models.DomainZone
	deleted    []uuid.UUID
	audits     []*models.AuditEntry
}

func newIdentityTestStore() *identityTestStore {
	return &identityTestStore{
		identities: map[uuid.UUID]*models.SendIdentity{},
		zones:      map[uuid.UUID]*models.DomainZone{},
	}
}

func (s *identityTestStore) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

func (s *identityTestStore) InsertAudit(_ context.Context, entry *models.AuditEntry) error {
	cp := *entry
	s.audits = append(s.audits, &cp)
	return nil
}

func (s *identityTestStore) CreateSendIdentity(_ context.Context, si *models.SendIdentity) error {
	cp := *si
	if cp.ID == uuid.Nil {
		cp.ID = uuid.New()
	}
	s.identities[cp.ID] = &cp
	si.ID = cp.ID
	return nil
}

func (s *identityTestStore) GetSendIdentity(_ context.Context, id uuid.UUID) (*models.SendIdentity, error) {
	si, found := s.identities[id]
	if !found {
		return nil, nil
	}
	cp := *si
	return &cp, nil
}

func (s *identityTestStore) ListSendIdentities(_ context.Context, tenantID uuid.UUID) ([]*models.SendIdentity, error) {
	var out []*models.SendIdentity
	for _, si := range s.identities {
		if si.TenantID == tenantID {
			cp := *si
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (s *identityTestStore) DeleteSendIdentity(_ context.Context, id uuid.UUID) error {
	delete(s.identities, id)
	s.deleted = append(s.deleted, id)
	return nil
}

func (s *identityTestStore) GetZone(_ context.Context, id uuid.UUID) (*models.DomainZone, error) {
	zone, found := s.zones[id]
	if !found {
		return nil, nil
	}
	cp := *zone
	return &cp, nil
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

func TestListRequiresTenant(t *testing.T) {
	svc := NewService(newIdentityTestStore(), nil, zerolog.Nop())
	_, err := svc.List(context.Background(), authz.Actor{}, uuid.Nil)
	requireKind(t, err, app.KindForbidden)
}

func TestListFiltersNonAdminByAllowedZones(t *testing.T) {
	st := newIdentityTestStore()
	tenantID := uuid.New()
	allowedZone := uuid.New()
	blockedZone := uuid.New()
	st.identities[uuid.New()] = &models.SendIdentity{ID: uuid.New(), TenantID: tenantID, ZoneID: allowedZone, Address: "a@allowed.test"}
	st.identities[uuid.New()] = &models.SendIdentity{ID: uuid.New(), TenantID: tenantID, ZoneID: blockedZone, Address: "b@blocked.test"}

	svc := NewService(st, nil, zerolog.Nop())
	actor := authz.Actor{
		Type:       authz.PrincipalUser,
		TenantID:   tenantID,
		Permission: &models.EffectivePermission{AllowedZoneIDs: []uuid.UUID{allowedZone}},
	}
	items, err := svc.List(context.Background(), actor, tenantID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ZoneID != allowedZone {
		t.Fatalf("expected only the allowed-zone identity, got %#v", items)
	}

	admin := authz.Actor{Type: authz.PrincipalUser, TenantID: tenantID, IsAdmin: true}
	items, err = svc.List(context.Background(), admin, tenantID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("expected admin to see both identities, got %d", len(items))
	}
}

func TestListReturnsEmptySliceNotNil(t *testing.T) {
	svc := NewService(newIdentityTestStore(), nil, zerolog.Nop())
	items, err := svc.List(context.Background(), authz.Actor{IsAdmin: true}, uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if items == nil {
		t.Fatal("expected empty slice, got nil")
	}
}

func TestCreateRequiresAdmin(t *testing.T) {
	st := newIdentityTestStore()
	tenantID := uuid.New()
	zoneID := uuid.New()
	st.zones[zoneID] = &models.DomainZone{ID: zoneID, TenantID: tenantID, Domain: "mail.test", IsVerified: true, MXVerified: true}

	svc := NewService(st, nil, zerolog.Nop())
	actor := authz.Actor{Type: authz.PrincipalUser, TenantID: tenantID}
	_, err := svc.Create(context.Background(), actor, CreateRequest{ZoneID: zoneID, Address: "x@mail.test"})
	requireKind(t, err, app.KindForbidden)
}

func TestCreateHidesForeignZoneAsNotFound(t *testing.T) {
	st := newIdentityTestStore()
	zoneID := uuid.New()
	st.zones[zoneID] = &models.DomainZone{ID: zoneID, TenantID: uuid.New(), Domain: "other.test"}

	svc := NewService(st, nil, zerolog.Nop())
	actor := authz.Actor{Type: authz.PrincipalUser, TenantID: uuid.New(), IsAdmin: true}
	_, err := svc.Create(context.Background(), actor, CreateRequest{ZoneID: zoneID, Address: "x@other.test"})
	requireKind(t, err, app.KindNotFound)
}

func TestCreateCanonicalizesAndBindsIdentityToZone(t *testing.T) {
	st := newIdentityTestStore()
	tenantID := uuid.New()
	zoneID := uuid.New()
	st.zones[zoneID] = &models.DomainZone{ID: zoneID, TenantID: tenantID, Domain: "Mail.Test.", IsVerified: true, MXVerified: true}

	svc := NewService(st, nil, zerolog.Nop())
	actor := authz.Actor{Type: authz.PrincipalUser, TenantID: tenantID, IsAdmin: true}

	wildcard, err := svc.Create(context.Background(), actor, CreateRequest{ZoneID: zoneID, Address: " *@MAIL.TEST. "})
	if err != nil {
		t.Fatal(err)
	}
	if wildcard.IdentityType != models.SendIdentityDomainWildcard || wildcard.Address != "*@mail.test" {
		t.Fatalf("unexpected wildcard identity: %#v", wildcard)
	}
	if !wildcard.Verified {
		t.Fatal("expected identity to require both TXT and MX verification")
	}

	exact, err := svc.Create(context.Background(), actor, CreateRequest{ZoneID: zoneID, Address: "Team <TEAM@MAIL.TEST>"})
	if err != nil {
		t.Fatal(err)
	}
	if exact.IdentityType != models.SendIdentityExact || exact.Address != "team@mail.test" {
		t.Fatalf("unexpected canonical exact identity: %#v", exact)
	}

	_, err = svc.Create(context.Background(), actor, CreateRequest{ZoneID: zoneID, Address: "x@other.test"})
	requireKind(t, err, app.KindBadRequest)
}

func TestCreateIdentityIsUnverifiedUntilMXAlsoPasses(t *testing.T) {
	st := newIdentityTestStore()
	tenantID := uuid.New()
	zoneID := uuid.New()
	st.zones[zoneID] = &models.DomainZone{ID: zoneID, TenantID: tenantID, Domain: "mail.test", IsVerified: true, MXVerified: false}

	svc := NewService(st, nil, zerolog.Nop())
	actor := authz.Actor{Type: authz.PrincipalUser, TenantID: tenantID, IsAdmin: true}
	identity, err := svc.Create(context.Background(), actor, CreateRequest{ZoneID: zoneID, Address: "x@mail.test"})
	if err != nil {
		t.Fatal(err)
	}
	if identity.Verified {
		t.Fatal("identity must remain unverified until both domain checks pass")
	}
}

func TestDeleteRequiresAdminAndHidesForeignIdentity(t *testing.T) {
	st := newIdentityTestStore()
	tenantID := uuid.New()
	id := uuid.New()
	st.identities[id] = &models.SendIdentity{ID: id, TenantID: tenantID, Address: "x@mail.test"}

	svc := NewService(st, nil, zerolog.Nop())
	user := authz.Actor{Type: authz.PrincipalUser, TenantID: tenantID}
	requireKind(t, svc.Delete(context.Background(), user, id), app.KindForbidden)

	admin := authz.Actor{Type: authz.PrincipalUser, TenantID: uuid.New(), IsAdmin: true}
	requireKind(t, svc.Delete(context.Background(), admin, id), app.KindNotFound)
	if len(st.deleted) != 0 {
		t.Fatalf("expected no deletions, got %v", st.deleted)
	}
}

func TestDeleteRemovesOwnedIdentity(t *testing.T) {
	st := newIdentityTestStore()
	tenantID := uuid.New()
	id := uuid.New()
	st.identities[id] = &models.SendIdentity{ID: id, TenantID: tenantID, Address: "x@mail.test"}

	svc := NewService(st, nil, zerolog.Nop())
	actor := authz.Actor{Type: authz.PrincipalUser, TenantID: tenantID, IsAdmin: true}
	if err := svc.Delete(context.Background(), actor, id); err != nil {
		t.Fatal(err)
	}
	if len(st.deleted) != 1 || st.deleted[0] != id {
		t.Fatalf("expected identity %s deleted, got %v", id, st.deleted)
	}
}
