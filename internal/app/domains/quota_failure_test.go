package domainapp

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/app"
	"tabmail/internal/models"
)

type quotaFailureStore struct {
	*domainTestStore
	listErr                                error
	listCalls, creates, identities, audits int
}

func (s *quotaFailureStore) ListZones(ctx context.Context, tenant uuid.UUID) ([]*models.DomainZone, error) {
	s.listCalls++
	zones, _ := s.domainTestStore.ListZones(ctx, tenant)
	return zones, s.listErr
}
func (s *quotaFailureStore) CreateZone(ctx context.Context, zone *models.DomainZone) error {
	s.creates++
	return s.domainTestStore.CreateZone(ctx, zone)
}
func (s *quotaFailureStore) CreateSendIdentity(context.Context, *models.SendIdentity) error {
	s.identities++
	return nil
}
func (s *quotaFailureStore) InsertAudit(context.Context, *models.AuditEntry) error {
	s.audits++
	return nil
}

func TestCreateZoneQuotaLookupFailureStopsSideEffects(t *testing.T) {
	for _, cause := range []error{errors.New("quota query unavailable"), context.Canceled, context.DeadlineExceeded} {
		for _, partial := range []bool{false, true} {
			t.Run(cause.Error()+"/partial="+map[bool]string{false: "no", true: "yes"}[partial], func(t *testing.T) {
				tenant, owner := &models.Tenant{ID: uuid.New()}, uuid.New()
				st := &quotaFailureStore{domainTestStore: newDomainTestStore(), listErr: cause}
				if partial {
					id := uuid.New()
					st.zones[id] = &models.DomainZone{ID: id, TenantID: tenant.ID, OwnerUserID: &owner, Domain: "existing.example"}
				}
				actor := userActor(tenant.ID, owner)
				actor.Permission = &models.EffectivePermission{CanCreateDomains: true, MaxDomains: 2, DomainAccessMode: "all"}
				inv := &fakeResolverInvalidator{}
				svc := NewService(st, nil, "mx.example", inv, zerolog.Nop())
				zone, err := svc.CreateZone(context.Background(), actor, tenant, "new.example")
				if zone != nil || !errors.Is(err, cause) {
					t.Errorf("failed quota lookup must retain cause and reject creation: zone=%v err=%v", zone, err)
				}
				if e, ok := app.As(err); !ok || e.Kind != app.KindInternal {
					t.Errorf("lookup failure must be internal, not permission denial: %v", err)
				}
				if st.creates != 0 || st.identities != 0 || st.audits != 0 || len(inv.zonesCleared) != 0 {
					t.Errorf("quota failure performed side effects: create=%d identity=%d audit=%d invalidations=%d", st.creates, st.identities, st.audits, len(inv.zonesCleared))
				}
			})
		}
	}
}

func TestCreateZoneQuotaSuccessfulBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name         string
		limit, owned int
		allowed      bool
	}{
		{"below", 2, 1, true}, {"at_limit", 2, 2, false}, {"zero_means_unlimited", 0, 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tenant, owner, other := &models.Tenant{ID: uuid.New()}, uuid.New(), uuid.New()
			st := &quotaFailureStore{domainTestStore: newDomainTestStore()}
			for i := 0; i < tc.owned; i++ {
				id := uuid.New()
				st.zones[id] = &models.DomainZone{ID: id, TenantID: tenant.ID, OwnerUserID: &owner, Domain: id.String() + ".example"}
			}
			id := uuid.New()
			st.zones[id] = &models.DomainZone{ID: id, TenantID: tenant.ID, OwnerUserID: &other, Domain: "other.example"}
			actor := userActor(tenant.ID, owner)
			actor.Permission = &models.EffectivePermission{CanCreateDomains: true, MaxDomains: tc.limit, DomainAccessMode: "all"}
			svc := NewService(st, nil, "mx.example", nil, zerolog.Nop())
			zone, err := svc.CreateZone(context.Background(), actor, tenant, "new.example")
			if tc.allowed {
				if err != nil || zone == nil || st.creates != 1 {
					t.Fatalf("expected permitted creation: zone=%v err=%v creates=%d", zone, err, st.creates)
				}
			} else if e, ok := app.As(err); !ok || e.Kind != app.KindForbidden || st.creates != 0 {
				t.Fatalf("expected quota refusal without write: %v creates=%d", err, st.creates)
			}
		})
	}
}

func TestCreateZoneAdminBypassesOwnerQuotaLookup(t *testing.T) {
	tenant := &models.Tenant{ID: uuid.New()}
	st := &quotaFailureStore{domainTestStore: newDomainTestStore(), listErr: errors.New("unused quota lookup")}
	actor := adminActor(tenant.ID)
	actor.Permission = &models.EffectivePermission{MaxDomains: 1}
	zone, err := NewService(st, nil, "mx.example", nil, zerolog.Nop()).CreateZone(context.Background(), actor, tenant, "admin.example")
	if err != nil || zone == nil || st.listCalls != 0 {
		t.Fatalf("admin semantics changed: zone=%v err=%v list=%d", zone, err, st.listCalls)
	}
}
