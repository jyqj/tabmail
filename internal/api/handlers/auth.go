package handlers

import (
	"context"
	"net/http"
	"strings"
	"time"

	"tabmail/internal/api/middleware"
	"tabmail/internal/authn"
	"tabmail/internal/models"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"golang.org/x/crypto/bcrypt"
)

// authStore is the subset of the store the session-lifecycle handler needs.
type authStore interface {
	GetUserByEmail(ctx context.Context, email string) (*models.User, error)
	CreateUser(ctx context.Context, u *models.User) error
	GetUser(ctx context.Context, id uuid.UUID) (*models.User, error)
	UpdateUserPassword(ctx context.Context, id uuid.UUID, passwordHash string) error
	TouchUserLogin(ctx context.Context, id uuid.UUID) error
	CreateRefreshToken(ctx context.Context, rt *models.RefreshToken) error
	GetRefreshToken(ctx context.Context, tokenHash string) (*models.RefreshToken, error)
	RevokeRefreshToken(ctx context.Context, id uuid.UUID) error
	RevokeUserRefreshTokens(ctx context.Context, userID uuid.UUID) error
	CreateTenant(ctx context.Context, t *models.Tenant) error
	GetAdminInvitationByCode(ctx context.Context, code string) (*models.AdminInvitation, error)
	MarkInvitationAccepted(ctx context.Context, id uuid.UUID) error
}

type settingsReader interface {
	GetBool(ctx context.Context, key string, defaultVal bool) bool
}

// AuthHandler handles authentication endpoints.
type AuthHandler struct {
	store                   authStore
	jwtSecret               string
	defaultPlanID           uuid.UUID
	defaultOpenRegistration bool
	settings                settingsReader
	cookieSecure            bool
	logger                  zerolog.Logger
}

func NewAuthHandler(s authStore, jwtSecret string, defaultPlanID uuid.UUID, openRegistration bool, settings settingsReader, cookieSecure bool, l zerolog.Logger) *AuthHandler {
	return &AuthHandler{
		store:                   s,
		jwtSecret:               jwtSecret,
		defaultPlanID:           defaultPlanID,
		defaultOpenRegistration: openRegistration,
		settings:                settings,
		cookieSecure:            cookieSecure,
		logger:                  l.With().Str("handler", "auth").Logger(),
	}
}

// RefreshCookieName is the httpOnly cookie that carries the refresh token.
// The token never appears in a JSON response body, so XSS cannot exfiltrate
// it from localStorage.
const RefreshCookieName = "tabmail_refresh_token"

// refreshCookiePath restricts the cookie to the auth endpoints that actually
// consume it (/api/v1/auth/refresh, /api/v1/auth/logout).
const refreshCookiePath = "/api/v1/auth"

func (h *AuthHandler) setRefreshCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     RefreshCookieName,
		Value:    token,
		Path:     refreshCookiePath,
		MaxAge:   int(authn.RefreshTokenTTL.Seconds()),
		HttpOnly: true,
		Secure:   h.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (h *AuthHandler) clearRefreshCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     RefreshCookieName,
		Value:    "",
		Path:     refreshCookiePath,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

