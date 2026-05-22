package handlers

import (
	"context"
	"net"
	"net/http"

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
	"tabmail/internal/store"
)

type domainStore interface {
	app.AuditStore
	app.PrincipalStore
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
	CountRawObjectReferences(ctx context.Context, objectKey string) (int, error)
	// Send identities & grants
	CreateSendIdentity(ctx context.Context, si *models.SendIdentity) error
	CreateSendAsGrant(ctx context.Context, g *models.SendAsGrant) error
	ListSendIdentitiesByZone(ctx context.Context, zoneID uuid.UUID) ([]*models.SendIdentity, error)
	UpdateSendIdentitiesVerifiedByZone(ctx context.Context, zoneID uuid.UUID, verified bool) error
	// Zone grants
	CreateZoneGrant(ctx context.Context, g *models.ZoneGrant) error
	GetHighestZoneRole(ctx context.Context, zoneID uuid.UUID, principalType string, principalID uuid.UUID) (models.ZoneGrantRole, error)
	ListGrantedZoneIDs(ctx context.Context, principalType string, principalID uuid.UUID) ([]uuid.UUID, error)
	ListZoneGrants(ctx context.Context, zoneID uuid.UUID) ([]*models.ZoneGrant, error)
	DeleteZoneGrant(ctx context.Context, id uuid.UUID) error
	GetZoneGrant(ctx context.Context, zoneID uuid.UUID, principalType string, principalID uuid.UUID) (*models.ZoneGrant, error)
}

type DomainHandler struct {
	service     *domainapp.Service
	store       domainStore
	objectStore store.ObjectStore
	lookupTXT   func(string) ([]string, error)
	lookupMX    func(string) ([]*net.MX, error)
	logger      zerolog.Logger
}

func NewDomainHandler(s domainStore, obj store.ObjectStore, dispatcher *hooks.Dispatcher, expectedMXHost string, namingMode policy.NamingMode, addressSecret string, l zerolog.Logger) *DomainHandler {
	service := domainapp.NewService(s, dispatcher, expectedMXHost, namingMode, addressSecret, l)
	return &DomainHandler{service: service, store: s, objectStore: obj, lookupTXT: net.LookupTXT, lookupMX: net.LookupMX, logger: l.With().Str("handler", "domains").Logger()}
}

// SetResolvers overrides DNS resolvers on the underlying service. For test use only.
func (h *DomainHandler) SetResolvers(lookupTXT func(string) ([]string, error), lookupMX func(string) ([]*net.MX, error)) {
	h.lookupTXT = lookupTXT
	h.lookupMX = lookupMX
	h.service.SetResolvers(lookupTXT, lookupMX)
}

