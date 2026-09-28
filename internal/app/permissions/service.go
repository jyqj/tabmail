// Package permissions owns the business rules behind the permission profile
// and user-override endpoints: tenant scoping, system-profile immutability,
// and allowed-zone validation. HTTP parsing and responses stay in the handler.
package permissions

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/models"
)

// Store is the narrow consumer interface the service needs.
type Store interface {
	ListPermissionProfiles(ctx context.Context, tenantID *uuid.UUID) ([]*models.PermissionProfile, error)
	GetPermissionProfile(ctx context.Context, id uuid.UUID) (*models.PermissionProfile, error)
	CreatePermissionProfile(ctx context.Context, p *models.PermissionProfile) error
	UpdatePermissionProfile(ctx context.Context, p *models.PermissionProfile) error
	DeletePermissionProfile(ctx context.Context, id uuid.UUID, tenantID *uuid.UUID) error
	GetZone(ctx context.Context, id uuid.UUID) (*models.DomainZone, error)
	GetUser(ctx context.Context, id uuid.UUID) (*models.User, error)
	UpsertUserPermissionOverride(ctx context.Context, o *models.UserPermissionOverride) error
	DeleteUserPermissionOverride(ctx context.Context, userID uuid.UUID) error
	EffectivePermission(ctx context.Context, userID uuid.UUID) (*models.EffectivePermission, error)
}

type Service struct {
	store Store
}

func New(st Store) *Service { return &Service{store: st} }

// CreateInput carries the create-profile request body. TenantID is only
// honored for super admins; tenant admins always create in their own tenant.
type CreateInput struct {
	Name              string      `json:"name"`
	Description       string      `json:"description"`
	TenantID          *uuid.UUID  `json:"tenant_id,omitempty"`
	CanSend           bool        `json:"can_send"`
	DailySendQuota    int         `json:"daily_send_quota"`
	DailyReceiveQuota int         `json:"daily_receive_quota"`
	MaxMailboxes      int         `json:"max_mailboxes"`
	MaxDomains        int         `json:"max_domains"`
	AllowedZoneIDs    []uuid.UUID `json:"allowed_zone_ids,omitempty"`
	CanCreateDomains  bool        `json:"can_create_domains"`
	CanCreateRoutes   bool        `json:"can_create_routes"`
	CanCreateAPIKeys  bool        `json:"can_create_api_keys"`
}

// UpdateInput carries the update-profile request body. AllowedZoneIDs follows
// the patch semantics of the original handler: nil means "leave unchanged",
// a non-nil slice (including empty) replaces the list.
type UpdateInput struct {
	Name              *string     `json:"name,omitempty"`
	Description       *string     `json:"description,omitempty"`
	CanSend           *bool       `json:"can_send,omitempty"`
	DailySendQuota    *int        `json:"daily_send_quota,omitempty"`
	DailyReceiveQuota *int        `json:"daily_receive_quota,omitempty"`
	MaxMailboxes      *int        `json:"max_mailboxes,omitempty"`
	MaxDomains        *int        `json:"max_domains,omitempty"`
	AllowedZoneIDs    []uuid.UUID `json:"allowed_zone_ids,omitempty"`
	CanCreateDomains  *bool       `json:"can_create_domains,omitempty"`
	CanCreateRoutes   *bool       `json:"can_create_routes,omitempty"`
	CanCreateAPIKeys  *bool       `json:"can_create_api_keys,omitempty"`
}

// ListProfiles returns profiles visible to the actor: all profiles for a
// super admin, system + own-tenant profiles otherwise.
func (s *Service) ListProfiles(ctx context.Context, actor authz.Actor, tenantCtx *uuid.UUID) ([]*models.PermissionProfile, error) {
	if actor.IsSuperAdmin {
		return s.listProfiles(ctx, nil)
	}
	if tenantCtx == nil {
		return nil, app.Forbidden("no tenant context")
	}
	return s.listProfiles(ctx, tenantCtx)
}

