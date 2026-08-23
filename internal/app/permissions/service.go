// Package permissionsapp holds the permission profile and per-user override
// flows behind the /admin/permissions and /admin/users/{id}/permissions
// endpoints. The handlers in internal/api/handlers only decode requests and
// shape responses; every rule about who may see or change a profile lives here.
package permissionsapp

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/models"
)

type storeRepo interface {
	ListPermissionProfiles(ctx context.Context, tenantID *uuid.UUID) ([]*models.PermissionProfile, error)
	CreatePermissionProfile(ctx context.Context, p *models.PermissionProfile) error
	GetPermissionProfile(ctx context.Context, id uuid.UUID) (*models.PermissionProfile, error)
	UpdatePermissionProfile(ctx context.Context, p *models.PermissionProfile) error
	DeletePermissionProfile(ctx context.Context, id uuid.UUID, tenantID *uuid.UUID) error
	GetUser(ctx context.Context, id uuid.UUID) (*models.User, error)
	EffectivePermission(ctx context.Context, userID uuid.UUID) (*models.EffectivePermission, error)
	UpsertUserPermissionOverride(ctx context.Context, o *models.UserPermissionOverride) error
	DeleteUserPermissionOverride(ctx context.Context, userID uuid.UUID) error
	GetZone(ctx context.Context, id uuid.UUID) (*models.DomainZone, error)
}

type Service struct {
	store  storeRepo
	logger zerolog.Logger
}

func NewService(s storeRepo, logger zerolog.Logger) *Service {
	return &Service{store: s, logger: logger.With().Str("service", "permissions").Logger()}
}

// CreateProfileRequest is a full profile description. Every field is supplied,
// so absent booleans mean false rather than "unchanged".
type CreateProfileRequest struct {
	Name              string
	Description       string
	TenantID          *uuid.UUID
	CanSend           bool
	DailySendQuota    int
	DailyReceiveQuota int
	MaxMailboxes      int
	MaxDomains        int
	AllowedZoneIDs    []uuid.UUID
	CanCreateDomains  bool
	CanCreateRoutes   bool
	CanCreateAPIKeys  bool
}

// UpdateProfileRequest describes a partial update: a nil field is left as it is.
type UpdateProfileRequest struct {
	Name              *string
	Description       *string
	CanSend           *bool
	DailySendQuota    *int
	DailyReceiveQuota *int
	MaxMailboxes      *int
	MaxDomains        *int
	AllowedZoneIDs    []uuid.UUID
	CanCreateDomains  *bool
	CanCreateRoutes   *bool
	CanCreateAPIKeys  *bool
}

// ListProfiles returns the profiles visible to the caller: a platform admin
// sees every profile, a tenant admin sees the system ones plus its own.
func (s *Service) ListProfiles(ctx context.Context, actor authz.Actor, tenant *models.Tenant) ([]*models.PermissionProfile, error) {
	var tenantID *uuid.UUID
	if !actor.IsSuperAdmin {
		if tenant == nil {
			return nil, app.Forbidden("no tenant context")
		}
		tenantID = &tenant.ID
	}

	items, err := s.store.ListPermissionProfiles(ctx, tenantID)
	if err != nil {
		return nil, app.Internal(err)
	}
	return items, nil
}

