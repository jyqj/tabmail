package permissionsapp

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/models"
)

func TestTenantAdminPermissionEndpointsManageOrdinaryMembersOnly(t *testing.T) {
	store := newPermTestStore()
	tenant := &models.Tenant{ID: uuid.New()}
	member := &models.User{ID: uuid.New(), TenantID: tenant.ID, Role: models.RoleUser, IsActive: true}
	peerAdmin := &models.User{ID: uuid.New(), TenantID: tenant.ID, Role: models.RoleAdmin, IsActive: true}
	store.users[member.ID] = member
	store.users[peerAdmin.ID] = peerAdmin
	svc := newTestService(store)
	actor := authz.Actor{Type: authz.PrincipalUser, ID: uuid.New(), TenantID: tenant.ID, IsAdmin: true, Role: models.RoleAdmin}

	if _, err := svc.UserPermissionForActor(context.Background(), actor, tenant, member.ID); err != nil {
		t.Fatalf("tenant admin should inspect an ordinary member: %v", err)
	}
	if _, err := svc.UserPermissionForActor(context.Background(), actor, tenant, peerAdmin.ID); err == nil {
		t.Fatal("tenant admin must not inspect peer-administrator effective permission")
	} else {
		requireKind(t, err, app.KindForbidden)
	}

	canSend := true
	if _, err := svc.SetUserOverrideForActor(context.Background(), actor, tenant, member.ID, models.UserPermissionOverride{CanSend: &canSend}); err != nil {
		t.Fatalf("tenant admin should override an ordinary member: %v", err)
	}
	if _, err := svc.SetUserOverrideForActor(context.Background(), actor, tenant, peerAdmin.ID, models.UserPermissionOverride{CanSend: &canSend}); err == nil {
		t.Fatal("tenant admin must not change a peer administrator override")
	} else {
		requireKind(t, err, app.KindForbidden)
	}
	if err := svc.DeleteUserOverrideForActor(context.Background(), actor, tenant, peerAdmin.ID); err == nil {
		t.Fatal("tenant admin must not clear a peer administrator override")
	} else {
		requireKind(t, err, app.KindForbidden)
	}
}

func TestPlatformAdminPermissionEndpointsRequireSelectedTenant(t *testing.T) {
	store := newPermTestStore()
	tenant := &models.Tenant{ID: uuid.New()}
	otherTenant := &models.Tenant{ID: uuid.New()}
	target := &models.User{ID: uuid.New(), TenantID: tenant.ID, Role: models.RoleAdmin, IsActive: true}
	store.users[target.ID] = target
	svc := newTestService(store)

	wrongScope := authz.Actor{Type: authz.PrincipalUser, ID: uuid.New(), TenantID: otherTenant.ID, IsAdmin: true, IsSuperAdmin: true, Role: models.RoleSuperAdmin}
	if _, err := svc.UserPermissionForActor(context.Background(), wrongScope, tenant, target.ID); err == nil {
		t.Fatal("platform admin must explicitly select the target tenant")
	} else {
		requireKind(t, err, app.KindForbidden)
	}

	selected := wrongScope
	selected.TenantID = tenant.ID
	if _, err := svc.UserPermissionForActor(context.Background(), selected, tenant, target.ID); err != nil {
		t.Fatalf("selected-tenant platform admin should inspect target permission: %v", err)
	}
}
