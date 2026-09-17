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
	"tabmail/internal/authz"
	tabdkim "tabmail/internal/dkim"
	"tabmail/internal/hooks"
	"tabmail/internal/models"
	"tabmail/internal/policy"
)

type store interface {
	app.AuditStore
	ListZones(ctx context.Context, tenantID uuid.UUID) ([]*models.DomainZone, error)
	ListZonesScoped(ctx context.Context, scope authz.ZoneListFilter) ([]*models.DomainZone, error)
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
	// Send identities
	CreateSendIdentity(ctx context.Context, si *models.SendIdentity) error
	ListSendIdentitiesByZone(ctx context.Context, zoneID uuid.UUID) ([]*models.SendIdentity, error)
	UpdateSendIdentitiesVerifiedByZone(ctx context.Context, zoneID uuid.UUID, verified bool) error
}

// ResolverInvalidator drops resolver-side caches after a zone/route write so the
// SMTP path does not serve from stale entries for the resolver TTL. Defined in
// this package to avoid a reverse import of internal/resolver; the concrete
// *resolver.Resolver satisfies it via InvalidateZone / InvalidateRoutes.
type ResolverInvalidator interface {
	InvalidateZone(domain string)
	InvalidateRoutes(zoneID uuid.UUID)
}

type Service struct {
	store          store
	az             *authz.Authorizer
	dispatcher     *hooks.Dispatcher
	expectedMXHost string
	// resolverInv drops resolver caches after zone/route writes. May be nil
	// (e.g. in tests or when the resolver is not wired in); the resolver TTL
	// still bounds drift.
	resolverInv ResolverInvalidator
	lookupTXT   func(string) ([]string, error)
	lookupMX    func(string) ([]*net.MX, error)
	logger      zerolog.Logger
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

const dnsLookupTimeout = 3 * time.Second

func lookupTXTWithTimeout(name string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), dnsLookupTimeout)
	defer cancel()
	return net.DefaultResolver.LookupTXT(ctx, name)
}

func lookupMXWithTimeout(name string) ([]*net.MX, error) {
	ctx, cancel := context.WithTimeout(context.Background(), dnsLookupTimeout)
	defer cancel()
	return net.DefaultResolver.LookupMX(ctx, name)
}

func NewService(s store, dispatcher *hooks.Dispatcher, expectedMXHost string, resolverInv ResolverInvalidator, logger zerolog.Logger) *Service {
	return &Service{
		store:          s,
		az:             authz.New(s),
		dispatcher:     dispatcher,
		expectedMXHost: normalizeDNSName(expectedMXHost),
		resolverInv:    resolverInv,
		lookupTXT:      lookupTXTWithTimeout,
		lookupMX:       lookupMXWithTimeout,
		logger:         logger.With().Str("service", "domains").Logger(),
	}
}

// invalidateZone forwards to the resolver cache when wired; no-op otherwise.
func (s *Service) invalidateZone(domain string) {
	if s == nil || s.resolverInv == nil {
		return
	}
	s.resolverInv.InvalidateZone(domain)
}