// refreshTokenFromRequest prefers the httpOnly cookie and falls back to a JSON
// body field for non-browser API clients.
func refreshTokenFromRequest(r *http.Request, bodyToken string) string {
	if c, err := r.Cookie(RefreshCookieName); err == nil && c.Value != "" {
		return c.Value
	}
	return bodyToken
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
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	if req.Email == "" || req.Password == "" {
		errBadRequest(w, "email and password are required")
		return
	}

	user, err := h.store.GetUserByEmail(r.Context(), req.Email)
	if err != nil {
		h.logger.Err(err).Msg("login: get user")
		errInternal(w)
		return
	}
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, envelope{Error: &apiErr{Code: "UNAUTHORIZED", Message: "invalid email or password"}})
		return
	}
	if !user.IsActive {
		writeJSON(w, http.StatusForbidden, envelope{Error: &apiErr{Code: "FORBIDDEN", Message: "account is disabled"}})
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		time.Sleep(500 * time.Millisecond) // slow down brute-force attempts
		writeJSON(w, http.StatusUnauthorized, envelope{Error: &apiErr{Code: "UNAUTHORIZED", Message: "invalid email or password"}})
		return
	}

	accessToken, refreshToken, err := h.issueTokenPair(r.Context(), user)
	if err != nil {
		h.logger.Err(err).Msg("login: issue tokens")
		errInternal(w)
		return
	}

	go func() { _ = h.store.TouchUserLogin(context.Background(), user.ID) }()

	h.setRefreshCookie(w, refreshToken)
	ok(w, map[string]any{
		"access_token": accessToken,
		"token_type":   "Bearer",
		"expires_in":   int(authn.AccessTokenTTL.Seconds()),
		"user": map[string]any{
			"id":           user.ID,
			"email":        user.Email,
			"display_name": user.DisplayName,
			"role":         user.Role,
			"tenant_id":    user.TenantID,
		},
	})
}

// Register handles POST /api/v1/auth/register
func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	open := h.defaultOpenRegistration
	if h.settings != nil {
		open = h.settings.GetBool(r.Context(), models.SettingOpenRegistration, h.defaultOpenRegistration)
	}
	if !open {
		writeJSON(w, http.StatusForbidden, envelope{Error: &apiErr{Code: "FORBIDDEN", Message: "registration is not open"}})
		return
	}

	var req struct {
		Email       string `json:"email"`
		Password    string `json:"password"`
		DisplayName string `json:"display_name"`
	}
	if err := decodeBody(r, &req); err != nil {
		errBadRequest(w, "invalid request body")
		return
	}
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	if req.Email == "" || req.Password == "" {
		errBadRequest(w, "email and password are required")
		return
	}
	if len(req.Password) < 8 {
		errBadRequest(w, "password must be at least 8 characters")
		return
	}

	existing, err := h.store.GetUserByEmail(r.Context(), req.Email)
	if err != nil {
		h.logger.Err(err).Msg("register: check existing")
		errInternal(w)
		return
	}
	if existing != nil {
		errConflict(w, "email already registered")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		h.logger.Err(err).Msg("register: hash password")
		errInternal(w)
		return
	}

	// Create a tenant for this user
	tenant := &models.Tenant{
		Name:   req.Email,
		PlanID: h.defaultPlanID,
	}
	if err := h.store.CreateTenant(r.Context(), tenant); err != nil {
		h.logger.Err(err).Msg("register: create tenant")
		errInternal(w)
		return
	}

	displayName := strings.TrimSpace(req.DisplayName)
	if displayName == "" {
		displayName = strings.Split(req.Email, "@")[0]
	}

	user := &models.User{
		TenantID:     tenant.ID,
		Email:        req.Email,
		PasswordHash: string(hash),
		DisplayName:  displayName,
		Role:         models.RoleUser,
		IsActive:     true,
	}
	if err := h.store.CreateUser(r.Context(), user); err != nil {
		h.logger.Err(err).Msg("register: create user")
		errInternal(w)
		return
	}

	accessToken, refreshToken, err := h.issueTokenPair(r.Context(), user)
	if err != nil {
		h.logger.Err(err).Msg("register: issue tokens")
		errInternal(w)
		return
	}

	h.setRefreshCookie(w, refreshToken)
	created(w, map[string]any{
		"access_token": accessToken,
		"token_type":   "Bearer",
		"expires_in":   int(authn.AccessTokenTTL.Seconds()),
		"user": map[string]any{
			"id":           user.ID,
			"email":        user.Email,
			"display_name": user.DisplayName,
			"role":         user.Role,
			"tenant_id":    user.TenantID,
		},
	})
}

