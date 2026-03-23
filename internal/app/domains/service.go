package domainapp

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/app"
	"tabmail/internal/hooks"
	"tabmail/internal/models"
	"tabmail/internal/policy"
)

type store interface {
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

type Service struct {
	store          store
	dispatcher     *hooks.Dispatcher
	expectedMXHost string
	namingMode     policy.NamingMode
	addressSecret  string
	lookupTXT      func(string) ([]string, error)
	lookupMX       func(string) ([]*net.MX, error)
	logger         zerolog.Logger
}

type DNSCheck struct {
	Status  string   `json:"status"`
	Details []string `json:"details,omitempty"`
}

type VerificationChecks struct {
	TXT   DNSCheck `json:"txt"`
	MX    DNSCheck `json:"mx"`
	SPF   DNSCheck `json:"spf"`
	DKIM  DNSCheck `json:"dkim"`
	DMARC DNSCheck `json:"dmarc"`
}

type VerificationStatus struct {
	TXTExpected string             `json:"txt_expected"`
	ExpectedMX  string             `json:"expected_mx"`
	IsVerified  bool               `json:"is_verified"`
	MXVerified  bool               `json:"mx_verified"`
	Checks      VerificationChecks `json:"checks"`
}

type CreateRouteInput struct {
	RouteType              models.RouteType
	MatchValue             string
	RangeStart             *int
	RangeEnd               *int
	AutoCreateMailbox      *bool
	RetentionHoursOverride *int
	AccessModeDefault      models.AccessMode
}

type SuggestedAddress struct {
	ZoneID         uuid.UUID `json:"zone_id"`
	BaseDomain     string    `json:"base_domain"`
	Domain         string    `json:"domain"`
	SubdomainLabel string    `json:"subdomain_label,omitempty"`
	LocalPart      string    `json:"local_part"`
	Address        string    `json:"address"`
	Mode           string    `json:"mode"`
	Algorithm      string    `json:"algorithm"`
}

func NewService(s store, dispatcher *hooks.Dispatcher, expectedMXHost string, namingMode policy.NamingMode, addressSecret string, logger zerolog.Logger) *Service {
	return &Service{
		store:          s,
		dispatcher:     dispatcher,
		expectedMXHost: normalizeDNSName(expectedMXHost),
		namingMode:     namingMode,
		addressSecret:  strings.TrimSpace(addressSecret),
		lookupTXT:      net.LookupTXT,
		lookupMX:       net.LookupMX,
		logger:         logger.With().Str("service", "domains").Logger(),
	}
}

func (s *Service) SetResolvers(lookupTXT func(string) ([]string, error), lookupMX func(string) ([]*net.MX, error)) {
	if lookupTXT != nil {
		s.lookupTXT = lookupTXT
	}
	if lookupMX != nil {
		s.lookupMX = lookupMX
	}
}

func (s *Service) ListZones(ctx context.Context, tenant *models.Tenant, isAdmin bool) ([]*models.DomainZone, error) {
	if err := ensureTenantScope(tenant, isAdmin); err != nil {
		return nil, err
	}
	items, err := s.store.ListZones(ctx, tenant.ID)
	if err != nil {
		return nil, app.Internal(err)
	}
	return items, nil
}

func (s *Service) CreateZone(ctx context.Context, tenant *models.Tenant, isAdmin bool, domain, actor string) (*models.DomainZone, error) {
	if err := ensureTenantScope(tenant, isAdmin); err != nil {
		return nil, err
	}
	domain = normalizeDNSName(domain)
	cfg, err := s.store.EffectiveConfig(ctx, tenant.ID)
	if err != nil {
		return nil, app.Internal(err)
	}
	count, err := s.store.CountZones(ctx, tenant.ID)
	if err != nil {
		return nil, app.Internal(err)
	}
	if !tenant.IsSuper && count >= cfg.MaxDomains {
		return nil, app.Forbidden(fmt.Sprintf("domain limit reached (%d)", cfg.MaxDomains))
	}
	zone := &models.DomainZone{
		TenantID:  tenant.ID,
		Domain:    domain,
		TXTRecord: fmt.Sprintf("tabmail-verify=%s", uuid.New().String()[:8]),
	}
	if err := s.store.CreateZone(ctx, zone); err != nil {
		return nil, app.Conflict("domain already exists")
	}
	app.InsertAudit(ctx, s.store, s.logger, models.AuditEntry{
		TenantID:     app.UUIDPtr(tenant.ID),
		Actor:        actor,
		Action:       "domain.create",
		ResourceType: "domain_zone",
		ResourceID:   app.UUIDPtr(zone.ID),
		Details:      app.MustJSON(map[string]any{"domain": zone.Domain, "txt_record": zone.TXTRecord}),
	})
	if s.dispatcher != nil {
		s.dispatcher.Publish(hooks.Event{Type: "domain.created", TenantID: tenant.ID.String(), OccurredAt: time.Now().UTC(), Metadata: map[string]any{"domain": zone.Domain, "zone_id": zone.ID.String()}})
	}
	return zone, nil
}

func (s *Service) DeleteZone(ctx context.Context, zoneID uuid.UUID, tenant *models.Tenant, isAdmin bool, actor string) error {
	zone, err := s.ownedZone(ctx, zoneID, tenant, isAdmin)
	if err != nil {
		return err
	}
	if err := s.store.DeleteZone(ctx, zoneID); err != nil {
		return app.Internal(err)
	}
	app.InsertAudit(ctx, s.store, s.logger, models.AuditEntry{
		TenantID:     app.UUIDPtr(zone.TenantID),
		Actor:        actor,
		Action:       "domain.delete",
		ResourceType: "domain_zone",
		ResourceID:   app.UUIDPtr(zone.ID),
		Details:      app.MustJSON(map[string]any{"domain": zone.Domain}),
	})
	if s.dispatcher != nil {
		s.dispatcher.Publish(hooks.Event{Type: "domain.deleted", TenantID: zone.TenantID.String(), OccurredAt: time.Now().UTC(), Metadata: map[string]any{"domain": zone.Domain, "zone_id": zone.ID.String()}})
	}
	return nil
}

func (s *Service) TriggerVerify(ctx context.Context, zoneID uuid.UUID, tenant *models.Tenant, isAdmin bool, actor string) (*models.DomainZone, VerificationChecks, error) {
	zone, err := s.ownedZone(ctx, zoneID, tenant, isAdmin)
	if err != nil {
		return nil, VerificationChecks{}, err
	}
	checks := s.lookupVerification(zone)
	zone.IsVerified = checks.TXT.Status == "pass"
	zone.MXVerified = checks.MX.Status == "pass"
	if zone.IsVerified && zone.MXVerified {
		now := time.Now()
		zone.VerifiedAt = &now
	} else {
		zone.VerifiedAt = nil
	}
	if err := s.store.UpdateZone(ctx, zone); err != nil {
		return nil, VerificationChecks{}, app.Internal(err)
	}
	app.InsertAudit(ctx, s.store, s.logger, models.AuditEntry{
		TenantID:     app.UUIDPtr(zone.TenantID),
		Actor:        actor,
		Action:       "domain.verify",
		ResourceType: "domain_zone",
		ResourceID:   app.UUIDPtr(zone.ID),
		Details:      app.MustJSON(map[string]any{"domain": zone.Domain, "is_verified": zone.IsVerified, "mx_verified": zone.MXVerified}),
	})
	if s.dispatcher != nil {
		s.dispatcher.Publish(hooks.Event{Type: "domain.verified", TenantID: zone.TenantID.String(), OccurredAt: time.Now().UTC(), Metadata: map[string]any{"domain": zone.Domain, "zone_id": zone.ID.String(), "is_verified": zone.IsVerified, "mx_verified": zone.MXVerified}})
	}
	return zone, checks, nil
}

func (s *Service) VerificationStatus(ctx context.Context, zoneID uuid.UUID, tenant *models.Tenant, isAdmin bool) (*VerificationStatus, error) {
	zone, err := s.ownedZone(ctx, zoneID, tenant, isAdmin)
	if err != nil {
		return nil, err
	}
	checks := s.lookupVerification(zone)
	return &VerificationStatus{TXTExpected: zone.TXTRecord, ExpectedMX: s.expectedMX(), IsVerified: checks.TXT.Status == "pass", MXVerified: checks.MX.Status == "pass", Checks: checks}, nil
}

func (s *Service) ListRoutes(ctx context.Context, zoneID uuid.UUID, tenant *models.Tenant, isAdmin bool) ([]*models.DomainRoute, error) {
	if _, err := s.ownedZone(ctx, zoneID, tenant, isAdmin); err != nil {
		return nil, err
	}
	items, err := s.store.ListRoutes(ctx, zoneID)
	if err != nil {
		return nil, app.Internal(err)
	}
	return items, nil
}

func (s *Service) SuggestAddress(ctx context.Context, zoneID uuid.UUID, tenant *models.Tenant, isAdmin bool, useSubdomain bool) (*SuggestedAddress, error) {
	zone, err := s.ownedZone(ctx, zoneID, tenant, isAdmin)
	if err != nil {
		return nil, err
	}
	if s.namingMode != policy.NamingFull {
		return nil, app.BadRequest("random address suggestion requires TABMAIL_MAILBOXNAMING=full")
	}
	resolvedDomain := zone.Domain
	subdomainLabel := ""
	if useSubdomain {
		label, fqdn, err := policy.GenerateSuggestedSubdomainAddress(time.Now().UTC(), zone.Domain, s.addressSecret)
		if err != nil {
			return nil, app.Internal(err)
		}
		subdomainLabel = label
		resolvedDomain = fqdn
	}
	local, address, err := policy.GenerateSuggestedAddress(time.Now().UTC(), resolvedDomain, s.addressSecret)
	if err != nil {
		return nil, app.Internal(err)
	}
	return &SuggestedAddress{
		ZoneID:         zone.ID,
		BaseDomain:     zone.Domain,
		Domain:         resolvedDomain,
		SubdomainLabel: subdomainLabel,
		LocalPart:      local,
		Address:        address,
		Mode:           map[bool]string{true: "subdomain", false: "mailbox"}[useSubdomain],
		Algorithm:      policy.AddressSuggestionAlgorithm,
	}, nil
}

func (s *Service) CreateRoute(ctx context.Context, zoneID uuid.UUID, tenant *models.Tenant, isAdmin bool, input CreateRouteInput, actor string) (*models.DomainRoute, error) {
	zone, err := s.ownedZone(ctx, zoneID, tenant, isAdmin)
	if err != nil {
		return nil, err
	}
	if input.RouteType == "" || input.MatchValue == "" {
		return nil, app.BadRequest("route_type and match_value are required")
	}
	if !input.RouteType.Valid() {
		return nil, app.BadRequest("invalid route_type")
	}
	autoCreate := true
	if input.AutoCreateMailbox != nil {
		autoCreate = *input.AutoCreateMailbox
	}
	am := input.AccessModeDefault
	if am == "" {
		am = models.AccessPublic
	}
	if !am.Valid() {
		return nil, app.BadRequest("invalid access_mode_default")
	}
	if input.RetentionHoursOverride != nil && *input.RetentionHoursOverride <= 0 {
		return nil, app.BadRequest("retention_hours_override must be greater than 0")
	}
	if input.RouteType == models.RouteSequence {
		if input.RangeStart == nil || input.RangeEnd == nil || *input.RangeStart > *input.RangeEnd {
			return nil, app.BadRequest("sequence routes require valid range_start and range_end")
		}
	}
	if input.RouteType == models.RouteDeepWildcard && !strings.HasPrefix(normalizeDNSName(input.MatchValue), "**.") {
		return nil, app.BadRequest("deep_wildcard routes must use a **.suffix pattern")
	}
	route := &models.DomainRoute{ZoneID: zoneID, RouteType: input.RouteType, MatchValue: normalizeDNSName(input.MatchValue), RangeStart: input.RangeStart, RangeEnd: input.RangeEnd, AutoCreateMailbox: autoCreate, RetentionHoursOverride: input.RetentionHoursOverride, AccessModeDefault: am}
	if err := s.store.CreateRoute(ctx, route); err != nil {
		return nil, app.Internal(err)
	}
	app.InsertAudit(ctx, s.store, s.logger, models.AuditEntry{
		TenantID:     app.UUIDPtr(zone.TenantID),
		Actor:        actor,
		Action:       "route.create",
		ResourceType: "domain_route",
		ResourceID:   app.UUIDPtr(route.ID),
		Details:      app.MustJSON(map[string]any{"zone_id": zone.ID, "match_value": route.MatchValue, "route_type": route.RouteType}),
	})
	if s.dispatcher != nil {
		s.dispatcher.Publish(hooks.Event{Type: "route.created", TenantID: zone.TenantID.String(), OccurredAt: time.Now().UTC(), Metadata: map[string]any{"zone_id": zone.ID.String(), "route_id": route.ID.String(), "route_type": route.RouteType, "match_value": route.MatchValue}})
	}
	return route, nil
}

func (s *Service) DeleteRoute(ctx context.Context, routeID uuid.UUID, tenant *models.Tenant, isAdmin bool, actor string) error {
	route, err := s.store.GetRoute(ctx, routeID)
	if err != nil {
		return app.Internal(err)
	}
	if route == nil {
		return app.NotFound("route not found")
	}
	zone, err := s.ownedZone(ctx, route.ZoneID, tenant, isAdmin)
	if err != nil {
		return err
	}
	if err := s.store.DeleteRoute(ctx, routeID); err != nil {
		return app.Internal(err)
	}
	app.InsertAudit(ctx, s.store, s.logger, models.AuditEntry{
		TenantID:     app.UUIDPtr(zone.TenantID),
		Actor:        actor,
		Action:       "route.delete",
		ResourceType: "domain_route",
		ResourceID:   app.UUIDPtr(route.ID),
		Details:      app.MustJSON(map[string]any{"match_value": route.MatchValue}),
	})
	if s.dispatcher != nil {
		s.dispatcher.Publish(hooks.Event{Type: "route.deleted", TenantID: zone.TenantID.String(), OccurredAt: time.Now().UTC(), Metadata: map[string]any{"route_id": route.ID.String(), "route_type": route.RouteType, "match_value": route.MatchValue}})
	}
	return nil
}

func (s *Service) ownedZone(ctx context.Context, zoneID uuid.UUID, tenant *models.Tenant, isAdmin bool) (*models.DomainZone, error) {
	zone, err := s.store.GetZone(ctx, zoneID)
	if err != nil {
		return nil, app.Internal(err)
	}
	if zone == nil {
		return nil, app.NotFound("zone not found")
	}
	if !isAdmin && (tenant == nil || zone.TenantID != tenant.ID) {
		return nil, app.Forbidden("not your domain")
	}
	return zone, nil
}

func (s *Service) lookupVerification(zone *models.DomainZone) VerificationChecks {
	expectedMX := s.expectedMX()
	vals, txtErr := s.lookupTXT(zone.Domain)
	txtCheck := DNSCheck{Status: "fail"}
	for _, txt := range vals {
		if strings.TrimSpace(txt) == zone.TXTRecord {
			txtCheck.Status = "pass"
		}
		txtCheck.Details = append(txtCheck.Details, txt)
	}
	if txtErr != nil {
		txtCheck.Details = append(txtCheck.Details, txtErr.Error())
	}
	mxVals, mxErr := s.lookupMX(zone.Domain)
	mxCheck := DNSCheck{Status: "fail"}
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
	return VerificationChecks{
		TXT: txtCheck,
		MX:  mxCheck,
		SPF: lookupTXTRecord(zone.Domain, func(v string) bool { return strings.HasPrefix(strings.ToLower(strings.TrimSpace(v)), "v=spf1") }),
		DKIM: lookupTXTRecord("default._domainkey."+zone.Domain, func(v string) bool {
			lower := strings.ToLower(strings.TrimSpace(v))
			return strings.Contains(lower, "dkim") || strings.HasPrefix(lower, "v=dkim1")
		}),
		DMARC: lookupTXTRecord("_dmarc."+zone.Domain, func(v string) bool { return strings.HasPrefix(strings.ToLower(strings.TrimSpace(v)), "v=dmarc1") }),
	}
}

func (s *Service) expectedMX() string {
	if s.expectedMXHost == "" {
		return "localhost"
	}
	return s.expectedMXHost
}

func ensureTenantScope(tenant *models.Tenant, isAdmin bool) error {
	if tenant == nil {
		return app.Forbidden("no tenant context")
	}
	if isAdmin && tenant.ID == uuid.Nil {
		return app.BadRequest("admin requests to tenant-scoped endpoints must include X-Tenant-ID")
	}
	return nil
}

func lookupTXTRecord(name string, match func(string) bool) DNSCheck {
	vals, err := net.LookupTXT(name)
	check := DNSCheck{Status: "fail"}
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
