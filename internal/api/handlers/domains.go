package handlers

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/api/middleware"
	"tabmail/internal/hooks"
	"tabmail/internal/models"
	"tabmail/internal/store"
)

type DomainHandler struct {
	store          store.Store
	dispatcher     *hooks.Dispatcher
	expectedMXHost string
	lookupTXT      func(string) ([]string, error)
	lookupMX       func(string) ([]*net.MX, error)
	logger         zerolog.Logger
}

func NewDomainHandler(s store.Store, dispatcher *hooks.Dispatcher, expectedMXHost string, l zerolog.Logger) *DomainHandler {
	return &DomainHandler{
		store:          s,
		dispatcher:     dispatcher,
		expectedMXHost: normalizeDNSName(expectedMXHost),
		lookupTXT:      net.LookupTXT,
		lookupMX:       net.LookupMX,
		logger:         l.With().Str("handler", "domains").Logger(),
	}
}

// ListZones GET /api/v1/domains
func (h *DomainHandler) ListZones(w http.ResponseWriter, r *http.Request) {
	t := middleware.TenantFromCtx(r.Context())
	if t == nil {
		errForbidden(w, "no tenant context")
		return
	}
	if middleware.IsAdmin(r.Context()) && t.ID == uuid.Nil {
		errBadRequest(w, "admin requests to tenant-scoped endpoints must include X-Tenant-ID")
		return
	}
	zones, err := h.store.ListZones(r.Context(), t.ID)
	if err != nil {
		h.logger.Err(err).Msg("list zones")
		errInternal(w)
		return
	}
	ok(w, zones)
}

// CreateZone POST /api/v1/domains
func (h *DomainHandler) CreateZone(w http.ResponseWriter, r *http.Request) {
	t := middleware.TenantFromCtx(r.Context())
	if t == nil {
		errForbidden(w, "no tenant context")
		return
	}
	if middleware.IsAdmin(r.Context()) && t.ID == uuid.Nil {
		errBadRequest(w, "admin requests to tenant-scoped endpoints must include X-Tenant-ID")
		return
	}

	var body struct {
		Domain string `json:"domain"`
	}
	if err := decodeBody(r, &body); err != nil || body.Domain == "" {
		errBadRequest(w, "domain is required")
		return
	}
	body.Domain = normalizeDNSName(body.Domain)

	cfg, err := h.store.EffectiveConfig(r.Context(), t.ID)
	if err != nil {
		errInternal(w)
		return
	}
	count, err := h.store.CountZones(r.Context(), t.ID)
	if err != nil {
		errInternal(w)
		return
	}
	if !t.IsSuper && count >= cfg.MaxDomains {
		errForbidden(w, fmt.Sprintf("domain limit reached (%d)", cfg.MaxDomains))
		return
	}

	z := &models.DomainZone{
		TenantID:  t.ID,
		Domain:    body.Domain,
		TXTRecord: fmt.Sprintf("tabmail-verify=%s", uuid.New().String()[:8]),
	}
	if err := h.store.CreateZone(r.Context(), z); err != nil {
		errConflict(w, "domain already exists")
		return
	}
	insertAudit(r.Context(), h.store, h.logger, models.AuditEntry{
		TenantID:     uuidPtr(t.ID),
		Actor:        actorFromRequest(r),
		Action:       "domain.create",
		ResourceType: "domain_zone",
		ResourceID:   uuidPtr(z.ID),
		Details:      mustJSON(map[string]any{"domain": z.Domain, "txt_record": z.TXTRecord}),
	})
	if h.dispatcher != nil {
		h.dispatcher.Publish(hooks.Event{
			Type:       "domain.created",
			TenantID:   t.ID.String(),
			OccurredAt: time.Now().UTC(),
			Metadata:   map[string]any{"domain": z.Domain, "zone_id": z.ID.String()},
		})
	}
	created(w, z)
}