func (s *Service) invalidateRoutes(zoneID uuid.UUID) {
	if s == nil || s.resolverInv == nil {
		return
	}
	s.resolverInv.InvalidateRoutes(zoneID)
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

func (s *Service) ListZones(ctx context.Context, actor authz.Actor, tenant *models.Tenant) ([]*models.DomainZone, error) {
	if err := ensureTenantScope(tenant, actor.IsTenantAdmin()); err != nil {
		return nil, err
	}
	// The zone allowlist and ownership dimensions are enforced in SQL via the
	// ZoneListFilter, replacing the previous in-memory filterAccessibleZones.
	items, err := s.store.ListZonesScoped(ctx, authz.ZoneListScope(actor, tenant.ID))
	if err != nil {
		return nil, app.Internal(err)
	}
	return items, nil
}

func (s *Service) ListAllZones(ctx context.Context, actor authz.Actor) ([]*models.DomainZone, error) {
	if !actor.IsTenantAdmin() {
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

func (s *Service) CreateZone(ctx context.Context, actor authz.Actor, tenant *models.Tenant, domain string) (*models.DomainZone, error) {
	isAdmin := actor.IsTenantAdmin()
	if err := ensureTenantScope(tenant, isAdmin); err != nil {
		return nil, err
	}
	// CanCreateDomains flag check via the authz seam (admins bypass inside).
	if err := s.authorize(ctx, actor, authz.ActionZoneCreate, authz.Resource{TenantID: tenant.ID}); err != nil {
		return nil, err
	}
	// Per-user domain quota for principals with an owning user. API keys
	// without an owner are gated by scopes and optional AllowedZoneIDs; they
	// do not have owner-level quotas.
	ownerUserID := actor.EffectiveUserID()
	if actor.Permission != nil && !isAdmin && ownerUserID != nil && !models.IsUnlimited(actor.Permission.MaxDomains) {
		owned := countOwnedZones(ctx, s.store, tenant.ID, ownerUserID)
		if owned >= actor.Permission.MaxDomains {
			return nil, app.Forbidden("domain limit reached")
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
	if parent != nil && !authz.CanManageZone(actor, parent) {
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
	if parent != nil && !authz.ZoneAllowed(actor, parent.ID) {
		return nil, app.Forbidden("parent zone not in allowed list")
	}
	if actor.Permission != nil && !isAdmin && parent == nil && len(actor.Permission.AllowedZoneIDs) > 0 {
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
	s.invalidateZone(zone.Domain)
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
	app.InsertAudit(ctx, s.store, s.logger, models.AuditEntry{
		TenantID:     app.UUIDPtr(tenant.ID),
		Actor:        actor.AuditLabel(),
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

func (s *Service) DeleteZone(ctx context.Context, actor authz.Actor, zoneID uuid.UUID) error {
	zone, err := s.ownedZone(ctx, actor, zoneID, authz.ActionZoneDelete)
	if err != nil {
		return err
	}
	if err := s.store.DeleteZone(ctx, zoneID); err != nil {
		return app.Internal(err)
	}
	s.invalidateZone(zone.Domain)
	s.invalidateRoutes(zoneID)
	app.InsertAudit(ctx, s.store, s.logger, models.AuditEntry{
		TenantID:     app.UUIDPtr(zone.TenantID),
		Actor:        actor.AuditLabel(),
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

func (s *Service) ManagedZone(ctx context.Context, actor authz.Actor, zoneID uuid.UUID) (*models.DomainZone, error) {
	return s.ownedZone(ctx, actor, zoneID, authz.ActionZoneManage)
}

func (s *Service) TriggerVerify(ctx context.Context, actor authz.Actor, zoneID uuid.UUID) (*models.DomainZone, VerificationChecks, error) {
	zone, err := s.ownedZone(ctx, actor, zoneID, authz.ActionZoneManage)
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
	s.invalidateZone(zone.Domain)
	// Sync send identity verified status with zone verification
	verified := zone.IsVerified && zone.MXVerified
	if err := s.store.UpdateSendIdentitiesVerifiedByZone(ctx, zone.ID, verified); err != nil {
		s.logger.Warn().Err(err).Msg("syncing send identity verified status")
	}
	app.InsertAudit(ctx, s.store, s.logger, models.AuditEntry{
		TenantID:     app.UUIDPtr(zone.TenantID),
		Actor:        actor.AuditLabel(),
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

func (s *Service) VerificationStatus(ctx context.Context, actor authz.Actor, zoneID uuid.UUID) (*VerificationStatus, error) {
	zone, err := s.ownedZone(ctx, actor, zoneID, authz.ActionZoneRead)
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
		DKIMEnabled: checks.DKIM.Status == "pass" && zone.DKIMPrivateKeyPEM != nil,
	}, nil
}

// ownedZone loads the zone and authorizes the action against it through the
// authz seam. Authorization failures map to HTTP 403 with the authz message.
func (s *Service) ownedZone(ctx context.Context, actor authz.Actor, zoneID uuid.UUID, action authz.Action) (*models.DomainZone, error) {
	zone, err := s.store.GetZone(ctx, zoneID)
	if err != nil {
		return nil, app.Internal(err)
	}
	if zone == nil {
		return nil, app.NotFound("zone not found")
	}
	if err := s.authorize(ctx, actor, action, authz.ZoneResource(zone)); err != nil {
		return nil, err
	}
	return zone, nil
}

// authorize runs the authz seam and converts AuthzError into an app-level
// Forbidden error so HTTP status and message stay stable.
func (s *Service) authorize(ctx context.Context, actor authz.Actor, action authz.Action, res authz.Resource) error {
	return app.FromAuthz(s.az.Authorize(ctx, actor, action, res))
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
		TXT:   txtCheck,
		MX:    mxCheck,
		SPF:   s.lookupTXTRecord(zone.Domain, func(v string) bool { return strings.HasPrefix(strings.ToLower(strings.TrimSpace(v)), "v=spf1") }),
		DKIM:  s.lookupDKIMRecord(zone),
		DMARC: s.lookupTXTRecord("_dmarc."+zone.Domain, func(v string) bool { return strings.HasPrefix(strings.ToLower(strings.TrimSpace(v)), "v=dmarc1") }),
	}
}

func (s *Service) expectedMX() string {
	if s.expectedMXHost == "" {
		return "localhost"
	}
	return s.expectedMXHost
}

// ZoneDNSRequirements returns the DNS records an administrator must publish
// for the zone, derived locally without live lookups: the ownership TXT value,
// the expected MX host, and the DKIM record host/value when a signing key
// exists. The company domain wizard uses it to render setup instructions; live
// verification stays in VerificationStatus / TriggerVerify.
func (s *Service) ZoneDNSRequirements(zone *models.DomainZone) (txtRecord, expectedMX, dkimHost, dkimRecord string) {
	txtRecord = zone.TXTRecord
	expectedMX = s.expectedMX()
	if zone.DKIMPrivateKeyPEM == nil || strings.TrimSpace(*zone.DKIMPrivateKeyPEM) == "" {
		return
	}
	selector := zone.DKIMSelector
	if selector == "" {
		selector = tabdkim.DefaultSelector
	}
	if pubB64, err := tabdkim.PublicKeyFromPEM(*zone.DKIMPrivateKeyPEM); err == nil {
		dkimHost = tabdkim.DNSRecordName(selector, zone.Domain)
		dkimRecord = tabdkim.DNSTXTValue(pubB64)
	}
	return
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

func ensureTenantScope(tenant *models.Tenant, isAdmin bool) error {
	return app.EnsureTenantScope(tenant, isAdmin)
}

func (s *Service) lookupTXTRecord(name string, match func(string) bool) DNSCheck {
	vals, err := s.lookupTXT(name)
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

func (s *Service) lookupDKIMRecord(zone *models.DomainZone) DNSCheck {
	check := DNSCheck{Status: "fail"}
	if zone == nil {
		check.Details = append(check.Details, "zone missing")
		return check
	}
	if zone.DKIMPrivateKeyPEM == nil || strings.TrimSpace(*zone.DKIMPrivateKeyPEM) == "" {
		check.Details = append(check.Details, "dkim private key missing")
		return check
	}
	publicKey, err := tabdkim.PublicKeyFromPEM(*zone.DKIMPrivateKeyPEM)
	if err != nil {
		check.Details = append(check.Details, "dkim private key invalid: "+err.Error())
		return check
	}
	selector := strings.TrimSpace(zone.DKIMSelector)
	if selector == "" {
		selector = tabdkim.DefaultSelector
	}
	name := tabdkim.DNSRecordName(selector, zone.Domain)
	vals, err := s.lookupTXT(name)
	for _, v := range vals {
		check.Details = append(check.Details, v)
		if tabdkim.TXTValueMatchesPublicKey(v, publicKey) {
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
