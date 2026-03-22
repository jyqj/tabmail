package resolver

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
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
}

func New(s store.Store, namingMode policy.NamingMode, stripPlus bool) *Resolver {
	return &Resolver{store: s, namingMode: namingMode, stripPlus: stripPlus}
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

	routes, err := rv.store.ListRoutes(ctx, zone.ID)
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
	z, err := rv.store.GetZoneByDomain(ctx, domain)
	if err != nil {
		return nil, err
	}
	if z != nil {
		return z, nil
	}
	parts := strings.SplitN(domain, ".", 2)
	if len(parts) < 2 {
		return nil, nil
	}
	return rv.findZone(ctx, parts[1])
}

var seqPattern = regexp.MustCompile(`\{n\}`)

func matchRoute(routes []*models.DomainRoute, local, fullDomain, zoneDomain string) *models.DomainRoute {
	subdomain := strings.TrimSuffix(fullDomain, "."+zoneDomain)
	if subdomain == zoneDomain {
		subdomain = ""
	}
	fqdn := fullDomain

	for _, r := range routes {
		switch r.RouteType {
		case models.RouteExact:
			if r.MatchValue == fqdn || r.MatchValue == subdomain {
				return r
			}
		case models.RouteWildcard:
			pattern := regexp.QuoteMeta(r.MatchValue)
			pattern = strings.ReplaceAll(pattern, `\*`, `[^.]+`)
			re, err := regexp.Compile("^" + pattern + "$")
			if err != nil {
				continue
			}
			if re.MatchString(fqdn) || re.MatchString(subdomain+"."+zoneDomain) {
				return r
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
				return r
			}
		}
	}
	return nil
}

// extractSeqNum tries to extract the integer from a pattern like "box-{n}.mail.example.com"
// given a candidate FQDN. Returns -1 on no match.
func extractSeqNum(pattern, candidate1, candidate2 string) int {
	re := seqPattern.ReplaceAllString(regexp.QuoteMeta(pattern), `(\d+)`)
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
