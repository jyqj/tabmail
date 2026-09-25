package handlers

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"tabmail/internal/api/middleware"
	"tabmail/internal/app"
	domainapp "tabmail/internal/app/domains"
	"tabmail/internal/authz"
	"tabmail/internal/hooks"
	"tabmail/internal/models"
	"tabmail/internal/rawobject"
	"tabmail/internal/resolver"
	"tabmail/internal/store"
)

type domainStore interface {
	app.AuditStore
	ListZones(ctx context.Context, tenantID uuid.UUID) ([]*models.DomainZone, error)
	ListZonesScoped(ctx context.Context, scope authz.ZoneListFilter) ([]*models.DomainZone, error)
	ListAllZones(ctx context.Context) ([]*models.DomainZone, error)
	ListPublicZones(ctx context.Context) ([]*models.DomainZone, error)
	ListZonesByVisibilities(ctx context.Context, visibilities []models.ResourceVisibility) ([]*models.DomainZone, error)
	EffectiveConfig(ctx context.Context, tenantID uuid.UUID) (*models.EffectiveConfig, error)
	CountZones(ctx context.Context, tenantID uuid.UUID) (int, error)
	CreateZone(ctx context.Context, z *models.DomainZone) error
	DeleteZone(ctx context.Context, id uuid.UUID, entry models.AuditEntry) error
	UpdateZone(ctx context.Context, z *models.DomainZone) error
	GetZone(ctx context.Context, id uuid.UUID) (*models.DomainZone, error)
	GetZoneByDomain(ctx context.Context, domain string) (*models.DomainZone, error)
	ListZoneObjectKeys(ctx context.Context, zoneID uuid.UUID) ([]string, error)
	// Send identities
	CreateSendIdentity(ctx context.Context, si *models.SendIdentity) error
	ListSendIdentitiesByZone(ctx context.Context, zoneID uuid.UUID) ([]*models.SendIdentity, error)
	UpdateSendIdentitiesVerifiedByZone(ctx context.Context, zoneID uuid.UUID, verified bool) error
}

type DomainHandler struct {
	service     *domainapp.Service
	store       domainStore
	objectStore store.ObjectStore
	objects     *rawobject.Store
	resolver    *resolver.Resolver
	logger      zerolog.Logger
}

func NewDomainHandler(s domainStore, obj store.ObjectStore, objects *rawobject.Store, dispatcher *hooks.Dispatcher, expectedMXHost string, res *resolver.Resolver, l zerolog.Logger) *DomainHandler {
	// The resolver doubles as the zone/route cache invalidator. Pass nil when
	// no resolver is configured so the service's nil-guard skips invalidation
	// (a typed-nil *resolver.Resolver would otherwise satisfy the interface and
	// panic on call).
	var inv domainapp.ResolverInvalidator
	if res != nil {
		inv = res
	}
	service := domainapp.NewService(s, dispatcher, expectedMXHost, inv, l)
	return &DomainHandler{service: service, store: s, objectStore: obj, objects: objects, resolver: res, logger: l.With().Str("handler", "domains").Logger()}
}

// Service exposes the domain application service so narrow surfaces (the
// company domain onboarding routes) reuse the same instance — one cache
// invalidator, one DNS resolver configuration.
func (h *DomainHandler) Service() *domainapp.Service { return h.service }

func (h *DomainHandler) ListZones(w http.ResponseWriter, r *http.Request) {
	actor := middleware.ActorFromContext(r.Context())
	tenant := middleware.TenantFromCtx(r.Context())
	items, err := h.service.ListZones(r.Context(), actor, tenant)
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	ok(w, items)
}

func (h *DomainHandler) AdminListZones(w http.ResponseWriter, r *http.Request) {
	actor := middleware.ActorFromContext(r.Context())
	if actor.IsSuperAdmin {
		items, err := h.service.ListAllZones(r.Context(), actor)
		if err != nil {
			respondAppError(w, h.logger, err)
			return
		}
		ok(w, items)
		return
	}

	tenant := middleware.TenantFromCtx(r.Context())
	items, err := h.service.ListZones(r.Context(), actor, tenant)
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	ok(w, items)
}
