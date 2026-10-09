package authz

import (
	"tabmail/internal/models"
)

// OutboundContentKeyMatches checks current key metadata, not a cached owner's
// role. Endpoint-specific RequireScopes remains mandatory: both send:read and
// send:write can produce redacted job responses, but only read permits GET.
// Expiry is checked by the adapter at its final decision clock.
func OutboundContentKeyMatches(a Actor, key *models.TenantAPIKey, job *models.OutboundJob) bool {
	if !OutboundKeyIdentityMatches(a, key) || job == nil || key.TenantID != job.TenantID || key.OwnerUserID == nil || !models.ZoneAllowed(key.AllowedZoneIDs, job.ZoneID) {
		return false
	}
	return OutboundKeyHasScope(key, "send:read", "send:write")
}
