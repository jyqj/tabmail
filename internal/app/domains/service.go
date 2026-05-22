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
	tabdkim "tabmail/internal/dkim"
	"tabmail/internal/hooks"
	"tabmail/internal/models"
	"tabmail/internal/permissions"
	"tabmail/internal/policy"
)

type store interface {
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
	DKIMRecord  string             `json:"dkim_record"`
	DKIMHost    string             `json:"dkim_host"`
	DKIMEnabled bool               `json:"dkim_enabled"`
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

type ZoneAccessInput struct {
	Visibility            models.ResourceVisibility
	AllowRandomSubdomains *bool
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

// SetResolvers overrides DNS resolvers. Must only be called during
// initialization (e.g., in tests), never during request handling.
func (s *Service) SetResolvers(lookupTXT func(string) ([]string, error), lookupMX func(string) ([]*net.MX, error)) {
	if lookupTXT != nil {
		s.lookupTXT = lookupTXT
	}
	if lookupMX != nil {
		s.lookupMX = lookupMX
	}
}

func (s *Service) ListZones(ctx context.Context, tenant *models.Tenant, isAdmin bool, ownerUserID *uuid.UUID, tenantWide bool) ([]*models.DomainZone, error) {
	if err := ensureTenantScope(tenant, isAdmin); err != nil {
		return nil, err
	}
	items, err := s.store.ListZones(ctx, tenant.ID)
	if err != nil {
		return nil, app.Internal(err)
	}
	if isAdmin || tenantWide {
		return items, nil
	}
	return s.filterAccessibleZones(ctx, items, ownerUserID), nil
}

func (s *Service) ListAllZones(ctx context.Context, isAdmin bool) ([]*models.DomainZone, error) {
	if !isAdmin {
		return nil, app.Forbidden("admin access required")
	}
	items, err := s.store.ListAllZones(ctx)
	if err != nil {
		return nil, app.Internal(err)
	}
	return items, nil
}

func (s *Service) ListOpenZones(ctx context.Context, includeAuthenticated bool) ([]*models.DomainZone, error) {
	vis := []models.ResourceVisibility{models.VisibilityPublic}
	if includeAuthenticated {
		vis = append(vis, models.VisibilityAuthenticated)
	}
	items, err := s.store.ListZonesByVisibilities(ctx, vis)
	if err != nil {
		return nil, app.Internal(err)
	}
	out := make([]*models.DomainZone, 0, len(items))
	for _, zone := range items {
		if zone.IsVerified && zone.MXVerified {
			out = append(out, zone)
		}
	}
	return out, nil
}

func (s *Service) CreateZone(ctx context.Context, tenant *models.Tenant, isAdmin bool, ownerUserID *uuid.UUID, tenantWide bool, perm *models.EffectivePermission, domain, actor string) (*models.DomainZone, error) {
	if err := ensureTenantScope(tenant, isAdmin); err != nil {
		return nil, err
	}
	// Permission checks for non-admin JWT users. API keys are gated by scopes
	// and optional AllowedZoneIDs; they do not have owner-level quotas.
	if perm != nil && !isAdmin {
		if ownerUserID != nil && !perm.CanCreateDomains {
			return nil, app.Forbidden("domain creation not allowed")
		}
		if ownerUserID != nil && !permissions.IsUnlimited(perm.MaxDomains) {
			owned := countOwnedZones(ctx, s.store, tenant.ID, ownerUserID)
			if owned >= perm.MaxDomains {
				return nil, app.Forbidden("domain limit reached")
			}
		}
	}
	domain = normalizeDNSName(domain)
	if !policy.ValidateDomainPart(domain) {
		return nil, app.BadRequest("invalid domain")
	}
	parent, err := s.findParentZone(ctx, domain)
	if err != nil {
		return nil, err
	}
	if parent != nil && !s.canManageZoneWithGrants(ctx, parent, tenant, isAdmin, ownerUserID, tenantWide) {
		return nil, app.Forbidden("parent domain permission required")
	}
	// Validate the full ancestor chain belongs to the same tenant.
	if parent != nil && !isAdmin {
		if err := s.validateZoneAncestry(ctx, parent, tenant); err != nil {
			return nil, err
		}
	}
	// AllowedZoneIDs check: a zone-restricted API key/user may only create
	// subdomains under an allowed parent, never new root domains.
	if perm != nil && !isAdmin && parent != nil {
		if !permissions.IsZoneAllowed(perm, parent.ID) {
			return nil, app.Forbidden("parent zone not in allowed list")
		}
	}
	if perm != nil && !isAdmin && parent == nil && len(perm.AllowedZoneIDs) > 0 {
		return nil, app.Forbidden("restricted credentials cannot create root domains")
	}
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
		TenantID:    tenant.ID,
		OwnerUserID: ownerUserID,
		Domain:      domain,
		Visibility:  models.VisibilityPrivate,
		TXTRecord:   fmt.Sprintf("tabmail-verify=%s", uuid.New().String()[:8]),
	}
	if parent != nil {
		zone.ParentZoneID = app.UUIDPtr(parent.ID)
	}
	privPEM, _, err := tabdkim.GenerateKeyPair()
	if err != nil {
		return nil, app.Internal(fmt.Errorf("generate dkim key: %w", err))
	}
	zone.DKIMPrivateKeyPEM = &privPEM
	zone.DKIMSelector = tabdkim.DefaultSelector
	zone.DKIMEnabled = false
	if err := s.store.CreateZone(ctx, zone); err != nil {
		errLower := strings.ToLower(err.Error())
		if strings.Contains(errLower, "duplicate") || strings.Contains(errLower, "unique") || strings.Contains(errLower, "23505") {
			return nil, app.Conflict("domain already exists")
		}
		return nil, app.Internal(err)
	}
	// Auto-create owner grant for the zone creator
	if ownerUserID != nil {
		grant := &models.ZoneGrant{
			TenantID:      tenant.ID,
			ZoneID:        zone.ID,
			PrincipalType: "user",
			PrincipalID:   *ownerUserID,
			Role:          models.ZoneRoleOwner,
			CreatedBy:     ownerUserID,
		}
		if err := s.store.CreateZoneGrant(ctx, grant); err != nil {
			s.logger.Warn().Err(err).Msg("creating owner zone grant")
		}
	}
	// Auto-create domain_wildcard send identity for this zone.
	si := &models.SendIdentity{
		TenantID:     tenant.ID,
		ZoneID:       zone.ID,
		Address:      "*@" + zone.Domain,
		IdentityType: models.SendIdentityDomainWildcard,
		Verified:     false, // will be verified when zone passes verification
	}
	if err := s.store.CreateSendIdentity(ctx, si); err != nil {
		s.logger.Warn().Err(err).Msg("creating domain wildcard send identity")
	}
	// Auto-grant send-as to zone owner.
	if ownerUserID != nil && si.ID != (uuid.UUID{}) {
		sag := &models.SendAsGrant{
			TenantID:      tenant.ID,
			IdentityID:    si.ID,
			PrincipalType: "user",
			PrincipalID:   *ownerUserID,
		}
		if err := s.store.CreateSendAsGrant(ctx, sag); err != nil {
			s.logger.Warn().Err(err).Msg("creating owner send-as grant")
		}
	}
	app.InsertAudit(ctx, s.store, s.logger, models.AuditEntry{
		TenantID:     app.UUIDPtr(tenant.ID),
		Actor:        actor,
		Action:       "domain.create",
		ResourceType: "domain_zone",
		ResourceID:   app.UUIDPtr(zone.ID),
		Details: app.MustJSON(map[string]any{
			"domain": zone.Domain, "txt_record": zone.TXTRecord, "parent_zone_id": zone.ParentZoneID,
			"visibility": zone.Visibility, "allow_random_subdomains": zone.AllowRandomSubdomains,
		}),
	})
	if s.dispatcher != nil {
		s.dispatcher.Publish(hooks.Event{Type: "domain.created", TenantID: tenant.ID.String(), OccurredAt: time.Now().UTC(), Metadata: map[string]any{"domain": zone.Domain, "zone_id": zone.ID.String()}})
	}
	return zone, nil
}

func (s *Service) UpdateZoneAccess(ctx context.Context, zoneID uuid.UUID, tenant *models.Tenant, isAdmin bool, input ZoneAccessInput, actor string) (*models.DomainZone, error) {
	if !isAdmin {
		return nil, app.Forbidden("admin access required")
	}
	zone, err := s.store.GetZone(ctx, zoneID)
	if err != nil {
		return nil, app.Internal(err)
	}
	if zone == nil {
		return nil, app.NotFound("zone not found")
	}
	if tenant != nil && zone.TenantID != tenant.ID {
		return nil, app.NotFound("zone not found")
	}
	if input.Visibility != "" {
		if !input.Visibility.Valid() {
			return nil, app.BadRequest("invalid visibility")
		}
		zone.Visibility = input.Visibility
	}
	if input.AllowRandomSubdomains != nil {
		zone.AllowRandomSubdomains = *input.AllowRandomSubdomains
	}
	if zone.AllowRandomSubdomains && (!zone.IsVerified || !zone.MXVerified) {
		return nil, app.BadRequest("random subdomains can only be enabled after TXT and MX verification")
	}
	if err := s.store.UpdateZone(ctx, zone); err != nil {
		return nil, app.Internal(err)
	}
	app.InsertAudit(ctx, s.store, s.logger, models.AuditEntry{
		TenantID:     app.UUIDPtr(zone.TenantID),
		Actor:        actor,
		Action:       "domain.access.update",
		ResourceType: "domain_zone",
		ResourceID:   app.UUIDPtr(zone.ID),
		Details:      app.MustJSON(map[string]any{"domain": zone.Domain, "visibility": zone.Visibility, "allow_random_subdomains": zone.AllowRandomSubdomains}),
	})
	return zone, nil
}

func (s *Service) DeleteZone(ctx context.Context, zoneID uuid.UUID, tenant *models.Tenant, isAdmin bool, ownerUserID *uuid.UUID, tenantWide bool, actor string) error {
	zone, err := s.ownedZone(ctx, zoneID, tenant, isAdmin, ownerUserID, tenantWide)
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

func (s *Service) ManagedZone(ctx context.Context, zoneID uuid.UUID, tenant *models.Tenant, isAdmin bool, ownerUserID *uuid.UUID, tenantWide bool) (*models.DomainZone, error) {
	return s.ownedZone(ctx, zoneID, tenant, isAdmin, ownerUserID, tenantWide)
}

func (s *Service) TriggerVerify(ctx context.Context, zoneID uuid.UUID, tenant *models.Tenant, isAdmin bool, ownerUserID *uuid.UUID, tenantWide bool, actor string) (*models.DomainZone, VerificationChecks, error) {
	zone, err := s.ownedZone(ctx, zoneID, tenant, isAdmin, ownerUserID, tenantWide)
	if err != nil {
		return nil, VerificationChecks{}, err
	}
	checks := s.lookupVerification(zone)
	zone.IsVerified = checks.TXT.Status == "pass"
	zone.MXVerified = checks.MX.Status == "pass"
	if checks.DKIM.Status == "pass" && zone.DKIMPrivateKeyPEM != nil {
		zone.DKIMEnabled = true
	} else {
		zone.DKIMEnabled = false
	}
	if zone.IsVerified && zone.MXVerified {
		now := time.Now()
		zone.VerifiedAt = &now
	} else {
		zone.VerifiedAt = nil
	}
	if err := s.store.UpdateZone(ctx, zone); err != nil {
		return nil, VerificationChecks{}, app.Internal(err)
	}
	// Sync send identity verified status with zone verification
	verified := zone.IsVerified && zone.MXVerified
	if err := s.store.UpdateSendIdentitiesVerifiedByZone(ctx, zone.ID, verified); err != nil {
		s.logger.Warn().Err(err).Msg("syncing send identity verified status")
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

func (s *Service) VerificationStatus(ctx context.Context, zoneID uuid.UUID, tenant *models.Tenant, isAdmin bool, ownerUserID *uuid.UUID, tenantWide bool) (*VerificationStatus, error) {
	zone, err := s.ownedZone(ctx, zoneID, tenant, isAdmin, ownerUserID, tenantWide)
	if err != nil {
		return nil, err
	}
	checks := s.lookupVerification(zone)
	dkimRecord := ""
	dkimHost := ""
	if zone.DKIMPrivateKeyPEM != nil {
		selector := zone.DKIMSelector
		if selector == "" {
			selector = tabdkim.DefaultSelector
		}
		if pubB64, err := tabdkim.PublicKeyFromPEM(*zone.DKIMPrivateKeyPEM); err == nil {
			dkimRecord = tabdkim.DNSTXTValue(pubB64)
			dkimHost = tabdkim.DNSRecordName(selector, zone.Domain)
		}
	}
	return &VerificationStatus{
		TXTExpected: zone.TXTRecord,
		ExpectedMX:  s.expectedMX(),
		IsVerified:  checks.TXT.Status == "pass",
		MXVerified:  checks.MX.Status == "pass",
		Checks:      checks,
		DKIMRecord:  dkimRecord,
		DKIMHost:    dkimHost,
		DKIMEnabled: zone.DKIMEnabled,
	}, nil
}

func (s *Service) ListRoutes(ctx context.Context, zoneID uuid.UUID, tenant *models.Tenant, isAdmin bool, ownerUserID *uuid.UUID, tenantWide bool) ([]*models.DomainRoute, error) {
	if _, err := s.ownedZone(ctx, zoneID, tenant, isAdmin, ownerUserID, tenantWide); err != nil {
		return nil, err
	}
	items, err := s.store.ListRoutes(ctx, zoneID)
	if err != nil {
		return nil, app.Internal(err)
	}
	return items, nil
}

func (s *Service) SuggestAddress(ctx context.Context, zoneID uuid.UUID, tenant *models.Tenant, isAdmin bool, ownerUserID *uuid.UUID, tenantWide bool, canManage bool, useSubdomain bool) (*SuggestedAddress, error) {
	zone, err := s.ownedZone(ctx, zoneID, tenant, isAdmin, ownerUserID, tenantWide)
	if err != nil {
		return nil, err
	}
	if useSubdomain && !canManage {
		return nil, app.Forbidden("full domain permission is required to generate random subdomains")
	}
	return s.suggestForZone(zone, useSubdomain, canManage || isAdmin)
}

func (s *Service) SuggestOpenAddress(ctx context.Context, zoneID uuid.UUID, includeAuthenticated bool, useSubdomain bool) (*SuggestedAddress, error) {
	zone, err := s.store.GetZone(ctx, zoneID)
	if err != nil {
		return nil, app.Internal(err)
	}
	if zone == nil {
		return nil, app.NotFound("zone not found")
	}
	if zone.Visibility != models.VisibilityPublic && !(includeAuthenticated && zone.Visibility == models.VisibilityAuthenticated) {
		return nil, app.Forbidden("domain is not open for this viewer")
	}
	if useSubdomain && !zone.AllowRandomSubdomains {
		return nil, app.Forbidden("random subdomains are not enabled for this domain")
	}
	return s.suggestForZone(zone, useSubdomain, zone.AllowRandomSubdomains)
}

func (s *Service) suggestForZone(zone *models.DomainZone, useSubdomain bool, canGenerateSubdomain bool) (*SuggestedAddress, error) {
	if s.namingMode != policy.NamingFull {
		return nil, app.BadRequest("random address suggestion requires TABMAIL_MAILBOXNAMING=full")
	}
	if !zone.IsVerified || !zone.MXVerified {
		return nil, app.Forbidden("domain must pass TXT and MX verification before address generation")
	}
	resolvedDomain := zone.Domain
	subdomainLabel := ""
	if useSubdomain {
		if !canGenerateSubdomain {
			return nil, app.Forbidden("random subdomains are not enabled for this viewer")
		}
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

func (s *Service) CreateRoute(ctx context.Context, zoneID uuid.UUID, tenant *models.Tenant, isAdmin bool, ownerUserID *uuid.UUID, tenantWide bool, perm *models.EffectivePermission, input CreateRouteInput, actor string) (*models.DomainRoute, error) {
	// Permission checks for non-admin JWT users plus zone allowlists for API keys.
	if perm != nil && !isAdmin {
		if ownerUserID != nil && !perm.CanCreateRoutes {
			return nil, app.Forbidden("route creation not allowed")
		}
		if !permissions.IsZoneAllowed(perm, zoneID) {
			return nil, app.Forbidden("zone not in allowed list")
		}
	}
	zone, err := s.ownedZone(ctx, zoneID, tenant, isAdmin, ownerUserID, tenantWide)
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
	if autoCreate && am == models.AccessToken {
		return nil, app.BadRequest("token access routes cannot auto-create mailboxes because each token mailbox needs its own password")
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

func (s *Service) DeleteRoute(ctx context.Context, routeID uuid.UUID, tenant *models.Tenant, isAdmin bool, ownerUserID *uuid.UUID, tenantWide bool, actor string) error {
	route, err := s.store.GetRoute(ctx, routeID)
	if err != nil {
		return app.Internal(err)
	}
	if route == nil {
		return app.NotFound("route not found")
	}
	zone, err := s.ownedZone(ctx, route.ZoneID, tenant, isAdmin, ownerUserID, tenantWide)
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

func (s *Service) ownedZone(ctx context.Context, zoneID uuid.UUID, tenant *models.Tenant, isAdmin bool, ownerUserID *uuid.UUID, tenantWide bool) (*models.DomainZone, error) {
	zone, err := s.store.GetZone(ctx, zoneID)
	if err != nil {
		return nil, app.Internal(err)
	}
	if zone == nil {
		return nil, app.NotFound("zone not found")
	}
	if canManageZone(zone, tenant, isAdmin, ownerUserID, tenantWide) {
		return zone, nil
	}
	// Fallback: check zone grants
	if ownerUserID != nil {
		role, _ := s.store.GetHighestZoneRole(ctx, zoneID, "user", *ownerUserID)
		if role.CanManage() {
			return zone, nil
		}
	}
	return nil, app.Forbidden("not your domain")
}

// ListZoneGrants returns all grants for a zone. Requires manage access.
func (s *Service) ListZoneGrants(ctx context.Context, zoneID uuid.UUID, tenant *models.Tenant, isAdmin bool, ownerUserID *uuid.UUID, tenantWide bool) ([]*models.ZoneGrant, error) {
	if _, err := s.ownedZone(ctx, zoneID, tenant, isAdmin, ownerUserID, tenantWide); err != nil {
		return nil, err
	}
	grants, err := s.store.ListZoneGrants(ctx, zoneID)
	if err != nil {
		return nil, app.Internal(err)
	}
	return grants, nil
}

// CreateZoneGrant adds a new grant for a zone. Requires manage access.
func (s *Service) CreateZoneGrant(ctx context.Context, zoneID uuid.UUID, tenant *models.Tenant, isAdmin bool, ownerUserID *uuid.UUID, tenantWide bool, grant *models.ZoneGrant, actor string) (*models.ZoneGrant, error) {
	zone, err := s.ownedZone(ctx, zoneID, tenant, isAdmin, ownerUserID, tenantWide)
	if err != nil {
		return nil, err
	}
	if !grant.Role.Valid() {
		return nil, app.BadRequest("invalid role")
	}
	if grant.PrincipalType != "user" && grant.PrincipalType != "api_key" {
		return nil, app.BadRequest("principal_type must be 'user' or 'api_key'")
	}
	// Verify the principal belongs to the same tenant.
	if err := app.ValidatePrincipalTenant(ctx, s.store, zone.TenantID, grant.PrincipalType, grant.PrincipalID); err != nil {
		return nil, err
	}
	// Check for existing grant
	existing, err := s.store.GetZoneGrant(ctx, zoneID, grant.PrincipalType, grant.PrincipalID)
	if err != nil {
		return nil, app.Internal(err)
	}
	if existing != nil {
		return nil, app.Conflict("grant already exists for this principal")
	}
	grant.TenantID = zone.TenantID
	grant.ZoneID = zoneID
	grant.CreatedBy = ownerUserID
	if err := s.store.CreateZoneGrant(ctx, grant); err != nil {
		return nil, app.Internal(err)
	}
	app.InsertAudit(ctx, s.store, s.logger, models.AuditEntry{
		TenantID:     app.UUIDPtr(zone.TenantID),
		Actor:        actor,
		Action:       "zone_grant.create",
		ResourceType: "zone_grant",
		ResourceID:   app.UUIDPtr(grant.ID),
		Details:      app.MustJSON(map[string]any{"zone_id": zoneID, "principal_type": grant.PrincipalType, "principal_id": grant.PrincipalID, "role": grant.Role}),
	})
	return grant, nil
}

// DeleteZoneGrant removes a grant from a zone. Requires manage access.
func (s *Service) DeleteZoneGrant(ctx context.Context, zoneID, grantID uuid.UUID, tenant *models.Tenant, isAdmin bool, ownerUserID *uuid.UUID, tenantWide bool, actor string) error {
	zone, err := s.ownedZone(ctx, zoneID, tenant, isAdmin, ownerUserID, tenantWide)
	if err != nil {
		return err
	}
	// Verify the grant belongs to this zone
	grants, err := s.store.ListZoneGrants(ctx, zoneID)
	if err != nil {
		return app.Internal(err)
	}
	found := false
	for _, g := range grants {
		if g.ID == grantID {
			found = true
			break
		}
	}
	if !found {
		return app.NotFound("grant not found")
	}
	if err := s.store.DeleteZoneGrant(ctx, grantID); err != nil {
		return app.Internal(err)
	}
	app.InsertAudit(ctx, s.store, s.logger, models.AuditEntry{
		TenantID:     app.UUIDPtr(zone.TenantID),
		Actor:        actor,
		Action:       "zone_grant.delete",
		ResourceType: "zone_grant",
		ResourceID:   app.UUIDPtr(grantID),
		Details:      app.MustJSON(map[string]any{"zone_id": zoneID}),
	})
	return nil
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

func (s *Service) findParentZone(ctx context.Context, domain string) (*models.DomainZone, error) {
	parts := strings.Split(domain, ".")
	for i := 1; i < len(parts)-1; i++ {
		parentDomain := strings.Join(parts[i:], ".")
		zone, err := s.store.GetZoneByDomain(ctx, parentDomain)
		if err != nil {
			return nil, app.Internal(err)
		}
		if zone != nil {
			return zone, nil
		}
	}
	return nil, nil
}

// validateZoneAncestry walks up the parent_zone_id chain and verifies that all
// ancestors belong to the same tenant. It also detects circular references.
func (s *Service) validateZoneAncestry(ctx context.Context, parent *models.DomainZone, tenant *models.Tenant) error {
	current := parent
	visited := make(map[uuid.UUID]bool)
	for current != nil {
		if visited[current.ID] {
			return fmt.Errorf("circular zone hierarchy detected")
		}
		visited[current.ID] = true
		if current.TenantID != tenant.ID {
			return app.Forbidden("ancestor domain belongs to a different tenant")
		}
		if current.ParentZoneID == nil {
			break
		}
		next, err := s.store.GetZone(ctx, *current.ParentZoneID)
		if err != nil {
			return app.Internal(err)
		}
		current = next
	}
	return nil
}

func canManageZone(zone *models.DomainZone, tenant *models.Tenant, isAdmin bool, ownerUserID *uuid.UUID, tenantWide bool) bool {
	return app.CanManageZone(zone, tenant, isAdmin, ownerUserID, tenantWide)
}

func (s *Service) canManageZoneWithGrants(ctx context.Context, zone *models.DomainZone, tenant *models.Tenant, isAdmin bool, ownerUserID *uuid.UUID, tenantWide bool) bool {
	if canManageZone(zone, tenant, isAdmin, ownerUserID, tenantWide) {
		return true
	}
	if ownerUserID != nil {
		role, _ := s.store.GetHighestZoneRole(ctx, zone.ID, "user", *ownerUserID)
		return role.CanManage()
	}
	return false
}

func (s *Service) filterAccessibleZones(ctx context.Context, items []*models.DomainZone, ownerUserID *uuid.UUID) []*models.DomainZone {
	if ownerUserID == nil {
		return []*models.DomainZone{}
	}
	// Get zones with grants
	grantedIDs, _ := s.store.ListGrantedZoneIDs(ctx, "user", *ownerUserID)
	grantedSet := make(map[uuid.UUID]struct{}, len(grantedIDs))
	for _, id := range grantedIDs {
		grantedSet[id] = struct{}{}
	}
	out := make([]*models.DomainZone, 0, len(items))
	for _, zone := range items {
		if zone.OwnerUserID != nil && *zone.OwnerUserID == *ownerUserID {
			out = append(out, zone)
		} else if _, ok := grantedSet[zone.ID]; ok {
			out = append(out, zone)
		}
	}
	return out
}

func ensureTenantScope(tenant *models.Tenant, isAdmin bool) error {
	return app.EnsureTenantScope(tenant, isAdmin)
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

func countOwnedZones(ctx context.Context, st store, tenantID uuid.UUID, ownerUserID *uuid.UUID) int {
	if ownerUserID == nil {
		return 0
	}
	zones, err := st.ListZones(ctx, tenantID)
	if err != nil {
		return 0
	}
	n := 0
	for _, z := range zones {
		if z.OwnerUserID != nil && *z.OwnerUserID == *ownerUserID {
			n++
		}
	}
	return n
}

func normalizeDNSName(v string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(v)), ".")
}
