package authz

import (
	"testing"

	"github.com/google/uuid"
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
