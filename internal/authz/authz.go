package authz

import (
	"context"
	"errors"

	"github.com/google/uuid"
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

// IsTenantAdmin reports whether the actor has admin authority within its tenant
// — a tenant admin or a global super admin. Use this for tenant-scoped
// privileged access (the common case). See docs/adr/0001-admin-and-super-admin-roles.md.
func (a Actor) IsTenantAdmin() bool { return a.IsSuperAdmin || a.IsAdmin }

// IsGlobalAdmin reports whether the actor is a global platform operator (super
// admin) able to act across tenants. Use this only for cross-tenant actions:
// tenant impersonation, system-wide configuration, and role escalation.
func (a Actor) IsGlobalAdmin() bool { return a.IsSuperAdmin }

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
	if actor.IsGlobalAdmin() {
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
	if actor.IsTenantAdmin() {
		return true
	}
	return actor.Permission.AllowsZone(zoneID)
}

// OwnerScope describes which owned resources an actor may see when listing or
// fetching resources that carry an owning user / API key (e.g. outbound jobs).
// It is the single home for the rule "a tenant admin sees every owned resource
// in the tenant; a regular user or API key sees only its own", so the list and
// get paths cannot drift apart. ActionOutboundRead authorizes the action and
// defers the row scope to the query level; ListScope is that query scope.
//
// This is preserved as the legacy owner-only scope. New list paths should use
// ListScope (the tagged union) via ZoneListScope / OwnerListScope, which carry
// TenantID into SQL so tenant isolation is enforced at the query level rather
// than relying on in-memory fallbacks.
type OwnerScope struct {
	// AllInTenant is true when the actor may see every owned resource in the
	// tenant. When true, UserID and APIKeyID are nil.
	AllInTenant bool
	// UserID, when set, restricts results to resources owned by this user.
	UserID *uuid.UUID
	// APIKeyID, when set, restricts results to resources created by this API key.
	APIKeyID *uuid.UUID
}

// ListScope returns the owner scope an actor gets for listing owned resources:
// a tenant admin sees all; a user sees its own; an API key sees its own. Any
// other principal sees nothing (the zero OwnerScope).
//
// Preserved as a thin legacy constructor for outbound.go during the list-scope
// migration. Prefer OwnerListScope(actor, tenantID) for new call sites — it
// additionally pins TenantID so the store can enforce tenant isolation in SQL.
func ListScope(actor Actor) OwnerScope {
	if actor.IsTenantAdmin() {
		return OwnerScope{AllInTenant: true}
	}
	switch actor.Type {
	case PrincipalUser:
		id := actor.ID
		return OwnerScope{UserID: &id}
	case PrincipalAPIKey:
		id := actor.ID
		return OwnerScope{APIKeyID: &id}
	}
	return OwnerScope{}
}

// ZoneListFilter is the zone-scoped list query shape for domain zones,
// mailboxes, and send identities. It is constructed by ZoneListScope so that
// tenant isolation, the zone allowlist, and (for non-admin principals) zone
// ownership all become SQL WHERE clauses rather than in-memory fallbacks.
//
// Field semantics:
//   - TenantID is always set and must be applied as WHERE tenant_id = $1.
//   - AllZones=true means every zone in the tenant passes the zone dimension.
//     This is the case for an admin without an allowlist.
//   - AllZones=false means ZoneIDs holds the allowed-zone set and must be
//     applied as WHERE zone_id = ANY($2). An empty ZoneIDs slice means "no
//     visible zone" and the store must return an empty result — it must NOT
//     fall back to AllZones.
//   - OwnerUserID, when non-nil, restricts to zones owned by that user
//     (regular users / user-owned API keys). Admins and tenant-wide keys get a
//     nil OwnerUserID (no owner filter). This is the zone.owner_user_id
//     dimension; the caller resolves owned-zone IDs for the mailbox path and
//     injects them via ZoneIDs.
type ZoneListFilter struct {
	TenantID    uuid.UUID
	AllZones    bool
	ZoneIDs     []uuid.UUID
	OwnerUserID *uuid.UUID
}

// OwnerListFilter is the owner-scoped list query shape for resources that carry
// an owning user / API key (outbound jobs). AllInTenant, UserID, and APIKeyID
// are mutually exclusive: AllInTenant is true for tenant admins; otherwise
// exactly one of UserID / APIKeyID is set. TenantID is always set and must be
// applied as WHERE tenant_id = $1.
type OwnerListFilter struct {
	TenantID    uuid.UUID
	AllInTenant bool
	UserID      *uuid.UUID
	APIKeyID    *uuid.UUID
}

// ZoneListScope resolves the zone-scoped list filter for an actor within a
// tenant. Rules (must match the previous in-memory filters exactly):
//
//   - Tenant admin (or super admin) with an empty allowlist: AllZones=true,
//     OwnerUserID=nil. The owner dimension is unrestricted.
//   - Tenant admin with a non-empty allowlist: AllZones=false, ZoneIDs=allowlist,
//     OwnerUserID=nil. (Admin + allowlist restricts to the listed zones.)
//   - Tenant-wide (ownerless integration) API key with an empty allowlist:
//     AllZones=true, OwnerUserID=nil.
//   - Tenant-wide API key with a non-empty allowlist: AllZones=false,
//     ZoneIDs=allowlist, OwnerUserID=nil.
//   - Regular user or user-owned API key with an empty allowlist: AllZones=true,
//     OwnerUserID=effective user. (The store applies owner_user_id; for the
//     mailbox path the caller pre-resolves owned zone IDs into ZoneIDs.)
//   - Regular user / user-owned API key with a non-empty allowlist:
//     AllZones=false, ZoneIDs=allowlist, OwnerUserID=effective user.
//
// ZoneListScope never reads the store; owned-zone resolution for mailboxes
// stays in the mailboxes service.
func ZoneListScope(actor Actor, tenantID uuid.UUID) ZoneListFilter {
	f := ZoneListFilter{TenantID: tenantID}
	allowlist := actor.allowlist()
	if actor.IsTenantAdmin() || actor.TenantWide {
		if len(allowlist) > 0 {
			f.ZoneIDs = allowlist
		} else {
			f.AllZones = true
		}
		return f
	}
	// Regular user or user-owned API key.
	f.OwnerUserID = actor.EffectiveUserID()
	if len(allowlist) > 0 {
		f.ZoneIDs = allowlist
	} else {
		f.AllZones = true
	}
	return f
}

// OwnerListScope resolves the owner-scoped list filter for an actor within a
// tenant. It mirrors the legacy ListScope(actor) rule and additionally pins
// TenantID so the store can apply WHERE tenant_id = $1. The three owner
// dimensions are mutually exclusive by construction.
func OwnerListScope(actor Actor, tenantID uuid.UUID) OwnerListFilter {
	f := OwnerListFilter{TenantID: tenantID}
	if actor.IsTenantAdmin() {
		f.AllInTenant = true
		return f
	}
	switch actor.Type {
	case PrincipalUser:
		id := actor.ID
		f.UserID = &id
	case PrincipalAPIKey:
		id := actor.ID
		f.APIKeyID = &id
	}
	return f
}

// allowlist returns the actor's zone allowlist, or nil when the actor has no
// permission profile (treated as unrestricted, matching ZoneAllowed).
func (a Actor) allowlist() []uuid.UUID {
	if a.Permission == nil {
		return nil
	}
	return a.Permission.AllowedZoneIDs
}

// CanAccessOwned reports whether the actor may access a single resource owned by
// ownerUserID / ownerAPIKeyID, applying the same rule as ListScope. Tenant
// isolation is the caller's responsibility and must be checked separately.
func CanAccessOwned(actor Actor, ownerUserID, ownerAPIKeyID *uuid.UUID) bool {
	if actor.IsTenantAdmin() {
		return true
	}
	switch actor.Type {
	case PrincipalUser:
		return ownerUserID != nil && *ownerUserID == actor.ID
	case PrincipalAPIKey:
		return ownerAPIKeyID != nil && *ownerAPIKeyID == actor.ID
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

// Authorize checks whether the actor can perform the action on the resource.
func (a *Authorizer) Authorize(_ context.Context, actor Actor, action Action, res Resource) error {
	// super_admin can do everything.
	if actor.IsGlobalAdmin() {
		return nil
	}

	// Tenant isolation: non-super-admin must belong to the same tenant.
	if res.TenantID != (uuid.UUID{}) && actor.TenantID != res.TenantID {
		return forbidden(KindTenantIsolation, "access denied")
	}

	// admin has full access within their tenant, except managing other admins.
	if actor.IsAdmin {
		if action == ActionTenantUsersManage {
			return forbidden(KindAdminRequired, "super admin required")
		}
		return nil
	}

	// Regular users and API keys — check per-action rules.
	switch action {
	case ActionTenantManage, ActionTenantUsersManage:
		return forbidden(KindAdminRequired, "admin access required")

	case ActionZoneCreate:
		return a.checkZoneCreate(actor)

	case ActionZoneManage, ActionZoneDelete:
		return a.checkZoneAccessAndOwnership(actor, res)

	case ActionZoneRead:
		return a.checkZoneAccessAndOwnership(actor, res)

	case ActionRouteManage:
		if actor.Permission != nil && !actor.Permission.CanCreateRoutes {
			return forbidden(KindCapability, "route creation not allowed")
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
			return forbidden(KindCapability, "API key creation not allowed")
		}
		return nil

	case ActionAPIKeyManage:
		return forbidden(KindAdminRequired, "admin access required")

	default:
		return ErrForbidden("unknown action")
	}
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
		return forbidden(KindCapability, "domain creation not allowed")
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
	return forbidden(KindOwnership, "not your domain")
}

// checkZoneAccess verifies the actor has access to the zone via
// EffectivePermission.AllowedZoneIDs. Admins are handled before this is called.
func (a *Authorizer) checkZoneAccess(actor Actor, res Resource) error {
	if actor.TenantWide {
		if res.ZoneID != (uuid.UUID{}) && !actor.Permission.AllowsZone(res.ZoneID) {
			return forbidden(KindZoneNotAllowed, "zone not in allowed list")
		}
		return nil
	}
	if res.ZoneID == (uuid.UUID{}) {
		return nil
	}
	if !actor.Permission.AllowsZone(res.ZoneID) {
		return forbidden(KindZoneNotAllowed, "zone not in allowed list")
	}
	return nil
}

func (a *Authorizer) checkSendFrom(actor Actor) error {
	perm := actor.Permission
	if perm != nil && !perm.CanSend {
		return forbidden(KindCapability, "sending not allowed")
	}
	return nil
}

// ---------------------------------------------------------------------------
// Error types
// ---------------------------------------------------------------------------

// Kind classifies why authorization was denied, so callers and tests can
// distinguish denial reasons without string-matching the message.
type Kind string

const (
	KindForbidden       Kind = "forbidden"        // generic / unclassified denial
	KindTenantIsolation Kind = "tenant_isolation" // actor and resource are in different tenants
	KindAdminRequired   Kind = "admin_required"   // action needs admin / super-admin
	KindOwnership       Kind = "ownership"        // actor does not own the resource
	KindZoneNotAllowed  Kind = "zone_not_allowed" // zone is outside the actor's allowlist
	KindCapability      Kind = "capability"       // a permission flag (can_create_* / can_send) is off
)

// AuthzError is a typed authorization error carrying a denial Kind.
type AuthzError struct {
	Reason  Kind
	Message string
}

func (e *AuthzError) Error() string {
	return e.Message
}

// ErrForbidden creates a generic (unclassified) authorization denial.
func ErrForbidden(msg string) *AuthzError {
	return forbidden(KindForbidden, msg)
}

// forbidden creates a classified authorization denial.
func forbidden(kind Kind, msg string) *AuthzError {
	return &AuthzError{Reason: kind, Message: msg}
}

// IsAuthzError returns true if the error is an AuthzError.
func IsAuthzError(err error) bool {
	var ae *AuthzError
	return errors.As(err, &ae)
}

// KindOf returns the denial Kind for err, defaulting to KindForbidden for any
// authorization error that was not explicitly classified and for non-authz
// errors.
func KindOf(err error) Kind {
	var ae *AuthzError
	if errors.As(err, &ae) && ae.Reason != "" {
		return ae.Reason
	}
	return KindForbidden
}
