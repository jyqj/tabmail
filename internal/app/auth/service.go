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
	"tabmail/internal/hooks"
	"tabmail/internal/models"
	"tabmail/internal/store"
)

// MinPasswordLength is the shortest password the service accepts.
const MinPasswordLength = 8

const inviteTTL = 72 * time.Hour

type storeRepo interface {
	store.Transactor
	app.AuditStore
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
	ConsumeRefreshToken(ctx context.Context, id uuid.UUID) (bool, error)
	RevokeUserRefreshTokens(ctx context.Context, userID uuid.UUID) error
	CreateTenant(ctx context.Context, t *models.Tenant) error
	GetTenant(ctx context.Context, id uuid.UUID) (*models.Tenant, error)
	CreateAdminInvitation(ctx context.Context, inv *models.AdminInvitation) error
	GetAdminInvitationByCode(ctx context.Context, code string) (*models.AdminInvitation, error)
	AcceptAdminInvitation(ctx context.Context, id uuid.UUID) (bool, error)
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
	store      storeRepo
	dispatcher *hooks.Dispatcher
	cfg        Config
	logger     zerolog.Logger
}

func NewService(s storeRepo, dispatcher *hooks.Dispatcher, cfg Config, logger zerolog.Logger) *Service {
	return &Service{store: s, dispatcher: dispatcher, cfg: cfg, logger: logger.With().Str("service", "auth").Logger()}
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

	session, refresh, err := s.prepareSession(user)
	if err != nil {
		return nil, err
	}
	if err := app.WithinTransaction(ctx, s.store, func(txCtx context.Context) error {
		if err := s.store.CreateRefreshToken(txCtx, refresh); err != nil {
			return err
		}
		return app.InsertAuditRequired(txCtx, s.store, models.AuditEntry{
			TenantID: app.UUIDPtr(user.TenantID), Actor: "user:" + user.ID.String(),
			Action: "session.login", ResourceType: "refresh_token", ResourceID: app.UUIDPtr(refresh.ID),
		})
	}); err != nil {
		return nil, err
	}
	// last_login_at is operational metadata, not part of the credential
	// issuance invariant. Keep login available if this best-effort write fails.
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
	if existing, err := s.store.GetUserByEmail(ctx, email); err != nil {
		return nil, app.Internal(err)
	} else if existing != nil {
		return nil, app.Conflict("email already registered")
	}

	tenant, user, session, refresh, err := s.prepareProvisionedSession(email, password, displayName, models.RoleUser)
	if err != nil {
		return nil, err
	}
	audit := models.AuditEntry{
		TenantID: app.UUIDPtr(tenant.ID), Actor: "user:" + user.ID.String(),
		Action: "user.register", ResourceType: "user", ResourceID: app.UUIDPtr(user.ID),
		Details: app.MustJSON(map[string]any{"email": user.Email, "role": user.Role}),
	}
	event := hooks.Event{Type: "user.registered", TenantID: tenant.ID.String(), OccurredAt: time.Now().UTC(), Metadata: map[string]any{"user_id": user.ID.String(), "role": user.Role}}
	if err := app.CommitMutation(ctx, s.store, s.store, s.dispatcher, audit, &event, func(txCtx context.Context) error {
		if err := s.store.CreateTenant(txCtx, tenant); err != nil {
			return err
		}
		if err := s.store.CreateUser(txCtx, user); err != nil {
			if isUniqueViolation(err) {
				return app.Conflict("email already registered")
			}
			return err
		}
		return s.store.CreateRefreshToken(txCtx, refresh)
	}); err != nil {
		return nil, err
	}
	return session, nil
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
	user, err := s.store.GetUser(ctx, rt.UserID)
	if err != nil || user == nil || !user.IsActive {
		return nil, app.Unauthorized("user not found or inactive")
	}
	if rt.RevokedAt != nil {
		s.logger.Warn().Str("user_id", rt.UserID.String()).Msg("refresh: revoked token reuse detected, revoking all user tokens")
		if err := s.revokeRefreshFamily(ctx, user.TenantID, rt, "already_revoked"); err != nil {
			return nil, err
		}
		return nil, app.Unauthorized("invalid or expired refresh token")
	}
	session, replacement, err := s.prepareSession(user)
	if err != nil {
		return nil, err
	}
	reused := false
	if err := app.WithinTransaction(ctx, s.store, func(txCtx context.Context) error {
		consumed, err := s.store.ConsumeRefreshToken(txCtx, rt.ID)
		if err != nil {
			return err
		}
		if !consumed {
			reused = true
			if err := s.store.RevokeUserRefreshTokens(txCtx, rt.UserID); err != nil {
				return err
			}
			return app.InsertAuditRequired(txCtx, s.store, models.AuditEntry{
				TenantID: app.UUIDPtr(user.TenantID), Actor: "user:" + user.ID.String(),
				Action: "session.refresh_reuse", ResourceType: "refresh_token", ResourceID: app.UUIDPtr(rt.ID),
				Details: app.MustJSON(map[string]any{"reason": "concurrent_or_replayed"}),
			})
		}
		if err := s.store.CreateRefreshToken(txCtx, replacement); err != nil {
			return err
		}
		return app.InsertAuditRequired(txCtx, s.store, models.AuditEntry{
			TenantID: app.UUIDPtr(user.TenantID), Actor: "user:" + user.ID.String(),
			Action: "session.refresh", ResourceType: "refresh_token", ResourceID: app.UUIDPtr(replacement.ID),
			Details: app.MustJSON(map[string]any{"rotated_from": rt.ID}),
		})
	}); err != nil {
		return nil, err
	}
	if reused {
		s.logger.Warn().Str("user_id", rt.UserID.String()).Msg("refresh: concurrent or replayed token use detected, revoked token family")
		return nil, app.Unauthorized("invalid or expired refresh token")
	}
	session.User = nil
	return session, nil
}

