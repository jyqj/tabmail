package authz

import (
	"testing"

	"github.com/google/uuid"
	"tabmail/internal/models"
)

func TestCanManageTenantMember(t *testing.T) {
	tenant := uuid.New()
	other := uuid.New()
	admin := Actor{Type: PrincipalUser, ID: uuid.New(), TenantID: tenant, IsAdmin: true, Role: models.RoleAdmin}
	root := Actor{Type: PrincipalUser, ID: uuid.New(), TenantID: tenant, IsAdmin: true, IsSuperAdmin: true, Role: models.RoleSuperAdmin}

	if !CanManageTenantMember(admin, tenant, models.RoleUser) {
		t.Fatal("tenant admin should manage an ordinary member in the selected tenant")
	}
	if CanManageTenantMember(admin, tenant, models.RoleAdmin) {
		t.Fatal("tenant admin must not manage a peer administrator")
	}
	if CanManageTenantMember(admin, other, models.RoleUser) {
		t.Fatal("tenant admin must not manage another tenant")
	}
	if !CanManageTenantMember(root, tenant, models.RoleAdmin) {
		t.Fatal("platform administrator should manage a selected-tenant administrator")
	}
	root.TenantID = other
	if CanManageTenantMember(root, tenant, models.RoleAdmin) {
		t.Fatal("platform administrator must explicitly select the target tenant")
	}
}
