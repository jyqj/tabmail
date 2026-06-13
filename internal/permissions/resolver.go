package permissions

import (
	"github.com/google/uuid"
	"tabmail/internal/models"
)

// DefaultPermission returns the hardcoded default when no profile or override exists.
func DefaultPermission() *models.EffectivePermission {
	return &models.EffectivePermission{
		CanSend:           false,
		DailySendQuota:    0,
		DailyReceiveQuota: 500,
		MaxMailboxes:      10,
		MaxDomains:        1,
		AllowedZoneIDs:    nil,
		CanCreateDomains:  false,
		CanCreateRoutes:   false,
		CanCreateAPIKeys:  true,
	}
}

// Resolve merges a permission profile with optional user overrides.
// Override values (non-nil) take precedence over profile values.
func Resolve(profile *models.PermissionProfile, override *models.UserPermissionOverride) *models.EffectivePermission {
	if profile == nil {
		return DefaultPermission()
	}

	perm := &models.EffectivePermission{
		CanSend:           profile.CanSend,
		DailySendQuota:    profile.DailySendQuota,
		DailyReceiveQuota: profile.DailyReceiveQuota,
		MaxMailboxes:      profile.MaxMailboxes,
		MaxDomains:        profile.MaxDomains,
		AllowedZoneIDs:    profile.AllowedZoneIDs,
		CanCreateDomains:  profile.CanCreateDomains,
		CanCreateRoutes:   profile.CanCreateRoutes,
		CanCreateAPIKeys:  profile.CanCreateAPIKeys,
	}

	if override == nil {
		return perm
	}

	// Apply non-nil overrides
	if override.CanSend != nil {
		perm.CanSend = *override.CanSend
	}
	if override.DailySendQuota != nil {
		perm.DailySendQuota = *override.DailySendQuota
	}
	if override.DailyReceiveQuota != nil {
		perm.DailyReceiveQuota = *override.DailyReceiveQuota
	}
	if override.MaxMailboxes != nil {
		perm.MaxMailboxes = *override.MaxMailboxes
	}
	if override.MaxDomains != nil {
		perm.MaxDomains = *override.MaxDomains
	}
	if override.AllowedZoneIDs != nil {
		perm.AllowedZoneIDs = override.AllowedZoneIDs
	}
	if override.CanCreateDomains != nil {
		perm.CanCreateDomains = *override.CanCreateDomains
	}
	if override.CanCreateRoutes != nil {
		perm.CanCreateRoutes = *override.CanCreateRoutes
	}
	if override.CanCreateAPIKeys != nil {
		perm.CanCreateAPIKeys = *override.CanCreateAPIKeys
	}

	return perm
}

// IsZoneAllowed checks whether a zone ID is in the allowed list.
// If AllowedZoneIDs is nil/empty, all zones are allowed. The rule itself lives
// on models.EffectivePermission; this is a thin delegate.
func IsZoneAllowed(perm *models.EffectivePermission, zoneID uuid.UUID) bool {
	return perm.AllowsZone(zoneID)
}

// IsUnlimited returns true if the value is 0, which means unlimited.
func IsUnlimited(v int) bool {
	return v == 0
}
