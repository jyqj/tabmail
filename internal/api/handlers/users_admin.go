package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"tabmail/internal/api/middleware"
	"tabmail/internal/models"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// userAdminStore is the subset of the store the user-admin handler needs.
type userAdminStore interface {
	GetUserByEmail(ctx context.Context, email string) (*models.User, error)
	GetUser(ctx context.Context, id uuid.UUID) (*models.User, error)
	UpdateUser(ctx context.Context, u *models.User) error
	ListUsers(ctx context.Context, tenantID uuid.UUID, pg models.Page) ([]*models.User, int, error)
	DeleteUser(ctx context.Context, id uuid.UUID) error
	CreateAdminInvitation(ctx context.Context, inv *models.AdminInvitation) error
	GetPermissionProfile(ctx context.Context, id uuid.UUID) (*models.PermissionProfile, error)
}

// UserAdminHandler manages tenant users and super-admin invitations
// (/api/v1/admin/users, /api/v1/admin/invite). Session lifecycle endpoints
// live in AuthHandler.
type UserAdminHandler struct {
	store  userAdminStore
	logger zerolog.Logger
}

func NewUserAdminHandler(s userAdminStore, l zerolog.Logger) *UserAdminHandler {
	return &UserAdminHandler{
		store:  s,
		logger: l.With().Str("handler", "user_admin").Logger(),
	}
}

// InviteAdmin handles POST /api/v1/admin/invite.
// This endpoint is super-admin only because accepting an invitation creates
// a super_admin user.
func (h *UserAdminHandler) InviteAdmin(w http.ResponseWriter, r *http.Request) {

	var req struct {
		Email string `json:"email"`
	}
	if err := decodeBody(r, &req); err != nil {
		errBadRequest(w, "invalid request body")
		return
	}
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	if req.Email == "" {
		errBadRequest(w, "email is required")
		return
	}

	existing, err := h.store.GetUserByEmail(r.Context(), req.Email)
	if err != nil {
		h.logger.Err(err).Msg("invite: check existing")
		errInternal(w)
		return
	}
	if existing != nil {
		errConflict(w, "email already registered")
		return
	}

	code, err := generateInviteCode()
	if err != nil {
		h.logger.Err(err).Msg("invite: generate code")
		errInternal(w)
		return
	}

	inviter := middleware.UserFromCtx(r.Context())
	var inviterID *uuid.UUID
	if inviter != nil {
		id := inviter.ID
		inviterID = &id
	}

	inv := &models.AdminInvitation{
		Email:      req.Email,
		InviteCode: code,
		InvitedBy:  inviterID,
		ExpiresAt:  time.Now().Add(72 * time.Hour),
	}
	if err := h.store.CreateAdminInvitation(r.Context(), inv); err != nil {
		h.logger.Err(err).Msg("invite: create invitation")
		errInternal(w)
		return
	}

	created(w, map[string]any{
		"id":          inv.ID,
		"email":       inv.Email,
		"invite_code": code,
		"expires_at":  inv.ExpiresAt,
	})
}

// ListUsers handles GET /api/v1/admin/users
func (h *UserAdminHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	tenant := middleware.TenantFromCtx(r.Context())
	if tenant == nil {
		errForbidden(w, "no tenant context")
		return
	}
	pg := pageFromReq(r)
	users, total, err := h.store.ListUsers(r.Context(), tenant.ID, pg)
	if err != nil {
		h.logger.Err(err).Msg("list users")
		errInternal(w)
		return
	}
	// Strip password hashes from response
	type safeUser struct {
		ID                  uuid.UUID       `json:"id"`
		TenantID            uuid.UUID       `json:"tenant_id"`
		Email               string          `json:"email"`
		DisplayName         string          `json:"display_name"`
		Role                models.UserRole `json:"role"`
		PermissionProfileID *uuid.UUID      `json:"permission_profile_id,omitempty"`
		IsActive            bool            `json:"is_active"`
		CreatedAt           time.Time       `json:"created_at"`
		UpdatedAt           time.Time       `json:"updated_at"`
		LastLoginAt         *time.Time      `json:"last_login_at,omitempty"`
	}
	safe := make([]safeUser, 0, len(users))
	for _, u := range users {
		safe = append(safe, safeUser{
			ID: u.ID, TenantID: u.TenantID, Email: u.Email,
			DisplayName: u.DisplayName, Role: u.Role, PermissionProfileID: u.PermissionProfileID,
			IsActive: u.IsActive, CreatedAt: u.CreatedAt, UpdatedAt: u.UpdatedAt, LastLoginAt: u.LastLoginAt,
		})
	}
	okList(w, safe, total, pg.Page, pg.PerPage)
}

