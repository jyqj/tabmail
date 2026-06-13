package handlers

import (
	"context"
	"net"
	"net/http"

	"encoding/json"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"tabmail/internal/api/middleware"
	"tabmail/internal/app"
	domainapp "tabmail/internal/app/domains"
	"tabmail/internal/authz"
	"tabmail/internal/hooks"
	"tabmail/internal/models"
	"tabmail/internal/policy"
	"tabmail/internal/rawobject"
	"tabmail/internal/resolver"
	"tabmail/internal/store"
)

type domainStore interface {
	app.AuditStore
	ListZones(ctx context.Context, tenantID uuid.UUID) ([]*models.DomainZone, error)
	ListAllZones(ctx context.Context) ([]*models.DomainZone, error)
	ListPublicZones(ctx context.Context) ([]*models.DomainZone, error)
	ListZonesByVisibilities(ctx context.Context, visibilities []models.ResourceVisibility) ([]*models.DomainZone, error)
	EffectiveConfig(ctx context.Context, tenantID uuid.UUID) (*models.EffectiveConfig, error)
	CountZones(ctx context.Context, tenantID uuid.UUID) (int, error)
	CreateZone(ctx context.Context, z *models.DomainZone) error
	DeleteZone(ctx context.Context, id uuid.UUID) error
	UpdateZone(ctx context.Context, z *models.DomainZone) error
	GetZone(ctx context.Context, id uuid.UUID) (*models.DomainZone, error)
	GetZoneByDomain(ctx context.Context, domain string) (*models.DomainZone, error)
	ListRoutes(ctx context.Context, zoneID uuid.UUID) ([]*models.DomainRoute, error)
	CreateRoute(ctx context.Context, r *models.DomainRoute) error
	GetRoute(ctx context.Context, id uuid.UUID) (*models.DomainRoute, error)
	DeleteRoute(ctx context.Context, id uuid.UUID) error
	ListZoneObjectKeys(ctx context.Context, zoneID uuid.UUID) ([]string, error)
	ReleaseRawObjectIfUnreferenced(ctx context.Context, key string, del func(context.Context) error) (bool, error)
	// Send identities
	CreateSendIdentity(ctx context.Context, si *models.SendIdentity) error
	ListSendIdentitiesByZone(ctx context.Context, zoneID uuid.UUID) ([]*models.SendIdentity, error)
	UpdateSendIdentitiesVerifiedByZone(ctx context.Context, zoneID uuid.UUID, verified bool) error
}

type DomainHandler struct {
	service     *domainapp.Service
	store       domainStore
	objectStore store.ObjectStore
	resolver    *resolver.Resolver
	lookupTXT   func(string) ([]string, error)
	lookupMX    func(string) ([]*net.MX, error)
	logger      zerolog.Logger
}

func NewDomainHandler(s domainStore, obj store.ObjectStore, dispatcher *hooks.Dispatcher, expectedMXHost string, namingMode policy.NamingMode, addressSecret string, res *resolver.Resolver, l zerolog.Logger) *DomainHandler {
	service := domainapp.NewService(s, dispatcher, expectedMXHost, namingMode, addressSecret, l)
	return &DomainHandler{service: service, store: s, objectStore: obj, resolver: res, lookupTXT: net.LookupTXT, lookupMX: net.LookupMX, logger: l.With().Str("handler", "domains").Logger()}
}

// SetResolvers overrides DNS resolvers on the underlying service. For test use only.
func (h *DomainHandler) SetResolvers(lookupTXT func(string) ([]string, error), lookupMX func(string) ([]*net.MX, error)) {
	h.lookupTXT = lookupTXT
	h.lookupMX = lookupMX
	h.service.SetResolvers(lookupTXT, lookupMX)
}

func (h *DomainHandler) ListZones(w http.ResponseWriter, r *http.Request) {
	actor := authz.ActorFromContext(r.Context())
	tenant := middleware.TenantFromCtx(r.Context())
	items, err := h.service.ListZones(r.Context(), actor, tenant)
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	ok(w, items)
}

