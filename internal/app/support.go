package app

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/models"
)

// PrincipalStore is the minimal store interface needed for principal-tenant validation.
type PrincipalStore interface {
	GetUser(ctx context.Context, id uuid.UUID) (*models.User, error)
	GetAPIKey(ctx context.Context, id uuid.UUID) (*models.TenantAPIKey, error)
}

type AuditStore interface {
	InsertAudit(ctx context.Context, e *models.AuditEntry) error
}

func InsertAudit(ctx context.Context, s AuditStore, logger zerolog.Logger, entry models.AuditEntry) {
	if s == nil {
		return
	}
	if entry.Details == nil {
		entry.Details = json.RawMessage(`{}`)
	}
	if err := s.InsertAudit(ctx, &entry); err != nil {
		logger.Warn().Err(err).Msg("insert audit")
	}
}

func MustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func UUIDPtr(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	v := id
	return &v
}

// CanManageZone checks whether the caller has management access to the given zone.
func CanManageZone(zone *models.DomainZone, tenant *models.Tenant, isAdmin bool, ownerUserID *uuid.UUID, tenantWide bool) bool {
	if zone == nil {
		return false
	}
	if isAdmin {
		return true
	}
	if tenant == nil || zone.TenantID != tenant.ID {
		return false
	}
	if tenantWide {
		return true
	}
	return ownerUserID != nil && zone.OwnerUserID != nil && *ownerUserID == *zone.OwnerUserID
}

// HasZoneAccess checks if a user has at least the given access level to a zone.
// Falls back to OwnerUserID check for backward compatibility.
func HasZoneAccess(zone *models.DomainZone, tenant *models.Tenant, isAdmin bool, ownerUserID *uuid.UUID, tenantWide bool, grantRole models.ZoneGrantRole) bool {
	if zone == nil {
		return false
	}
	if isAdmin {
		return true
	}
	if tenant == nil || zone.TenantID != tenant.ID {
		return false
	}
	if tenantWide {
		return true
	}
	// Check grant role
	if grantRole != "" && grantRole.CanManage() {
		return true
	}
	// Fallback to legacy OwnerUserID check
	return ownerUserID != nil && zone.OwnerUserID != nil && *ownerUserID == *zone.OwnerUserID
}

// EnsureTenantScope validates that a tenant context exists and is usable.
func EnsureTenantScope(tenant *models.Tenant, isAdmin bool) error {
	if tenant == nil {
		return Forbidden("no tenant context")
	}
	if isAdmin && tenant.ID == uuid.Nil {
		return BadRequest("admin requests to tenant-scoped endpoints must include X-Tenant-ID")
	}
	return nil
}

// ValidatePrincipalTenant verifies that the given principal exists and belongs
// to the specified tenant. Returns an error if the principal is from a different
// tenant or does not exist.
func ValidatePrincipalTenant(ctx context.Context, st PrincipalStore, tenantID uuid.UUID, principalType string, principalID uuid.UUID) error {
	switch principalType {
	case "user":
		user, err := st.GetUser(ctx, principalID)
		if err != nil {
			return Internal(err)
		}
		if user == nil {
			return NotFound("user not found")
		}
		if user.TenantID != tenantID {
			return Forbidden("user belongs to different tenant")
		}
	case "api_key":
		key, err := st.GetAPIKey(ctx, principalID)
		if err != nil {
			return Internal(err)
		}
		if key == nil {
			return NotFound("api key not found")
		}
		if key.TenantID != tenantID {
			return Forbidden("api key belongs to different tenant")
		}
	default:
		return BadRequest(fmt.Sprintf("unknown principal type: %s", principalType))
	}
	return nil
}
