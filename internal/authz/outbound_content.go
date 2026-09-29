package authz

import (
	"strings"
	"tabmail/internal/models"
)

// OutboundContentKeyMatches checks current key metadata, not a cached owner's
// role. Endpoint-specific RequireScopes remains mandatory: both send:read and
// send:write can produce redacted job responses, but only read permits GET.
// Expiry is checked by the adapter at its final decision clock.
func OutboundContentKeyMatches(a Actor, key *models.TenantAPIKey, job *models.OutboundJob) bool {
	if a.Type != PrincipalAPIKey || key == nil || job == nil || key.ID != a.ID || key.TenantID != a.TenantID || key.TenantID != job.TenantID || key.OwnerUserID == nil || a.OwnerUserID == nil || *key.OwnerUserID != *a.OwnerUserID || !models.ZoneAllowed(key.AllowedZoneIDs, job.ZoneID) {
		return false
	}
	for _, scope := range key.Scopes {
		switch strings.ToLower(strings.TrimSpace(scope)) {
		case "send:read", "send:write":
			return true
		}
	}
	return false
}
