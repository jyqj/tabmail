package resolver

import (
	"context"
	"fmt"
	"strings"
	"time"

	"tabmail/internal/models"
	"tabmail/internal/policy"
)

// Explain simulates address resolution without side effects, returning a
// step-by-step trace of which zone, route, and mailbox would be matched.
func (rv *Resolver) Explain(ctx context.Context, address string) (*ExplainResult, error) {
	result := &ExplainResult{}

	local, domain, err := policy.NormalizeAddressParts(address, rv.stripPlus)
	if err != nil {
		result.ReasonCode = ReasonRouteNotFound
		result.Steps = append(result.Steps, fmt.Sprintf("failed to parse address: %s", err))
		return result, nil
	}
	result.Steps = append(result.Steps, fmt.Sprintf("normalized: local=%q domain=%q", local, domain))

	mailboxKey, err := policy.ExtractMailbox(address, rv.namingMode, rv.stripPlus)
	if err != nil {
		result.ReasonCode = ReasonRouteNotFound
		result.Steps = append(result.Steps, fmt.Sprintf("failed to extract mailbox key: %s", err))
		return result, nil
	}
	result.Steps = append(result.Steps, fmt.Sprintf("mailbox key: %q (naming=%v)", mailboxKey, rv.namingMode))

	zone, err := rv.findZone(ctx, domain)
	if err != nil {
		return nil, err
	}
	if zone == nil {
		result.ReasonCode = ReasonDomainNotFound
		result.Steps = append(result.Steps, fmt.Sprintf("no zone found for domain %q", domain))
		return result, nil
	}
	result.ZoneID = zone.ID.String()
	result.ZoneDomain = zone.Domain
	result.TenantID = zone.TenantID.String()
	result.Steps = append(result.Steps, fmt.Sprintf("matched zone %q (id=%s)", zone.Domain, zone.ID))

	if !zone.IsVerified {
		result.ReasonCode = ReasonDomainNotVerified
		result.Steps = append(result.Steps, "zone ownership not verified")
		return result, nil
	}

	mb, err := rv.store.GetMailboxByAddress(ctx, mailboxKey)
	if err != nil {
		return nil, err
	}
	if mb != nil {
		if mb.ZoneID != zone.ID {
			result.Steps = append(result.Steps, fmt.Sprintf("ignoring existing mailbox %q from a different zone", mailboxKey))
			mb = nil
		}
	}
	if mb != nil {
		if mb.ExpiresAt != nil && mb.ExpiresAt.Before(time.Now()) {
			result.ReasonCode = ReasonMailboxExpired
			result.Steps = append(result.Steps, fmt.Sprintf("mailbox %q exists but expired at %s", mailboxKey, mb.ExpiresAt.Format(time.RFC3339)))
			return result, nil
		}
		result.Accepted = true
		result.MailboxID = mb.ID.String()
		result.MailboxAddress = mb.FullAddress
		result.ReasonCode = ReasonRouteMatched
		if mb.RouteID != nil {
			result.RouteID = mb.RouteID.String()
		}
		result.Steps = append(result.Steps, fmt.Sprintf("found existing mailbox %q (id=%s)", mb.FullAddress, mb.ID))
		return result, nil
	}
	result.Steps = append(result.Steps, fmt.Sprintf("no existing mailbox for %q", mailboxKey))

	routes, err := rv.listRoutes(ctx, zone.ID)
	if err != nil {
		return nil, err
	}
	result.Steps = append(result.Steps, fmt.Sprintf("loaded %d routes for zone", len(routes)))

	route := matchRoute(routes, local, domain, zone.Domain)
	if route == nil {
		result.ReasonCode = ReasonRouteNotFound
		result.Steps = append(result.Steps, "no matching route found")
		return result, nil
	}
	result.RouteID = route.ID.String()
	result.RouteType = string(route.RouteType)
	result.AutoCreateMailbox = route.AutoCreateMailbox

	matchDesc := describeRouteMatch(route)
	result.Steps = append(result.Steps, fmt.Sprintf("matched %s route: %s", route.RouteType, matchDesc))

	if !route.AutoCreateMailbox {
		result.ReasonCode = ReasonMailboxNotFound
		result.Steps = append(result.Steps, "route matched but auto-create disabled")
		return result, nil
	}

	cfg, err := rv.store.EffectiveConfig(ctx, zone.TenantID)
	if err != nil {
		return nil, err
	}
	if cfg != nil && cfg.MaxMailboxesPerDomain > 0 {
		count, err := rv.store.CountMailboxes(ctx, zone.ID)
		if err != nil {
			return nil, err
		}
		if count >= cfg.MaxMailboxesPerDomain {
			result.ReasonCode = ReasonMailboxQuotaExceeded
			result.Steps = append(result.Steps, fmt.Sprintf("mailbox quota exceeded: %d/%d", count, cfg.MaxMailboxesPerDomain))
			return result, nil
		}
		result.Steps = append(result.Steps, fmt.Sprintf("quota check passed: %d/%d mailboxes", count, cfg.MaxMailboxesPerDomain))
	}

	result.Accepted = true
	result.WouldCreateMailbox = true
	result.MailboxAddress = mailboxKey
	result.ReasonCode = ReasonMailboxWouldCreate
	result.Steps = append(result.Steps, fmt.Sprintf("would auto-create mailbox %q with access_mode=%s", mailboxKey, route.AccessModeDefault))

	return result, nil
}

func describeRouteMatch(r *models.DomainRoute) string {
	switch r.RouteType {
	case models.RouteExact:
		return fmt.Sprintf("exact match on %q", r.MatchValue)
	case models.RouteWildcard:
		return fmt.Sprintf("wildcard pattern %q", r.MatchValue)
	case models.RouteDeepWildcard:
		suffix := strings.TrimPrefix(r.MatchValue, "**.")
		return fmt.Sprintf("deep wildcard **.%s", suffix)
	case models.RouteSequence:
		if r.RangeStart != nil && r.RangeEnd != nil {
			return fmt.Sprintf("sequence %q range [%d..%d]", r.MatchValue, *r.RangeStart, *r.RangeEnd)
		}
		return fmt.Sprintf("sequence %q", r.MatchValue)
	default:
		return r.MatchValue
	}
}