// Refresh handles POST /api/v1/auth/refresh
func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	_ = decodeBody(r, &req)

	rawToken := refreshTokenFromRequest(r, req.RefreshToken)
	if rawToken == "" {
		errBadRequest(w, "refresh_token is required")
		return
	}

	tokenHash := authn.HashToken(rawToken)
	rt, err := h.store.GetRefreshToken(r.Context(), tokenHash)
	if err != nil {
		h.logger.Err(err).Msg("refresh: lookup token")
		errInternal(w)
		return
	}
	if rt == nil || rt.ExpiresAt.Before(time.Now()) {
		writeJSON(w, http.StatusUnauthorized, envelope{Error: &apiErr{Code: "UNAUTHORIZED", Message: "invalid or expired refresh token"}})
		return
	}
	if rt.RevokedAt != nil {
		// A revoked token was reused — possible token theft. Revoke all tokens for this user.
		h.logger.Warn().Str("user_id", rt.UserID.String()).Msg("refresh: revoked token reuse detected, revoking all user tokens")
		_ = h.store.RevokeUserRefreshTokens(r.Context(), rt.UserID)
		writeJSON(w, http.StatusUnauthorized, envelope{Error: &apiErr{Code: "UNAUTHORIZED", Message: "invalid or expired refresh token"}})
		return
	}

	// Revoke old refresh token (rotation)
	_ = h.store.RevokeRefreshToken(r.Context(), rt.ID)

	user, err := h.store.GetUser(r.Context(), rt.UserID)
	if err != nil || user == nil || !user.IsActive {
		writeJSON(w, http.StatusUnauthorized, envelope{Error: &apiErr{Code: "UNAUTHORIZED", Message: "user not found or inactive"}})
		return
	}

	accessToken, refreshToken, err := h.issueTokenPair(r.Context(), user)
	if err != nil {
		h.logger.Err(err).Msg("refresh: issue tokens")
		errInternal(w)
		return
	}

	h.setRefreshCookie(w, refreshToken)
	ok(w, map[string]any{
		"access_token": accessToken,
		"token_type":   "Bearer",
		"expires_in":   int(authn.AccessTokenTTL.Seconds()),
	})
}

// Logout handles POST /api/v1/auth/logout
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	_ = decodeBody(r, &req)

	rawToken := refreshTokenFromRequest(r, req.RefreshToken)
	if rawToken != "" {
		tokenHash := authn.HashToken(rawToken)
		rt, err := h.store.GetRefreshToken(r.Context(), tokenHash)
		if err == nil && rt != nil {
			_ = h.store.RevokeRefreshToken(r.Context(), rt.ID)
		}
	} else if user := middleware.UserFromCtx(r.Context()); user != nil {
		_ = h.store.RevokeUserRefreshTokens(r.Context(), user.ID)
	}
	h.clearRefreshCookie(w)
	noContent(w)
}

