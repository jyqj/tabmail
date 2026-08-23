package authz

import (
	"reflect"
	"testing"

	"github.com/google/uuid"
	"tabmail/internal/models"
)

func TestListScope(t *testing.T) {
	tests := []struct {
		name       string
		actor      Actor
		wantAll    bool
		wantUser   bool
		wantAPIKey bool
	}{
		{"super admin sees all", Actor{IsSuperAdmin: true, TenantID: tenantA}, true, false, false},
		{"tenant admin sees all", Actor{IsAdmin: true, Type: PrincipalUser, ID: userID, TenantID: tenantA}, true, false, false},
		{"user sees own", Actor{Type: PrincipalUser, ID: userID, TenantID: tenantA}, false, true, false},
		{"api key sees own", Actor{Type: PrincipalAPIKey, ID: keyID, TenantID: tenantA}, false, false, true},
		{"unknown principal sees nothing", Actor{}, false, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scope := ListScope(tt.actor)
			if scope.AllInTenant != tt.wantAll {
				t.Errorf("AllInTenant = %v, want %v", scope.AllInTenant, tt.wantAll)
			}
			if (scope.UserID != nil) != tt.wantUser {
				t.Errorf("UserID set = %v, want %v", scope.UserID != nil, tt.wantUser)
			}
			if scope.UserID != nil && *scope.UserID != tt.actor.ID {
				t.Errorf("UserID = %v, want actor ID %v", *scope.UserID, tt.actor.ID)
			}
			if (scope.APIKeyID != nil) != tt.wantAPIKey {
				t.Errorf("APIKeyID set = %v, want %v", scope.APIKeyID != nil, tt.wantAPIKey)
			}
			if scope.APIKeyID != nil && *scope.APIKeyID != tt.actor.ID {
				t.Errorf("APIKeyID = %v, want actor ID %v", *scope.APIKeyID, tt.actor.ID)
			}
		})
	}
}