// DeleteZone DELETE /api/v1/domains/{id}
func (h *DomainHandler) DeleteZone(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid id")
		return
	}
	t := middleware.TenantFromCtx(r.Context())
	zone, ok := h.ownedZone(w, r, id, t)
	if !ok {
		return
	}
	if err := h.store.DeleteZone(r.Context(), id); err != nil {
		errInternal(w)
		return
	}
	insertAudit(r.Context(), h.store, h.logger, models.AuditEntry{
		TenantID:     uuidPtr(zone.TenantID),
		Actor:        actorFromRequest(r),
		Action:       "domain.delete",
		ResourceType: "domain_zone",
		ResourceID:   uuidPtr(zone.ID),
		Details:      mustJSON(map[string]any{"domain": zone.Domain}),
	})
	if h.dispatcher != nil {
		h.dispatcher.Publish(hooks.Event{
			Type:       "domain.deleted",
			TenantID:   zone.TenantID.String(),
			OccurredAt: time.Now().UTC(),
			Metadata:   map[string]any{"domain": zone.Domain, "zone_id": zone.ID.String()},
		})
	}
	noContent(w)
}

// TriggerVerify POST /api/v1/domains/{id}/verify
func (h *DomainHandler) TriggerVerify(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid id")
		return
	}
	zone, owned := h.ownedZone(w, r, id, middleware.TenantFromCtx(r.Context()))
	if !owned {
		return
	}
	status := h.lookupVerification(zone)
	zone.IsVerified = status.TXT.Status == "pass"
	zone.MXVerified = status.MX.Status == "pass"
	if zone.IsVerified && zone.MXVerified {
		now := time.Now()
		zone.VerifiedAt = &now
	} else {
		zone.VerifiedAt = nil
	}
	if err := h.store.UpdateZone(r.Context(), zone); err != nil {
		h.logger.Err(err).Str("domain", zone.Domain).Msg("update verification status")
		errInternal(w)
		return
	}
	insertAudit(r.Context(), h.store, h.logger, models.AuditEntry{
		TenantID:     uuidPtr(zone.TenantID),
		Actor:        actorFromRequest(r),
		Action:       "domain.verify",
		ResourceType: "domain_zone",
		ResourceID:   uuidPtr(zone.ID),
		Details:      mustJSON(map[string]any{"domain": zone.Domain, "is_verified": zone.IsVerified, "mx_verified": zone.MXVerified}),
	})
	if h.dispatcher != nil {
		h.dispatcher.Publish(hooks.Event{
			Type:       "domain.verified",
			TenantID:   zone.TenantID.String(),
			OccurredAt: time.Now().UTC(),
			Metadata: map[string]any{
				"domain":      zone.Domain,
				"zone_id":     zone.ID.String(),
				"is_verified": zone.IsVerified,
				"mx_verified": zone.MXVerified,
			},
		})
	}
	ok(w, map[string]any{
		"id":          zone.ID,
		"domain":      zone.Domain,
		"txt_record":  zone.TXTRecord,
		"is_verified": zone.IsVerified,
		"mx_verified": zone.MXVerified,
		"checks":      status,
		"hint":        fmt.Sprintf("Add TXT record: %s", zone.TXTRecord),
	})
}

// VerificationStatus GET /api/v1/domains/{id}/verification-status
func (h *DomainHandler) VerificationStatus(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid id")
		return
	}
	zone, owned := h.ownedZone(w, r, id, middleware.TenantFromCtx(r.Context()))
	if !owned {
		return
	}
	status := h.lookupVerification(zone)
	ok(w, map[string]any{
		"txt_expected": zone.TXTRecord,
		"expected_mx":  h.expectedMXHost,
		"is_verified":  status.TXT.Status == "pass",
		"mx_verified":  status.MX.Status == "pass",
		"checks":       status,
	})
}

// --- Routes ----------------------------------------------------------

