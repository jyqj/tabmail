package resolver

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/configcache"
	"tabmail/internal/models"
	"tabmail/internal/policy"
)

type resolverStore interface {
	GetMailboxByAddress(ctx context.Context, address string) (*models.Mailbox, error)
	ListRoutes(ctx context.Context, zoneID uuid.UUID) ([]*models.DomainRoute, error)
	EffectiveConfig(ctx context.Context, tenantID uuid.UUID) (*models.EffectiveConfig, error)
	CountMailboxes(ctx context.Context, zoneID uuid.UUID) (int, error)
	CreateMailbox(ctx context.Context, m *models.Mailbox) error
	GetZoneByDomain(ctx context.Context, domain string) (*models.DomainZone, error)
}

type autoCreateLimiter interface {
	Allow(ctx context.Context, tenantID, routeID uuid.UUID) (bool, error)
}

// Result is the outcome of resolving an email address against domain routes.
type Result struct {
	Zone    *models.DomainZone
	Route   *models.DomainRoute
	Mailbox *models.Mailbox
	Created bool // true if the mailbox was auto-created
}

// Reusable reports whether this Result can short-circuit a later Resolve call.
//
// Check (materialize=false) returns the same Zone/Mailbox as Resolve for an
// already-existing mailbox, so the SMTP RCPT result is safe to hand to DATA.
// It must NOT be reused when:
//   - Mailbox is nil: Check on an auto-create route returns {Zone, Route}
//     without materializing, so the Result has no Mailbox and Resolve would
//     still need to run (and possibly create).
//   - Created is true: the mailbox was just materialized in this call. A second
//     delivery in the same session should not assume the Result is stable — a
//     concurrent retention sweep or quota change can invalidate it, and
//     re-running Resolve lets the limiter/quota gates fire again.
//
// Both conditions collapse to: Mailbox is present and was not just created.
func (r *Result) Reusable() bool { return r != nil && r.Mailbox != nil && !r.Created }

// resolverCacheTTL is how long zone/route lookups stay cached. Writes from the
// domain service invalidate entries immediately; this TTL is the crash-consistent
// fallback (a writer that crashes between committing and invalidating is
// eventually self-consistent).
const resolverCacheTTL = 15 * time.Second

// Resolver maps an incoming email address to a mailbox, auto-creating if allowed.
type Resolver struct {
	store      resolverStore
	namingMode policy.NamingMode
	stripPlus  bool
	limiter    autoCreateLimiter
	// zoneCache uses negative caching so the parent-domain walk in findZone
	// does not re-hit the store for every level on each lookup.
	zoneCache  *configcache.ConfigCache[string, *models.DomainZone]
	routeCache *configcache.ConfigCache[uuid.UUID, []*models.DomainRoute]
}

func New(s resolverStore, namingMode policy.NamingMode, stripPlus bool, limiters ...autoCreateLimiter) *Resolver {
	var limiter autoCreateLimiter
	if len(limiters) > 0 {
		limiter = limiters[0]
	}
	rv := &Resolver{
		store:      s,
		namingMode: namingMode,
		stripPlus:  stripPlus,
		limiter:    limiter,
	}
	rv.zoneCache = configcache.New(resolverCacheTTL, func(ctx context.Context, domain string) (*models.DomainZone, error) {
		zone, err := s.GetZoneByDomain(ctx, domain)
		if err != nil {
			return nil, err
		}
		if zone != nil {
			cp := *zone
			zone = &cp
		}
		return zone, nil
	}, configcache.WithNilCache[string, *models.DomainZone](true))
	// routeCache uses negative caching too: a zone with no routes returns a nil
	// slice that would otherwise be re-queried on every message. Safe because
	// CreateRoute/DeleteRoute invalidate the zone's entry, so a freshly-added
	// route is visible immediately instead of after the TTL.
	rv.routeCache = configcache.New(resolverCacheTTL, func(ctx context.Context, zoneID uuid.UUID) ([]*models.DomainRoute, error) {
		routes, err := s.ListRoutes(ctx, zoneID)
		if err != nil {
			return nil, err
		}
		return cloneRoutes(routes), nil
	}, configcache.WithNilCache[uuid.UUID, []*models.DomainRoute](true))
	return rv
}

// InvalidateZone drops the cached zone lookup for domain (including a cached
// "no zone" negative entry). Callers should invoke it after any zone write.
func (rv *Resolver) InvalidateZone(domain string) {
	rv.zoneCache.Invalidate(domain)
}

// InvalidateRoutes drops the cached route list for a zone. Callers should
// invoke it after any route write (create/delete) for the zone.
func (rv *Resolver) InvalidateRoutes(zoneID uuid.UUID) {
	rv.routeCache.Invalidate(zoneID)
}

