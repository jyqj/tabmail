// Package authapp holds the credential and account flows behind the /auth and
// /admin/users endpoints. The handlers in internal/api/handlers only decode
// requests and shape responses; everything that decides whether a login,
// registration or role change is allowed lives here.
package authapp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"golang.org/x/crypto/bcrypt"
	"tabmail/internal/app"
	"tabmail/internal/authn"
	"tabmail/internal/authz"
	"tabmail/internal/models"
)

// MinPasswordLength is the shortest password the service accepts.
const MinPasswordLength = 8

const inviteTTL = 72 * time.Hour

type storeRepo interface {
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

// SettingsReader exposes the runtime settings the service consults.
type SettingsReader interface {
	GetBool(ctx context.Context, key string, defaultVal bool) bool
}

// LoginThrottle bounds password guessing against a single account.
type LoginThrottle interface {
	LoginAttemptsExceeded(ctx context.Context, identity string) bool
	RecordLoginFailure(ctx context.Context, identity string) error
}

// Config carries the deployment-level knobs of the auth flows.
type Config struct {
	JWTSecret        string
	DefaultPlanID    uuid.UUID
	OpenRegistration bool
	// Settings, when set, overrides OpenRegistration at runtime.
	Settings SettingsReader
	// Throttle records failed logins. When nil or unreachable, FailedLoginDelay
	// is applied instead.
	Throttle LoginThrottle
	// FailedLoginDelay slows a failed login response when the throttle is
	// unavailable. Tests set it to zero.
	FailedLoginDelay time.Duration
}

type Service struct {
	store  storeRepo
	cfg    Config
	logger zerolog.Logger
}

func NewService(s storeRepo, cfg Config, logger zerolog.Logger) *Service {
	return &Service{store: s, cfg: cfg, logger: logger.With().Str("service", "auth").Logger()}
}

// Session is the token pair handed back by every flow that authenticates a
// caller. User is nil for refreshes, which do not re-state the identity.
type Session struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int
	User         *models.User
}

var errInvalidCredentials = app.Unauthorized("invalid email or password")

func (s *Service) Login(ctx context.Context, email, password string) (*Session, error) {
	email = normalizeEmail(email)
	if email == "" || password == "" {
		return nil, app.BadRequest("email and password are required")
	}

	if s.cfg.Throttle != nil && s.cfg.Throttle.LoginAttemptsExceeded(ctx, email) {
		return nil, app.RateLimited("too many failed login attempts, try again later")
	}

	user, err := s.store.GetUserByEmail(ctx, email)
	if err != nil {
		return nil, app.Internal(err)
	}
	if user == nil {
		s.penalizeFailedLogin(ctx, email)
		return nil, errInvalidCredentials
	}
	if !user.IsActive {
		return nil, app.Forbidden("account is disabled")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		s.penalizeFailedLogin(ctx, email)
		return nil, errInvalidCredentials
	}

	session, err := s.issueSession(ctx, user)
	if err != nil {
		return nil, err
	}

	// Logins are already throttled, so this runs inline rather than in a
	// detached goroutine; a failed bookkeeping write must not fail the login.
	if err := s.store.TouchUserLogin(ctx, user.ID); err != nil {
		s.logger.Warn().Err(err).Str("user_id", user.ID.String()).Msg("login: record last login")
	}
	return session, nil
}

func (s *Service) Register(ctx context.Context, email, password, displayName string) (*Session, error) {
	open := s.cfg.OpenRegistration
	if s.cfg.Settings != nil {
		open = s.cfg.Settings.GetBool(ctx, models.SettingOpenRegistration, s.cfg.OpenRegistration)
	}
	if !open {
		return nil, app.Forbidden("registration is not open")
	}

	email = normalizeEmail(email)
	if email == "" || password == "" {
		return nil, app.BadRequest("email and password are required")
	}
	if len(password) < MinPasswordLength {
		return nil, app.BadRequest("password must be at least 8 characters")
	}

	existing, err := s.store.GetUserByEmail(ctx, email)
	if err != nil {
		return nil, app.Internal(err)
	}
	if existing != nil {
		return nil, app.Conflict("email already registered")
	}

	user, err := s.createUserWithTenant(ctx, email, password, displayName, models.RoleUser)
	if err != nil {
		return nil, err
	}
	return s.issueSession(ctx, user)
}

