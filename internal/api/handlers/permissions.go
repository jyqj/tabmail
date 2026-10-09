package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"tabmail/internal/api/middleware"
	"tabmail/internal/app/permissions"
	"tabmail/internal/authz"
	"tabmail/internal/company"
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
// A current platform administrator may create ordinary global or tenant profiles;
// system profiles are never created through this management endpoint.
// Tenant admin always creates tenant-scoped profiles.
func (h *PermissionHandler) CreateProfile(w http.ResponseWriter, r *http.Request) {
	var body permissions.CreateInput
	if err := decodePermissionProfileBody(w, r, &body, false); err != nil {
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
	if err := decodePermissionProfileBody(w, r, &body, true); err != nil {
		errBadRequest(w, "invalid body")
		return
	}

	profile, err := h.service.UpdateProfileVersioned(r.Context(), middleware.ActorFromContext(r.Context()), tenantCtx(r), id, body.ExpectedRevision, body)
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

	var body struct {
		ExpectedRevision string                       `json:"expected_revision"`
		ConfirmedMembers []company.PermissionRevision `json:"confirmed_members"`
	}
	if err := decodePermissionVersionedBody(w, r, &body); err != nil {
		errBadRequest(w, "invalid profile deletion confirmation")
		return
	}
	if body.ConfirmedMembers == nil {
		errBadRequest(w, "explicit affected member confirmation required")
		return
	}
	if err := h.service.DeleteProfileVersioned(r.Context(), middleware.ActorFromContext(r.Context()), tenantCtx(r), id, body.ExpectedRevision, body.ConfirmedMembers); err != nil {
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
	errConflict(w, "permission write protocol upgraded; load permission-editor and submit expected_revision")
}

// DeleteUserPermissionOverride deletes a user's permission override.
func (h *PermissionHandler) DeleteUserPermissionOverride(w http.ResponseWriter, r *http.Request) {
	errConflict(w, "permission clear protocol upgraded; use versioned permission-editor field inheritance")
}

func (h *PermissionHandler) PermissionEditor(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid user id")
		return
	}
	out, err := h.service.EditorSnapshot(r.Context(), middleware.ActorFromContext(r.Context()), tenantCtx(r), id)
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	ok(w, out)
}
func (h *PermissionHandler) PatchPermissionEditor(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid user id")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	cmd, err := company.DecodePermissionEditorCommand(r.Body)
	if err != nil {
		errBadRequest(w, "invalid permission editor command")
		return
	}
	out, err := h.service.PatchEditor(r.Context(), middleware.ActorFromContext(r.Context()), tenantCtx(r), id, cmd)
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	ok(w, out)
}

// MyPermissions returns the calling user's own effective permission.
func (h *PermissionHandler) MyPermissions(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromCtx(r.Context())
	if user == nil {
		errForbidden(w, "user context required")
		return
	}

	actor := middleware.ActorFromContext(r.Context())
	if actor.Type == authz.PrincipalUser && actor.IsTenantAdmin() {
		// Display the same principal policy that PermissionLoader installed. The
		// editor remains a separate raw/canonical profile observation; this is only
		// a view and never substitutes for transaction-time write authorization.
		perm := middleware.PermissionFromCtx(r.Context())
		if perm == nil {
			errInternal(w)
			return
		}
		ok(w, perm)
		return
	}
	perm, err := h.service.MyPermissions(r.Context(), user.ID)
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	ok(w, perm)
}

func decodePermissionVersionedBody(w http.ResponseWriter, r *http.Request, out any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	return company.DecodePermissionJSONObject(r.Body, out, []string{"expected_revision", "confirmed_members"}, nil)
}

func (h *PermissionHandler) ProfileDeletionPreview(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid profile id")
		return
	}
	out, err := h.service.ProfileDeletionPreview(r.Context(), middleware.ActorFromContext(r.Context()), tenantCtx(r), id)
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	ok(w, out)
}
func (h *PermissionHandler) AssignPermissionEditor(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid user id")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	cmd, err := company.DecodePermissionAssignmentCommand(r.Body)
	if err != nil {
		errBadRequest(w, "invalid permission assignment command")
		return
	}
	out, err := h.service.AssignEditor(r.Context(), middleware.ActorFromContext(r.Context()), tenantCtx(r), id, cmd)
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	ok(w, out)
}

func decodePermissionProfileBody(w http.ResponseWriter, r *http.Request, out any, update bool) error {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	allowed := []string{"name", "description", "can_send", "daily_send_quota", "daily_receive_quota", "max_mailboxes", "max_domains", "allowed_zone_ids", "can_create_domains", "can_create_routes", "can_create_api_keys"}
	if update {
		allowed = append(allowed, "expected_revision")
	} else {
		allowed = append(allowed, "tenant_id")
	}
	return company.DecodePermissionJSONObject(r.Body, out, allowed, nil)
}
