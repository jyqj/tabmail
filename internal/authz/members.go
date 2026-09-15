package authz

import (
	"github.com/google/uuid"

	"tabmail/internal/models"
)

// CanManageTenantMember is adapted from archive/codex-deep-authz. The actor's
// tenant is the selected, middleware-validated tenant (including impersonation).
// Administrative authority is deliberately separate from mailbox content rights.
func CanManageTenantMember(actor Actor, targetTenantID uuid.UUID, targetRole models.UserRole) bool {
	if targetTenantID == uuid.Nil || actor.TenantID != targetTenantID {
		return false
	}
	if actor.IsSuperAdmin {
		return true
	}
	return actor.IsAdmin && targetRole == models.RoleUser
}
