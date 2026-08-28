package authapp

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/models"
)

func TestTenantAdminMayManageOrdinaryMemberOnly(t *testing.T) {
	store := newAuthTestStore()
	tenant := &models.Tenant{ID: uuid.New()}
	admin := store.addUser(&models.User{TenantID: tenant.ID, Email: "admin@example.test", Role: models.RoleAdmin, IsActive: true})
	member := store.addUser(&models.User{TenantID: tenant.ID, Email: "member@example.test", Role: models.RoleUser, IsActive: true})
	peerAdmin := store.addUser(&models.User{TenantID: tenant.ID, Email: "peer@example.test", Role: models.RoleAdmin, IsActive: true})
	svc := newTestService(t, store, Config{})
	actor := authz.Actor{Type: authz.PrincipalUser, ID: admin.ID, TenantID: tenant.ID, Role: models.RoleAdmin, IsAdmin: true}

	name := "Managed Member"
	updated, err := svc.UpdateUser(context.Background(), actor, tenant, member.ID, UpdateUserRequest{DisplayName: &name})
	if err != nil {
		t.Fatal(err)
	}
	if updated.DisplayName != name {
		t.Fatalf("expected display name %q, got %q", name, updated.DisplayName)
	}

	promote := string(models.RoleAdmin)
	_, err = svc.UpdateUser(context.Background(), actor, tenant, member.ID, UpdateUserRequest{Role: &promote})
	requireKind(t, err, app.KindForbidden)

	active := false
	_, err = svc.UpdateUser(context.Background(), actor, tenant, peerAdmin.ID, UpdateUserRequest{IsActive: &active})
	requireKind(t, err, app.KindForbidden)

	if err := svc.DeleteUser(context.Background(), actor, tenant, peerAdmin.ID); err == nil {
		t.Fatal("tenant admin must not delete a peer administrator")
	} else {
		requireKind(t, err, app.KindForbidden)
	}
	if err := svc.DeleteUser(context.Background(), actor, tenant, member.ID); err != nil {
		t.Fatalf("tenant admin should delete an ordinary member: %v", err)
	}
}

func TestTenantAdminCannotMutateSelfOrForeignTenant(t *testing.T) {
	store := newAuthTestStore()
	tenant := &models.Tenant{ID: uuid.New()}
	foreignTenant := &models.Tenant{ID: uuid.New()}
	admin := store.addUser(&models.User{TenantID: tenant.ID, Email: "admin@example.test", Role: models.RoleAdmin, IsActive: true})
	foreign := store.addUser(&models.User{TenantID: foreignTenant.ID, Email: "foreign@example.test", Role: models.RoleUser, IsActive: true})
	svc := newTestService(t, store, Config{})
	actor := authz.Actor{Type: authz.PrincipalUser, ID: admin.ID, TenantID: tenant.ID, Role: models.RoleAdmin, IsAdmin: true}

	role := string(models.RoleUser)
	_, err := svc.UpdateUser(context.Background(), actor, tenant, admin.ID, UpdateUserRequest{Role: &role})
	requireKind(t, err, app.KindForbidden)

	name := "leak"
	_, err = svc.UpdateUser(context.Background(), actor, tenant, foreign.ID, UpdateUserRequest{DisplayName: &name})
	requireKind(t, err, app.KindNotFound)
}

func TestPlatformAdminMayManageSelectedTenantAdministrators(t *testing.T) {
	store := newAuthTestStore()
	tenant := &models.Tenant{ID: uuid.New()}
	platformTenant := &models.Tenant{ID: uuid.New()}
	root := store.addUser(&models.User{TenantID: platformTenant.ID, Email: "root@example.test", Role: models.RoleSuperAdmin, IsActive: true})
	admin := store.addUser(&models.User{TenantID: tenant.ID, Email: "admin@example.test", Role: models.RoleAdmin, IsActive: true})
	svc := newTestService(t, store, Config{})
	actor := authz.Actor{Type: authz.PrincipalUser, ID: root.ID, TenantID: tenant.ID, Role: models.RoleSuperAdmin, IsSuperAdmin: true, IsAdmin: true}

	active := false
	updated, err := svc.UpdateUser(context.Background(), actor, tenant, admin.ID, UpdateUserRequest{IsActive: &active})
	if err != nil {
		t.Fatal(err)
	}
	if updated.IsActive {
		t.Fatal("expected selected-tenant administrator to be deactivated")
	}
}
