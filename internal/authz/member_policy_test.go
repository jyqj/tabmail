package authz

import (
	"testing"

	"github.com/google/uuid"
	"tabmail/internal/models"
)

func TestIsInteractiveMemberPrincipal(t *testing.T) {
	tenant := uuid.New()
	cases := []struct {
		name  string
		actor Actor
		want  bool
	}{
		{"user in tenant", Actor{Type: PrincipalUser, TenantID: tenant}, true},
		{"api key", Actor{Type: PrincipalAPIKey, TenantID: tenant}, false},
		{"user in other tenant", Actor{Type: PrincipalUser, TenantID: uuid.New()}, false},
	}
	for _, tc := range cases {
		if got := IsInteractiveMemberPrincipal(tc.actor, tenant); got != tc.want {
			t.Fatalf("%s: IsInteractiveMemberPrincipal=%v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestRefreshMemberActor(t *testing.T) {
	tenant := uuid.New()
	actorID := uuid.New()

	cases := []struct {
		name      string
		actor     Actor
		stored    *models.User
		wantOK    bool
		wantAdmin bool
		wantSuper bool
		wantRole  models.UserRole
	}{
		{
			name:   "active admin in tenant",
			actor:  Actor{Type: PrincipalUser, ID: actorID, TenantID: tenant, Role: models.RoleAdmin},
			stored: &models.User{ID: actorID, TenantID: tenant, IsActive: true, Role: models.RoleAdmin},
			wantOK: true, wantAdmin: true, wantSuper: false, wantRole: models.RoleAdmin,
		},
		{
			name:   "stored role wins over cached role (demotion drift)",
			actor:  Actor{Type: PrincipalUser, ID: actorID, TenantID: tenant, Role: models.RoleAdmin, IsAdmin: true},
			stored: &models.User{ID: actorID, TenantID: tenant, IsActive: true, Role: models.RoleUser},
			wantOK: true, wantAdmin: false, wantSuper: false, wantRole: models.RoleUser,
		},
		{
			name:   "stored role wins over cached role (promotion drift)",
			actor:  Actor{Type: PrincipalUser, ID: actorID, TenantID: tenant, Role: models.RoleUser},
			stored: &models.User{ID: actorID, TenantID: tenant, IsActive: true, Role: models.RoleSuperAdmin},
			wantOK: true, wantAdmin: false, wantSuper: true, wantRole: models.RoleSuperAdmin,
		},
		{
			name:   "super admin stored in another tenant",
			actor:  Actor{Type: PrincipalUser, ID: actorID, TenantID: tenant},
			stored: &models.User{ID: actorID, TenantID: uuid.New(), IsActive: true, Role: models.RoleSuperAdmin},
			wantOK: true, wantAdmin: false, wantSuper: true, wantRole: models.RoleSuperAdmin,
		},
		{
			name:   "inactive member",
			actor:  Actor{Type: PrincipalUser, ID: actorID, TenantID: tenant},
			stored: &models.User{ID: actorID, TenantID: tenant, IsActive: false, Role: models.RoleAdmin},
			wantOK: false,
		},
		{
			name:   "cross-tenant non-super-admin",
			actor:  Actor{Type: PrincipalUser, ID: actorID, TenantID: tenant},
			stored: &models.User{ID: actorID, TenantID: uuid.New(), IsActive: true, Role: models.RoleAdmin},
			wantOK: false,
		},
		{
			name:   "stored row id mismatch",
			actor:  Actor{Type: PrincipalUser, ID: actorID, TenantID: tenant},
			stored: &models.User{ID: uuid.New(), TenantID: tenant, IsActive: true, Role: models.RoleAdmin},
			wantOK: false,
		},
		{
			name:   "missing stored row",
			actor:  Actor{Type: PrincipalUser, ID: actorID, TenantID: tenant},
			stored: nil,
			wantOK: false,
		},
		{
			name:   "api key principal",
			actor:  Actor{Type: PrincipalAPIKey, ID: actorID, TenantID: tenant},
			stored: &models.User{ID: actorID, TenantID: tenant, IsActive: true, Role: models.RoleAdmin},
			wantOK: false,
		},
		{
			name:   "user principal selected into another tenant",
			actor:  Actor{Type: PrincipalUser, ID: actorID, TenantID: uuid.New()},
			stored: &models.User{ID: actorID, TenantID: tenant, IsActive: true, Role: models.RoleAdmin},
			wantOK: false,
		},
	}
	for _, tc := range cases {
		refreshed, ok := RefreshMemberActor(tc.actor, tenant, tc.stored)
		if ok != tc.wantOK {
			t.Fatalf("%s: ok=%v, want %v", tc.name, ok, tc.wantOK)
		}
		if !tc.wantOK {
			continue
		}
		if refreshed.IsAdmin != tc.wantAdmin || refreshed.IsSuperAdmin != tc.wantSuper {
			t.Fatalf("%s: flags admin=%v super=%v, want admin=%v super=%v", tc.name, refreshed.IsAdmin, refreshed.IsSuperAdmin, tc.wantAdmin, tc.wantSuper)
		}
		if refreshed.Role != tc.wantRole {
			t.Fatalf("%s: role=%q, want %q", tc.name, refreshed.Role, tc.wantRole)
		}
	}
}

func TestMemberRemovalRequiresAdminCount(t *testing.T) {
	admin := &models.User{IsActive: true, Role: models.RoleAdmin}
	inactiveAdmin := &models.User{IsActive: false, Role: models.RoleAdmin}
	user := &models.User{IsActive: true, Role: models.RoleUser}

	cases := []struct {
		name string
		old  *models.User
		next *models.User
		want bool
	}{
		{"deactivate admin", admin, &models.User{IsActive: false, Role: models.RoleAdmin}, true},
		{"demote admin", admin, &models.User{IsActive: true, Role: models.RoleUser}, true},
		{"delete admin (nil next)", admin, nil, true},
		{"super to active admin retains administrator", &models.User{IsActive: true, Role: models.RoleSuperAdmin}, &models.User{IsActive: true, Role: models.RoleAdmin}, false},
		{"super to ordinary user removes administrator", &models.User{IsActive: true, Role: models.RoleSuperAdmin}, &models.User{IsActive: true, Role: models.RoleUser}, true},
		{"promote user to admin", user, &models.User{IsActive: true, Role: models.RoleAdmin}, false},
		{"deactivate plain user", user, &models.User{IsActive: false, Role: models.RoleUser}, false},
		{"inactive old admin", inactiveAdmin, nil, false},
		{"nil old", nil, nil, false},
	}
	for _, tc := range cases {
		if got := MemberRemovalRequiresAdminCount(tc.old, tc.next); got != tc.want {
			t.Fatalf("%s: requires=%v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestMemberRemovalBlockedAsLastAdmin(t *testing.T) {
	admin := &models.User{IsActive: true, Role: models.RoleAdmin}
	user := &models.User{IsActive: true, Role: models.RoleUser}

	cases := []struct {
		name   string
		old    *models.User
		next   *models.User
		others int
		want   bool
	}{
		{"delete last admin", admin, nil, 0, true},
		{"delete admin with another", admin, nil, 1, false},
		{"demote last admin", admin, &models.User{IsActive: true, Role: models.RoleUser}, 0, true},
		{"demote admin with another", admin, &models.User{IsActive: true, Role: models.RoleUser}, 2, false},
		{"non-admin change never blocked", user, nil, 0, false},
		{"promote never blocked", user, &models.User{IsActive: true, Role: models.RoleAdmin}, 0, false},
	}
	for _, tc := range cases {
		if got := MemberRemovalBlockedAsLastAdmin(tc.old, tc.next, tc.others); got != tc.want {
			t.Fatalf("%s: blocked=%v, want %v", tc.name, got, tc.want)
		}
	}
}
