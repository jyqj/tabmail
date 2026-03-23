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
	"tabmail/internal/hooks"
	"tabmail/internal/models"
	"tabmail/internal/policy"
)

type domainStore interface {
	app.AuditStore
	ListZones(ctx context.Context, tenantID uuid.UUID) ([]*models.DomainZone, error)
	EffectiveConfig(ctx context.Context, tenantID uuid.UUID) (*models.EffectiveConfig, error)
	CountZones(ctx context.Context, tenantID uuid.UUID) (int, error)
	CreateZone(ctx context.Context, z *models.DomainZone) error
	DeleteZone(ctx context.Context, id uuid.UUID) error
	UpdateZone(ctx context.Context, z *models.DomainZone) error
	GetZone(ctx context.Context, id uuid.UUID) (*models.DomainZone, error)
	ListRoutes(ctx context.Context, zoneID uuid.UUID) ([]*models.DomainRoute, error)
	CreateRoute(ctx context.Context, r *models.DomainRoute) error
	GetRoute(ctx context.Context, id uuid.UUID) (*models.DomainRoute, error)
	DeleteRoute(ctx context.Context, id uuid.UUID) error
}

type DomainHandler struct {
	service   *domainapp.Service
	lookupTXT func(string) ([]string, error)
	lookupMX  func(string) ([]*net.MX, error)
	logger    zerolog.Logger
}

func NewDomainHandler(s domainStore, dispatcher *hooks.Dispatcher, expectedMXHost string, namingMode policy.NamingMode, addressSecret string, l zerolog.Logger) *DomainHandler {
	service := domainapp.NewService(s, dispatcher, expectedMXHost, namingMode, addressSecret, l)
	return &DomainHandler{service: service, lookupTXT: net.LookupTXT, lookupMX: net.LookupMX, logger: l.With().Str("handler", "domains").Logger()}
}

func (h *DomainHandler) ListZones(w http.ResponseWriter, r *http.Request) {
	items, err := h.service.ListZones(r.Context(), middleware.TenantFromCtx(r.Context()), middleware.IsAdmin(r.Context()))
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
	item, err := h.service.CreateZone(r.Context(), middleware.TenantFromCtx(r.Context()), middleware.IsAdmin(r.Context()), body.Domain, actorFromRequest(r))
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	created(w, item)
}

func (h *DomainHandler) DeleteZone(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid id")
		return
	}
	if err := h.service.DeleteZone(r.Context(), id, middleware.TenantFromCtx(r.Context()), middleware.IsAdmin(r.Context()), actorFromRequest(r)); err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	noContent(w)
}

func (h *DomainHandler) TriggerVerify(w http.ResponseWriter, r *http.Request) {
	h.service.SetResolvers(h.lookupTXT, h.lookupMX)
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid id")
		return
	}
	zone, checks, err := h.service.TriggerVerify(r.Context(), id, middleware.TenantFromCtx(r.Context()), middleware.IsAdmin(r.Context()), actorFromRequest(r))
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	ok(w, map[string]any{
		"id":          zone.ID,
		"domain":      zone.Domain,
		"txt_record":  zone.TXTRecord,
		"is_verified": zone.IsVerified,
		"mx_verified": zone.MXVerified,
		"checks":      checks,
		"hint":        "Add TXT record: " + zone.TXTRecord,
	})
}

func (h *DomainHandler) VerificationStatus(w http.ResponseWriter, r *http.Request) {
	h.service.SetResolvers(h.lookupTXT, h.lookupMX)
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid id")
		return
	}
	item, err := h.service.VerificationStatus(r.Context(), id, middleware.TenantFromCtx(r.Context()), middleware.IsAdmin(r.Context()))
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
	items, err := h.service.ListRoutes(r.Context(), zoneID, middleware.TenantFromCtx(r.Context()), middleware.IsAdmin(r.Context()))
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
	item, err := h.service.SuggestAddress(r.Context(), zoneID, middleware.TenantFromCtx(r.Context()), middleware.IsAdmin(r.Context()), useSubdomain)
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
	item, err := h.service.CreateRoute(r.Context(), zoneID, middleware.TenantFromCtx(r.Context()), middleware.IsAdmin(r.Context()), domainapp.CreateRouteInput{
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
	if err := h.service.DeleteRoute(r.Context(), routeID, middleware.TenantFromCtx(r.Context()), middleware.IsAdmin(r.Context()), actorFromRequest(r)); err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	noContent(w)
}
