package authz

import (
	"strings"
	"tabmail/internal/models"
)

// OutboundKeyIdentityMatches never trusts cached administrative flags and
// preserves the distinction between an ownerless key and a reassigned key.
func OutboundKeyIdentityMatches(a Actor, key *models.TenantAPIKey) bool {
	if a.Type != PrincipalAPIKey || key == nil || key.ID != a.ID || key.TenantID != a.TenantID {
		return false
	}
	if (a.OwnerUserID == nil) != (key.OwnerUserID == nil) {
		return false
	}
	return a.OwnerUserID == nil || *a.OwnerUserID == *key.OwnerUserID
}

// Only outbound-specific scopes are admitted. A send:write response and a
// send:read GET have different authority; a wildcard is not an outbound scope.
func OutboundKeyHasScope(key *models.TenantAPIKey, required ...string) bool {
	if key == nil {
		return false
	}
	for _, want := range required {
		if want != "send:read" && want != "send:write" {
			continue
		}
		for _, got := range key.Scopes {
			if strings.ToLower(strings.TrimSpace(got)) == want {
				return true
			}
		}
	}
	return false
}
