package authz_test

import (
	"testing"

	"github.com/google/uuid"
	"tabmail/internal/authz"
	"tabmail/internal/models"
)

// These independent boundary cases supplement the real PostgreSQL issuance
// suite. They make no database, lock, rollback or new TODO completion claim.
func advanceIssuerReviewIdentity() (authz.Actor, *models.User) {
	u := &models.User{ID: uuid.New(), TenantID: uuid.New(), Role: models.RoleUser, IsActive: true}
	version := int64(0)
	a := authz.Actor{Type: authz.PrincipalUser, ID: u.ID, TenantID: u.TenantID, Role: u.Role, SessionVersion: &version}
	return a, u
}

func TestAdvanceAPIKeyIssuerReviewProof(t *testing.T) {
	t.Run("owns_zero_version_and_discards_cached_permission", func(t *testing.T) {
		a, u := advanceIssuerReviewIdentity()
		a.Permission = &models.EffectivePermission{CanSend: true, CanCreateAPIKeys: true}
		issuer, ok := authz.NewAPIKeyIssuer(a, u)
		if !ok || issuer.Actor.Permission != nil || issuer.Actor.SessionVersion == a.SessionVersion {
			t.Fatal("issuer did not own its zero-version JWT proof independently of cached permissions")
		}
		*a.SessionVersion = 9
		current, ok := issuer.Refresh(u, u.TenantID)
		if !ok || *issuer.Actor.SessionVersion != 0 || current.Permission != nil {
			t.Fatal("mutating the caller snapshot changed the owned JWT proof")
		}
	})
	t.Run("missing_JWT_version", func(t *testing.T) {
		a, u := advanceIssuerReviewIdentity()
		a.SessionVersion = nil
		if _, ok := authz.NewAPIKeyIssuer(a, u); ok {
			t.Fatal("trusted internal actor without JWT proof was admitted as a credential issuer")
		}
	})
	t.Run("owned_API_key_cannot_mint", func(t *testing.T) {
		a, u := advanceIssuerReviewIdentity()
		a.Type, a.OwnerUserID = authz.PrincipalAPIKey, &u.ID
		if _, ok := authz.NewAPIKeyIssuer(a, u); ok {
			t.Fatal("owned API-key principal inherited its owner's JWT issuing authority")
		}
	})
	t.Run("inconsistent_admin_claim", func(t *testing.T) {
		a, u := advanceIssuerReviewIdentity()
		a.IsSuperAdmin = true
		if _, ok := authz.NewAPIKeyIssuer(a, u); ok {
			t.Fatal("cached admin flag overrode the authenticated user role")
		}
	})
	t.Run("missing_authenticated_user", func(t *testing.T) {
		a, _ := advanceIssuerReviewIdentity()
		if _, ok := authz.NewAPIKeyIssuer(a, nil); ok {
			t.Fatal("issuer accepted an actor without an authenticated home identity")
		}
	})
	t.Run("current_home_identity_changed", func(t *testing.T) {
		a, u := advanceIssuerReviewIdentity()
		a.Role, a.IsSuperAdmin, u.Role = models.RoleSuperAdmin, true, models.RoleSuperAdmin
		issuer, ok := authz.NewAPIKeyIssuer(a, u)
		if !ok {
			t.Fatal("valid platform issuer rejected")
		}
		current := *u
		current.TenantID = uuid.New()
		if _, ok := issuer.Refresh(&current, uuid.New()); ok {
			t.Fatal("current super role erased the authenticated home-tenant proof")
		}
	})
	t.Run("non_super_cannot_select_or_target_foreign_tenant", func(t *testing.T) {
		a, u := advanceIssuerReviewIdentity()
		issuer, ok := authz.NewAPIKeyIssuer(a, u)
		if !ok {
			t.Fatal("valid local issuer rejected")
		}
		if _, ok := issuer.Refresh(u, uuid.New()); ok {
			t.Fatal("ordinary issuer reached a different target company")
		}
		a.TenantID = uuid.New()
		if _, ok := authz.NewAPIKeyIssuer(a, u); ok {
			t.Fatal("ordinary issuer selected a different company")
		}
	})
	t.Run("super_home_selected_and_target_are_distinct", func(t *testing.T) {
		a, u := advanceIssuerReviewIdentity()
		a.Role, a.IsSuperAdmin, u.Role = models.RoleSuperAdmin, true, models.RoleSuperAdmin
		a.TenantID = uuid.New()
		target := uuid.New()
		issuer, ok := authz.NewAPIKeyIssuer(a, u)
		if !ok {
			t.Fatal("valid selected-company platform issuer rejected")
		}
		current, ok := issuer.Refresh(u, target)
		if !ok || current.TenantID != target || current.ID != u.ID || !current.IsGlobalAdmin() || issuer.HomeTenantID != u.TenantID || issuer.Actor.TenantID != a.TenantID {
			t.Fatal("platform issuance confused the JWT home, selected company and target company")
		}
	})
}

