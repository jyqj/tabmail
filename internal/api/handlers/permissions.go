package handlers

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"tabmail/internal/api/middleware"
	"tabmail/internal/authz"
	"tabmail/internal/models"
	"tabmail/internal/store"
)

type PermissionHandler struct {
	store  store.Store
	logger zerolog.Logger
}

func NewPermissionHandler(st store.Store, l zerolog.Logger) *PermissionHandler {
	return &PermissionHandler{store: st, logger: l.With().Str("handler", "permissions").Logger()}
}

// ListProfiles returns permission profiles visible to the caller.
// Platform admin sees all profiles; tenant admin sees system + own tenant profiles.
func (h *PermissionHandler) ListProfiles(w http.ResponseWriter, r *http.Request) {
	actor := authz.ActorFromContext(r.Context())

	var tenantID *uuid.UUID
	if !actor.IsPlatformAdmin {
		tenant := middleware.TenantFromCtx(r.Context())
		if tenant == nil {
			errForbidden(w, "no tenant context")
			return
		}
		tenantID = &tenant.ID
	}

	items, err := h.store.ListPermissionProfiles(r.Context(), tenantID)
	if err != nil {
		h.logger.Err(err).Msg("failed to list permission profiles")
		errInternal(w)
		return
	}
	ok(w, items)
}

// CreateProfile creates a new permission profile.
// Platform admin can create system profiles (tenant_id=nil) or tenant-scoped.
// Tenant admin always creates tenant-scoped profiles.
func (h *PermissionHandler) CreateProfile(w http.ResponseWriter, r *http.Request) {
	actor := authz.ActorFromContext(r.Context())
	tenant := middleware.TenantFromCtx(r.Context())

	var body struct {
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
	if err := decodeBody(r, &body); err != nil {
		errBadRequest(w, "invalid body")
		return
	}
	if body.Name == "" {
		errBadRequest(w, "name is required")
		return
	}

	var profileTenantID *uuid.UUID
	if actor.IsPlatformAdmin {
		profileTenantID = body.TenantID
	} else {
		if tenant == nil {
			errForbidden(w, "no tenant context")
			return
		}
		profileTenantID = &tenant.ID
	}

	profile := &models.PermissionProfile{
		ID:                uuid.New(),
		TenantID:          profileTenantID,
		Name:              body.Name,
		Description:       body.Description,
		CanSend:           body.CanSend,
		DailySendQuota:    body.DailySendQuota,
		DailyReceiveQuota: body.DailyReceiveQuota,
		MaxMailboxes:      body.MaxMailboxes,
		MaxDomains:        body.MaxDomains,
		AllowedZoneIDs:    body.AllowedZoneIDs,
		CanCreateDomains:  body.CanCreateDomains,
		CanCreateRoutes:   body.CanCreateRoutes,
		CanCreateAPIKeys:  body.CanCreateAPIKeys,
		IsSystem:          false,
		CreatedAt:         time.Now(),
		UpdatedAt:         time.Now(),
	}

	if err := h.store.CreatePermissionProfile(r.Context(), profile); err != nil {
		h.logger.Err(err).Msg("failed to create permission profile")
		errInternal(w)
		return
	}
	created(w, profile)
}

// UpdateProfile updates an existing permission profile.
func (h *PermissionHandler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	actor := authz.ActorFromContext(r.Context())
	tenant := middleware.TenantFromCtx(r.Context())

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid id")
		return
	}

	existing, err := h.store.GetPermissionProfile(r.Context(), id)
	if err != nil {
		h.logger.Err(err).Msg("failed to get permission profile")
		errInternal(w)
		return
	}
	if existing == nil {
		errNotFound(w, "permission profile not found")
		return
	}
	if existing.IsSystem {
		errForbidden(w, "cannot modify system profile")
		return
	}

	if !actor.IsPlatformAdmin {
		if tenant == nil {
			errForbidden(w, "no tenant context")
			return
		}
		if existing.TenantID == nil || *existing.TenantID != tenant.ID {
			errNotFound(w, "permission profile not found")
			return
		}
	}

	var body struct {
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
	if err := decodeBody(r, &body); err != nil {
		errBadRequest(w, "invalid body")
		return
	}

	if body.Name != nil {
		existing.Name = *body.Name
	}
	if body.Description != nil {
		existing.Description = *body.Description
	}
	if body.CanSend != nil {
		existing.CanSend = *body.CanSend
	}
	if body.DailySendQuota != nil {
		existing.DailySendQuota = *body.DailySendQuota
	}
	if body.DailyReceiveQuota != nil {
		existing.DailyReceiveQuota = *body.DailyReceiveQuota
	}
	if body.MaxMailboxes != nil {
		existing.MaxMailboxes = *body.MaxMailboxes
	}
	if body.MaxDomains != nil {
		existing.MaxDomains = *body.MaxDomains
	}
	if body.AllowedZoneIDs != nil {
		existing.AllowedZoneIDs = body.AllowedZoneIDs
	}
	if body.CanCreateDomains != nil {
		existing.CanCreateDomains = *body.CanCreateDomains
	}
	if body.CanCreateRoutes != nil {
		existing.CanCreateRoutes = *body.CanCreateRoutes
	}
	if body.CanCreateAPIKeys != nil {
		existing.CanCreateAPIKeys = *body.CanCreateAPIKeys
	}

	if err := h.store.UpdatePermissionProfile(r.Context(), existing); err != nil {
		h.logger.Err(err).Msg("failed to update permission profile")
		errInternal(w)
		return
	}
	ok(w, existing)
}

// DeleteProfile deletes a permission profile.
func (h *PermissionHandler) DeleteProfile(w http.ResponseWriter, r *http.Request) {
	actor := authz.ActorFromContext(r.Context())
	tenant := middleware.TenantFromCtx(r.Context())

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid id")
		return
	}

	existing, err := h.store.GetPermissionProfile(r.Context(), id)
	if err != nil {
		h.logger.Err(err).Msg("failed to get permission profile")
		errInternal(w)
		return
	}
	if existing == nil {
		errNotFound(w, "permission profile not found")
		return
	}
	if existing.IsSystem {
		errForbidden(w, "cannot delete system profile")
		return
	}

	if !actor.IsPlatformAdmin {
		if tenant == nil {
			errForbidden(w, "no tenant context")
			return
		}
		if existing.TenantID == nil || *existing.TenantID != tenant.ID {
			errNotFound(w, "permission profile not found")
			return
		}
	}

	var deleteTenantID *uuid.UUID
	if !actor.IsPlatformAdmin && tenant != nil {
		deleteTenantID = &tenant.ID
	}

	if err := h.store.DeletePermissionProfile(r.Context(), id, deleteTenantID); err != nil {
		h.logger.Err(err).Msg("failed to delete permission profile")
		errInternal(w)
		return
	}
	noContent(w)
}

