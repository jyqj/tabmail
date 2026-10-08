package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/mail"
	"strings"
	"tabmail/internal/app/credentials"
	"time"
	"unicode/utf8"

	"tabmail/internal/api/middleware"
	"tabmail/internal/authz"
	"tabmail/internal/models"
	"tabmail/internal/store"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// userAdminStore is the subset of the store the user-admin handler needs.
type userAdminStore interface {
	store.MemberGuardStore
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
	if err := decodeAuthBody(w, r, &req); err != nil {
		errBadRequest(w, "invalid request body")
		return
	}
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	if req.Email == "" {
		errBadRequest(w, "email is required")
		return
	}
	// Invitations persist the same VARCHAR(255) identity consumed on account
	// acceptance. Reject invalid text before lookup or credential generation.
	// Parse the address instead of rejecting quoted/Unicode local parts.
	address, addressErr := mail.ParseAddress(req.Email)
	if strings.ContainsRune(req.Email, 0) || utf8.RuneCountInString(req.Email) > 255 ||
		addressErr != nil || address.Name != "" || strings.HasPrefix(req.Email, "<") || strings.HasSuffix(req.Email, ">") {
		errBadRequest(w, "email must be a valid address of at most 255 characters")
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

	actor := middleware.ActorFromContext(r.Context())
	if !authz.CanManageTenantMember(actor, tenant.ID, user.Role) {
		errForbidden(w, "cannot manage this member role")
		return
	}
	patch := models.UserAdminPatch{}
	var req *struct {
		Role                *string         `json:"role"`
		IsActive            *bool           `json:"is_active"`
		DisplayName         *string         `json:"display_name"`
		PermissionProfileID json.RawMessage `json:"permission_profile_id"`
	}
	if err := decodeAuthBody(w, r, &req); err != nil || req == nil {
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
			patch.Role = &newRole
		default:
			errBadRequest(w, "invalid role, must be super_admin, admin or user")
			return
		}
	}
	if req.IsActive != nil {
		patch.IsActive = req.IsActive
	}
	if req.DisplayName != nil {
		// Keep PATCH's exact text and null/omission semantics, but reject text
		// PostgreSQL cannot persist before any role/status/profile command.
		if strings.ContainsRune(*req.DisplayName, 0) || utf8.RuneCountInString(*req.DisplayName) > 255 {
			errBadRequest(w, "display_name must contain at most 255 characters and no NUL characters")
			return
		}
		patch.DisplayName = req.DisplayName
	}
	if req.PermissionProfileID != nil {
		errConflict(w, "profile assignment protocol upgraded; use permission-editor/assignment with expected_revision")
		return
	}

	updated, err := h.store.UpdateUserGuarded(r.Context(), actor, tenant.ID, userID, patch)
	if err != nil {
		h.writeMemberError(w, err)
		return
	}
	user = updated
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
	actor := middleware.ActorFromContext(r.Context())
	if !authz.CanManageTenantMember(actor, tenant.ID, user.Role) {
		errForbidden(w, "cannot manage this member role")
		return
	}
	if err := h.store.DeleteUserGuarded(r.Context(), actor, tenant.ID, userID); err != nil {
		h.writeMemberError(w, err)
		return
	}
	noContent(w)
}

func generateInviteCode() (string, error) {
	raw, _, err := credentials.IssueInvitation()
	return raw, err
}

func chiURLParam(r *http.Request, key string) string {
	return chi.URLParam(r, key)
}
