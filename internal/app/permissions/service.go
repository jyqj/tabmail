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
	"tabmail/internal/hooks"
	"tabmail/internal/models"
	"tabmail/internal/store"
)

type storeRepo interface {
	store.Transactor
	app.AuditStore
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
	store      storeRepo
	dispatcher *hooks.Dispatcher
	logger     zerolog.Logger
}

func NewService(s storeRepo, dispatcher *hooks.Dispatcher, logger zerolog.Logger) *Service {
	return &Service{store: s, dispatcher: dispatcher, logger: logger.With().Str("service", "permissions").Logger()}
}

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
		ID: uuid.New(), TenantID: profileTenantID, Name: req.Name,
		Description: req.Description, CanSend: req.CanSend,
		DailySendQuota: req.DailySendQuota, DailyReceiveQuota: req.DailyReceiveQuota,
		MaxMailboxes: req.MaxMailboxes, MaxDomains: req.MaxDomains,
		AllowedZoneIDs: req.AllowedZoneIDs, CanCreateDomains: req.CanCreateDomains,
		CanCreateRoutes: req.CanCreateRoutes, CanCreateAPIKeys: req.CanCreateAPIKeys,
		IsSystem: false, CreatedAt: now, UpdatedAt: now,
	}
	audit := models.AuditEntry{
		TenantID: profileTenantID,
		Actor:    actor.AuditLabel(), Action: "permission_profile.create",
		ResourceType: "permission_profile", ResourceID: app.UUIDPtr(profile.ID),
		Details: app.MustJSON(map[string]any{"name": profile.Name, "tenant_id": profile.TenantID}),
	}
	event := hooks.Event{Type: "permission_profile.created", TenantID: tenantIDString(profileTenantID), OccurredAt: time.Now().UTC(), Metadata: map[string]any{"profile_id": profile.ID.String(), "name": profile.Name}}
	if err := app.CommitMutation(ctx, s.store, s.store, s.dispatcher, audit, &event, func(txCtx context.Context) error {
		return s.store.CreatePermissionProfile(txCtx, profile)
	}); err != nil {
		return nil, err
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
	audit := models.AuditEntry{
		TenantID: existing.TenantID,
		Actor:    actor.AuditLabel(), Action: "permission_profile.update",
		ResourceType: "permission_profile", ResourceID: app.UUIDPtr(existing.ID),
		Details: app.MustJSON(map[string]any{"name": existing.Name}),
	}
	event := hooks.Event{Type: "permission_profile.updated", TenantID: tenantIDString(existing.TenantID), OccurredAt: time.Now().UTC(), Metadata: map[string]any{"profile_id": existing.ID.String(), "name": existing.Name}}
	if err := app.CommitMutation(ctx, s.store, s.store, s.dispatcher, audit, &event, func(txCtx context.Context) error {
		return s.store.UpdatePermissionProfile(txCtx, existing)
	}); err != nil {
		return nil, err
	}
	return existing, nil
}

func (s *Service) DeleteProfile(ctx context.Context, actor authz.Actor, tenant *models.Tenant, id uuid.UUID) error {
	existing, err := s.writableProfile(ctx, actor, tenant, id, "cannot delete system profile")
	if err != nil {
		return err
	}
	var deleteTenantID *uuid.UUID
	if !actor.IsSuperAdmin && tenant != nil {
		deleteTenantID = &tenant.ID
	}
	audit := models.AuditEntry{
		TenantID: existing.TenantID,
		Actor:    actor.AuditLabel(), Action: "permission_profile.delete",
		ResourceType: "permission_profile", ResourceID: app.UUIDPtr(existing.ID),
		Details: app.MustJSON(map[string]any{"name": existing.Name}),
	}
	event := hooks.Event{Type: "permission_profile.deleted", TenantID: tenantIDString(existing.TenantID), OccurredAt: time.Now().UTC(), Metadata: map[string]any{"profile_id": existing.ID.String(), "name": existing.Name}}
	return app.CommitMutation(ctx, s.store, s.store, s.dispatcher, audit, &event, func(txCtx context.Context) error {
		return s.store.DeletePermissionProfile(txCtx, id, deleteTenantID)
	})
}

// UserPermission resolves a user's effective permission within a tenant. It is
// retained for internal callers that already performed target authorization.
func (s *Service) UserPermission(ctx context.Context, tenant *models.Tenant, userID uuid.UUID) (*models.EffectivePermission, error) {
	if _, err := s.tenantUser(ctx, tenant, userID); err != nil {
		return nil, err
	}
	return s.effectivePermission(ctx, userID)
}