// UpdateUserByAdmin handles PATCH /api/v1/admin/users/{id}
func (h *UserAdminHandler) UpdateUserByAdmin(w http.ResponseWriter, r *http.Request) {
	tenant := middleware.TenantFromCtx(r.Context())
	if tenant == nil {
		errForbidden(w, "no tenant context")
		return
	}
	userID, err := uuid.Parse(chiURLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid user id")
		return
	}
	user, err := h.store.GetUser(r.Context(), userID)
	if err != nil {
		h.logger.Err(err).Msg("update user: lookup")
		errInternal(w)
		return
	}
	if user == nil || user.TenantID != tenant.ID {
		errNotFound(w, "user not found")
		return
	}

	var req struct {
		Role                *string          `json:"role"`
		IsActive            *bool            `json:"is_active"`
		DisplayName         *string          `json:"display_name"`
		PermissionProfileID *json.RawMessage `json:"permission_profile_id"`
	}
	if err := decodeBody(r, &req); err != nil {
		errBadRequest(w, "invalid request body")
		return
	}
	if req.Role != nil {
		newRole := models.UserRole(*req.Role)
		actor := middleware.ActorFromContext(r.Context())
		switch newRole {
		case models.RoleSuperAdmin, models.RoleAdmin, models.RoleUser:
			// Only super_admin can promote to super_admin
			if newRole == models.RoleSuperAdmin && !actor.IsSuperAdmin {
				errForbidden(w, "only super admin can assign super_admin role")
				return
			}
			user.Role = newRole
		default:
			errBadRequest(w, "invalid role, must be super_admin, admin or user")
			return
		}
	}
	if req.IsActive != nil {
		user.IsActive = *req.IsActive
	}
	if req.DisplayName != nil {
		user.DisplayName = *req.DisplayName
	}
	if req.PermissionProfileID != nil {
		raw := strings.TrimSpace(string(*req.PermissionProfileID))
		if raw == "" || raw == "null" {
			user.PermissionProfileID = nil
		} else {
			var profileID uuid.UUID
			if err := json.Unmarshal(*req.PermissionProfileID, &profileID); err != nil {
				errBadRequest(w, "invalid permission_profile_id")
				return
			}
			profile, err := h.store.GetPermissionProfile(r.Context(), profileID)
			if err != nil {
				h.logger.Err(err).Msg("update user: lookup permission profile")
				errInternal(w)
				return
			}
			if profile == nil {
				errBadRequest(w, "permission profile not found")
				return
			}
			if profile.TenantID != nil && *profile.TenantID != user.TenantID {
				errForbidden(w, "permission profile belongs to a different tenant")
				return
			}
			user.PermissionProfileID = &profileID
		}
	}
	if err := h.store.UpdateUser(r.Context(), user); err != nil {
		h.logger.Err(err).Msg("update user")
		errInternal(w)
		return
	}
	ok(w, map[string]any{
		"id": user.ID, "email": user.Email, "display_name": user.DisplayName,
		"role": user.Role, "is_active": user.IsActive, "tenant_id": user.TenantID,
		"permission_profile_id": user.PermissionProfileID,
		"created_at":            user.CreatedAt,
		"updated_at":            user.UpdatedAt,
		"last_login_at":         user.LastLoginAt,
	})
}

// DeleteUserByAdmin handles DELETE /api/v1/admin/users/{id}
func (h *UserAdminHandler) DeleteUserByAdmin(w http.ResponseWriter, r *http.Request) {
	tenant := middleware.TenantFromCtx(r.Context())
	if tenant == nil {
		errForbidden(w, "no tenant context")
		return
	}
	userID, err := uuid.Parse(chiURLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid user id")
		return
	}
	// Prevent self-deletion
	if caller := middleware.UserFromCtx(r.Context()); caller != nil && caller.ID == userID {
		errBadRequest(w, "cannot delete yourself")
		return
	}
	user, err := h.store.GetUser(r.Context(), userID)
	if err != nil {
		h.logger.Err(err).Msg("delete user: lookup")
		errInternal(w)
		return
	}
	if user == nil || user.TenantID != tenant.ID {
		errNotFound(w, "user not found")
		return
	}
	if err := h.store.DeleteUser(r.Context(), userID); err != nil {
		h.logger.Err(err).Msg("delete user")
		errInternal(w)
		return
	}
	noContent(w)
}

func generateInviteCode() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func chiURLParam(r *http.Request, key string) string {
	return chi.URLParam(r, key)
}