func (s *Service) Refresh(ctx context.Context, refreshToken string) (*Session, error) {
	if refreshToken == "" {
		return nil, app.BadRequest("refresh_token is required")
	}

	rt, err := s.store.GetRefreshToken(ctx, authn.HashToken(refreshToken))
	if err != nil {
		return nil, app.Internal(err)
	}
	if rt == nil || rt.ExpiresAt.Before(time.Now()) {
		return nil, app.Unauthorized("invalid or expired refresh token")
	}
	if rt.RevokedAt != nil {
		// A revoked token was reused — possible token theft. Revoke all tokens for this user.
		s.logger.Warn().Str("user_id", rt.UserID.String()).Msg("refresh: revoked token reuse detected, revoking all user tokens")
		_ = s.store.RevokeUserRefreshTokens(ctx, rt.UserID)
		return nil, app.Unauthorized("invalid or expired refresh token")
	}

	// Revoke old refresh token (rotation)
	_ = s.store.RevokeRefreshToken(ctx, rt.ID)

	user, err := s.store.GetUser(ctx, rt.UserID)
	if err != nil || user == nil || !user.IsActive {
		return nil, app.Unauthorized("user not found or inactive")
	}

	session, err := s.issueSession(ctx, user)
	if err != nil {
		return nil, err
	}
	session.User = nil
	return session, nil
}

// Logout revokes the presented refresh token, or every token the caller holds
// when no specific token is given. It never reports a failure: a logout that
// cannot find its token has still achieved what the caller asked for.
func (s *Service) Logout(ctx context.Context, refreshToken string, caller *models.User) {
	if refreshToken != "" {
		if rt, err := s.store.GetRefreshToken(ctx, authn.HashToken(refreshToken)); err == nil && rt != nil {
			_ = s.store.RevokeRefreshToken(ctx, rt.ID)
		}
		return
	}
	if caller != nil {
		_ = s.store.RevokeUserRefreshTokens(ctx, caller.ID)
	}
}

func (s *Service) ChangePassword(ctx context.Context, caller *models.User, oldPassword, newPassword string) error {
	if caller == nil {
		return app.Unauthorized("not logged in")
	}
	if oldPassword == "" || newPassword == "" {
		return app.BadRequest("old_password and new_password are required")
	}
	if len(newPassword) < MinPasswordLength {
		return app.BadRequest("new password must be at least 8 characters")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(caller.PasswordHash), []byte(oldPassword)); err != nil {
		return app.Unauthorized("incorrect old password")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return app.Internal(err)
	}
	if err := s.store.UpdateUserPassword(ctx, caller.ID, string(hash)); err != nil {
		return app.Internal(err)
	}
	// Revoke all refresh tokens to force re-login
	_ = s.store.RevokeUserRefreshTokens(ctx, caller.ID)
	return nil
}

// InviteAdmin mints an invitation code. Accepting it creates a super_admin, so
// the route is restricted to super admins.
func (s *Service) InviteAdmin(ctx context.Context, email string, inviter *models.User) (*models.AdminInvitation, error) {
	email = normalizeEmail(email)
	if email == "" {
		return nil, app.BadRequest("email is required")
	}

	existing, err := s.store.GetUserByEmail(ctx, email)
	if err != nil {
		return nil, app.Internal(err)
	}
	if existing != nil {
		return nil, app.Conflict("email already registered")
	}

	code, err := generateInviteCode()
	if err != nil {
		return nil, app.Internal(err)
	}

	var inviterID *uuid.UUID
	if inviter != nil {
		id := inviter.ID
		inviterID = &id
	}

	inv := &models.AdminInvitation{
		Email:      email,
		InviteCode: code,
		InvitedBy:  inviterID,
		ExpiresAt:  time.Now().Add(inviteTTL),
	}
	if err := s.store.CreateAdminInvitation(ctx, inv); err != nil {
		return nil, app.Internal(err)
	}
	return inv, nil
}

func (s *Service) AcceptInvite(ctx context.Context, inviteCode, password, displayName string) (*Session, error) {
	if inviteCode == "" || password == "" {
		return nil, app.BadRequest("invite_code and password are required")
	}
	if len(password) < MinPasswordLength {
		return nil, app.BadRequest("password must be at least 8 characters")
	}

	inv, err := s.store.GetAdminInvitationByCode(ctx, inviteCode)
	if err != nil {
		return nil, app.Internal(err)
	}
	if inv == nil || inv.AcceptedAt != nil || inv.ExpiresAt.Before(time.Now()) {
		return nil, app.BadRequest("invalid or expired invitation")
	}

	existing, err := s.store.GetUserByEmail(ctx, inv.Email)
	if err != nil {
		return nil, app.Internal(err)
	}
	if existing != nil {
		return nil, app.Conflict("email already registered")
	}

	// Admin users get their own tenant (power comes from User.Role, not Tenant.IsSuper)
	user, err := s.createUserWithTenant(ctx, inv.Email, password, displayName, models.RoleSuperAdmin)
	if err != nil {
		return nil, err
	}

	_ = s.store.MarkInvitationAccepted(ctx, inv.ID)
	return s.issueSession(ctx, user)
}

func (s *Service) ListUsers(ctx context.Context, tenant *models.Tenant, pg models.Page) ([]*models.User, int, error) {
	if tenant == nil {
		return nil, 0, app.Forbidden("no tenant context")
	}
	users, total, err := s.store.ListUsers(ctx, tenant.ID, pg)
	if err != nil {
		return nil, 0, app.Internal(err)
	}
	return users, total, nil
}