func (h *DomainHandler) ListOpenZones(w http.ResponseWriter, r *http.Request) {
	includeAuthenticated := middleware.AuthModeFromCtx(r.Context()) != middleware.AuthModePublic
	items, err := h.service.ListOpenZones(r.Context(), includeAuthenticated)
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	ok(w, items)
}

func (h *DomainHandler) AdminListZones(w http.ResponseWriter, r *http.Request) {
	actor := authz.ActorFromContext(r.Context())
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

func (h *DomainHandler) CreateZone(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Domain string `json:"domain"`
	}
	if err := decodeBody(r, &body); err != nil || body.Domain == "" {
		errBadRequest(w, "domain is required")
		return
	}
	actor := authz.ActorFromContext(r.Context())
	tenant := middleware.TenantFromCtx(r.Context())
	item, err := h.service.CreateZone(r.Context(), actor, tenant, body.Domain)
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	created(w, item)
}

func (h *DomainHandler) AdminUpdateZoneAccess(w http.ResponseWriter, r *http.Request) {
	zoneID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid id")
		return
	}
	var body struct {
		Visibility            models.ResourceVisibility `json:"visibility"`
		AllowRandomSubdomains *bool                     `json:"allow_random_subdomains,omitempty"`
	}
	if err := decodeBody(r, &body); err != nil {
		errBadRequest(w, "invalid body")
		return
	}
	actor := authz.ActorFromContext(r.Context())
	tenant := middleware.TenantFromCtx(r.Context())
	if actor.IsSuperAdmin {
		tenant = nil
	}
	item, err := h.service.UpdateZoneAccess(r.Context(), actor, tenant, zoneID, domainapp.ZoneAccessInput{
		Visibility:            body.Visibility,
		AllowRandomSubdomains: body.AllowRandomSubdomains,
	})
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	ok(w, item)
}

func (h *DomainHandler) DeleteZone(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid id")
		return
	}

	actor := authz.ActorFromContext(r.Context())
	if _, err := h.service.ManagedZone(r.Context(), actor, id); err != nil {
		respondAppError(w, h.logger, err)
		return
	}

	// Collect object keys after authorization but before CASCADE delete removes rows.
	var objectKeys []string
	if h.objectStore != nil {
		objectKeys, err = h.store.ListZoneObjectKeys(r.Context(), id)
		if err != nil {
			h.logger.Warn().Err(err).Stringer("zone_id", id).Msg("list zone object keys before delete")
			// Non-fatal: proceed with deletion even if we can't list keys.
		}
	}
	if err := h.service.DeleteZone(r.Context(), actor, id); err != nil {
		respondAppError(w, h.logger, err)
		return
	}

	// Clean up orphaned object store blobs asynchronously.
	if h.objectStore != nil && len(objectKeys) > 0 {
		ctx := context.Background()
		go func() {
			for _, key := range objectKeys {
				if _, err := rawobject.Release(ctx, h.store, h.objectStore, key); err != nil {
					h.logger.Warn().Err(err).Str("key", key).Msg("release raw object during zone delete")
				}
			}
		}()
	}

	noContent(w)
}

func (h *DomainHandler) TriggerVerify(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid id")
		return
	}
	actor := authz.ActorFromContext(r.Context())
	zone, checks, err := h.service.TriggerVerify(r.Context(), actor, id)
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	ok(w, map[string]any{
		"id":           zone.ID,
		"domain":       zone.Domain,
		"txt_record":   zone.TXTRecord,
		"is_verified":  zone.IsVerified,
		"mx_verified":  zone.MXVerified,
		"dkim_enabled": zone.DKIMEnabled,
		"checks":       checks,
		"hint":         "Add TXT record: " + zone.TXTRecord,
	})
}

func (h *DomainHandler) VerificationStatus(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid id")
		return
	}
	actor := authz.ActorFromContext(r.Context())
	item, err := h.service.VerificationStatus(r.Context(), actor, id)
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	ok(w, item)
}

