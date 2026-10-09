package authz

import (
	"github.com/google/uuid"
	"tabmail/internal/models"
)

// IsInteractiveMemberPrincipal reports whether the actor can even be a
// company member administrator: an interactive user principal selected into
// the target tenant. Kept as its own predicate so the Pg adapter can skip the
// FOR SHARE re-read for principals that can never pass, without duplicating
// the rule.
func IsInteractiveMemberPrincipal(actor Actor, tenant uuid.UUID) bool {
	return actor.Type == PrincipalUser && actor.TenantID == tenant
}

// RefreshMemberActor re-derives the actor's administrative flags from the
// stored user row just re-read inside a guarded transaction. It returns the
// refreshed actor and whether the actor still represents an active member of
// the tenant: the stored row must match the actor's ID, be active, and belong
// to the tenant unless it is a super admin (who acts across tenants).
//
// The stored Role always wins over the actor's cached Role, so a stale actor
// cache (role changed or demoted elsewhere) cannot carry authorization.
//
// Pure: no store access. The adapters translate !ok into their own
// forbidden error text, keeping the external guard error semantics unchanged.
func RefreshMemberActor(actor Actor, tenant uuid.UUID, u *models.User) (Actor, bool) {
	if !IsInteractiveMemberPrincipal(actor, tenant) {
		return actor, false
	}
	if u == nil || u.ID != actor.ID || !u.IsActive || (u.TenantID != tenant && u.Role != models.RoleSuperAdmin) {
		return actor, false
	}
	// The HTTP authentication read precedes this transaction. Password change,
	// revocation, or freeze/reactivation may have advanced the version while
	// it waited for the user fence. Do not refresh an old credential into a
	// new session merely because the same user is active again.
	if actor.SessionVersion != nil && *actor.SessionVersion != u.SessionVersion {
		return actor, false
	}
	actor.IsAdmin = u.Role == models.RoleAdmin
	actor.IsSuperAdmin = u.Role == models.RoleSuperAdmin
	actor.Role = u.Role
	return actor, true
}

// MemberRemovalRequiresAdminCount reports whether the old→next member change
// removes or demotes an active administrator, so the caller must count the
// other active administrators of the tenant before applying it. Any other
// transition can never strand a tenant without an administrator.
func MemberRemovalRequiresAdminCount(old, next *models.User) bool {
	return models.IsActiveAdministrator(old) && !models.IsActiveAdministrator(next)
}

// MemberRemovalBlockedAsLastAdmin reports whether the old→next change must be
// rejected because it would leave the tenant without any other active
// administrator. otherActiveAdmins is the count of active administrators
// (admin/super_admin) in the tenant excluding old, gathered by the caller
// (SQL count or in-memory scan). Pure: the adapters own counting, locking,
// and error translation.
func MemberRemovalBlockedAsLastAdmin(old, next *models.User, otherActiveAdmins int) bool {
	if !MemberRemovalRequiresAdminCount(old, next) {
		return false
	}
	return otherActiveAdmins == 0
}