// Me handles GET /api/v1/auth/me
func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromCtx(r.Context())
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, envelope{Error: &apiErr{Code: "UNAUTHORIZED", Message: "not logged in"}})
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
	if req.InviteCode == "" || req.Password == "" {
		errBadRequest(w, "invite_code and password are required")
		return
	}
	if len(req.Password) < 8 {
		errBadRequest(w, "password must be at least 8 characters")
		return
	}

	inv, err := h.store.GetAdminInvitationByCode(r.Context(), req.InviteCode)
	if err != nil {
		h.logger.Err(err).Msg("accept-invite: lookup")
		errInternal(w)
		return
	}
	if inv == nil || inv.AcceptedAt != nil || inv.ExpiresAt.Before(time.Now()) {
		writeJSON(w, http.StatusBadRequest, envelope{Error: &apiErr{Code: "BAD_REQUEST", Message: "invalid or expired invitation"}})
		return
	}

	existing, err := h.store.GetUserByEmail(r.Context(), inv.Email)
	if err != nil {
		h.logger.Err(err).Msg("accept-invite: check existing")
		errInternal(w)
		return
	}
	if existing != nil {
		errConflict(w, "email already registered")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		h.logger.Err(err).Msg("accept-invite: hash password")
		errInternal(w)
		return
	}

	// Admin users get their own tenant (power comes from User.Role, not Tenant.IsSuper)
	tenant := &models.Tenant{
		Name:   inv.Email,
		PlanID: h.defaultPlanID,
	}
	if err := h.store.CreateTenant(r.Context(), tenant); err != nil {
		h.logger.Err(err).Msg("accept-invite: create tenant")
		errInternal(w)
		return
	}

	displayName := strings.TrimSpace(req.DisplayName)
	if displayName == "" {
		displayName = strings.Split(inv.Email, "@")[0]
	}

	user := &models.User{
		TenantID:     tenant.ID,
		Email:        inv.Email,
		PasswordHash: string(hash),
		DisplayName:  displayName,
		Role:         models.RoleSuperAdmin,
		IsActive:     true,
	}
	if err := h.store.CreateUser(r.Context(), user); err != nil {
		h.logger.Err(err).Msg("accept-invite: create user")
		errInternal(w)
		return
	}

	_ = h.store.MarkInvitationAccepted(r.Context(), inv.ID)

	accessToken, refreshToken, err := h.issueTokenPair(r.Context(), user)
	if err != nil {
		h.logger.Err(err).Msg("accept-invite: issue tokens")
		errInternal(w)
		return
	}

	h.setRefreshCookie(w, refreshToken)
	created(w, map[string]any{
		"access_token": accessToken,
		"token_type":   "Bearer",
		"expires_in":   int(authn.AccessTokenTTL.Seconds()),
		"user": map[string]any{
			"id":           user.ID,
			"email":        user.Email,
			"display_name": user.DisplayName,
			"role":         user.Role,
			"tenant_id":    user.TenantID,
		},
	})
}

// ChangePassword handles POST /api/v1/auth/change-password
func (h *AuthHandler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromCtx(r.Context())
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, envelope{Error: &apiErr{Code: "UNAUTHORIZED", Message: "not logged in"}})
		return
	}
	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := decodeBody(r, &req); err != nil {
		errBadRequest(w, "invalid request body")
		return
	}
	if req.OldPassword == "" || req.NewPassword == "" {
		errBadRequest(w, "old_password and new_password are required")
		return
	}
	if len(req.NewPassword) < 8 {
		errBadRequest(w, "new password must be at least 8 characters")
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.OldPassword)); err != nil {
		writeJSON(w, http.StatusUnauthorized, envelope{Error: &apiErr{Code: "UNAUTHORIZED", Message: "incorrect old password"}})
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		h.logger.Err(err).Msg("change-password: hash")
		errInternal(w)
		return
	}
	// Need to update password_hash directly
	freshUser, err := h.store.GetUser(r.Context(), user.ID)
	if err != nil || freshUser == nil {
		errInternal(w)
		return
	}
	freshUser.PasswordHash = string(hash)
	freshUser.UpdatedAt = time.Now()
	// We need a direct update for password, use a small helper
	if err := h.updatePasswordHash(r.Context(), user.ID, string(hash)); err != nil {
		h.logger.Err(err).Msg("change-password: update")
		errInternal(w)
		return
	}
	// Revoke all refresh tokens to force re-login
	_ = h.store.RevokeUserRefreshTokens(r.Context(), user.ID)
	ok(w, map[string]string{"status": "password changed"})
}

func (h *AuthHandler) issueTokenPair(ctx context.Context, user *models.User) (accessToken, refreshToken string, err error) {
	accessToken, err = authn.IssueAccessToken(h.jwtSecret, user)
	if err != nil {
		return "", "", err
	}

	rawRefresh, refreshHash, err := authn.GenerateRefreshToken()
	if err != nil {
		return "", "", err
	}

	rt := &models.RefreshToken{
		UserID:    user.ID,
		TokenHash: refreshHash,
		ExpiresAt: time.Now().Add(authn.RefreshTokenTTL),
	}
	if err := h.store.CreateRefreshToken(ctx, rt); err != nil {
		return "", "", err
	}

	return accessToken, rawRefresh, nil
}

func (h *AuthHandler) updatePasswordHash(ctx context.Context, userID uuid.UUID, hash string) error {
	return h.store.UpdateUserPassword(ctx, userID, hash)
}
