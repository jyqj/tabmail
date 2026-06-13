package mailboxapp

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/models"
	"tabmail/internal/policy"
	"tabmail/internal/store"
)

func userActor(tenantID uuid.UUID, userID uuid.UUID) authz.Actor {
	return authz.Actor{Type: authz.PrincipalUser, ID: userID, TenantID: tenantID, Role: models.RoleUser}
}

func adminActor(tenantID uuid.UUID) authz.Actor {
	return authz.Actor{Type: authz.PrincipalUser, ID: uuid.New(), TenantID: tenantID, Role: models.RoleAdmin, IsAdmin: true}
}

type mailboxTestStore struct {
	zones         map[uuid.UUID]*models.DomainZone
	mailboxes     map[uuid.UUID]*models.Mailbox
	usedGlobalGet bool
	usedTenantGet bool
}

func newMailboxTestStore() *mailboxTestStore {
	return &mailboxTestStore{zones: map[uuid.UUID]*models.DomainZone{}, mailboxes: map[uuid.UUID]*models.Mailbox{}}
}

func (s *mailboxTestStore) InsertAudit(context.Context, *models.AuditEntry) error { return nil }
func (s *mailboxTestStore) GetZone(_ context.Context, id uuid.UUID) (*models.DomainZone, error) {
	if z := s.zones[id]; z != nil {
		cp := *z
		return &cp, nil
	}
	return nil, nil
}
func (s *mailboxTestStore) GetZoneByDomain(_ context.Context, domain string) (*models.DomainZone, error) {
	for _, z := range s.zones {
		if z.Domain == domain {
			cp := *z
			return &cp, nil
		}
	}
	return nil, nil
}
func (s *mailboxTestStore) ListZones(_ context.Context, tenantID uuid.UUID) ([]*models.DomainZone, error) {
	var out []*models.DomainZone
	for _, z := range s.zones {
		if z.TenantID == tenantID {
			cp := *z
			out = append(out, &cp)
		}
	}
	return out, nil
}
func (s *mailboxTestStore) EffectiveConfig(context.Context, uuid.UUID) (*models.EffectiveConfig, error) {
	return &models.EffectiveConfig{MaxDomains: 100, MaxMailboxesPerDomain: 100}, nil
}
func (s *mailboxTestStore) CountMailboxes(context.Context, uuid.UUID) (int, error) { return 0, nil }
func (s *mailboxTestStore) CreateMailbox(_ context.Context, m *models.Mailbox) error {
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	cp := *m
	s.mailboxes[m.ID] = &cp
	return nil
}
func (s *mailboxTestStore) ListMailboxes(context.Context, uuid.UUID, models.Page) ([]*models.Mailbox, int, error) {
	return nil, 0, nil
}
func (s *mailboxTestStore) ListMailboxesByZones(context.Context, uuid.UUID, []uuid.UUID, models.Page) ([]*models.Mailbox, int, error) {
	return nil, 0, nil
}
func (s *mailboxTestStore) GetMailbox(_ context.Context, id uuid.UUID) (*models.Mailbox, error) {
	s.usedGlobalGet = true
	if m := s.mailboxes[id]; m != nil {
		cp := *m
		return &cp, nil
	}
	return nil, nil
}
func (s *mailboxTestStore) ForTenant(tenantID uuid.UUID) store.TenantScoped {
	return &mailboxTestTenantView{store: s, tenantID: tenantID}
}

// mailboxTestTenantView implements store.TenantScoped over the test store's
// maps; cross-tenant rows read as not found (nil, nil), like the real views.
type mailboxTestTenantView struct {
	store    *mailboxTestStore
	tenantID uuid.UUID
}

func (v *mailboxTestTenantView) GetMailbox(_ context.Context, id uuid.UUID) (*models.Mailbox, error) {
	v.store.usedTenantGet = true
	if m := v.store.mailboxes[id]; m != nil && m.TenantID == v.tenantID {
		cp := *m
		return &cp, nil
	}
	return nil, nil
}

func (v *mailboxTestTenantView) GetMailboxByAddress(_ context.Context, address string) (*models.Mailbox, error) {
	for _, m := range v.store.mailboxes {
		if m.FullAddress == address && m.TenantID == v.tenantID {
			cp := *m
			return &cp, nil
		}
	}
	return nil, nil
}

