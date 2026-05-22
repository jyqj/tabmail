package authz

import (
	"context"
	"fmt"

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

	// Mailbox actions
	ActionMailboxRead   Action = "mailbox.read"
	ActionMailboxWrite  Action = "mailbox.write"
	ActionMailboxCreate Action = "mailbox.create"
	ActionMailboxDelete Action = "mailbox.delete"

	// Message actions
	ActionMessageRead   Action = "message.read"
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
	Type            PrincipalType
	ID              uuid.UUID
	TenantID        uuid.UUID
	Role            models.UserRole
	IsPlatformAdmin bool
	IsTenantAdmin   bool
	TenantWide      bool // true for API key access (no specific user)
	Permission      *models.EffectivePermission
	OwnerUserID     *uuid.UUID // For API keys with an active owner user
}

// Resource identifies what is being accessed.
type Resource struct {
	Type     string    // "zone", "mailbox", "message", "outbound_job", etc.
	ID       uuid.UUID // resource primary key
	TenantID uuid.UUID
	ZoneID   uuid.UUID // for zone-scoped resources
}

// Store is the minimal store interface needed by the authorizer.
type Store interface {
	GetHighestZoneRole(ctx context.Context, zoneID uuid.UUID, principalType string, principalID uuid.UUID) (models.ZoneGrantRole, error)
	GetMailboxGrant(ctx context.Context, mailboxID uuid.UUID, principalType string, principalID uuid.UUID) (*models.MailboxGrant, error)
	HasSendAsGrant(ctx context.Context, tenantID uuid.UUID, address string, principalType string, principalID uuid.UUID) (bool, error)
}

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

	actor.IsPlatformAdmin = middleware.IsAdmin(ctx)
	actor.IsTenantAdmin = middleware.IsTenantAdmin(ctx)
	actor.Permission = middleware.PermissionFromCtx(ctx)

	return actor
}

// Authorize checks whether the actor can perform the action on the resource.
func (a *Authorizer) Authorize(ctx context.Context, actor Actor, action Action, res Resource) error {
	// Platform admin can do anything.
	if actor.IsPlatformAdmin {
		return nil
	}

	// Tenant isolation: non-platform-admin must belong to the same tenant.
	if res.TenantID != (uuid.UUID{}) && actor.TenantID != res.TenantID {
		return ErrForbidden("access denied")
	}

	// Tenant admin can do most things within their tenant.
	if actor.IsTenantAdmin {
		switch action {
		case ActionTenantManage:
			return ErrForbidden("platform admin required")
		default:
			return nil
		}
	}

	// Regular users and API keys — check per-action rules.
	switch action {
	case ActionTenantManage, ActionTenantUsersManage:
		return ErrForbidden("admin access required")

	case ActionZoneCreate:
		return a.checkZoneCreate(actor)

	case ActionZoneManage, ActionZoneDelete:
		return a.checkZoneAccess(ctx, actor, res, true)

	case ActionZoneRead:
		return a.checkZoneAccess(ctx, actor, res, false)

	case ActionRouteManage:
		if actor.Permission != nil && !actor.Permission.CanCreateRoutes {
			return ErrForbidden("route creation not allowed")
		}
		return a.checkZoneAccess(ctx, actor, res, true)

	case ActionRouteRead:
		return a.checkZoneAccess(ctx, actor, res, false)

	case ActionMailboxCreate:
		return a.checkMailboxCreate(ctx, actor, res)

	case ActionMailboxRead:
		return a.checkMailboxAccess(ctx, actor, res, false)

	case ActionMailboxWrite, ActionMailboxDelete:
		return a.checkMailboxAccess(ctx, actor, res, true)

	case ActionMessageRead:
		return a.checkMailboxAccess(ctx, actor, res, false)

	case ActionMessageWrite, ActionMessageDelete:
		return a.checkMailboxAccess(ctx, actor, res, true)

	case ActionSendFrom:
		return a.checkSendFrom(actor)

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

func (a *Authorizer) checkZoneAccess(ctx context.Context, actor Actor, res Resource, requireManage bool) error {
	if actor.TenantWide {
		if res.ZoneID != (uuid.UUID{}) && actor.Permission != nil && !isZoneAllowed(actor.Permission, res.ZoneID) {
			return ErrForbidden("zone not in allowed list")
		}
		return nil // API key scope already checked by middleware
	}
	if res.ZoneID == (uuid.UUID{}) {
		return nil // no zone constraint
	}
	if actor.Permission != nil && !isZoneAllowed(actor.Permission, res.ZoneID) {
		return ErrForbidden("zone not in allowed list")
	}
	// Check zone grants
	role, err := a.store.GetHighestZoneRole(ctx, res.ZoneID, string(actor.Type), actor.ID)
	if err != nil {
		return fmt.Errorf("checking zone grant: %w", err)
	}
	if role != "" {
		if requireManage && !role.CanManage() {
			return ErrForbidden("zone management access required")
		}
		return nil
	}
	if actor.OwnerUserID != nil {
		role, err := a.store.GetHighestZoneRole(ctx, res.ZoneID, string(PrincipalUser), *actor.OwnerUserID)
		if err != nil {
			return fmt.Errorf("checking owner zone grant: %w", err)
		}
		if role != "" {
			if requireManage && !role.CanManage() {
				return ErrForbidden("zone management access required")
			}
			return nil
		}
	}
	// No grant found
	if requireManage {
		return ErrForbidden("not your domain")
	}
	return ErrForbidden("zone access denied")
}

func (a *Authorizer) checkMailboxCreate(ctx context.Context, actor Actor, res Resource) error {
	perm := actor.Permission
	if perm != nil {
		if !isZoneAllowed(perm, res.ZoneID) {
			return ErrForbidden("zone not in allowed list")
		}
	}
	return a.checkZoneAccess(ctx, actor, res, true)
}

func (a *Authorizer) checkMailboxAccess(ctx context.Context, actor Actor, res Resource, requireWrite bool) error {
	if actor.TenantWide {
		if res.ZoneID != (uuid.UUID{}) && actor.Permission != nil && !isZoneAllowed(actor.Permission, res.ZoneID) {
			return ErrForbidden("zone not in allowed list")
		}
		return nil
	}
	// Check mailbox-level grant first
	if res.ID != (uuid.UUID{}) {
		grant, err := a.store.GetMailboxGrant(ctx, res.ID, string(actor.Type), actor.ID)
		if err != nil {
			return fmt.Errorf("checking mailbox grant: %w", err)
		}
		if grant != nil {
			if requireWrite && !grant.Role.CanWrite() {
				return ErrForbidden("mailbox write access required")
			}
			return nil
		}
		if actor.OwnerUserID != nil {
			grant, err := a.store.GetMailboxGrant(ctx, res.ID, string(PrincipalUser), *actor.OwnerUserID)
			if err != nil {
				return fmt.Errorf("checking owner mailbox grant: %w", err)
			}
			if grant != nil {
				if requireWrite && !grant.Role.CanWrite() {
					return ErrForbidden("mailbox write access required")
				}
				return nil
			}
		}
	}
	// Fall back to zone-level access
	return a.checkZoneAccess(ctx, actor, res, requireWrite)
}

func (a *Authorizer) checkSendFrom(actor Actor) error {
	perm := actor.Permission
	if perm != nil && !perm.CanSend {
		return ErrForbidden("sending not allowed")
	}
	// Address-level send-as grant check is deferred to the handler,
	// because the address string is not part of Resource.
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
