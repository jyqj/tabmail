package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/api/middleware"
	appcore "tabmail/internal/app"
	authapp "tabmail/internal/app/auth"
	"tabmail/internal/models"
)

type authStore interface {
	GetUserByEmail(ctx context.Context, email string) (*models.User, error)
	CreateUser(ctx context.Context, u *models.User) error
	GetUser(ctx context.Context, id uuid.UUID) (*models.User, error)
	UpdateUser(ctx context.Context, u *models.User) error
	UpdateUserPassword(ctx context.Context, id uuid.UUID, passwordHash string) error
	ListUsers(ctx context.Context, tenantID uuid.UUID, pg models.Page) ([]*models.User, int, error)
	DeleteUser(ctx context.Context, id uuid.UUID) error
	TouchUserLogin(ctx context.Context, id uuid.UUID) error
	CreateRefreshToken(ctx context.Context, rt *models.RefreshToken) error
	GetRefreshToken(ctx context.Context, tokenHash string) (*models.RefreshToken, error)
	RevokeRefreshToken(ctx context.Context, id uuid.UUID) error
	RevokeUserRefreshTokens(ctx context.Context, userID uuid.UUID) error
	CreateTenant(ctx context.Context, t *models.Tenant) error
	GetTenant(ctx context.Context, id uuid.UUID) (*models.Tenant, error)
	CreateAdminInvitation(ctx context.Context, inv *models.AdminInvitation) error
	GetAdminInvitationByCode(ctx context.Context, code string) (*models.AdminInvitation, error)
	MarkInvitationAccepted(ctx context.Context, id uuid.UUID) error
	InsertAudit(ctx context.Context, e *models.AuditEntry) error
	GetPermissionProfile(ctx context.Context, id uuid.UUID) (*models.PermissionProfile, error)
}

// AuthHandlerConfig bundles the optional knobs for NewAuthHandler.
type AuthHandlerConfig struct {
	// Throttle records failed logins. When nil or unreachable, FailedLoginDelay
	// is applied instead.
	Throttle authapp.LoginThrottle
	// FailedLoginDelay slows a failed login response when the throttle is
	// unavailable. Tests set it to zero.
	FailedLoginDelay time.Duration
}

// AuthHandler handles authentication endpoints.
type AuthHandler struct {
	service *authapp.Service
	logger  zerolog.Logger
}

func NewAuthHandler(s authStore, jwtSecret string, defaultPlanID uuid.UUID, openRegistration bool, settings authapp.SettingsReader, authCfg AuthHandlerConfig, l zerolog.Logger) *AuthHandler {
	service := authapp.NewService(s, authapp.Config{
		JWTSecret:        jwtSecret,
		DefaultPlanID:    defaultPlanID,
		OpenRegistration: openRegistration,
		Settings:         settings,
		Throttle:         authCfg.Throttle,
		FailedLoginDelay: authCfg.FailedLoginDelay,
	}, l)
	return &AuthHandler{service: service, logger: l.With().Str("handler", "auth").Logger()}
}

// Login handles POST /api/v1/auth/login
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decodeBody(r, &req); err != nil {
		errBadRequest(w, "invalid request body")
		return
	}
	session, err := h.service.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		h.respondAuthError(w, err)
		return
	}
	ok(w, sessionPayload(session))
}

// Register handles POST /api/v1/auth/register
func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email       string `json:"email"`
		Password    string `json:"password"`
		DisplayName string `json:"display_name"`
	}
	if err := decodeBody(r, &req); err != nil {
		errBadRequest(w, "invalid request body")
		return
	}
	session, err := h.service.Register(r.Context(), req.Email, req.Password, req.DisplayName)
	if err != nil {
		h.respondAuthError(w, err)
		return
	}
	created(w, sessionPayload(session))
}

// Refresh handles POST /api/v1/auth/refresh
func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := decodeBody(r, &req); err != nil {
		errBadRequest(w, "invalid request body")
		return
	}
	session, err := h.service.Refresh(r.Context(), req.RefreshToken)
	if err != nil {
		h.respondAuthError(w, err)
		return
	}
	ok(w, sessionPayload(session))
}

// Logout handles POST /api/v1/auth/logout
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	caller := middleware.UserFromCtx(r.Context())
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := decodeBody(r, &req); err != nil {
		// Even without body, revoke all tokens for the logged-in user
		h.service.Logout(r.Context(), "", caller)
		noContent(w)
		return
	}
	h.service.Logout(r.Context(), req.RefreshToken, caller)
	noContent(w)
}

// Me handles GET /api/v1/auth/me
func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromCtx(r.Context())
	if user == nil {
		errUnauthorized(w, "not logged in")
		return
	}
	ok(w, map[string]any{
		"id":            user.ID,
		"email":         user.Email,
		"display_name":  user.DisplayName,
		"role":          user.Role,
		"tenant_id":     user.TenantID,
		"is_active":     user.IsActive,
		"created_at":    user.CreatedAt,
		"last_login_at": user.LastLoginAt,
	})
}

