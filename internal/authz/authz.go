package authz

import (
	"context"

	"github.com/google/uuid"
	"tabmail/internal/api/middleware"
	"tabmail/internal/models"
)

// Action represents a standard authorization action.
type Action string

const (
	// Tenant management
	ActionTenantManage      Action = "tenant.manage"
	ActionTenantUsersManage Action = "tenant.users.manage"

	// Zone / domain actions
	ActionZoneRead   Action = "zone.read"
	ActionZoneManage Action = "zone.manage"
	ActionZoneCreate Action = "zone.create"
	ActionZoneDelete Action = "zone.delete"

	// Route actions
	ActionRouteRead   Action = "route.read"
	ActionRouteManage Action = "route.manage"
	ActionRouteDelete Action = "route.delete"

	// Mailbox actions
	ActionMailboxRead   Action = "mailbox.read"
	ActionMailboxWrite  Action = "mailbox.write"
	ActionMailboxCreate Action = "mailbox.create"
	ActionMailboxDelete Action = "mailbox.delete"

	// Message actions
	ActionMessageList   Action = "message.list"
	ActionMessageRead   Action = "message.read"
	ActionMessageSource Action = "message.source"
	ActionMessageWrite  Action = "message.write"
	ActionMessageDelete Action = "message.delete"

	// Send actions
	ActionSendFrom     Action = "send.from"
	ActionOutboundRead Action = "outbound.read"

	// API key actions
	ActionAPIKeyCreate Action = "api_key.create"
	ActionAPIKeyManage Action = "api_key.manage"
)

// PrincipalType identifies the type of actor.
type PrincipalType string

const (
	PrincipalUser   PrincipalType = "user"
	PrincipalAPIKey PrincipalType = "api_key"
)

// Actor represents the authenticated caller.
type Actor struct {
	Type         PrincipalType
	ID           uuid.UUID
	TenantID     uuid.UUID
	Role         models.UserRole
	IsSuperAdmin bool
	IsAdmin      bool
	TenantWide   bool // true for API key access (no specific user)
	Permission   *models.EffectivePermission
	OwnerUserID  *uuid.UUID // For API keys with an active owner user
}

// EffectiveUserID returns the user identity ownership checks should use:
// the user itself for PrincipalUser, the owning user for PrincipalAPIKey
// (nil for ownerless integration keys), and nil for anything else.
func (a Actor) EffectiveUserID() *uuid.UUID {
	switch a.Type {
	case PrincipalUser:
		id := a.ID
		return &id
	case PrincipalAPIKey:
		return a.OwnerUserID
	}
	return nil
}

// AuditLabel returns the audit actor label in the exact format produced by
// the handlers' actorFromRequest helper: "user:<uuid>" for users,
// "api_key:<uuid>" for API keys, the tenant ID string when only a tenant
// context exists, and "public" otherwise.
func (a Actor) AuditLabel() string {
	switch a.Type {
	case PrincipalUser:
		return "user:" + a.ID.String()
	case PrincipalAPIKey:
		return "api_key:" + a.ID.String()
	}
	if a.TenantID != uuid.Nil {
		return a.TenantID.String()
	}
	return "public"
}

// Resource identifies what is being accessed.
type Resource struct {
	Type        string    // "zone", "mailbox", "message", "outbound_job", etc.
	ID          uuid.UUID // resource primary key
	TenantID    uuid.UUID
	ZoneID      uuid.UUID  // for zone-scoped resources
	OwnerUserID *uuid.UUID // the resource's owning user, e.g. zone.OwnerUserID
}

// ZoneResource builds the Resource for a loaded domain zone.
func ZoneResource(zone *models.DomainZone) Resource {
	return Resource{Type: "zone", ID: zone.ID, TenantID: zone.TenantID, ZoneID: zone.ID, OwnerUserID: zone.OwnerUserID}
}

// CanManageZone reports whether the actor has management access to the zone.
// It reproduces app.CanManageZone as invoked with parameters derived by the
// handlers' domainActorParams helper: super admins manage any zone, tenant
// isolation precedes the admin bypass, tenant-wide keys bypass ownership,
// and regular actors must own the zone.
func CanManageZone(actor Actor, zone *models.DomainZone) bool {
	if zone == nil {
		return false
	}
	if actor.IsSuperAdmin {
		return true
	}
	if actor.TenantID == uuid.Nil {
		return false
	}
	if zone.TenantID != actor.TenantID {
		return false
	}
	if actor.IsAdmin {
		return true
	}
	if actor.TenantWide {
		return true
	}
	uid := actor.EffectiveUserID()
	return uid != nil && zone.OwnerUserID != nil && *uid == *zone.OwnerUserID
}