func (rv *Resolver) StripPlus() bool {
	return rv.stripPlus
}

func (rv *Resolver) Check(ctx context.Context, address string) (*Result, error) {
	return rv.resolve(ctx, address, false)
}

// Resolve takes a full email address (local@domain) and:
//  1. Finds the matching domain_zone
//  2. Finds the best matching domain_route
//  3. Returns or auto-creates the mailbox
func (rv *Resolver) Resolve(ctx context.Context, address string) (*Result, error) {
	return rv.resolve(ctx, address, true)
}

func (rv *Resolver) resolve(ctx context.Context, address string, materialize bool) (*Result, error) {
	local, domain, err := policy.NormalizeAddressParts(address, rv.stripPlus)
	if err != nil {
		return nil, fmt.Errorf("resolver: %w", err)
	}
	mailboxKey, err := policy.ExtractMailbox(address, rv.namingMode, rv.stripPlus)
	if err != nil {
		return nil, fmt.Errorf("resolver: %w", err)
	}

	zone, err := rv.findZone(ctx, domain)
	if err != nil {
		return nil, err
	}
	if zone == nil {
		return nil, nil
	}

	mb, err := rv.store.GetMailboxByAddress(ctx, mailboxKey)
	if err != nil {
		return nil, err
	}
	if mb != nil {
		if mb.ZoneID != zone.ID {
			mb = nil
		}
	}
	if mb != nil {
		if mb.ExpiresAt != nil && mb.ExpiresAt.Before(time.Now()) {
			return nil, nil
		}
		return &Result{Zone: zone, Mailbox: mb}, nil
	}

	routes, err := rv.listRoutes(ctx, zone.ID)
	if err != nil {
		return nil, err
	}

	route := matchRoute(routes, local, domain, zone.Domain)
	if route == nil || !route.AutoCreateMailbox || !materialize {
		if route == nil || !route.AutoCreateMailbox {
			return nil, nil
		}
		return &Result{Zone: zone, Route: route}, nil
	}
	if rv.limiter != nil {
		allowed, err := rv.limiter.Allow(ctx, zone.TenantID, route.ID)
		if err != nil {
			return nil, fmt.Errorf("resolver: auto-create rate limit check: %w", err)
		}
		if !allowed {
			return nil, fmt.Errorf("resolver: auto-create rate limit exceeded")
		}
	}

	cfg, err := rv.store.EffectiveConfig(ctx, zone.TenantID)
	if err != nil {
		return nil, fmt.Errorf("resolver: effective config: %w", err)
	}
	if cfg != nil && cfg.MaxMailboxesPerDomain > 0 {
		count, err := rv.store.CountMailboxes(ctx, zone.ID)
		if err != nil {
			return nil, fmt.Errorf("resolver: count mailboxes: %w", err)
		}
		if count >= cfg.MaxMailboxesPerDomain {
			return nil, fmt.Errorf("resolver: mailbox quota exceeded (%d/%d)", count, cfg.MaxMailboxesPerDomain)
		}
	}

	retentionH := route.RetentionHoursOverride
	mb = &models.Mailbox{
		ID:                     uuid.New(),
		TenantID:               zone.TenantID,
		ZoneID:                 zone.ID,
		RouteID:                &route.ID,
		LocalPart:              local,
		ResolvedDomain:         domain,
		FullAddress:            mailboxKey,
		AccessMode:             route.AccessModeDefault,
		RetentionHoursOverride: retentionH,
		CreatedAt:              time.Now(),
	}
	if err := rv.store.CreateMailbox(ctx, mb); err != nil {
		if existing, _ := rv.store.GetMailboxByAddress(ctx, mailboxKey); existing != nil && existing.ZoneID == zone.ID {
			return &Result{Zone: zone, Route: route, Mailbox: existing}, nil
		}
		return nil, fmt.Errorf("resolver: create mailbox: %w", err)
	}
	return &Result{Zone: zone, Route: route, Mailbox: mb, Created: true}, nil
}

// findZone tries exact match first, then walks up parent domains.
func (rv *Resolver) findZone(ctx context.Context, domain string) (*models.DomainZone, error) {
	zone, err := rv.zoneCache.Get(ctx, domain)
	if err != nil {
		return nil, err
	}
	if zone != nil {
		return zone, nil
	}
	parts := strings.SplitN(domain, ".", 2)
	if len(parts) < 2 {
		return nil, nil
	}
	return rv.findZone(ctx, parts[1])
}

type routeCandidate struct {
	route         *models.DomainRoute
	priority      int
	specificity   int
	sequenceWidth int
	exactFQDN     bool
}

