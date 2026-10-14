package authz

import (
	"testing"

	"github.com/google/uuid"
	"tabmail/internal/models"
)

func advanceKeyProofUser(role models.UserRole) (*models.User, Actor) {
	user := &models.User{ID: uuid.New(), TenantID: uuid.New(), Role: role, IsActive: true, SessionVersion: 7}
	version := user.SessionVersion
	actor := Actor{Type: PrincipalUser, ID: user.ID, TenantID: user.TenantID, Role: role,
		IsAdmin:      role == models.RoleAdmin || role == models.RoleSuperAdmin,
		IsSuperAdmin: role == models.RoleSuperAdmin, SessionVersion: &version}
	return user, actor
}

func TestAdvanceAPIKeyIssuerRejectsNonJWTProof(t *testing.T) {
	for _, name := range []string{"missing_user", "public", "owned_api_key", "nil_session", "wrong_id", "stale_session", "wrong_role", "wrong_admin_flag", "foreign_employee", "inactive"} {
		t.Run(name, func(t *testing.T) {
			user, actor := advanceKeyProofUser(models.RoleUser)
			switch name {
			case "missing_user":
				user = nil
			case "public":
				actor.Type = ""
			case "owned_api_key":
				actor.Type, actor.OwnerUserID = PrincipalAPIKey, &user.ID
			case "nil_session":
				actor.SessionVersion = nil
			case "wrong_id":
				actor.ID = uuid.New()
			case "stale_session":
				user.SessionVersion++
			case "wrong_role":
				actor.Role = models.RoleAdmin
			case "wrong_admin_flag":
				actor.IsAdmin = true
			case "foreign_employee":
				actor.TenantID = uuid.New()
			case "inactive":
				user.IsActive = false
			}
			if _, ok := NewAPIKeyIssuer(actor, user); ok {
				t.Fatal("invalid credential was accepted as an API key issuer")
			}
		})
	}
}

func TestAdvanceAPIKeyIssuerPreservesHomeAndVersion(t *testing.T) {
	for _, role := range []models.UserRole{models.RoleUser, models.RoleAdmin, models.RoleSuperAdmin} {
		t.Run(string(role), func(t *testing.T) {
			user, actor := advanceKeyProofUser(role)
			target := user.TenantID
			if role == models.RoleSuperAdmin {
				actor.TenantID, target = uuid.New(), uuid.New()
			}
			actor.Permission = &models.EffectivePermission{CanCreateAPIKeys: true}
			issuer, ok := NewAPIKeyIssuer(actor, user)
			if !ok {
				t.Fatal("legitimate interactive issuer rejected")
			}
			*actor.SessionVersion++
			current, ok := issuer.Refresh(user, target)
			if !ok || issuer.HomeTenantID != user.TenantID || issuer.Actor.TenantID != actor.TenantID ||
				current.TenantID != target || current.ID != user.ID || current.Permission != nil || issuer.Actor.Permission != nil {
				t.Fatal("home identity, owned session proof or target scope was lost")
			}
		})
	}
}

func TestAdvanceAPIKeyIssuerRejectsChangedCurrentIdentity(t *testing.T) {
	for _, name := range []string{"deleted", "home", "id", "inactive", "session", "role"} {
		t.Run(name, func(t *testing.T) {
			user, actor := advanceKeyProofUser(models.RoleSuperAdmin)
			actor.TenantID = uuid.New()
			issuer, ok := NewAPIKeyIssuer(actor, user)
			if !ok {
				t.Fatal("fixture issuer rejected")
			}
			switch name {
			case "deleted":
				user = nil
			case "home":
				user.TenantID = uuid.New()
			case "id":
				user.ID = uuid.New()
			case "inactive":
				user.IsActive = false
			case "session":
				user.SessionVersion++
			case "role":
				user.Role = models.RoleAdmin
			}
			if _, ok := issuer.Refresh(user, uuid.New()); ok {
				t.Fatal("stale platform credential was upgraded to the current identity")
			}
		})
	}
}