// InviteAdmin handles POST /api/v1/admin/invite.
// This endpoint is super-admin only because accepting an invitation creates
// a super_admin user.
func (h *AuthHandler) InviteAdmin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
	}
	if err := decodeBody(r, &req); err != nil {
		errBadRequest(w, "invalid request body")
		return
	}
	inv, err := h.service.InviteAdmin(r.Context(), req.Email, middleware.UserFromCtx(r.Context()))
	if err != nil {
		h.respondAuthError(w, err)
		return
	}
	created(w, map[string]any{
		"id":          inv.ID,
		"email":       inv.Email,
		"invite_code": inv.InviteCode,
		"expires_at":  inv.ExpiresAt,
	})
}

// AcceptInvite handles POST /api/v1/auth/accept-invite
func (h *AuthHandler) AcceptInvite(w http.ResponseWriter, r *http.Request) {
	var req struct {
		InviteCode  string `json:"invite_code"`
		Password    string `json:"password"`
		DisplayName string `json:"display_name"`
	}
	if err := decodeBody(r, &req); err != nil {
		errBadRequest(w, "invalid request body")
		return
	}
	session, err := h.service.AcceptInvite(r.Context(), req.InviteCode, req.Password, req.DisplayName)
	if err != nil {
		h.respondAuthError(w, err)
		return
	}
	created(w, sessionPayload(session))
}

// ListUsers handles GET /api/v1/admin/users
func (h *AuthHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	pg := pageFromReq(r)
	users, total, err := h.service.ListUsers(r.Context(), middleware.TenantFromCtx(r.Context()), pg)
	if err != nil {
		h.respondAuthError(w, err)
		return
	}
	safe := make([]safeUser, 0, len(users))
	for _, u := range users {
		safe = append(safe, newSafeUser(u))
	}
	okList(w, safe, total, pg.Page, pg.PerPage)
}

// UpdateUserByAdmin handles PATCH /api/v1/admin/users/{id}
func (h *AuthHandler) UpdateUserByAdmin(w http.ResponseWriter, r *http.Request) {
	userID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid user id")
		return
	}
	var body struct {
		Role                *string          `json:"role"`
		IsActive            *bool            `json:"is_active"`
		DisplayName         *string          `json:"display_name"`
		PermissionProfileID *json.RawMessage `json:"permission_profile_id"`
	}
	if err := decodeBody(r, &body); err != nil {
		errBadRequest(w, "invalid request body")
		return
	}

	req := authapp.UpdateUserRequest{Role: body.Role, IsActive: body.IsActive, DisplayName: body.DisplayName}
	if body.PermissionProfileID != nil {
		req.ProfileSet = true
		if raw := strings.TrimSpace(string(*body.PermissionProfileID)); raw != "" && raw != "null" {
			var profileID uuid.UUID
			if err := json.Unmarshal(*body.PermissionProfileID, &profileID); err != nil {
				errBadRequest(w, "invalid permission_profile_id")
				return
			}
			req.ProfileID = &profileID
		}
	}

	user, err := h.service.UpdateUser(
		r.Context(),
		middleware.ActorFromContext(r.Context()),
		middleware.TenantFromCtx(r.Context()),
		userID,
		req,
	)
	if err != nil {
		h.respondAuthError(w, err)
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
func (h *AuthHandler) DeleteUserByAdmin(w http.ResponseWriter, r *http.Request) {
	userID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		errBadRequest(w, "invalid user id")
		return
	}
	err = h.service.DeleteUser(
		r.Context(),
		middleware.TenantFromCtx(r.Context()),
		middleware.UserFromCtx(r.Context()),
		userID,
	)
	if err != nil {
		h.respondAuthError(w, err)
		return
	}
	noContent(w)
}

// ChangePassword handles POST /api/v1/auth/change-password
func (h *AuthHandler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := decodeBody(r, &req); err != nil {
		errBadRequest(w, "invalid request body")
		return
	}
	err := h.service.ChangePassword(r.Context(), middleware.UserFromCtx(r.Context()), req.OldPassword, req.NewPassword)
	if err != nil {
		h.respondAuthError(w, err)
		return
	}
	ok(w, map[string]string{"status": "password changed"})
}

// respondAuthError adds the throttling hint that only the HTTP layer can
// express, then defers to the shared application error mapping.
func (h *AuthHandler) respondAuthError(w http.ResponseWriter, err error) {
	if appErr, okAs := appcore.As(err); okAs && appErr.Kind == appcore.KindRateLimited {
		w.Header().Set("Retry-After", strconv.Itoa(int(middleware.LoginFailureWindow.Seconds())))
	}
	respondAppError(w, h.logger, err)
}

func sessionPayload(s *authapp.Session) map[string]any {
	payload := map[string]any{
		"access_token":  s.AccessToken,
		"refresh_token": s.RefreshToken,
		"token_type":    "Bearer",
		"expires_in":    s.ExpiresIn,
	}
	if s.User != nil {
		payload["user"] = map[string]any{
			"id":           s.User.ID,
			"email":        s.User.Email,
			"display_name": s.User.DisplayName,
			"role":         s.User.Role,
			"tenant_id":    s.User.TenantID,
		}
	}
	return payload
}

// safeUser is the user shape the API exposes: everything except the password hash.
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

func newSafeUser(u *models.User) safeUser {
	return safeUser{
		ID: u.ID, TenantID: u.TenantID, Email: u.Email,
		DisplayName: u.DisplayName, Role: u.Role, PermissionProfileID: u.PermissionProfileID,
		IsActive: u.IsActive, CreatedAt: u.CreatedAt, UpdatedAt: u.UpdatedAt, LastLoginAt: u.LastLoginAt,
	}
}