// ListRoutes GET /api/v1/domains/{id}/routes
func (h *DomainHandler) ListRoutes(w http.ResponseWriter, r *http.Request) {
	zoneID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid id")
		return
	}
	if _, ok := h.ownedZone(w, r, zoneID, middleware.TenantFromCtx(r.Context())); !ok {
		return
	}
	routes, err := h.store.ListRoutes(r.Context(), zoneID)
	if err != nil {
		errInternal(w)
		return
	}
	ok(w, routes)
}

// CreateRoute POST /api/v1/domains/{id}/routes
func (h *DomainHandler) CreateRoute(w http.ResponseWriter, r *http.Request) {
	zoneID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid id")
		return
	}
	zone, ok := h.ownedZone(w, r, zoneID, middleware.TenantFromCtx(r.Context()))
	if !ok {
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
	if body.RouteType == "" || body.MatchValue == "" {
		errBadRequest(w, "route_type and match_value are required")
		return
	}
	if !body.RouteType.Valid() {
		errBadRequest(w, "invalid route_type")
		return
	}

	acm := body.AutoCreateMailbox
	autoCreate := true
	if acm != nil {
		autoCreate = *acm
	}
	am := body.AccessModeDefault
	if am == "" {
		am = models.AccessPublic
	}
	if !am.Valid() {
		errBadRequest(w, "invalid access_mode_default")
		return
	}
	if body.RouteType == models.RouteSequence {
		if body.RangeStart == nil || body.RangeEnd == nil || *body.RangeStart > *body.RangeEnd {
			errBadRequest(w, "sequence routes require valid range_start and range_end")
			return
		}
	}

	route := &models.DomainRoute{
		ZoneID:                 zoneID,
		RouteType:              body.RouteType,
		MatchValue:             normalizeDNSName(body.MatchValue),
		RangeStart:             body.RangeStart,
		RangeEnd:               body.RangeEnd,
		AutoCreateMailbox:      autoCreate,
		RetentionHoursOverride: body.RetentionHoursOverride,
		AccessModeDefault:      am,
	}
	if err := h.store.CreateRoute(r.Context(), route); err != nil {
		h.logger.Err(err).Msg("create route")
		errInternal(w)
		return
	}
	insertAudit(r.Context(), h.store, h.logger, models.AuditEntry{
		TenantID:     uuidPtr(zone.TenantID),
		Actor:        actorFromRequest(r),
		Action:       "route.create",
		ResourceType: "domain_route",
		ResourceID:   uuidPtr(route.ID),
		Details:      mustJSON(map[string]any{"zone_id": zone.ID, "match_value": route.MatchValue, "route_type": route.RouteType}),
	})
	if h.dispatcher != nil {
		h.dispatcher.Publish(hooks.Event{
			Type:       "route.created",
			TenantID:   zone.TenantID.String(),
			OccurredAt: time.Now().UTC(),
			Metadata: map[string]any{
				"zone_id":     zone.ID.String(),
				"route_id":    route.ID.String(),
				"route_type":  route.RouteType,
				"match_value": route.MatchValue,
			},
		})
	}
	created(w, route)
}

// DeleteRoute DELETE /api/v1/domains/{id}/routes/{routeId}
func (h *DomainHandler) DeleteRoute(w http.ResponseWriter, r *http.Request) {
	routeID, err := uuid.Parse(chi.URLParam(r, "routeId"))
	if err != nil {
		errBadRequest(w, "invalid route id")
		return
	}
	route, err := h.store.GetRoute(r.Context(), routeID)
	if err != nil {
		errInternal(w)
		return
	}
	if route == nil {
		errNotFound(w, "route not found")
		return
	}
	zone, ok := h.ownedZone(w, r, route.ZoneID, middleware.TenantFromCtx(r.Context()))
	if !ok {
		return
	}
	if err := h.store.DeleteRoute(r.Context(), routeID); err != nil {
		errInternal(w)
		return
	}
	insertAudit(r.Context(), h.store, h.logger, models.AuditEntry{
		TenantID:     uuidPtr(zone.TenantID),
		Actor:        actorFromRequest(r),
		Action:       "route.delete",
		ResourceType: "domain_route",
		ResourceID:   uuidPtr(route.ID),
		Details:      mustJSON(map[string]any{"match_value": route.MatchValue}),
	})
	if h.dispatcher != nil {
		h.dispatcher.Publish(hooks.Event{
			Type:       "route.deleted",
			TenantID:   zone.TenantID.String(),
			OccurredAt: time.Now().UTC(),
			Metadata: map[string]any{
				"route_id":    route.ID.String(),
				"route_type":  route.RouteType,
				"match_value": route.MatchValue,
			},
		})
	}
	noContent(w)
}

