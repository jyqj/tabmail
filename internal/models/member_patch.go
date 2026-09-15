package models

import "github.com/google/uuid"

// UserAdminPatch preserves absent fields. All administrative updates use this
// patch under the tenant/target locks; an old display-name form must never
// overwrite a concurrent role change or reactivate a disabled account.
type UserAdminPatch struct {
 Role *UserRole
 IsActive *bool
 DisplayName *string
 SetPermissionProfile bool
 PermissionProfileID *uuid.UUID
}

func (p UserAdminPatch) Apply(u *User) {
 if p.Role != nil { u.Role = *p.Role }
 if p.IsActive != nil { u.IsActive = *p.IsActive }
 if p.DisplayName != nil { u.DisplayName = *p.DisplayName }
 if p.SetPermissionProfile { u.PermissionProfileID = p.PermissionProfileID }
}

func IsActiveAdministrator(u *User) bool {
 return u != nil && u.IsActive && (u.Role == RoleAdmin || u.Role == RoleSuperAdmin)
}