// CreateProfile stores a new profile. A platform admin may create a system
// profile (no tenant) or scope one to a tenant it names; a tenant admin can
// only ever create a profile inside its own tenant.
func (s *Service) CreateProfile(ctx context.Context, actor authz.Actor, tenant *models.Tenant, req CreateProfileRequest) (*models.PermissionProfile, error) {
	if req.Name == "" {
		return nil, app.BadRequest("name is required")
	}

	var profileTenantID *uuid.UUID
	if actor.IsSuperAdmin {
		profileTenantID = req.TenantID
	} else {
		if tenant == nil {
			return nil, app.Forbidden("no tenant context")
		}
		profileTenantID = &tenant.ID
	}

	if err := s.validateZoneScope(ctx, req.AllowedZoneIDs, profileTenantID); err != nil {
		return nil, err
	}

	now := time.Now()
	profile := &models.PermissionProfile{
		ID:                uuid.New(),
		TenantID:          profileTenantID,
		Name:              req.Name,
		Description:       req.Description,
		CanSend:           req.CanSend,
		DailySendQuota:    req.DailySendQuota,
		DailyReceiveQuota: req.DailyReceiveQuota,
		MaxMailboxes:      req.MaxMailboxes,
		MaxDomains:        req.MaxDomains,
		AllowedZoneIDs:    req.AllowedZoneIDs,
		CanCreateDomains:  req.CanCreateDomains,
		CanCreateRoutes:   req.CanCreateRoutes,
		CanCreateAPIKeys:  req.CanCreateAPIKeys,
		IsSystem:          false,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if err := s.store.CreatePermissionProfile(ctx, profile); err != nil {
		return nil, app.Internal(err)
	}
	return profile, nil
}

func (s *Service) UpdateProfile(ctx context.Context, actor authz.Actor, tenant *models.Tenant, id uuid.UUID, req UpdateProfileRequest) (*models.PermissionProfile, error) {
	existing, err := s.writableProfile(ctx, actor, tenant, id, "cannot modify system profile")
	if err != nil {
		return nil, err
	}

	if req.Name != nil {
		existing.Name = *req.Name
	}
	if req.Description != nil {
		existing.Description = *req.Description
	}
	if req.CanSend != nil {
		existing.CanSend = *req.CanSend
	}
	if req.DailySendQuota != nil {
		existing.DailySendQuota = *req.DailySendQuota
	}
	if req.DailyReceiveQuota != nil {
		existing.DailyReceiveQuota = *req.DailyReceiveQuota
	}
	if req.MaxMailboxes != nil {
		existing.MaxMailboxes = *req.MaxMailboxes
	}
	if req.MaxDomains != nil {
		existing.MaxDomains = *req.MaxDomains
	}
	if req.AllowedZoneIDs != nil {
		existing.AllowedZoneIDs = req.AllowedZoneIDs
	}
	if req.CanCreateDomains != nil {
		existing.CanCreateDomains = *req.CanCreateDomains
	}
	if req.CanCreateRoutes != nil {
		existing.CanCreateRoutes = *req.CanCreateRoutes
	}
	if req.CanCreateAPIKeys != nil {
		existing.CanCreateAPIKeys = *req.CanCreateAPIKeys
	}

	if err := s.validateZoneScope(ctx, req.AllowedZoneIDs, existing.TenantID); err != nil {
		return nil, err
	}

	if err := s.store.UpdatePermissionProfile(ctx, existing); err != nil {
		return nil, app.Internal(err)
	}
	return existing, nil
}

func (s *Service) DeleteProfile(ctx context.Context, actor authz.Actor, tenant *models.Tenant, id uuid.UUID) error {
	if _, err := s.writableProfile(ctx, actor, tenant, id, "cannot delete system profile"); err != nil {
		return err
	}

	var deleteTenantID *uuid.UUID
	if !actor.IsSuperAdmin && tenant != nil {
		deleteTenantID = &tenant.ID
	}
	if err := s.store.DeletePermissionProfile(ctx, id, deleteTenantID); err != nil {
		return app.Internal(err)
	}
	return nil
}

// UserPermission resolves the effective permission of a user in the caller's tenant.
func (s *Service) UserPermission(ctx context.Context, tenant *models.Tenant, userID uuid.UUID) (*models.EffectivePermission, error) {
	if _, err := s.tenantUser(ctx, tenant, userID); err != nil {
		return nil, err
	}
	return s.effectivePermission(ctx, userID)
}

// OwnPermission resolves the calling user's own effective permission.
func (s *Service) OwnPermission(ctx context.Context, caller *models.User) (*models.EffectivePermission, error) {
	if caller == nil {
		return nil, app.Forbidden("user context required")
	}
	return s.effectivePermission(ctx, caller.ID)
}

// SetUserOverride stores the per-user exceptions layered on top of a profile.
// The override is returned as stored, with UserID taken from the path rather
// than from the body.
func (s *Service) SetUserOverride(ctx context.Context, tenant *models.Tenant, userID uuid.UUID, override models.UserPermissionOverride) (*models.UserPermissionOverride, error) {
	if _, err := s.tenantUser(ctx, tenant, userID); err != nil {
		return nil, err
	}
	override.UserID = userID

	for _, zoneID := range override.AllowedZoneIDs {
		zone, err := s.store.GetZone(ctx, zoneID)
		if err != nil {
			return nil, app.Internal(err)
		}
		if zone == nil || zone.TenantID != tenant.ID {
			return nil, app.BadRequest(fmt.Sprintf("zone %s not found or does not belong to tenant", zoneID))
		}
	}

	if err := s.store.UpsertUserPermissionOverride(ctx, &override); err != nil {
		return nil, app.Internal(err)
	}
	return &override, nil
}

func (s *Service) DeleteUserOverride(ctx context.Context, tenant *models.Tenant, userID uuid.UUID) error {
	if _, err := s.tenantUser(ctx, tenant, userID); err != nil {
		return err
	}
	if err := s.store.DeleteUserPermissionOverride(ctx, userID); err != nil {
		return app.Internal(err)
	}
	return nil
}

// writableProfile loads a profile the caller is allowed to change. A profile in
// another tenant reports as missing rather than forbidden so the endpoint does
// not confirm that an id exists elsewhere.
func (s *Service) writableProfile(ctx context.Context, actor authz.Actor, tenant *models.Tenant, id uuid.UUID, systemMsg string) (*models.PermissionProfile, error) {
	existing, err := s.store.GetPermissionProfile(ctx, id)
	if err != nil {
		return nil, app.Internal(err)
	}
	if existing == nil {
		return nil, app.NotFound("permission profile not found")
	}
	if existing.IsSystem {
		return nil, app.Forbidden(systemMsg)
	}
	if !actor.IsSuperAdmin {
		if tenant == nil {
			return nil, app.Forbidden("no tenant context")
		}
		if existing.TenantID == nil || *existing.TenantID != tenant.ID {
			return nil, app.NotFound("permission profile not found")
		}
	}
	return existing, nil
}

// validateZoneScope rejects zone ids that the profile's tenant does not own.
// A global profile is reusable across tenants, so it cannot carry tenant-local
// zone ids at all.
func (s *Service) validateZoneScope(ctx context.Context, zoneIDs []uuid.UUID, tenantID *uuid.UUID) error {
	if len(zoneIDs) == 0 {
		return nil
	}
	if tenantID == nil {
		return app.BadRequest("allowed_zone_ids require a tenant-scoped permission profile")
	}
	for _, zoneID := range zoneIDs {
		zone, err := s.store.GetZone(ctx, zoneID)
		if err != nil {
			return app.Internal(err)
		}
		if zone == nil || zone.TenantID != *tenantID {
			return app.BadRequest(fmt.Sprintf("zone %s not found or does not belong to target tenant", zoneID))
		}
	}
	return nil
}

// tenantUser loads a user that the caller's tenant owns, reporting anyone else
// as missing.
func (s *Service) tenantUser(ctx context.Context, tenant *models.Tenant, userID uuid.UUID) (*models.User, error) {
	if tenant == nil {
		return nil, app.Forbidden("no tenant context")
	}
	user, err := s.store.GetUser(ctx, userID)
	if err != nil {
		return nil, app.Internal(err)
	}
	if user == nil || user.TenantID != tenant.ID {
		return nil, app.NotFound("user not found")
	}
	return user, nil
}

func (s *Service) effectivePermission(ctx context.Context, userID uuid.UUID) (*models.EffectivePermission, error) {
	perm, err := s.store.EffectivePermission(ctx, userID)
	if err != nil {
		return nil, app.Internal(err)
	}
	return perm, nil
}