// Logout revokes the presented refresh token, or every token the caller holds
// when no specific token is given. The revocation and audit record share one
// transaction; a token owned by another user is deliberately ignored.
func (s *Service) Logout(ctx context.Context, refreshToken string, caller *models.User) error {
	if caller == nil {
		return app.Unauthorized("not logged in")
	}
	return app.WithinTransaction(ctx, s.store, func(txCtx context.Context) error {
		if refreshToken != "" {
			rt, err := s.store.GetRefreshToken(txCtx, authn.HashToken(refreshToken))
			if err != nil {
				return err
			}
			if rt != nil && rt.UserID == caller.ID {
				if err := s.store.RevokeRefreshToken(txCtx, rt.ID); err != nil {
					return err
				}
			}
		} else if err := s.store.RevokeUserRefreshTokens(txCtx, caller.ID); err != nil {
			return err
		}
		return app.InsertAuditRequired(txCtx, s.store, models.AuditEntry{
			TenantID: app.UUIDPtr(caller.TenantID), Actor: "user:" + caller.ID.String(),
			Action: "session.logout", ResourceType: "user", ResourceID: app.UUIDPtr(caller.ID),
		})
	})
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
	audit := models.AuditEntry{TenantID: app.UUIDPtr(caller.TenantID), Actor: "user:" + caller.ID.String(), Action: "user.password.change", ResourceType: "user", ResourceID: app.UUIDPtr(caller.ID)}
	event := hooks.Event{Type: "user.password.changed", TenantID: caller.TenantID.String(), OccurredAt: time.Now().UTC(), Metadata: map[string]any{"user_id": caller.ID.String()}}
	return app.CommitMutation(ctx, s.store, s.store, s.dispatcher, audit, &event, func(txCtx context.Context) error {
		if err := s.store.UpdateUserPassword(txCtx, caller.ID, string(hash)); err != nil {
			return err
		}
		return s.store.RevokeUserRefreshTokens(txCtx, caller.ID)
	})
}