func (h *DomainHandler) ownedZone(w http.ResponseWriter, r *http.Request, zoneID uuid.UUID, t *models.Tenant) (*models.DomainZone, bool) {
	zone, err := h.store.GetZone(r.Context(), zoneID)
	if err != nil {
		errInternal(w)
		return nil, false
	}
	if zone == nil {
		errNotFound(w, "zone not found")
		return nil, false
	}
	if !middleware.IsAdmin(r.Context()) && (t == nil || zone.TenantID != t.ID) {
		errForbidden(w, "not your domain")
		return nil, false
	}
	return zone, true
}

type dnsCheck struct {
	Status  string   `json:"status"`
	Details []string `json:"details,omitempty"`
}

type verificationChecks struct {
	TXT   dnsCheck `json:"txt"`
	MX    dnsCheck `json:"mx"`
	SPF   dnsCheck `json:"spf"`
	DKIM  dnsCheck `json:"dkim"`
	DMARC dnsCheck `json:"dmarc"`
}

func (h *DomainHandler) lookupVerification(zone *models.DomainZone) verificationChecks {
	expectedMX := h.expectedMXHost
	if expectedMX == "" {
		expectedMX = "localhost"
	}

	txtVals, txtErr := h.lookupTXT(zone.Domain)
	txtCheck := dnsCheck{Status: "fail"}
	for _, txt := range txtVals {
		if strings.TrimSpace(txt) == zone.TXTRecord {
			txtCheck.Status = "pass"
		}
		txtCheck.Details = append(txtCheck.Details, txt)
	}
	if txtErr != nil {
		txtCheck.Details = append(txtCheck.Details, txtErr.Error())
	}

	mxVals, mxErr := h.lookupMX(zone.Domain)
	mxCheck := dnsCheck{Status: "fail"}
	for _, mx := range mxVals {
		host := normalizeDNSName(mx.Host)
		mxCheck.Details = append(mxCheck.Details, host)
		if host == expectedMX {
			mxCheck.Status = "pass"
		}
	}
	if mxErr != nil {
		mxCheck.Details = append(mxCheck.Details, mxErr.Error())
	}

	spfCheck := lookupTXTRecord(zone.Domain, func(v string) bool { return strings.HasPrefix(strings.ToLower(strings.TrimSpace(v)), "v=spf1") })
	dkimCheck := lookupTXTRecord("default._domainkey."+zone.Domain, func(v string) bool {
		return strings.Contains(strings.ToLower(v), "dkim") || strings.HasPrefix(strings.ToLower(strings.TrimSpace(v)), "v=dkim1")
	})
	dmarcCheck := lookupTXTRecord("_dmarc."+zone.Domain, func(v string) bool { return strings.HasPrefix(strings.ToLower(strings.TrimSpace(v)), "v=dmarc1") })

	return verificationChecks{
		TXT:   txtCheck,
		MX:    mxCheck,
		SPF:   spfCheck,
		DKIM:  dkimCheck,
		DMARC: dmarcCheck,
	}
}

func lookupTXTRecord(name string, match func(string) bool) dnsCheck {
	vals, err := net.LookupTXT(name)
	check := dnsCheck{Status: "fail"}
	for _, v := range vals {
		check.Details = append(check.Details, v)
		if match(v) {
			check.Status = "pass"
		}
	}
	if err != nil {
		check.Details = append(check.Details, err.Error())
	}
	return check
}

func normalizeDNSName(v string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(v)), ".")
}