func (h *DomainHandler) ListZones(w http.ResponseWriter, r *http.Request) {
	tenant, isAdmin, ownerUserID, tenantWide := domainActorParams(r)
	actor := authz.ActorFromContext(r.Context())
	items, err := h.service.ListZones(r.Context(), tenant, isAdmin, ownerUserID, tenantWide)
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	items = filterZonesForActor(actor, items)
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
	if actor.IsPlatformAdmin {
		items, err := h.service.ListAllZones(r.Context(), true)
		if err != nil {
			respondAppError(w, h.logger, err)
			return
		}
		ok(w, items)
		return
	}

	tenant := middleware.TenantFromCtx(r.Context())
	items, err := h.service.ListZones(r.Context(), tenant, true, nil, true)
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
	tenant, isAdmin, ownerUserID, tenantWide := domainActorParams(r)
	actor := authz.ActorFromContext(r.Context())
	item, err := h.service.CreateZone(r.Context(), tenant, isAdmin, ownerUserID, tenantWide, actor.Permission, body.Domain, actorFromRequest(r))
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
	tenant, isAdmin, _, _ := domainActorParams(r)
	if actor.IsPlatformAdmin {
		tenant = nil
	}
	item, err := h.service.UpdateZoneAccess(r.Context(), zoneID, tenant, isAdmin, domainapp.ZoneAccessInput{
		Visibility:            body.Visibility,
		AllowRandomSubdomains: body.AllowRandomSubdomains,
	}, actorFromRequest(r))
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

	tenant, isAdmin, ownerUserID, tenantWide := domainActorParams(r)
	actor := authz.ActorFromContext(r.Context())
	if !ensureZoneAllowedForActor(w, actor, id) {
		return
	}
	if _, err := h.service.ManagedZone(r.Context(), id, tenant, isAdmin, ownerUserID, tenantWide); err != nil {
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
	if err := h.service.DeleteZone(r.Context(), id, tenant, isAdmin, ownerUserID, tenantWide, actorFromRequest(r)); err != nil {
		respondAppError(w, h.logger, err)
		return
	}

	// Clean up orphaned object store blobs asynchronously.
	if h.objectStore != nil && len(objectKeys) > 0 {
		ctx := context.Background()
		go func() {
			for _, key := range objectKeys {
				refs, err := h.store.CountRawObjectReferences(ctx, key)
				if err != nil {
					h.logger.Warn().Err(err).Str("key", key).Msg("count object references during zone delete")
					continue
				}
				if refs == 0 {
					if err := h.objectStore.Delete(ctx, key); err != nil {
						h.logger.Warn().Err(err).Str("key", key).Msg("delete raw object during zone delete")
					}
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
	tenant, isAdmin, ownerUserID, tenantWide := domainActorParams(r)
	actor := authz.ActorFromContext(r.Context())
	if !ensureZoneAllowedForActor(w, actor, id) {
		return
	}
	zone, checks, err := h.service.TriggerVerify(r.Context(), id, tenant, isAdmin, ownerUserID, tenantWide, actorFromRequest(r))
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
	tenant, isAdmin, ownerUserID, tenantWide := domainActorParams(r)
	actor := authz.ActorFromContext(r.Context())
	if !ensureZoneAllowedForActor(w, actor, id) {
		return
	}
	item, err := h.service.VerificationStatus(r.Context(), id, tenant, isAdmin, ownerUserID, tenantWide)
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
	tenant, isAdmin, ownerUserID, tenantWide := domainActorParams(r)
	actor := authz.ActorFromContext(r.Context())
	if !ensureZoneAllowedForActor(w, actor, zoneID) {
		return
	}
	items, err := h.service.ListRoutes(r.Context(), zoneID, tenant, isAdmin, ownerUserID, tenantWide)
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
	tenant, isAdmin, ownerUserID, tenantWide := domainActorParams(r)
	actor := authz.ActorFromContext(r.Context())
	if !ensureZoneAllowedForActor(w, actor, zoneID) {
		return
	}
	canManage := middleware.HasScope(r.Context(), "domains:write")
	item, err := h.service.SuggestAddress(r.Context(), zoneID, tenant, isAdmin, ownerUserID, tenantWide, canManage, useSubdomain)
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
	tenant, isAdmin, ownerUserID, tenantWide := domainActorParams(r)
	actor := authz.ActorFromContext(r.Context())
	if !ensureZoneAllowedForActor(w, actor, zoneID) {
		return
	}
	item, err := h.service.CreateRoute(r.Context(), zoneID, tenant, isAdmin, ownerUserID, tenantWide, actor.Permission, domainapp.CreateRouteInput{
		RouteType:              body.RouteType,
		MatchValue:             body.MatchValue,
		RangeStart:             body.RangeStart,
		RangeEnd:               body.RangeEnd,
		AutoCreateMailbox:      body.AutoCreateMailbox,
		RetentionHoursOverride: body.RetentionHoursOverride,
		AccessModeDefault:      body.AccessModeDefault,
	}, actorFromRequest(r))
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
	if route, err := h.store.GetRoute(r.Context(), routeID); err == nil && route != nil {
		if !ensureZoneAllowedForActor(w, actor, route.ZoneID) {
			return
		}
	} else if err != nil {
		h.logger.Err(err).Stringer("route_id", routeID).Msg("lookup route for authz")
		errInternal(w)
		return
	}
	tenant, isAdmin, ownerUserID, tenantWide := domainActorParams(r)
	if err := h.service.DeleteRoute(r.Context(), routeID, tenant, isAdmin, ownerUserID, tenantWide, actorFromRequest(r)); err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	noContent(w)
}

// ListZoneGrants handles GET /api/v1/domains/{id}/grants
func (h *DomainHandler) ListZoneGrants(w http.ResponseWriter, r *http.Request) {
	zoneID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid id")
		return
	}
	tenant, isAdmin, ownerUserID, tenantWide := domainActorParams(r)
	actor := authz.ActorFromContext(r.Context())
	if !ensureZoneAllowedForActor(w, actor, zoneID) {
		return
	}
	items, err := h.service.ListZoneGrants(r.Context(), zoneID, tenant, isAdmin, ownerUserID, tenantWide)
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	ok(w, items)
}

// CreateZoneGrant handles POST /api/v1/domains/{id}/grants
func (h *DomainHandler) CreateZoneGrant(w http.ResponseWriter, r *http.Request) {
	zoneID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid id")
		return
	}
	var body struct {
		PrincipalType string               `json:"principal_type"`
		PrincipalID   uuid.UUID            `json:"principal_id"`
		Role          models.ZoneGrantRole `json:"role"`
	}
	if err := decodeBody(r, &body); err != nil {
		errBadRequest(w, "invalid body")
		return
	}
	grant := &models.ZoneGrant{
		PrincipalType: body.PrincipalType,
		PrincipalID:   body.PrincipalID,
		Role:          body.Role,
	}
	tenant, isAdmin, ownerUserID, tenantWide := domainActorParams(r)
	actor := authz.ActorFromContext(r.Context())
	if !ensureZoneAllowedForActor(w, actor, zoneID) {
		return
	}
	item, err := h.service.CreateZoneGrant(r.Context(), zoneID, tenant, isAdmin, ownerUserID, tenantWide, grant, actorFromRequest(r))
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	created(w, item)
}

// DeleteZoneGrant handles DELETE /api/v1/domains/{id}/grants/{grantId}
func (h *DomainHandler) DeleteZoneGrant(w http.ResponseWriter, r *http.Request) {
	zoneID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid id")
		return
	}
	grantID, err := uuid.Parse(chi.URLParam(r, "grantId"))
	if err != nil {
		errBadRequest(w, "invalid grant id")
		return
	}
	tenant, isAdmin, ownerUserID, tenantWide := domainActorParams(r)
	actor := authz.ActorFromContext(r.Context())
	if !ensureZoneAllowedForActor(w, actor, zoneID) {
		return
	}
	if err := h.service.DeleteZoneGrant(r.Context(), zoneID, grantID, tenant, isAdmin, ownerUserID, tenantWide, actorFromRequest(r)); err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	noContent(w)
}

func ensureZoneAllowedForActor(w http.ResponseWriter, actor authz.Actor, zoneID uuid.UUID) bool {
	if actor.IsPlatformAdmin || actor.IsTenantAdmin {
		return true
	}
	if actor.Permission == nil || len(actor.Permission.AllowedZoneIDs) == 0 {
		return true
	}
	for _, id := range actor.Permission.AllowedZoneIDs {
		if id == zoneID {
			return true
		}
	}
	errForbidden(w, "zone not in allowed list")
	return false
}

func filterZonesForActor(actor authz.Actor, zones []*models.DomainZone) []*models.DomainZone {
	if actor.IsPlatformAdmin || actor.IsTenantAdmin || actor.Permission == nil || len(actor.Permission.AllowedZoneIDs) == 0 {
		return zones
	}
	allowed := make(map[uuid.UUID]struct{}, len(actor.Permission.AllowedZoneIDs))
	for _, id := range actor.Permission.AllowedZoneIDs {
		allowed[id] = struct{}{}
	}
	out := make([]*models.DomainZone, 0, len(zones))
	for _, z := range zones {
		if _, ok := allowed[z.ID]; ok {
			out = append(out, z)
		}
	}
	return out
}