// InviteAdmin mints an invitation code. Accepting it creates a super_admin, so
// the route is restricted to super admins.
func (s *Service) InviteAdmin(ctx context.Context, email string, inviter *models.User) (*models.AdminInvitation, error) {
	email = normalizeEmail(email)
	if email == "" {
		return nil, app.BadRequest("email is required")
	}
	if existing, err := s.store.GetUserByEmail(ctx, email); err != nil {
		return nil, app.Internal(err)
	} else if existing != nil {
		return nil, app.Conflict("email already registered")
	}
	code, err := generateInviteCode()
	if err != nil {
		return nil, app.Internal(err)
	}
	var inviterID *uuid.UUID
	actorLabel := "platform"
	if inviter != nil {
		id := inviter.ID
		inviterID = &id
		actorLabel = "user:" + inviter.ID.String()
	}
	inv := &models.AdminInvitation{ID: uuid.New(), Email: email, InviteCode: code, InvitedBy: inviterID, ExpiresAt: time.Now().Add(inviteTTL)}
	audit := models.AuditEntry{Actor: actorLabel, Action: "admin_invitation.create", ResourceType: "admin_invitation", ResourceID: app.UUIDPtr(inv.ID), Details: app.MustJSON(map[string]any{"email": email, "expires_at": inv.ExpiresAt})}
	event := hooks.Event{Type: "admin_invitation.created", OccurredAt: time.Now().UTC(), Metadata: map[string]any{"invitation_id": inv.ID.String(), "email": email, "expires_at": inv.ExpiresAt}}
	if err := app.CommitMutation(ctx, s.store, s.store, s.dispatcher, audit, &event, func(txCtx context.Context) error {
		return s.store.CreateAdminInvitation(txCtx, inv)
	}); err != nil {
		return nil, err
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
	if existing, err := s.store.GetUserByEmail(ctx, inv.Email); err != nil {
		return nil, app.Internal(err)
	} else if existing != nil {
		return nil, app.Conflict("email already registered")
	}

	tenant, user, session, refresh, err := s.prepareProvisionedSession(inv.Email, password, displayName, models.RoleSuperAdmin)
	if err != nil {
		return nil, err
	}
	audit := models.AuditEntry{TenantID: app.UUIDPtr(tenant.ID), Actor: "user:" + user.ID.String(), Action: "admin_invitation.accept", ResourceType: "admin_invitation", ResourceID: app.UUIDPtr(inv.ID), Details: app.MustJSON(map[string]any{"user_id": user.ID, "tenant_id": tenant.ID})}
	event := hooks.Event{Type: "admin_invitation.accepted", TenantID: tenant.ID.String(), OccurredAt: time.Now().UTC(), Metadata: map[string]any{"invitation_id": inv.ID.String(), "user_id": user.ID.String()}}
	if err := app.CommitMutation(ctx, s.store, s.store, s.dispatcher, audit, &event, func(txCtx context.Context) error {
		claimed, err := s.store.AcceptAdminInvitation(txCtx, inv.ID)
		if err != nil {
			return err
		}
		if !claimed {
			return app.BadRequest("invalid or expired invitation")
		}
		if err := s.store.CreateTenant(txCtx, tenant); err != nil {
			return err
		}
		if err := s.store.CreateUser(txCtx, user); err != nil {
			if isUniqueViolation(err) {
				return app.Conflict("email already registered")
			}
			return err
		}
		return s.store.CreateRefreshToken(txCtx, refresh)
	}); err != nil {
		return nil, err
	}
	return session, nil
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
	if !authz.CanManageTenantMember(actor, user.TenantID, user.Role) {
		return nil, app.Forbidden("tenant admins may manage ordinary members only")
	}
	beforeRole, beforeActive, beforeProfile := user.Role, user.IsActive, user.PermissionProfileID
	if req.Role != nil {
		newRole := models.UserRole(*req.Role)
		switch newRole {
		case models.RoleSuperAdmin, models.RoleAdmin, models.RoleUser:
		default:
			return nil, app.BadRequest("invalid role, must be super_admin, admin or user")
		}
		if !actor.IsSuperAdmin && newRole != models.RoleUser {
			return nil, app.Forbidden("tenant admins cannot assign administrator roles")
		}
		if actor.ID == user.ID && newRole != user.Role {
			return nil, app.Forbidden("cannot change your own role")
		}
		user.Role = newRole
	}
	if req.IsActive != nil {
		if actor.ID == user.ID && !*req.IsActive {
			return nil, app.Forbidden("cannot deactivate yourself")
		}
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
	audit := models.AuditEntry{TenantID: app.UUIDPtr(user.TenantID), Actor: actor.AuditLabel(), Action: "user.update", ResourceType: "user", ResourceID: app.UUIDPtr(user.ID), Details: app.MustJSON(map[string]any{"before_role": beforeRole, "role": user.Role, "before_active": beforeActive, "is_active": user.IsActive, "before_profile_id": beforeProfile, "permission_profile_id": user.PermissionProfileID})}
	event := hooks.Event{Type: "user.updated", TenantID: user.TenantID.String(), OccurredAt: time.Now().UTC(), Metadata: map[string]any{"user_id": user.ID.String(), "role": user.Role, "is_active": user.IsActive}}
	if err := app.CommitMutation(ctx, s.store, s.store, s.dispatcher, audit, &event, func(txCtx context.Context) error {
		return s.store.UpdateUser(txCtx, user)
	}); err != nil {
		return nil, err
	}
	return user, nil
}

func (s *Service) DeleteUser(ctx context.Context, actor authz.Actor, tenant *models.Tenant, userID uuid.UUID) error {
	if tenant == nil {
		return app.Forbidden("no tenant context")
	}
	if actor.Type != authz.PrincipalUser {
		return app.Forbidden("authenticated administrator required")
	}
	if actor.ID == userID {
		return app.BadRequest("cannot delete yourself")
	}
	user, err := s.store.GetUser(ctx, userID)
	if err != nil {
		return app.Internal(err)
	}
	if user == nil || user.TenantID != tenant.ID {
		return app.NotFound("user not found")
	}
	if !authz.CanManageTenantMember(actor, user.TenantID, user.Role) {
		return app.Forbidden("tenant admins may manage ordinary members only")
	}
	audit := models.AuditEntry{TenantID: app.UUIDPtr(user.TenantID), Actor: actor.AuditLabel(), Action: "user.delete", ResourceType: "user", ResourceID: app.UUIDPtr(user.ID), Details: app.MustJSON(map[string]any{"email": user.Email, "role": user.Role})}
	event := hooks.Event{Type: "user.deleted", TenantID: user.TenantID.String(), OccurredAt: time.Now().UTC(), Metadata: map[string]any{"user_id": user.ID.String(), "role": user.Role}}
	return app.CommitMutation(ctx, s.store, s.store, s.dispatcher, audit, &event, func(txCtx context.Context) error {
		return s.store.DeleteUser(txCtx, userID)
	})
}

func (s *Service) revokeRefreshFamily(ctx context.Context, tenantID uuid.UUID, rt *models.RefreshToken, reason string) error {
	return app.WithinTransaction(ctx, s.store, func(txCtx context.Context) error {
		if err := s.store.RevokeUserRefreshTokens(txCtx, rt.UserID); err != nil {
			return err
		}
		return app.InsertAuditRequired(txCtx, s.store, models.AuditEntry{
			TenantID: app.UUIDPtr(tenantID), Actor: "user:" + rt.UserID.String(), Action: "session.refresh_reuse",
			ResourceType: "refresh_token", ResourceID: app.UUIDPtr(rt.ID),
			Details: app.MustJSON(map[string]any{"reason": reason}),
		})
	})
}

// createUserWithTenant provisions the tenant a new account owns and the account
// itself. Registration and invitation acceptance differ only in the role.
func (s *Service) prepareProvisionedSession(email, password, displayName string, role models.UserRole) (*models.Tenant, *models.User, *Session, *models.RefreshToken, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, nil, nil, nil, app.Internal(err)
	}
	tenant := &models.Tenant{ID: uuid.New(), Name: email, PlanID: s.cfg.DefaultPlanID}
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		displayName = strings.Split(email, "@")[0]
	}
	user := &models.User{ID: uuid.New(), TenantID: tenant.ID, Email: email, PasswordHash: string(hash), DisplayName: displayName, Role: role, IsActive: true}
	session, refresh, err := s.prepareSession(user)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	return tenant, user, session, refresh, nil
}

func (s *Service) prepareSession(user *models.User) (*Session, *models.RefreshToken, error) {
	accessToken, err := authn.IssueAccessToken(s.cfg.JWTSecret, user)
	if err != nil {
		return nil, nil, app.Internal(err)
	}
	rawRefresh, refreshHash, err := authn.GenerateRefreshToken()
	if err != nil {
		return nil, nil, app.Internal(err)
	}
	refresh := &models.RefreshToken{ID: uuid.New(), UserID: user.ID, TokenHash: refreshHash, ExpiresAt: time.Now().Add(authn.RefreshTokenTTL)}
	return &Session{AccessToken: accessToken, RefreshToken: rawRefresh, ExpiresIn: int(authn.AccessTokenTTL.Seconds()), User: user}, refresh, nil
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

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "duplicate") || strings.Contains(message, "unique") || strings.Contains(message, "23505")
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