// CreateProfile creates a non-system profile. Super admins choose the target
// tenant (or global); tenant admins always create in their own tenant.
func (s *Service) CreateProfile(ctx context.Context, actor authz.Actor, tenantCtx *uuid.UUID, in CreateInput) (*models.PermissionProfile, error) {
	if in.Name == "" {
		return nil, app.BadRequest("name is required")
	}

	var profileTenantID *uuid.UUID
	if actor.IsSuperAdmin {
		profileTenantID = in.TenantID
	} else {
		if tenantCtx == nil {
			return nil, app.Forbidden("no tenant context")
		}
		profileTenantID = tenantCtx
	}

	// Global profiles (TenantID=nil) are reusable across tenants and therefore
	// cannot carry tenant-local zone IDs.
	if len(in.AllowedZoneIDs) > 0 && profileTenantID == nil {
		return nil, app.BadRequest("allowed_zone_ids require a tenant-scoped permission profile")
	}
	if err := s.validateZones(ctx, in.AllowedZoneIDs, profileTenantID, "target tenant"); err != nil {
		return nil, err
	}

	now := time.Now()
	profile := &models.PermissionProfile{
		ID:                uuid.New(),
		TenantID:          profileTenantID,
		Name:              in.Name,
		Description:       in.Description,
		CanSend:           in.CanSend,
		DailySendQuota:    in.DailySendQuota,
		DailyReceiveQuota: in.DailyReceiveQuota,
		MaxMailboxes:      in.MaxMailboxes,
		MaxDomains:        in.MaxDomains,
		AllowedZoneIDs:    in.AllowedZoneIDs,
		CanCreateDomains:  in.CanCreateDomains,
		CanCreateRoutes:   in.CanCreateRoutes,
		CanCreateAPIKeys:  in.CanCreateAPIKeys,
		IsSystem:          false,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if err := s.store.CreatePermissionProfile(ctx, profile); err != nil {
		return nil, app.Internal(err)
	}
	return profile, nil
}

// UpdateProfile applies the patch to an existing profile after enforcing the
// system-profile immutability guard and tenant ownership.
func (s *Service) UpdateProfile(ctx context.Context, actor authz.Actor, tenantCtx *uuid.UUID, id uuid.UUID, in UpdateInput) (*models.PermissionProfile, error) {
	existing, err := s.store.GetPermissionProfile(ctx, id)
	if err != nil {
		return nil, app.Internal(err)
	}
	if existing == nil {
		return nil, app.NotFound("permission profile not found")
	}
	if err := rejectSystemProfile(existing, "modify"); err != nil {
		return nil, err
	}
	if !actor.IsSuperAdmin {
		if err := requireProfileTenant(tenantCtx, existing); err != nil {
			return nil, err
		}
	}

	// A repository may return a cached/reference-backed value. Failed patch
	// validation must not mutate it before the persistence method is called.
	copyOfExisting := *existing
	copyOfExisting.AllowedZoneIDs = append([]uuid.UUID(nil), existing.AllowedZoneIDs...)
	existing = &copyOfExisting

	if in.Name != nil {
		existing.Name = *in.Name
	}
	if in.Description != nil {
		existing.Description = *in.Description
	}
	if in.CanSend != nil {
		existing.CanSend = *in.CanSend
	}
	if in.DailySendQuota != nil {
		existing.DailySendQuota = *in.DailySendQuota
	}
	if in.DailyReceiveQuota != nil {
		existing.DailyReceiveQuota = *in.DailyReceiveQuota
	}
	if in.MaxMailboxes != nil {
		existing.MaxMailboxes = *in.MaxMailboxes
	}
	if in.MaxDomains != nil {
		existing.MaxDomains = *in.MaxDomains
	}
	if in.AllowedZoneIDs != nil {
		existing.AllowedZoneIDs = in.AllowedZoneIDs
	}
	if in.CanCreateDomains != nil {
		existing.CanCreateDomains = *in.CanCreateDomains
	}
	if in.CanCreateRoutes != nil {
		existing.CanCreateRoutes = *in.CanCreateRoutes
	}
	if in.CanCreateAPIKeys != nil {
		existing.CanCreateAPIKeys = *in.CanCreateAPIKeys
	}

	// Global profiles (TenantID=nil) are reusable across tenants and therefore
	// cannot carry tenant-local zone IDs.
	if len(in.AllowedZoneIDs) > 0 && existing.TenantID == nil {
		return nil, app.BadRequest("allowed_zone_ids require a tenant-scoped permission profile")
	}
	if err := s.validateZones(ctx, in.AllowedZoneIDs, existing.TenantID, "target tenant"); err != nil {
		return nil, err
	}

	if err := s.store.UpdatePermissionProfile(ctx, existing); err != nil {
		return nil, app.Internal(err)
	}
	return existing, nil
}

// DeleteProfile deletes a non-system profile. Tenant admins are scoped to
// their own tenant at the store layer; super admins may delete any profile.
func (s *Service) DeleteProfile(ctx context.Context, actor authz.Actor, tenantCtx *uuid.UUID, id uuid.UUID) error {
	existing, err := s.store.GetPermissionProfile(ctx, id)
	if err != nil {
		return app.Internal(err)
	}
	if existing == nil {
		return app.NotFound("permission profile not found")
	}
	if err := rejectSystemProfile(existing, "delete"); err != nil {
		return err
	}
	if !actor.IsSuperAdmin {
		if err := requireProfileTenant(tenantCtx, existing); err != nil {
			return err
		}
	}

	var deleteTenantID *uuid.UUID
	if !actor.IsSuperAdmin && tenantCtx != nil {
		deleteTenantID = tenantCtx
	}
	if err := s.store.DeletePermissionProfile(ctx, id, deleteTenantID); err != nil {
		return app.Internal(err)
	}
	return nil
}

// GetUserPermission returns the effective permission of a user inside the
// caller's tenant.
func (s *Service) GetUserPermission(ctx context.Context, tenantCtx *uuid.UUID, userID uuid.UUID) (*models.EffectivePermission, error) {
	if err := s.requireTenantUser(ctx, tenantCtx, userID); err != nil {
		return nil, err
	}
	return s.effectivePermission(ctx, userID)
}

// SetUserPermissionOverride validates and upserts a user's permission
// override, binding it to the requested user.
func (s *Service) SetUserPermissionOverride(ctx context.Context, tenantCtx *uuid.UUID, userID uuid.UUID, override *models.UserPermissionOverride) (*models.UserPermissionOverride, error) {
	if err := s.requireTenantUser(ctx, tenantCtx, userID); err != nil {
		return nil, err
	}
	override.UserID = userID
	if err := s.validateZones(ctx, override.AllowedZoneIDs, tenantCtx, "tenant"); err != nil {
		return nil, err
	}
	if err := s.store.UpsertUserPermissionOverride(ctx, override); err != nil {
		return nil, app.Internal(err)
	}
	return override, nil
}

// DeleteUserPermissionOverride removes a user's permission override.
func (s *Service) DeleteUserPermissionOverride(ctx context.Context, tenantCtx *uuid.UUID, userID uuid.UUID) error {
	if err := s.requireTenantUser(ctx, tenantCtx, userID); err != nil {
		return err
	}
	if err := s.store.DeleteUserPermissionOverride(ctx, userID); err != nil {
		return app.Internal(err)
	}
	return nil
}

// MyPermissions returns the calling user's own effective permission.
func (s *Service) MyPermissions(ctx context.Context, userID uuid.UUID) (*models.EffectivePermission, error) {
	return s.effectivePermission(ctx, userID)
}

// validateZones is the single enforcement point for allowed-zone validity and
// tenant ownership: every zone must exist and belong to the target tenant.
func (s *Service) validateZones(ctx context.Context, zoneIDs []uuid.UUID, tenantID *uuid.UUID, scope string) error {
	if len(zoneIDs) == 0 || tenantID == nil {
		return nil
	}
	for _, zoneID := range zoneIDs {
		zone, err := s.store.GetZone(ctx, zoneID)
		if err != nil {
			return app.Internal(err)
		}
		if zone == nil || zone.TenantID != *tenantID {
			return app.BadRequest(fmt.Sprintf("zone %s not found or does not belong to %s", zoneID, scope))
		}
	}
	return nil
}

// rejectSystemProfile is the single enforcement point for system-profile
// immutability: system profiles can be neither modified nor deleted.
func rejectSystemProfile(p *models.PermissionProfile, action string) error {
	if p.IsSystem {
		return app.Forbidden(fmt.Sprintf("cannot %s system profile", action))
	}
	return nil
}

// requireProfileTenant is the single tenant-ownership guard for profiles on
// the mutation paths: a missing tenant context is forbidden, and a profile
// outside the tenant is reported as not found to avoid existence leaks.
func requireProfileTenant(tenantCtx *uuid.UUID, p *models.PermissionProfile) error {
	if tenantCtx == nil {
		return app.Forbidden("no tenant context")
	}
	if p.TenantID == nil || *p.TenantID != *tenantCtx {
		return app.NotFound("permission profile not found")
	}
	return nil
}

// requireTenantUser is the single tenant-ownership guard for user lookups on
// the override paths.
func (s *Service) requireTenantUser(ctx context.Context, tenantCtx *uuid.UUID, userID uuid.UUID) error {
	if tenantCtx == nil {
		return app.Forbidden("no tenant context")
	}
	user, err := s.store.GetUser(ctx, userID)
	if err != nil {
		return app.Internal(err)
	}
	if user == nil || user.TenantID != *tenantCtx {
		return app.NotFound("user not found")
	}
	return nil
}

func (s *Service) listProfiles(ctx context.Context, tenantID *uuid.UUID) ([]*models.PermissionProfile, error) {
	items, err := s.store.ListPermissionProfiles(ctx, tenantID)
	if err != nil {
		return nil, app.Internal(err)
	}
	return items, nil
}

func (s *Service) effectivePermission(ctx context.Context, userID uuid.UUID) (*models.EffectivePermission, error) {
	perm, err := s.store.EffectivePermission(ctx, userID)
	if err != nil {
		return nil, app.Internal(err)
	}
	return perm, nil
}