func (v *mailboxTestTenantView) GetMessage(context.Context, uuid.UUID) (*models.Message, error) {
	return nil, nil
}
func (s *mailboxTestStore) GetMailboxByAddress(_ context.Context, address string) (*models.Mailbox, error) {
	for _, m := range s.mailboxes {
		if m.FullAddress == address {
			cp := *m
			return &cp, nil
		}
	}
	return nil, nil
}
func (s *mailboxTestStore) ListMailboxObjectKeys(context.Context, uuid.UUID) ([]string, error) {
	return nil, nil
}
func (s *mailboxTestStore) DeleteMailbox(_ context.Context, id uuid.UUID) error {
	delete(s.mailboxes, id)
	return nil
}
func (s *mailboxTestStore) ReleaseRawObjectIfUnreferenced(context.Context, string, func(context.Context) error) (bool, error) {
	return false, nil
}

func TestDeleteTenantAdminUsesTenantScopedMailboxLookup(t *testing.T) {
	ctx := context.Background()
	st := newMailboxTestStore()
	tenant := &models.Tenant{ID: uuid.New(), Name: "tenant"}
	zone := &models.DomainZone{ID: uuid.New(), TenantID: tenant.ID, Domain: "tenant.example"}
	mb := &models.Mailbox{ID: uuid.New(), TenantID: tenant.ID, ZoneID: zone.ID, LocalPart: "inbox", ResolvedDomain: zone.Domain, FullAddress: "inbox@tenant.example"}
	st.zones[zone.ID] = zone
	st.mailboxes[mb.ID] = mb

	svc := NewService(st, nil, nil, policy.NamingFull, false, "secret", zerolog.Nop())
	if err := svc.Delete(ctx, adminActor(tenant.ID), tenant, mb.ID); err != nil {
		t.Fatalf("tenant admin should delete own tenant mailbox: %v", err)
	}
	if !st.usedTenantGet {
		t.Fatal("expected tenant-scoped mailbox lookup")
	}
	if st.usedGlobalGet {
		t.Fatal("tenant-scoped delete must not use global GetMailbox")
	}
	if st.mailboxes[mb.ID] != nil {
		t.Fatal("mailbox should be deleted")
	}
}

func TestDeleteTenantAdminRejectsCrossTenantMailboxID(t *testing.T) {
	ctx := context.Background()
	st := newMailboxTestStore()
	tenantA := &models.Tenant{ID: uuid.New(), Name: "tenant-a"}
	tenantB := &models.Tenant{ID: uuid.New(), Name: "tenant-b"}
	zoneB := &models.DomainZone{ID: uuid.New(), TenantID: tenantB.ID, Domain: "b.example"}
	mbB := &models.Mailbox{ID: uuid.New(), TenantID: tenantB.ID, ZoneID: zoneB.ID, LocalPart: "inbox", ResolvedDomain: zoneB.Domain, FullAddress: "inbox@b.example"}
	st.zones[zoneB.ID] = zoneB
	st.mailboxes[mbB.ID] = mbB

	svc := NewService(st, nil, nil, policy.NamingFull, false, "secret", zerolog.Nop())
	err := svc.Delete(ctx, adminActor(tenantA.ID), tenantA, mbB.ID)
	if err == nil {
		t.Fatal("expected cross-tenant mailbox delete to be rejected")
	}
	if appErr, ok := app.As(err); !ok || appErr.Kind != app.KindNotFound {
		t.Fatalf("expected tenant-scoped not_found, got %#v", err)
	}
	if !st.usedTenantGet {
		t.Fatal("expected tenant-scoped mailbox lookup")
	}
	if st.usedGlobalGet {
		t.Fatal("cross-tenant tenant-scoped delete must not use global GetMailbox")
	}
	if st.mailboxes[mbB.ID] == nil {
		t.Fatal("cross-tenant mailbox must remain")
	}
}

func TestCreateRejectsCrossTenantZoneAccess(t *testing.T) {
	ctx := context.Background()
	st := newMailboxTestStore()
	tenantA := &models.Tenant{ID: uuid.New(), Name: "tenant-a"}
	tenantB := &models.Tenant{ID: uuid.New(), Name: "tenant-b"}
	userA := uuid.New()
	zoneB := &models.DomainZone{ID: uuid.New(), TenantID: tenantB.ID, Domain: "b.example"}
	st.zones[zoneB.ID] = zoneB

	svc := NewService(st, nil, nil, policy.NamingFull, false, "secret", zerolog.Nop())
	_, err := svc.Create(ctx, userActor(tenantA.ID, userA), tenantA, CreateRequest{Address: "inbox@b.example"})
	if err == nil {
		t.Fatal("expected cross-tenant zone access to be rejected")
	}
	if appErr, ok := app.As(err); !ok || appErr.Kind != app.KindForbidden {
		t.Fatalf("expected forbidden app error, got %#v", err)
	}
}