func TestAdvanceAPIKeyIssuerReviewZoneBoundary(t *testing.T) {
	for _, mode := range []string{"none", "list", "unrecognized-policy"} {
		t.Run("unrepresentable_"+mode, func(t *testing.T) {
			a, _ := advanceIssuerReviewIdentity()
			a.Permission = &models.EffectivePermission{CanCreateAPIKeys: true, DomainAccessMode: mode}
			key := &models.TenantAPIKey{TenantID: a.TenantID, Scopes: []string{"domains:read"}}
			err := authz.ConfigureIssuedAPIKey(a, key)
			if err == nil {
				// Existing key storage interprets nil/empty IDs as unrestricted.
				// An owner later granted all zones must not expand an inherited
				// empty issuing range into an unrestricted old credential.
				laterOwner := &models.EffectivePermission{DomainAccessMode: "all"}
				laterOwner.NarrowZones(key.AllowedZoneIDs)
				t.Fatalf("deny-all issuance produced a usable future credential: later_owner_allows_zone=%t", laterOwner.AllowsZone(uuid.New()))
			}
			if !authz.IsAuthzError(err) {
				t.Fatalf("unrepresentable issuing range should be an authorization denial: %T", err)
			}
		})
	}
	t.Run("missing_current_permission", func(t *testing.T) {
		a, _ := advanceIssuerReviewIdentity()
		key := &models.TenantAPIKey{TenantID: a.TenantID, Scopes: []string{"domains:read"}}
		if err := authz.ConfigureIssuedAPIKey(a, key); !authz.IsAuthzError(err) {
			t.Fatalf("ordinary issuer without a current permission snapshot was admitted: %v", err)
		}
	})
	t.Run("inherited_list_stays_bounded_after_owner_expands", func(t *testing.T) {
		a, _ := advanceIssuerReviewIdentity()
		allowed, other, forgedOwner := uuid.New(), uuid.New(), uuid.New()
		a.Permission = &models.EffectivePermission{CanCreateAPIKeys: true, DomainAccessMode: "list", AllowedZoneIDs: []uuid.UUID{allowed}}
		key := &models.TenantAPIKey{TenantID: a.TenantID, OwnerUserID: &forgedOwner, Scopes: []string{"domains:read"}}
		if err := authz.ConfigureIssuedAPIKey(a, key); err != nil {
			t.Fatal(err)
		}
		a.Permission.AllowedZoneIDs[0] = other
		laterOwner := &models.EffectivePermission{DomainAccessMode: "all"}
		laterOwner.NarrowZones(key.AllowedZoneIDs)
		if key.OwnerUserID == nil || *key.OwnerUserID != a.ID || !laterOwner.AllowsZone(allowed) || laterOwner.AllowsZone(other) {
			t.Fatal("inherited key range aliased the issuer snapshot, lost ownership, or expanded with later owner permissions")
		}
	})
	t.Run("explicit_zone_cannot_exceed_none", func(t *testing.T) {
		a, _ := advanceIssuerReviewIdentity()
		a.Permission = &models.EffectivePermission{CanCreateAPIKeys: true, DomainAccessMode: "none"}
		key := &models.TenantAPIKey{TenantID: a.TenantID, Scopes: []string{"domains:read"}, AllowedZoneIDs: []uuid.UUID{uuid.New()}}
		if err := authz.ConfigureIssuedAPIKey(a, key); !authz.IsAuthzError(err) {
			t.Fatalf("explicit zone escaped the current deny-all policy: %v", err)
		}
	})
	t.Run("legacy_unrestricted_remains_valid", func(t *testing.T) {
		a, _ := advanceIssuerReviewIdentity()
		a.Permission = &models.EffectivePermission{CanCreateAPIKeys: true}
		key := &models.TenantAPIKey{TenantID: a.TenantID, Scopes: []string{"domains:read"}}
		if err := authz.ConfigureIssuedAPIKey(a, key); err != nil {
			t.Fatal(err)
		}
		if len(key.AllowedZoneIDs) != 0 || key.OwnerUserID == nil || *key.OwnerUserID != a.ID {
			t.Fatal("legacy unrestricted owned key changed its issuing scope")
		}
	})
}