// UserPermissionForActor is the HTTP/admin boundary. It prevents tenant
// administrators from using the permission endpoint as a side channel for
// peer administrators.
func (s *Service) UserPermissionForActor(ctx context.Context, actor authz.Actor, tenant *models.Tenant, userID uuid.UUID) (*models.EffectivePermission, error) {
	if _, err := s.manageableTenantUser(ctx, actor, tenant, userID); err != nil {
		return nil, err
	}
	return s.effectivePermission(ctx, userID)
}

func (s *Service) OwnPermission(ctx context.Context, caller *models.User) (*models.EffectivePermission, error) {
	if caller == nil {
		return nil, app.Forbidden("user context required")
	}
	return s.effectivePermission(ctx, caller.ID)
}

func (s *Service) SetUserOverride(ctx context.Context, tenant *models.Tenant, userID uuid.UUID, override models.UserPermissionOverride) (*models.UserPermissionOverride, error) {
	if _, err := s.tenantUser(ctx, tenant, userID); err != nil {
		return nil, err
	}
	return s.storeUserOverride(ctx, tenant, userID, override, "system")
}

func (s *Service) SetUserOverrideForActor(ctx context.Context, actor authz.Actor, tenant *models.Tenant, userID uuid.UUID, override models.UserPermissionOverride) (*models.UserPermissionOverride, error) {
	if _, err := s.manageableTenantUser(ctx, actor, tenant, userID); err != nil {
		return nil, err
	}
	return s.storeUserOverride(ctx, tenant, userID, override, actor.AuditLabel())
}

func (s *Service) storeUserOverride(ctx context.Context, tenant *models.Tenant, userID uuid.UUID, override models.UserPermissionOverride, actorLabel string) (*models.UserPermissionOverride, error) {
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
	audit := models.AuditEntry{
		TenantID: app.UUIDPtr(tenant.ID), Actor: actorLabel,
		Action: "user_permission_override.upsert", ResourceType: "user", ResourceID: app.UUIDPtr(userID),
		Details: app.MustJSON(map[string]any{"allowed_zone_ids": override.AllowedZoneIDs}),
	}
	event := hooks.Event{Type: "user_permission_override.updated", TenantID: tenant.ID.String(), OccurredAt: time.Now().UTC(), Metadata: map[string]any{"user_id": userID.String()}}
	if err := app.CommitMutation(ctx, s.store, s.store, s.dispatcher, audit, &event, func(txCtx context.Context) error {
		return s.store.UpsertUserPermissionOverride(txCtx, &override)
	}); err != nil {
		return nil, err
	}
	return &override, nil
}

func (s *Service) DeleteUserOverride(ctx context.Context, tenant *models.Tenant, userID uuid.UUID) error {
	if _, err := s.tenantUser(ctx, tenant, userID); err != nil {
		return err
	}
	return s.deleteUserOverride(ctx, tenant, userID, "system")
}

func (s *Service) DeleteUserOverrideForActor(ctx context.Context, actor authz.Actor, tenant *models.Tenant, userID uuid.UUID) error {
	if _, err := s.manageableTenantUser(ctx, actor, tenant, userID); err != nil {
		return err
	}
	return s.deleteUserOverride(ctx, tenant, userID, actor.AuditLabel())
}

func (s *Service) deleteUserOverride(ctx context.Context, tenant *models.Tenant, userID uuid.UUID, actorLabel string) error {
	audit := models.AuditEntry{
		TenantID: app.UUIDPtr(tenant.ID), Actor: actorLabel,
		Action: "user_permission_override.delete", ResourceType: "user", ResourceID: app.UUIDPtr(userID),
	}
	event := hooks.Event{Type: "user_permission_override.deleted", TenantID: tenant.ID.String(), OccurredAt: time.Now().UTC(), Metadata: map[string]any{"user_id": userID.String()}}
	return app.CommitMutation(ctx, s.store, s.store, s.dispatcher, audit, &event, func(txCtx context.Context) error {
		return s.store.DeleteUserPermissionOverride(txCtx, userID)
	})
}

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

func (s *Service) manageableTenantUser(ctx context.Context, actor authz.Actor, tenant *models.Tenant, userID uuid.UUID) (*models.User, error) {
	user, err := s.tenantUser(ctx, tenant, userID)
	if err != nil {
		return nil, err
	}
	if !authz.CanManageTenantMember(actor, user.TenantID, user.Role) {
		return nil, app.Forbidden("tenant admins may manage ordinary members only")
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

func tenantIDString(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}