func TestCanAccessOwned(t *testing.T) {
	owner := userID
	other := otherUserID
	key := keyID

	tests := []struct {
		name          string
		actor         Actor
		ownerUserID   *uuid.UUID
		ownerAPIKeyID *uuid.UUID
		want          bool
	}{
		{"tenant admin accesses any", Actor{IsAdmin: true, TenantID: tenantA}, &other, nil, true},
		{"user accesses own", Actor{Type: PrincipalUser, ID: userID, TenantID: tenantA}, &owner, nil, true},
		{"user denied other's", Actor{Type: PrincipalUser, ID: userID, TenantID: tenantA}, &other, nil, false},
		{"user denied when ownerless", Actor{Type: PrincipalUser, ID: userID, TenantID: tenantA}, nil, nil, false},
		{"api key accesses own", Actor{Type: PrincipalAPIKey, ID: keyID, TenantID: tenantA}, nil, &key, true},
		{"api key denied other key", Actor{Type: PrincipalAPIKey, ID: keyID, TenantID: tenantA}, nil, nil, false},
		{"api key not matched by user owner", Actor{Type: PrincipalAPIKey, ID: keyID, TenantID: tenantA}, &owner, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CanAccessOwned(tt.actor, tt.ownerUserID, tt.ownerAPIKeyID); got != tt.want {
				t.Errorf("CanAccessOwned = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestZoneListScope(t *testing.T) {
	// Allowlist fixtures.
	empty := []uuid.UUID{}
	allow := []uuid.UUID{zoneID, uuid.MustParse("30000000-0000-0000-0000-000000000002")}
	otherAllow := []uuid.UUID{uuid.MustParse("30000000-0000-0000-0000-000000000099")}

	uid := userID

	tests := []struct {
		name           string
		actor          Actor
		wantAllZones   bool
		wantZoneIDs    []uuid.UUID
		wantOwnerNil   bool
		wantOwnerIsUID bool
	}{
		// Admin path.
		{"super admin, no allowlist", superAdmin(), true, nil, true, false},
		{"tenant admin, no allowlist", admin(), true, nil, true, false},
		{"tenant admin, with allowlist", Actor{IsAdmin: true, Type: PrincipalUser, ID: userID, TenantID: tenantA, Permission: &models.EffectivePermission{AllowedZoneIDs: allow}}, false, allow, true, false},
		{"super admin, with allowlist", Actor{IsSuperAdmin: true, TenantID: tenantA, Permission: &models.EffectivePermission{AllowedZoneIDs: allow}}, false, allow, true, false},

		// Tenant-wide (ownerless integration) API key.
		{"ownerless key, no allowlist", Actor{Type: PrincipalAPIKey, ID: keyID, TenantID: tenantA, TenantWide: true, Permission: &models.EffectivePermission{AllowedZoneIDs: empty}}, true, nil, true, false},
		{"ownerless key, with allowlist", Actor{Type: PrincipalAPIKey, ID: keyID, TenantID: tenantA, TenantWide: true, Permission: &models.EffectivePermission{AllowedZoneIDs: allow}}, false, allow, true, false},

		// Regular user.
		{"regular user, no allowlist", regularUser(&models.EffectivePermission{AllowedZoneIDs: empty}), true, nil, false, true},
		{"regular user, with allowlist", regularUser(&models.EffectivePermission{AllowedZoneIDs: allow}), false, allow, false, true},
		{"regular user, nil permission", Actor{Type: PrincipalUser, ID: userID, TenantID: tenantA, Role: models.RoleUser}, true, nil, false, true},

		// User-owned API key.
		{"user-owned key, no allowlist", Actor{Type: PrincipalAPIKey, ID: keyID, TenantID: tenantA, OwnerUserID: &uid, Permission: &models.EffectivePermission{AllowedZoneIDs: empty}}, true, nil, false, true},
		{"user-owned key, with allowlist", Actor{Type: PrincipalAPIKey, ID: keyID, TenantID: tenantA, OwnerUserID: &uid, Permission: &models.EffectivePermission{AllowedZoneIDs: otherAllow}}, false, otherAllow, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := ZoneListScope(tt.actor, tenantA)
			if f.TenantID != tenantA {
				t.Errorf("TenantID = %v, want %v", f.TenantID, tenantA)
			}
			if f.AllZones != tt.wantAllZones {
				t.Errorf("AllZones = %v, want %v", f.AllZones, tt.wantAllZones)
			}
			if tt.wantAllZones {
				if len(f.ZoneIDs) != 0 {
					t.Errorf("ZoneIDs = %v, want empty when AllZones=true", f.ZoneIDs)
				}
			} else if !reflect.DeepEqual(f.ZoneIDs, tt.wantZoneIDs) {
				t.Errorf("ZoneIDs = %v, want %v", f.ZoneIDs, tt.wantZoneIDs)
			}
			switch {
			case tt.wantOwnerNil && f.OwnerUserID != nil:
				t.Errorf("OwnerUserID = %v, want nil", f.OwnerUserID)
			case tt.wantOwnerIsUID && (f.OwnerUserID == nil || *f.OwnerUserID != userID):
				t.Errorf("OwnerUserID = %v, want %v", f.OwnerUserID, userID)
			}
		})
	}
}

func TestOwnerListScope(t *testing.T) {
	uid := userID
	kid := keyID

	tests := []struct {
		name            string
		actor           Actor
		wantAllInTenant bool
		wantUserID      *uuid.UUID
		wantAPIKeyID    *uuid.UUID
	}{
		{"super admin sees tenant", superAdmin(), true, nil, nil},
		{"tenant admin sees tenant", admin(), true, nil, nil},
		{"user sees own", regularUser(&models.EffectivePermission{}), false, &uid, nil},
		{"ownerless key sees own", Actor{Type: PrincipalAPIKey, ID: keyID, TenantID: tenantA, TenantWide: true}, false, nil, &kid},
		{"user-owned key sees own", Actor{Type: PrincipalAPIKey, ID: keyID, TenantID: tenantA, OwnerUserID: &uid}, false, nil, &kid},
		{"unknown principal sees nothing", Actor{}, false, nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := OwnerListScope(tt.actor, tenantA)
			if f.TenantID != tenantA {
				t.Errorf("TenantID = %v, want %v", f.TenantID, tenantA)
			}
			if f.AllInTenant != tt.wantAllInTenant {
				t.Errorf("AllInTenant = %v, want %v", f.AllInTenant, tt.wantAllInTenant)
			}
			if !ptrUUIDEq(f.UserID, tt.wantUserID) {
				t.Errorf("UserID = %v, want %v", f.UserID, tt.wantUserID)
			}
			if !ptrUUIDEq(f.APIKeyID, tt.wantAPIKeyID) {
				t.Errorf("APIKeyID = %v, want %v", f.APIKeyID, tt.wantAPIKeyID)
			}
			if f.AllInTenant && (f.UserID != nil || f.APIKeyID != nil) {
				t.Errorf("AllInTenant=true but owner dimension set (mutual-exclusivity violated)")
			}
		})
	}
}

func ptrUUIDEq(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