func matchRoute(routes []*models.DomainRoute, local, fullDomain, zoneDomain string) *models.DomainRoute {
	subdomain := strings.TrimSuffix(fullDomain, "."+zoneDomain)
	if subdomain == zoneDomain {
		subdomain = ""
	}
	fqdn := fullDomain
	var best *routeCandidate

	for _, r := range routes {
		switch r.RouteType {
		case models.RouteExact:
			if r.MatchValue == fqdn || r.MatchValue == subdomain {
				candidate := &routeCandidate{
					route:       r,
					priority:    routePriority(r.RouteType),
					specificity: len(r.MatchValue),
					exactFQDN:   r.MatchValue == fqdn,
				}
				if best == nil || betterRouteCandidate(candidate, best) {
					best = candidate
				}
			}
		case models.RouteWildcard:
			pattern := regexp.QuoteMeta(r.MatchValue)
			pattern = strings.ReplaceAll(pattern, `\*`, `[^.]+`)
			re, err := regexp.Compile("^" + pattern + "$")
			if err != nil {
				continue
			}
			if re.MatchString(fqdn) || re.MatchString(subdomain+"."+zoneDomain) {
				candidate := &routeCandidate{
					route:       r,
					priority:    routePriority(r.RouteType),
					specificity: len(r.MatchValue),
				}
				if best == nil || betterRouteCandidate(candidate, best) {
					best = candidate
				}
			}
		case models.RouteDeepWildcard:
			suffix := strings.TrimPrefix(r.MatchValue, "**.")
			if suffix == "" || suffix == r.MatchValue {
				continue
			}
			if fqdn != suffix && strings.HasSuffix(fqdn, "."+suffix) {
				candidate := &routeCandidate{
					route:       r,
					priority:    routePriority(r.RouteType),
					specificity: len(suffix),
				}
				if best == nil || betterRouteCandidate(candidate, best) {
					best = candidate
				}
			}
		case models.RouteSequence:
			if r.RangeStart == nil || r.RangeEnd == nil {
				continue
			}
			numPart := extractSeqNum(r.MatchValue, subdomain+"."+zoneDomain, fqdn)
			if numPart < 0 {
				continue
			}
			if numPart >= *r.RangeStart && numPart <= *r.RangeEnd {
				candidate := &routeCandidate{
					route:         r,
					priority:      routePriority(r.RouteType),
					specificity:   len(r.MatchValue),
					sequenceWidth: *r.RangeEnd - *r.RangeStart,
				}
				if best == nil || betterRouteCandidate(candidate, best) {
					best = candidate
				}
			}
		}
	}
	if best == nil {
		return nil
	}
	return best.route
}

func routePriority(t models.RouteType) int {
	switch t {
	case models.RouteExact:
		return 3
	case models.RouteSequence:
		return 2
	case models.RouteWildcard:
		return 1
	case models.RouteDeepWildcard:
		return 0
	default:
		return 0
	}
}

func betterRouteCandidate(a, b *routeCandidate) bool {
	if a.priority != b.priority {
		return a.priority > b.priority
	}
	if a.exactFQDN != b.exactFQDN {
		return a.exactFQDN
	}
	if a.specificity != b.specificity {
		return a.specificity > b.specificity
	}
	if a.sequenceWidth != b.sequenceWidth {
		return a.sequenceWidth < b.sequenceWidth
	}
	if !a.route.CreatedAt.Equal(b.route.CreatedAt) {
		return a.route.CreatedAt.Before(b.route.CreatedAt)
	}
	return a.route.ID.String() < b.route.ID.String()
}

func (rv *Resolver) listRoutes(ctx context.Context, zoneID uuid.UUID) ([]*models.DomainRoute, error) {
	return rv.routeCache.Get(ctx, zoneID)
}

func cloneRoutes(routes []*models.DomainRoute) []*models.DomainRoute {
	if routes == nil {
		return nil
	}
	out := make([]*models.DomainRoute, 0, len(routes))
	for _, route := range routes {
		if route == nil {
			continue
		}
		cp := *route
		out = append(out, &cp)
	}
	return out
}

// extractSeqNum tries to extract the integer from a pattern like "box-{n}.mail.example.com"
// given a candidate FQDN. Returns -1 on no match.
func extractSeqNum(pattern, candidate1, candidate2 string) int {
	re := strings.ReplaceAll(regexp.QuoteMeta(pattern), `\{n\}`, `(\d+)`)
	compiled, err := regexp.Compile("^" + re + "$")
	if err != nil {
		return -1
	}
	for _, c := range []string{candidate1, candidate2} {
		m := compiled.FindStringSubmatch(c)
		if len(m) >= 2 {
			n, err := strconv.Atoi(m[1])
			if err == nil {
				return n
			}
		}
	}
	return -1
}
