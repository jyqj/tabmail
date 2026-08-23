package handlers

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"tabmail/internal/api/middleware"
	permissionsapp "tabmail/internal/app/permissions"
	"tabmail/internal/models"
)

type permissionStore interface {
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

type PermissionHandler struct {
	service *permissionsapp.Service
	logger  zerolog.Logger
}

func NewPermissionHandler(s permissionStore, l zerolog.Logger) *PermissionHandler {
	return &PermissionHandler{
		service: permissionsapp.NewService(s, l),
		logger:  l.With().Str("handler", "permissions").Logger(),
	}
}

// ListProfiles handles GET /api/v1/admin/permissions
func (h *PermissionHandler) ListProfiles(w http.ResponseWriter, r *http.Request) {
	items, err := h.service.ListProfiles(
		r.Context(),
		middleware.ActorFromContext(r.Context()),
		middleware.TenantFromCtx(r.Context()),
	)
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	ok(w, items)
}

// CreateProfile handles POST /api/v1/admin/permissions
func (h *PermissionHandler) CreateProfile(w http.ResponseWriter, r *http.Request) {
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

	profile, err := h.service.CreateProfile(
		r.Context(),
		middleware.ActorFromContext(r.Context()),
		middleware.TenantFromCtx(r.Context()),
		permissionsapp.CreateProfileRequest{
			Name:              body.Name,
			Description:       body.Description,
			TenantID:          body.TenantID,
			CanSend:           body.CanSend,
			DailySendQuota:    body.DailySendQuota,
			DailyReceiveQuota: body.DailyReceiveQuota,
			MaxMailboxes:      body.MaxMailboxes,
			MaxDomains:        body.MaxDomains,
			AllowedZoneIDs:    body.AllowedZoneIDs,
			CanCreateDomains:  body.CanCreateDomains,
			CanCreateRoutes:   body.CanCreateRoutes,
			CanCreateAPIKeys:  body.CanCreateAPIKeys,
		},
	)
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	created(w, profile)
}

// UpdateProfile handles PATCH /api/v1/admin/permissions/{id}
func (h *PermissionHandler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid id")
		return
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

	profile, err := h.service.UpdateProfile(
		r.Context(),
		middleware.ActorFromContext(r.Context()),
		middleware.TenantFromCtx(r.Context()),
		id,
		permissionsapp.UpdateProfileRequest{
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
		},
	)
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	ok(w, profile)
}

// DeleteProfile handles DELETE /api/v1/admin/permissions/{id}
func (h *PermissionHandler) DeleteProfile(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid id")
		return
	}
	err = h.service.DeleteProfile(
		r.Context(),
		middleware.ActorFromContext(r.Context()),
		middleware.TenantFromCtx(r.Context()),
		id,
	)
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	noContent(w)
}

// GetUserPermission handles GET /api/v1/admin/users/{id}/permissions
func (h *PermissionHandler) GetUserPermission(w http.ResponseWriter, r *http.Request) {
	userID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid user id")
		return
	}
	perm, err := h.service.UserPermission(r.Context(), middleware.TenantFromCtx(r.Context()), userID)
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	ok(w, perm)
}

// SetUserPermissionOverride handles PUT /api/v1/admin/users/{id}/permissions
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
	override, err := h.service.SetUserOverride(r.Context(), middleware.TenantFromCtx(r.Context()), userID, body)
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	ok(w, override)
}

// DeleteUserPermissionOverride handles DELETE /api/v1/admin/users/{id}/permissions
func (h *PermissionHandler) DeleteUserPermissionOverride(w http.ResponseWriter, r *http.Request) {
	userID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid user id")
		return
	}
	err = h.service.DeleteUserOverride(r.Context(), middleware.TenantFromCtx(r.Context()), userID)
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	noContent(w)
}

// MyPermissions handles GET /api/v1/auth/me/permissions
func (h *PermissionHandler) MyPermissions(w http.ResponseWriter, r *http.Request) {
	perm, err := h.service.OwnPermission(r.Context(), middleware.UserFromCtx(r.Context()))
	if err != nil {
		respondAppError(w, h.logger, err)
		return
	}
	ok(w, perm)
}