// ZoneAllowed reports whether the zone is within the actor's allowed-zone
// list. Admins and super admins always pass; an absent permission or an
// empty allowlist means all zones are allowed.
func ZoneAllowed(actor Actor, zoneID uuid.UUID) bool {
	if actor.IsSuperAdmin || actor.IsAdmin {
		return true
	}
	if actor.Permission == nil || len(actor.Permission.AllowedZoneIDs) == 0 {
		return true
	}
	for _, id := range actor.Permission.AllowedZoneIDs {
		if id == zoneID {
			return true
		}
	}
	return false
}

// Store is the minimal store interface needed by the authorizer.
// After the grant system removal this is an empty marker; kept so the
// constructor signature stays stable and future checks can be added.
type Store interface{}

// Authorizer performs authorization checks against the store.
type Authorizer struct {
	store Store
}

// New creates an Authorizer backed by the given store.
func New(st Store) *Authorizer {
	return &Authorizer{store: st}
}

// ActorFromContext extracts an Actor from the request context using the
// existing middleware helpers, so callers don't need to build it manually.
//
// API key identity is checked first so that an API key with an owner is
// correctly identified as PrincipalAPIKey (not PrincipalUser). The owner's
// user ID is stored in OwnerUserID for fallback grant checks.
func ActorFromContext(ctx context.Context) Actor {
	actor := Actor{}

	keyID := middleware.APIKeyIDFromCtx(ctx)
	user := middleware.UserFromCtx(ctx)

	if keyID != nil {
		// API key is the primary identity — even when the key has an owner.
		actor.Type = PrincipalAPIKey
		actor.ID = *keyID
		if ownerID := middleware.OwnerUserIDFromCtx(ctx); ownerID != nil {
			actor.OwnerUserID = ownerID
		} else {
			// Only ownerless integration keys are tenant-wide. User-owned API keys
			// keep the API-key principal for audit/grants, but inherit ownership via
			// OwnerUserID rather than becoming broad tenant credentials.
			actor.TenantWide = true
		}
	} else if user != nil {
		actor.Type = PrincipalUser
		actor.ID = user.ID
		actor.TenantID = user.TenantID
		actor.Role = user.Role
	}

	if tenant := middleware.TenantFromCtx(ctx); tenant != nil {
		actor.TenantID = tenant.ID
	}

	actor.IsSuperAdmin = middleware.IsSuperAdmin(ctx)
	actor.IsAdmin = middleware.IsAdmin(ctx)
	actor.Permission = middleware.PermissionFromCtx(ctx)

	return actor
}

// Authorize checks whether the actor can perform the action on the resource.
func (a *Authorizer) Authorize(_ context.Context, actor Actor, action Action, res Resource) error {
	// super_admin can do everything.
	if actor.IsSuperAdmin {
		return nil
	}

	// Tenant isolation: non-super-admin must belong to the same tenant.
	if res.TenantID != (uuid.UUID{}) && actor.TenantID != res.TenantID {
		return ErrForbidden("access denied")
	}

	// admin has full access within their tenant, except managing other admins.
	if actor.IsAdmin {
		if action == ActionTenantUsersManage {
			return ErrForbidden("super admin required")
		}
		return nil
	}

	// Regular users and API keys — check per-action rules.
	switch action {
	case ActionTenantManage, ActionTenantUsersManage:
		return ErrForbidden("admin access required")

	case ActionZoneCreate:
		return a.checkZoneCreate(actor)

	case ActionZoneManage, ActionZoneDelete:
		return a.checkZoneAccessAndOwnership(actor, res)

	case ActionZoneRead:
		return a.checkZoneAccessAndOwnership(actor, res)

	case ActionRouteManage:
		if actor.Permission != nil && !actor.Permission.CanCreateRoutes {
			return ErrForbidden("route creation not allowed")
		}
		return a.checkZoneAccessAndOwnership(actor, res)

	case ActionRouteRead, ActionRouteDelete:
		// Deleting a route only requires zone allowlist + ownership; the
		// CanCreateRoutes flag gates creation, not deletion.
		return a.checkZoneAccessAndOwnership(actor, res)

	case ActionMailboxCreate:
		// Creating a mailbox requires zone allowlist membership and zone
		// ownership, mirroring the canManageZone + IsZoneAllowed pair the
		// mailboxes service previously enforced inline.
		return a.checkZoneAccessAndOwnership(actor, res)

	case ActionMailboxRead:
		return a.checkZoneAccessAndOwnership(actor, res)

	case ActionMailboxWrite, ActionMailboxDelete:
		return a.checkZoneAccessAndOwnership(actor, res)

	case ActionMessageList:
		return a.checkZoneAccessAndOwnership(actor, res)

	case ActionMessageRead, ActionMessageSource:
		return a.checkZoneAccessAndOwnership(actor, res)

	case ActionMessageWrite, ActionMessageDelete:
		return a.checkZoneAccessAndOwnership(actor, res)

	case ActionSendFrom:
		if err := a.checkSendFrom(actor); err != nil {
			return err
		}
		// Sending only requires the zone to be in the allowlist, not zone
		// ownership.
		return a.checkZoneAccess(actor, res)

	case ActionOutboundRead:
		return nil // filtered at query level

	case ActionAPIKeyCreate:
		if actor.Permission != nil && !actor.Permission.CanCreateAPIKeys {
			return ErrForbidden("API key creation not allowed")
		}
		return nil

	case ActionAPIKeyManage:
		return ErrForbidden("admin access required")

	default:
		return ErrForbidden("unknown action")
	}
}

