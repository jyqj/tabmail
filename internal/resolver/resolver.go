package resolver

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/models"
	"tabmail/internal/policy"
	"tabmail/internal/store"
)

// Result is the outcome of resolving an email address against domain routes.
type Result struct {
	Zone    *models.DomainZone
	Route   *models.DomainRoute
	Mailbox *models.Mailbox
	Created bool // true if the mailbox was auto-created
}

// Resolver maps an incoming email address to a mailbox, auto-creating if allowed.
type Resolver struct {
	store      store.Store
	namingMode policy.NamingMode
	stripPlus  bool
	cacheMu    sync.RWMutex
	zoneCache  map[string]zoneCacheEntry
	routeCache map[uuid.UUID]routeCacheEntry
}

func New(s store.Store, namingMode policy.NamingMode, stripPlus bool) *Resolver {
	return &Resolver{
		store:      s,
		namingMode: namingMode,
		stripPlus:  stripPlus,
		zoneCache:  map[string]zoneCacheEntry{},
		routeCache: map[uuid.UUID]routeCacheEntry{},
	}
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
		if existing, _ := rv.store.GetMailboxByAddress(ctx, mailboxKey); existing != nil {
			return &Result{Zone: zone, Route: route, Mailbox: existing}, nil
		}
		return nil, fmt.Errorf("resolver: create mailbox: %w", err)
	}
	return &Result{Zone: zone, Route: route, Mailbox: mb, Created: true}, nil
}

// findZone tries exact match first, then walks up parent domains.
func (rv *Resolver) findZone(ctx context.Context, domain string) (*models.DomainZone, error) {
	if zone, ok := rv.getCachedZone(domain); ok {
		return zone, nil
	}
	z, err := rv.store.GetZoneByDomain(ctx, domain)
	if err != nil {
		return nil, err
	}
	rv.setCachedZone(domain, z)
	if z != nil {
		return z, nil
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

type zoneCacheEntry struct {
	zone      *models.DomainZone
	expiresAt time.Time
}

type routeCacheEntry struct {
	routes    []*models.DomainRoute
	expiresAt time.Time
}

const resolverCacheTTL = 15 * time.Second

func (rv *Resolver) getCachedZone(domain string) (*models.DomainZone, bool) {
	rv.cacheMu.RLock()
	entry, ok := rv.zoneCache[domain]
	rv.cacheMu.RUnlock()
	if !ok || time.Now().After(entry.expiresAt) {
		if ok {
			rv.cacheMu.Lock()
			delete(rv.zoneCache, domain)
			rv.cacheMu.Unlock()
		}
		return nil, false
	}
	if entry.zone == nil {
		return nil, true
	}
	cp := *entry.zone
	return &cp, true
}

func (rv *Resolver) setCachedZone(domain string, zone *models.DomainZone) {
	var cp *models.DomainZone
	if zone != nil {
		copyZone := *zone
		cp = &copyZone
	}
	rv.cacheMu.Lock()
	rv.zoneCache[domain] = zoneCacheEntry{zone: cp, expiresAt: time.Now().Add(resolverCacheTTL)}
	rv.cacheMu.Unlock()
}

func (rv *Resolver) listRoutes(ctx context.Context, zoneID uuid.UUID) ([]*models.DomainRoute, error) {
	rv.cacheMu.RLock()
	entry, ok := rv.routeCache[zoneID]
	rv.cacheMu.RUnlock()
	if ok && time.Now().Before(entry.expiresAt) {
		return cloneRoutes(entry.routes), nil
	}

	routes, err := rv.store.ListRoutes(ctx, zoneID)
	if err != nil {
		return nil, err
	}
	rv.cacheMu.Lock()
	rv.routeCache[zoneID] = routeCacheEntry{
		routes:    cloneRoutes(routes),
		expiresAt: time.Now().Add(resolverCacheTTL),
	}
	rv.cacheMu.Unlock()
	return routes, nil
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
