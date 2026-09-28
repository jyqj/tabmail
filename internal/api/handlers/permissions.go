package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"tabmail/internal/api/middleware"
	"tabmail/internal/app/permissions"
	"tabmail/internal/models"
	"tabmail/internal/store"
)

type PermissionHandler struct {
	service *permissions.Service
	logger  zerolog.Logger
}

func NewPermissionHandler(st store.Store, l zerolog.Logger) *PermissionHandler {
	return &PermissionHandler{
		service: permissions.New(st),
		logger:  l.With().Str("handler", "permissions").Logger(),
	}
}

// tenantCtx extracts the caller's tenant id, or nil when the request carries
// no tenant context. The service decides whether a nil context is allowed.
func tenantCtx(r *http.Request) *uuid.UUID {
	if tenant := middleware.TenantFromCtx(r.Context()); tenant != nil {
		id := tenant.ID
		return &id
	}
	return nil
}

// ListProfiles returns permission profiles visible to the caller.
// Platform admin sees all profiles; tenant admin sees system + own tenant profiles.
func (h *PermissionHandler) ListProfiles(w http.ResponseWriter, r *http.Request) {
	items, err := h.service.ListProfiles(r.Context(), middleware.ActorFromContext(r.Context()), tenantCtx(r))
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	ok(w, items)
}

// CreateProfile creates a new permission profile.
// Platform admin can create system profiles (tenant_id=nil) or tenant-scoped.
// Tenant admin always creates tenant-scoped profiles.
func (h *PermissionHandler) CreateProfile(w http.ResponseWriter, r *http.Request) {
	var body permissions.CreateInput
	if err := decodeBody(r, &body); err != nil {
		errBadRequest(w, "invalid body")
		return
	}

	profile, err := h.service.CreateProfile(r.Context(), middleware.ActorFromContext(r.Context()), tenantCtx(r), body)
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	created(w, profile)
}

// UpdateProfile updates an existing permission profile.
func (h *PermissionHandler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid id")
		return
	}
	var body permissions.UpdateInput
	if err := decodeBody(r, &body); err != nil {
		errBadRequest(w, "invalid body")
		return
	}

	profile, err := h.service.UpdateProfile(r.Context(), middleware.ActorFromContext(r.Context()), tenantCtx(r), id, body)
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	ok(w, profile)
}

// DeleteProfile deletes a permission profile.
func (h *PermissionHandler) DeleteProfile(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid id")
		return
	}

	if err := h.service.DeleteProfile(r.Context(), middleware.ActorFromContext(r.Context()), tenantCtx(r), id); err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	noContent(w)
}

// GetUserPermission returns the effective permission for a user.
func (h *PermissionHandler) GetUserPermission(w http.ResponseWriter, r *http.Request) {
	userID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid user id")
		return
	}

	perm, err := h.service.GetUserPermission(r.Context(), tenantCtx(r), userID)
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	ok(w, perm)
}

// SetUserPermissionOverride sets or updates a user's permission override.
func (h *PermissionHandler) SetUserPermissionOverride(w http.ResponseWriter, r *http.Request) {
	userID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid user id")
		return
	}
	var body models.UserPermissionOverride
	if err := decodeBody(r, &body); err != nil {
		errBadRequest(w, "invalid body")
		return
	}

	override, err := h.service.SetUserPermissionOverride(r.Context(), tenantCtx(r), userID, &body)
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	ok(w, override)
}

// DeleteUserPermissionOverride deletes a user's permission override.
func (h *PermissionHandler) DeleteUserPermissionOverride(w http.ResponseWriter, r *http.Request) {
	userID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid user id")
		return
	}

	if err := h.service.DeleteUserPermissionOverride(r.Context(), tenantCtx(r), userID); err != nil {
		respondAppError(w, h.logger, err)
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

	perm, err := h.service.MyPermissions(r.Context(), user.ID)
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	ok(w, perm)
}