// AuthorizeFromContext is a convenience wrapper that extracts the Actor from
// the context and delegates to Authorize.
func (a *Authorizer) AuthorizeFromContext(ctx context.Context, action Action, res Resource) error {
	return a.Authorize(ctx, ActorFromContext(ctx), action, res)
}

// ---------------------------------------------------------------------------
// Internal checks
// ---------------------------------------------------------------------------

func (a *Authorizer) checkZoneCreate(actor Actor) error {
	perm := actor.Permission
	if perm == nil {
		return nil // API key — scope check happens at middleware level
	}
	if !perm.CanCreateDomains {
		return ErrForbidden("domain creation not allowed")
	}
	return nil
}

// checkZoneAccessAndOwnership applies the allowlist check first and then
// enforces zone ownership for non-tenant-wide actors. Admins are handled
// before this is called.
func (a *Authorizer) checkZoneAccessAndOwnership(actor Actor, res Resource) error {
	if err := a.checkZoneAccess(actor, res); err != nil {
		return err
	}
	return checkZoneOwnership(actor, res)
}

// checkZoneOwnership enforces ownership for regular users and user-owned API
// keys. Ownership is required when the resource carries an owner, or when a
// zone resource has been loaded (res.ID set). Pre-load/create-time checks
// (zero res.ID and nil owner) stay allowlist-only. A loaded zone with no
// owner is denied for regular actors, matching app.CanManageZone. Tenant-wide
// keys bypass ownership (but not the allowlist).
func checkZoneOwnership(actor Actor, res Resource) error {
	if actor.TenantWide {
		return nil
	}
	loadedZone := res.Type == "zone" && res.ID != (uuid.UUID{})
	if res.OwnerUserID == nil && !loadedZone {
		return nil
	}
	uid := actor.EffectiveUserID()
	if uid != nil && res.OwnerUserID != nil && *uid == *res.OwnerUserID {
		return nil
	}
	return ErrForbidden("not your domain")
}

// checkZoneAccess verifies the actor has access to the zone via
// EffectivePermission.AllowedZoneIDs. Admins are handled before this is called.
func (a *Authorizer) checkZoneAccess(actor Actor, res Resource) error {
	if actor.TenantWide {
		if res.ZoneID != (uuid.UUID{}) && actor.Permission != nil && !isZoneAllowed(actor.Permission, res.ZoneID) {
			return ErrForbidden("zone not in allowed list")
		}
		return nil
	}
	if res.ZoneID == (uuid.UUID{}) {
		return nil
	}
	if actor.Permission != nil && !isZoneAllowed(actor.Permission, res.ZoneID) {
		return ErrForbidden("zone not in allowed list")
	}
	return nil
}

func (a *Authorizer) checkSendFrom(actor Actor) error {
	perm := actor.Permission
	if perm != nil && !perm.CanSend {
		return ErrForbidden("sending not allowed")
	}
	return nil
}

func isZoneAllowed(perm *models.EffectivePermission, zoneID uuid.UUID) bool {
	if len(perm.AllowedZoneIDs) == 0 {
		return true
	}
	for _, id := range perm.AllowedZoneIDs {
		if id == zoneID {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Error types
// ---------------------------------------------------------------------------

// AuthzError is a typed authorization error.
type AuthzError struct {
	Message string
}

func (e *AuthzError) Error() string {
	return e.Message
}

// ErrForbidden creates a new AuthzError.
func ErrForbidden(msg string) *AuthzError {
	return &AuthzError{Message: msg}
}

// IsAuthzError returns true if the error is an AuthzError.
func IsAuthzError(err error) bool {
	if err == nil {
		return false
	}
	_, ok := err.(*AuthzError)
	return ok
}
