package authz

import (
	"github.com/google/uuid"
	"tabmail/internal/models"
)

// CanManageTenantMember is the target-aware member-management policy. The
// coarse ActionTenantUsersManage action intentionally remains platform-only
// because it has no target role to constrain. This predicate lets a tenant
// administrator manage ordinary members in the selected tenant without also
// granting power over peer administrators or platform operators.
func CanManageTenantMember(actor Actor, targetTenantID uuid.UUID, targetRole models.UserRole) bool {
	if targetTenantID == uuid.Nil || actor.TenantID != targetTenantID {
		return false
	}
	if actor.IsSuperAdmin {
		return true
	}
	return actor.IsAdmin && targetRole == models.RoleUser
}
