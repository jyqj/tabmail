package authz

import (
	"fmt"

	"github.com/google/uuid"
	"tabmail/internal/models"
)

// APIKeyIssuer preserves both the selected company in Actor and the actual
// home company of the JWT user. A platform administrator may select another
// company; replacing Actor.TenantID would otherwise lose that identity proof.
type APIKeyIssuer struct {
	Actor        Actor
	HomeTenantID uuid.UUID
}

func NewAPIKeyIssuer(actor Actor, authenticated *models.User) (APIKeyIssuer, bool) {
	if authenticated == nil {
		return APIKeyIssuer{}, false
	}
	issuer := APIKeyIssuer{Actor: actor, HomeTenantID: authenticated.TenantID}
	if _, ok := issuer.Refresh(authenticated, actor.TenantID); !ok {
		return APIKeyIssuer{}, false
	}
	// Own the authenticated version, not a caller-mutable pointer. Cached
	// permissions are deliberately absent; the adapter must load current ones.
	version := *actor.SessionVersion
	issuer.Actor.SessionVersion = &version
	issuer.Actor.Permission = nil
	return issuer, true
}

// Refresh accepts only the same still-active JWT identity and role. Trusted
// internal actors with no session proof and API keys (including owned keys)
// cannot mint credentials through this entry point. Call under the user fence.
func (issuer APIKeyIssuer) Refresh(current *models.User, target uuid.UUID) (Actor, bool) {
	a := issuer.Actor
	if a.Type != PrincipalUser || a.ID == uuid.Nil || a.TenantID == uuid.Nil ||
		issuer.HomeTenantID == uuid.Nil || target == uuid.Nil || a.SessionVersion == nil ||
		a.OwnerUserID != nil || a.TenantWide || current == nil || !current.IsActive ||
		current.ID != a.ID || current.TenantID != issuer.HomeTenantID ||
		current.Role != a.Role || current.SessionVersion != *a.SessionVersion {
		return Actor{}, false
	}
	if current.Role != models.RoleUser && current.Role != models.RoleAdmin && current.Role != models.RoleSuperAdmin {
		return Actor{}, false
	}
	super := current.Role == models.RoleSuperAdmin
	admin := super || current.Role == models.RoleAdmin
	if a.IsSuperAdmin != super || a.IsTenantAdmin() != admin ||
		!super && (a.TenantID != issuer.HomeTenantID || target != issuer.HomeTenantID) {
		return Actor{}, false
	}
	// The original selected tenant and home identity have both been checked.
	// Only a current super administrator can reach a different target here.
	a.TenantID = target
	a.IsSuperAdmin, a.IsAdmin = super, current.Role == models.RoleAdmin
	a.Permission = nil
	return a, true
}

// ConfigureIssuedAPIKey applies the existing scope/ownership rules to the
// transaction-refreshed issuer. It performs no I/O and never trusts permissions
// captured during HTTP authentication. The adapter checks the resulting zones
// against current, locked zone rows before persisting the key.
func ConfigureIssuedAPIKey(actor Actor, key *models.TenantAPIKey) error {
	if actor.Type != PrincipalUser || actor.TenantID != key.TenantID {
		return ErrForbidden("interactive API key issuer required")
	}
	key.OwnerUserID = nil
	if actor.IsTenantAdmin() {
		return nil
	}
	perm := actor.Permission
	if perm == nil || !perm.CanCreateAPIKeys {
		return ErrForbidden("API key creation not allowed")
	}
	for _, scope := range key.Scopes {
		switch scope {
		case "send:read", "send:write":
			if !perm.CanSend {
				return ErrForbidden("cannot create api key with " + scope + " scope: sending not allowed")
			}
		case "domains:write":
			if !perm.CanCreateDomains {
				return ErrForbidden("cannot create api key with domains:write scope: domain creation not allowed")
			}
		case "routes:write":
			if !perm.CanCreateRoutes {
				return ErrForbidden("cannot create api key with routes:write scope: route creation not allowed")
			}
		case "mailboxes:write", "messages:write", "webhooks:read", "webhooks:write":
			return ErrForbidden("cannot create api key with " + scope + " scope: admin approval required")
		}
	}
	if len(key.AllowedZoneIDs) > 0 {
		for _, zoneID := range key.AllowedZoneIDs {
			if !perm.AllowsZone(zoneID) {
				return ErrForbidden(fmt.Sprintf("zone %s is not in your allowed zone list", zoneID))
			}
		}
	} else if restricted, ids := perm.ZoneScope(); restricted {
		// The persisted key's empty zone array means unrestricted. Do not
		// encode deny-all as that value: a later owner permission expansion
		// would give this existing key authority absent when it was issued.
		if len(ids) == 0 {
			return ErrForbidden("cannot create api key without any permitted zone")
		}
		key.AllowedZoneIDs = append([]uuid.UUID(nil), ids...)
	}
	owner := actor.ID
	key.OwnerUserID = &owner
	return nil
}