// UpdateUserRequest describes a partial update. A nil field is left untouched;
// ProfileSet distinguishes "not supplied" from "explicitly cleared" for the
// permission profile, which ProfileID alone cannot express.
type UpdateUserRequest struct {
	Role        *string
	IsActive    *bool
	DisplayName *string
	ProfileSet  bool
	ProfileID   *uuid.UUID
}

func (s *Service) UpdateUser(ctx context.Context, actor authz.Actor, tenant *models.Tenant, userID uuid.UUID, req UpdateUserRequest) (*models.User, error) {
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

	if req.Role != nil {
		newRole := models.UserRole(*req.Role)
		switch newRole {
		case models.RoleSuperAdmin, models.RoleAdmin, models.RoleUser:
			// Only super_admin can promote to super_admin
			if newRole == models.RoleSuperAdmin && !actor.IsSuperAdmin {
				return nil, app.Forbidden("only super admin can assign super_admin role")
			}
			user.Role = newRole
		default:
			return nil, app.BadRequest("invalid role, must be super_admin, admin or user")
		}
	}
	if req.IsActive != nil {
		user.IsActive = *req.IsActive
	}
	if req.DisplayName != nil {
		user.DisplayName = *req.DisplayName
	}
	if req.ProfileSet {
		if req.ProfileID == nil {
			user.PermissionProfileID = nil
		} else {
			profile, err := s.store.GetPermissionProfile(ctx, *req.ProfileID)
			if err != nil {
				return nil, app.Internal(err)
			}
			if profile == nil {
				return nil, app.BadRequest("permission profile not found")
			}
			if profile.TenantID != nil && *profile.TenantID != user.TenantID {
				return nil, app.Forbidden("permission profile belongs to a different tenant")
			}
			profileID := *req.ProfileID
			user.PermissionProfileID = &profileID
		}
	}

	if err := s.store.UpdateUser(ctx, user); err != nil {
		return nil, app.Internal(err)
	}
	return user, nil
}

func (s *Service) DeleteUser(ctx context.Context, tenant *models.Tenant, caller *models.User, userID uuid.UUID) error {
	if tenant == nil {
		return app.Forbidden("no tenant context")
	}
	if caller != nil && caller.ID == userID {
		return app.BadRequest("cannot delete yourself")
	}
	user, err := s.store.GetUser(ctx, userID)
	if err != nil {
		return app.Internal(err)
	}
	if user == nil || user.TenantID != tenant.ID {
		return app.NotFound("user not found")
	}
	if err := s.store.DeleteUser(ctx, userID); err != nil {
		return app.Internal(err)
	}
	return nil
}

// createUserWithTenant provisions the tenant a new account owns and the account
// itself. Registration and invitation acceptance differ only in the role.
func (s *Service) createUserWithTenant(ctx context.Context, email, password, displayName string, role models.UserRole) (*models.User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, app.Internal(err)
	}

	tenant := &models.Tenant{Name: email, PlanID: s.cfg.DefaultPlanID}
	if err := s.store.CreateTenant(ctx, tenant); err != nil {
		return nil, app.Internal(err)
	}

	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		displayName = strings.Split(email, "@")[0]
	}

	user := &models.User{
		TenantID:     tenant.ID,
		Email:        email,
		PasswordHash: string(hash),
		DisplayName:  displayName,
		Role:         role,
		IsActive:     true,
	}
	if err := s.store.CreateUser(ctx, user); err != nil {
		return nil, app.Internal(err)
	}
	return user, nil
}

func (s *Service) issueSession(ctx context.Context, user *models.User) (*Session, error) {
	accessToken, err := authn.IssueAccessToken(s.cfg.JWTSecret, user)
	if err != nil {
		return nil, app.Internal(err)
	}

	rawRefresh, refreshHash, err := authn.GenerateRefreshToken()
	if err != nil {
		return nil, app.Internal(err)
	}

	rt := &models.RefreshToken{
		UserID:    user.ID,
		TokenHash: refreshHash,
		ExpiresAt: time.Now().Add(authn.RefreshTokenTTL),
	}
	if err := s.store.CreateRefreshToken(ctx, rt); err != nil {
		return nil, app.Internal(err)
	}

	return &Session{
		AccessToken:  accessToken,
		RefreshToken: rawRefresh,
		ExpiresIn:    int(authn.AccessTokenTTL.Seconds()),
		User:         user,
	}, nil
}

// penalizeFailedLogin counts the attempt against the account's throttle window.
// Only when that counter cannot be reached does it fall back to slowing the
// response down, which is the last line of defence against brute forcing.
func (s *Service) penalizeFailedLogin(ctx context.Context, email string) {
	if s.cfg.Throttle != nil && s.cfg.Throttle.RecordLoginFailure(ctx, email) == nil {
		return
	}
	if s.cfg.FailedLoginDelay > 0 {
		time.Sleep(s.cfg.FailedLoginDelay)
	}
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func generateInviteCode() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