// GetUserPermission returns the effective permission for a user.
func (h *PermissionHandler) GetUserPermission(w http.ResponseWriter, r *http.Request) {
	tenant := middleware.TenantFromCtx(r.Context())
	if tenant == nil {
		errForbidden(w, "no tenant context")
		return
	}
	userID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid user id")
		return
	}
	user, err := h.store.GetUser(r.Context(), userID)
	if err != nil {
		h.logger.Err(err).Str("user_id", userID.String()).Msg("failed to get user")
		errInternal(w)
		return
	}
	if user == nil || user.TenantID != tenant.ID {
		errNotFound(w, "user not found")
		return
	}

	perm, err := h.store.EffectivePermission(r.Context(), userID)
	if err != nil {
		h.logger.Err(err).Str("user_id", userID.String()).Msg("failed to get effective permission")
		errInternal(w)
		return
	}
	ok(w, perm)
}

// SetUserPermissionOverride sets or updates a user's permission override.
func (h *PermissionHandler) SetUserPermissionOverride(w http.ResponseWriter, r *http.Request) {
	tenant := middleware.TenantFromCtx(r.Context())
	if tenant == nil {
		errForbidden(w, "no tenant context")
		return
	}
	userID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid user id")
		return
	}
	user, err := h.store.GetUser(r.Context(), userID)
	if err != nil {
		h.logger.Err(err).Str("user_id", userID.String()).Msg("failed to get user")
		errInternal(w)
		return
	}
	if user == nil || user.TenantID != tenant.ID {
		errNotFound(w, "user not found")
		return
	}

	var body models.UserPermissionOverride
	if err := decodeBody(r, &body); err != nil {
		errBadRequest(w, "invalid body")
		return
	}
	body.UserID = userID

	if err := h.store.UpsertUserPermissionOverride(r.Context(), &body); err != nil {
		h.logger.Err(err).Str("user_id", userID.String()).Msg("failed to upsert permission override")
		errInternal(w)
		return
	}
	ok(w, body)
}

// DeleteUserPermissionOverride deletes a user's permission override.
func (h *PermissionHandler) DeleteUserPermissionOverride(w http.ResponseWriter, r *http.Request) {
	tenant := middleware.TenantFromCtx(r.Context())
	if tenant == nil {
		errForbidden(w, "no tenant context")
		return
	}
	userID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid user id")
		return
	}
	user, err := h.store.GetUser(r.Context(), userID)
	if err != nil {
		h.logger.Err(err).Str("user_id", userID.String()).Msg("failed to get user")
		errInternal(w)
		return
	}
	if user == nil || user.TenantID != tenant.ID {
		errNotFound(w, "user not found")
		return
	}

	if err := h.store.DeleteUserPermissionOverride(r.Context(), userID); err != nil {
		h.logger.Err(err).Str("user_id", userID.String()).Msg("failed to delete permission override")
		errInternal(w)
		return
	}
	noContent(w)
}

// MyPermissions returns the calling user's own effective permission.
func (h *PermissionHandler) MyPermissions(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromCtx(r.Context())
	if user == nil {
		errForbidden(w, "user context required")
		return
	}

	perm, err := h.store.EffectivePermission(r.Context(), user.ID)
	if err != nil {
		h.logger.Err(err).Str("user_id", user.ID.String()).Msg("failed to get own effective permission")
		errInternal(w)
		return
	}
	ok(w, perm)
}
