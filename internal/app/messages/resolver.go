package messageapp

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/app"
	"tabmail/internal/mailtoken"
	"tabmail/internal/models"
	"tabmail/internal/policy"
	"tabmail/internal/store"
)

// mailboxLookup is the slice of the store the mailbox resolver needs.
type mailboxLookup interface {
	GetMailboxByAddress(ctx context.Context, address string) (*models.Mailbox, error)
	GetZone(ctx context.Context, id uuid.UUID) (*models.DomainZone, error)
	ForTenant(tenantID uuid.UUID) store.TenantScoped
}

// mailboxResolver owns the mailbox-access decision: it maps an address to a
// mailbox and authorizes a Viewer against the mailbox's access mode. It is the
// single home for the public / api_key / token / admin access matrix, so the
// message Service operates on an already-resolved, already-authorized mailbox.
type mailboxResolver struct {
	store       mailboxLookup
	namingMode  policy.NamingMode
	stripPlus   bool
	tokenSecret string
}

func newMailboxResolver(st mailboxLookup, namingMode policy.NamingMode, stripPlus bool, tokenSecret string) *mailboxResolver {
	return &mailboxResolver{store: st, namingMode: namingMode, stripPlus: stripPlus, tokenSecret: tokenSecret}
}

// Resolve maps a read request's address to a mailbox the viewer may read.
func (r *mailboxResolver) Resolve(ctx context.Context, address string, viewer Viewer) (*models.Mailbox, error) {
	addr := strings.ToLower(strings.TrimSpace(address))
	if addr == "" {
		return nil, app.BadRequest("address is required")
	}
	mailboxKey, err := policy.ExtractMailbox(addr, r.namingMode, r.stripPlus)
	if err != nil {
		return nil, app.BadRequest("invalid address")
	}
	var mb *models.Mailbox
	// For public/mailbox-token access, don't restrict lookup to the public tenant.
	// For authenticated users/API keys, try tenant-local lookup first, then fall
	// back to global lookup so public/token mailboxes in other tenants remain
	// reachable; the access-mode checks below are the security boundary.
	if viewer.Tenant != nil && viewer.AuthMode != AuthModePublic {
		mb, err = r.store.ForTenant(viewer.Tenant.ID).GetMailboxByAddress(ctx, mailboxKey)
		if err == nil && mb == nil {
			mb, err = r.store.GetMailboxByAddress(ctx, mailboxKey)
		}
	} else {
		mb, err = r.store.GetMailboxByAddress(ctx, mailboxKey)
	}
	if err != nil {
		return nil, app.Internal(err)
	}
	if mb == nil {
		return nil, app.NotFound("mailbox not found")
	}
	if viewer.IsTenantAdmin() {
		return mb, nil
	}
	if mb.ExpiresAt != nil && mb.ExpiresAt.Before(time.Now()) {
		return nil, accessDeniedOrNotFound(viewer, "mailbox expired")
	}
	canManage, err := r.canAccess(ctx, mb, viewer, false)
	if err != nil {
		return nil, err
	}
	if canManage {
		return mb, nil
	}
	switch mb.AccessMode {
	case models.AccessPublic:
		return mb, nil
	case models.AccessAPIKey:
		if viewer.Tenant == nil || viewer.AuthMode != AuthModeAPIKey || mb.TenantID != viewer.Tenant.ID {
			return nil, accessDeniedOrNotFound(viewer, "api key access required")
		}
		if !viewerZoneAllowed(viewer, mb.ZoneID) {
			return nil, app.Forbidden("zone not in allowed list")
		}
		if !viewer.TenantWide {
			return nil, accessDeniedOrNotFound(viewer, "api key requires tenant-wide access")
		}
	case models.AccessToken:
		if viewer.Tenant != nil && viewer.AuthMode == AuthModeAPIKey && viewer.TenantWide && mb.TenantID == viewer.Tenant.ID {
			return mb, nil
		}
		if strings.TrimSpace(viewer.BearerToken) == "" {
			return nil, accessDeniedOrNotFound(viewer, "mailbox token required")
		}
		claims, err := mailtoken.Verify(r.tokenSecret, viewer.BearerToken)
		if err != nil || claims.MailboxID != mb.ID.String() {
			return nil, accessDeniedOrNotFound(viewer, "invalid mailbox token")
		}
	default:
		return nil, accessDeniedOrNotFound(viewer, "access denied")
	}
	return mb, nil
}

// ResolveForWrite maps a write request's address to a mailbox the viewer may
// mutate. Public viewers are rejected outright.
func (r *mailboxResolver) ResolveForWrite(ctx context.Context, address string, viewer Viewer) (*models.Mailbox, error) {
	if viewer.AuthMode == AuthModePublic {
		return nil, app.Forbidden("authentication required for write operations")
	}
	addr := strings.ToLower(strings.TrimSpace(address))
	if addr == "" {
		return nil, app.BadRequest("address is required")
	}
	mailboxKey, err := policy.ExtractMailbox(addr, r.namingMode, r.stripPlus)
	if err != nil {
		return nil, app.BadRequest("invalid address")
	}
	var mb *models.Mailbox
	// Public viewers were rejected above, so the unscoped branch only runs for
	// tenant-less (super-admin) viewers where no tenant filter exists.
	if viewer.Tenant != nil && viewer.AuthMode != AuthModePublic {
		mb, err = r.store.ForTenant(viewer.Tenant.ID).GetMailboxByAddress(ctx, mailboxKey)
	} else {
		mb, err = r.store.GetMailboxByAddress(ctx, mailboxKey)
	}
	if err != nil {
		return nil, app.Internal(err)
	}
	if mb == nil {
		return nil, app.NotFound("mailbox not found")
	}
	if viewer.IsTenantAdmin() {
		return mb, nil
	}
	if mb.ExpiresAt != nil && mb.ExpiresAt.Before(time.Now()) {
		return nil, app.Forbidden("mailbox expired")
	}
	canManage, err := r.canAccess(ctx, mb, viewer, true)
	if err != nil {
		return nil, err
	}
	if canManage {
		return mb, nil
	}
	return nil, app.Forbidden("write access requires mailbox owner or admin")
}

func (r *mailboxResolver) canAccess(ctx context.Context, mb *models.Mailbox, viewer Viewer, requireWrite bool) (bool, error) {
	if mb == nil {
		return false, nil
	}
	if viewer.IsTenantAdmin() {
		return true, nil
	}
	if viewer.Tenant == nil || mb.TenantID != viewer.Tenant.ID {
		return false, nil
	}
	if !viewerZoneAllowed(viewer, mb.ZoneID) {
		return false, app.Forbidden("zone not in allowed list")
	}
	if viewer.TenantWide {
		return true, nil
	}
	zone, err := r.store.GetZone(ctx, mb.ZoneID)
	if err != nil {
		return false, app.Internal(err)
	}
	if zone == nil {
		return false, nil
	}
	if zone.OwnerUserID == nil {
		return false, nil
	}
	if viewer.UserID != nil && *viewer.UserID == *zone.OwnerUserID {
		return true, nil
	}
	return viewer.OwnerUserID != nil && *viewer.OwnerUserID == *zone.OwnerUserID, nil
}

func viewerZoneAllowed(viewer Viewer, zoneID uuid.UUID) bool {
	return models.ZoneAllowed(viewer.AllowedZoneIDs, zoneID)
}

func accessDeniedOrNotFound(viewer Viewer, msg string) error {
	if viewer.AuthMode == AuthModePublic {
		return app.NotFound("mailbox not found")
	}
	return app.Forbidden(msg)
}