func (h *DomainHandler) ListRoutes(w http.ResponseWriter, r *http.Request) {
	zoneID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid id")
		return
	}
	actor := authz.ActorFromContext(r.Context())
	items, err := h.service.ListRoutes(r.Context(), actor, zoneID)
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	ok(w, items)
}

func (h *DomainHandler) SuggestAddress(w http.ResponseWriter, r *http.Request) {
	zoneID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid id")
		return
	}
	useSubdomain := r.URL.Query().Get("subdomain") == "true" || r.URL.Query().Get("subdomain") == "1"
	actor := authz.ActorFromContext(r.Context())
	canManage := middleware.HasScope(r.Context(), "domains:write")
	item, err := h.service.SuggestAddress(r.Context(), actor, zoneID, canManage, useSubdomain)
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	ok(w, item)
}

func (h *DomainHandler) SuggestOpenAddress(w http.ResponseWriter, r *http.Request) {
	zoneID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid id")
		return
	}
	useSubdomain := r.URL.Query().Get("subdomain") == "true" || r.URL.Query().Get("subdomain") == "1"
	includeAuthenticated := middleware.AuthModeFromCtx(r.Context()) != middleware.AuthModePublic
	item, err := h.service.SuggestOpenAddress(r.Context(), zoneID, includeAuthenticated, useSubdomain)
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	ok(w, item)
}

func (h *DomainHandler) CreateRoute(w http.ResponseWriter, r *http.Request) {
	zoneID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid id")
		return
	}
	var body struct {
		RouteType              models.RouteType  `json:"route_type"`
		MatchValue             string            `json:"match_value"`
		RangeStart             *int              `json:"range_start,omitempty"`
		RangeEnd               *int              `json:"range_end,omitempty"`
		AutoCreateMailbox      *bool             `json:"auto_create_mailbox,omitempty"`
		RetentionHoursOverride *int              `json:"retention_hours_override,omitempty"`
		AccessModeDefault      models.AccessMode `json:"access_mode_default,omitempty"`
	}
	if err := decodeBody(r, &body); err != nil {
		errBadRequest(w, "invalid body")
		return
	}
	actor := authz.ActorFromContext(r.Context())
	item, err := h.service.CreateRoute(r.Context(), actor, zoneID, domainapp.CreateRouteInput{
		RouteType:              body.RouteType,
		MatchValue:             body.MatchValue,
		RangeStart:             body.RangeStart,
		RangeEnd:               body.RangeEnd,
		AutoCreateMailbox:      body.AutoCreateMailbox,
		RetentionHoursOverride: body.RetentionHoursOverride,
		AccessModeDefault:      body.AccessModeDefault,
	})
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	created(w, item)
}

func (h *DomainHandler) DeleteRoute(w http.ResponseWriter, r *http.Request) {
	routeID, err := uuid.Parse(chi.URLParam(r, "routeId"))
	if err != nil {
		errBadRequest(w, "invalid route id")
		return
	}
	actor := authz.ActorFromContext(r.Context())
	if err := h.service.DeleteRoute(r.Context(), actor, routeID); err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	noContent(w)
}

// ExplainRoute handles POST /api/v1/domains/{id}/routes/explain
func (h *DomainHandler) ExplainRoute(w http.ResponseWriter, r *http.Request) {
	if h.resolver == nil {
		errInternal(w)
		return
	}
	zoneID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid id")
		return
	}
	var body struct {
		Address string `json:"address"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Address == "" {
		errBadRequest(w, "address is required")
		return
	}
	actor := authz.ActorFromContext(r.Context())
	zone, err := h.service.ManagedZone(r.Context(), actor, zoneID)
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	result, err := h.resolver.Explain(r.Context(), body.Address)
	if err != nil {
		h.logger.Err(err).Str("address", body.Address).Msg("explain route failed")
		errInternal(w)
		return
	}
	if result.ZoneID != "" && result.ZoneID != zone.ID.String() {
		errForbidden(w, "address resolves outside this domain")
		return
	}
	ok(w, result)
}
