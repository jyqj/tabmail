package authapp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"golang.org/x/crypto/bcrypt"
	"tabmail/internal/app"
	"tabmail/internal/authn"
	"tabmail/internal/authz"
	"tabmail/internal/models"
)

type authTestStore struct {
	users         map[uuid.UUID]*models.User
	tenants       map[uuid.UUID]*models.Tenant
	refreshTokens map[string]*models.RefreshToken
	profiles      map[uuid.UUID]*models.PermissionProfile
	invitations   map[string]*models.AdminInvitation
	revokedAll    []uuid.UUID
	touched       []uuid.UUID
}

func newAuthTestStore() *authTestStore {
	return &authTestStore{
		users:         map[uuid.UUID]*models.User{},
		tenants:       map[uuid.UUID]*models.Tenant{},
		refreshTokens: map[string]*models.RefreshToken{},
		profiles:      map[uuid.UUID]*models.PermissionProfile{},
		invitations:   map[string]*models.AdminInvitation{},
	}
}

func (s *authTestStore) addUser(u *models.User) *models.User {
	if u.ID == uuid.Nil {
		u.ID = uuid.New()
	}
	s.users[u.ID] = u
	return u
}

func (s *authTestStore) GetUserByEmail(_ context.Context, email string) (*models.User, error) {
	for _, u := range s.users {
		if u.Email == email {
			cp := *u
			return &cp, nil
		}
	}
	return nil, nil
}

func (s *authTestStore) CreateUser(_ context.Context, u *models.User) error {
	if u.ID == uuid.Nil {
		u.ID = uuid.New()
	}
	cp := *u
	s.users[cp.ID] = &cp
	return nil
}

func (s *authTestStore) GetUser(_ context.Context, id uuid.UUID) (*models.User, error) {
	u, found := s.users[id]
	if !found {
		return nil, nil
	}
	cp := *u
	return &cp, nil
}

func (s *authTestStore) UpdateUser(_ context.Context, u *models.User) error {
	if _, found := s.users[u.ID]; !found {
		return errors.New("user not found")
	}
	cp := *u
	s.users[cp.ID] = &cp
	return nil
}

func (s *authTestStore) UpdateUserPassword(_ context.Context, id uuid.UUID, hash string) error {
	u, found := s.users[id]
	if !found {
		return errors.New("user not found")
	}
	u.PasswordHash = hash
	return nil
}

func (s *authTestStore) ListUsers(_ context.Context, tenantID uuid.UUID, _ models.Page) ([]*models.User, int, error) {
	var out []*models.User
	for _, u := range s.users {
		if u.TenantID == tenantID {
			cp := *u
			out = append(out, &cp)
		}
	}
	return out, len(out), nil
}

func (s *authTestStore) DeleteUser(_ context.Context, id uuid.UUID) error {
	delete(s.users, id)
	return nil
}

func (s *authTestStore) TouchUserLogin(_ context.Context, id uuid.UUID) error {
	s.touched = append(s.touched, id)
	return nil
}

func (s *authTestStore) CreateRefreshToken(_ context.Context, rt *models.RefreshToken) error {
	if rt.ID == uuid.Nil {
		rt.ID = uuid.New()
	}
	cp := *rt
	s.refreshTokens[cp.TokenHash] = &cp
	return nil
}

func (s *authTestStore) GetRefreshToken(_ context.Context, hash string) (*models.RefreshToken, error) {
	rt, found := s.refreshTokens[hash]
	if !found {
		return nil, nil
	}
	cp := *rt
	return &cp, nil
}

func (s *authTestStore) RevokeRefreshToken(_ context.Context, id uuid.UUID) error {
	for _, rt := range s.refreshTokens {
		if rt.ID == id {
			now := time.Now()
			rt.RevokedAt = &now
		}
	}
	return nil
}

func (s *authTestStore) RevokeUserRefreshTokens(_ context.Context, userID uuid.UUID) error {
	s.revokedAll = append(s.revokedAll, userID)
	for _, rt := range s.refreshTokens {
		if rt.UserID == userID {
			now := time.Now()
			rt.RevokedAt = &now
		}
	}
	return nil
}

func (s *authTestStore) CreateTenant(_ context.Context, t *models.Tenant) error {
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}
	cp := *t
	s.tenants[cp.ID] = &cp
	return nil
}

func (s *authTestStore) GetTenant(_ context.Context, id uuid.UUID) (*models.Tenant, error) {
	t, found := s.tenants[id]
	if !found {
		return nil, nil
	}
	cp := *t
	return &cp, nil
}

func (s *authTestStore) CreateAdminInvitation(_ context.Context, inv *models.AdminInvitation) error {
	if inv.ID == uuid.Nil {
		inv.ID = uuid.New()
	}
	cp := *inv
	s.invitations[cp.InviteCode] = &cp
	return nil
}

func (s *authTestStore) GetAdminInvitationByCode(_ context.Context, code string) (*models.AdminInvitation, error) {
	inv, found := s.invitations[code]
	if !found {
		return nil, nil
	}
	cp := *inv
	return &cp, nil
}

func (s *authTestStore) MarkInvitationAccepted(_ context.Context, id uuid.UUID) error {
	for _, inv := range s.invitations {
		if inv.ID == id {
			now := time.Now()
			inv.AcceptedAt = &now
		}
	}
	return nil
}

func (s *authTestStore) InsertAudit(context.Context, *models.AuditEntry) error { return nil }

func (s *authTestStore) GetPermissionProfile(_ context.Context, id uuid.UUID) (*models.PermissionProfile, error) {
	p, found := s.profiles[id]
	if !found {
		return nil, nil
	}
	cp := *p
	return &cp, nil
}

type fakeThrottle struct {
	exceeded bool
	failures []string
	err      error
}

func (f *fakeThrottle) LoginAttemptsExceeded(_ context.Context, _ string) bool { return f.exceeded }

func (f *fakeThrottle) RecordLoginFailure(_ context.Context, identity string) error {
	f.failures = append(f.failures, identity)
	return f.err
}

func newTestService(t *testing.T, s storeRepo, cfg Config) *Service {
	t.Helper()
	if cfg.JWTSecret == "" {
		cfg.JWTSecret = "test-secret"
	}
	return NewService(s, cfg, zerolog.Nop())
}

func seedUser(t *testing.T, store *authTestStore, password string) *models.User {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	return store.addUser(&models.User{
		TenantID:     uuid.New(),
		Email:        "user@example.com",
		PasswordHash: string(hash),
		DisplayName:  "User",
		Role:         models.RoleUser,
		IsActive:     true,
	})
}

func requireKind(t *testing.T, err error, want app.ErrorKind) {
	t.Helper()
	appErr, found := app.As(err)
	if !found {
		t.Fatalf("expected an application error, got %v", err)
	}
	if appErr.Kind != want {
		t.Fatalf("expected kind %q, got %q (%s)", want, appErr.Kind, appErr.Message)
	}
}

func TestLoginIssuesSessionAndRecordsLastLogin(t *testing.T) {
	store := newAuthTestStore()
	user := seedUser(t, store, "correct-horse")
	svc := newTestService(t, store, Config{})

	session, err := svc.Login(context.Background(), "  User@Example.com ", "correct-horse")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if session.AccessToken == "" || session.RefreshToken == "" {
		t.Fatalf("expected a token pair, got %#v", session)
	}
	if session.User == nil || session.User.ID != user.ID {
		t.Fatalf("expected the session to carry the user, got %#v", session.User)
	}
	if len(store.touched) != 1 || store.touched[0] != user.ID {
		t.Fatalf("expected the last login to be recorded, got %v", store.touched)
	}
}

func TestLoginRejectsBadPasswordAndCountsIt(t *testing.T) {
	store := newAuthTestStore()
	seedUser(t, store, "correct-horse")
	throttle := &fakeThrottle{}
	svc := newTestService(t, store, Config{Throttle: throttle})

	_, err := svc.Login(context.Background(), "user@example.com", "wrong")
	requireKind(t, err, app.KindUnauthorized)
	if len(throttle.failures) != 1 {
		t.Fatalf("expected the failure to be counted, got %v", throttle.failures)
	}
}

func TestLoginRejectsUnknownEmailWithoutLeakingIt(t *testing.T) {
	store := newAuthTestStore()
	throttle := &fakeThrottle{}
	svc := newTestService(t, store, Config{Throttle: throttle})

	_, err := svc.Login(context.Background(), "nobody@example.com", "whatever")
	requireKind(t, err, app.KindUnauthorized)
	if err.Error() != "invalid email or password" {
		t.Fatalf("expected the same message as a wrong password, got %q", err.Error())
	}
	if len(throttle.failures) != 1 {
		t.Fatalf("expected the failure to be counted, got %v", throttle.failures)
	}
}

func TestLoginRefusesWhenThrottled(t *testing.T) {
	store := newAuthTestStore()
	seedUser(t, store, "correct-horse")
	svc := newTestService(t, store, Config{Throttle: &fakeThrottle{exceeded: true}})

	_, err := svc.Login(context.Background(), "user@example.com", "correct-horse")
	requireKind(t, err, app.KindRateLimited)
}

func TestLoginRefusesDisabledAccount(t *testing.T) {
	store := newAuthTestStore()
	user := seedUser(t, store, "correct-horse")
	user.IsActive = false
	svc := newTestService(t, store, Config{})

	_, err := svc.Login(context.Background(), "user@example.com", "correct-horse")
	requireKind(t, err, app.KindForbidden)
}

func TestRefreshRotatesTheTokenAndRejectsReuse(t *testing.T) {
	store := newAuthTestStore()
	user := seedUser(t, store, "correct-horse")
	svc := newTestService(t, store, Config{})

	first, err := svc.Login(context.Background(), "user@example.com", "correct-horse")
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	second, err := svc.Refresh(context.Background(), first.RefreshToken)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if second.RefreshToken == first.RefreshToken {
		t.Fatal("expected the refresh token to rotate")
	}
	if second.User != nil {
		t.Fatalf("expected refresh not to restate the identity, got %#v", second.User)
	}

	// Replaying the rotated-out token means it leaked; every session dies.
	_, err = svc.Refresh(context.Background(), first.RefreshToken)
	requireKind(t, err, app.KindUnauthorized)
	if len(store.revokedAll) != 1 || store.revokedAll[0] != user.ID {
		t.Fatalf("expected all tokens for the user to be revoked, got %v", store.revokedAll)
	}
}

func TestRefreshRejectsExpiredToken(t *testing.T) {
	store := newAuthTestStore()
	user := seedUser(t, store, "correct-horse")
	raw, hash, err := authn.GenerateRefreshToken()
	if err != nil {
		t.Fatalf("generate refresh token: %v", err)
	}
	store.refreshTokens[hash] = &models.RefreshToken{
		ID: uuid.New(), UserID: user.ID, TokenHash: hash,
		ExpiresAt: time.Now().Add(-time.Hour),
	}
	svc := newTestService(t, store, Config{})

	_, err = svc.Refresh(context.Background(), raw)
	requireKind(t, err, app.KindUnauthorized)
}

type closedRegistration struct{}

func (closedRegistration) GetBool(context.Context, string, bool) bool { return false }

func TestRegisterHonoursTheRuntimeSetting(t *testing.T) {
	store := newAuthTestStore()
	svc := newTestService(t, store, Config{OpenRegistration: true, Settings: closedRegistration{}})

	_, err := svc.Register(context.Background(), "new@example.com", "longenough", "")
	requireKind(t, err, app.KindForbidden)
}

func TestRegisterCreatesAnOwnedTenant(t *testing.T) {
	store := newAuthTestStore()
	svc := newTestService(t, store, Config{OpenRegistration: true})

	session, err := svc.Register(context.Background(), "New@Example.com", "longenough", "")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if session.User.Email != "new@example.com" {
		t.Fatalf("expected the email to be normalized, got %q", session.User.Email)
	}
	if session.User.DisplayName != "new" {
		t.Fatalf("expected the display name to default to the local part, got %q", session.User.DisplayName)
	}
	if _, found := store.tenants[session.User.TenantID]; !found {
		t.Fatal("expected the new user to own a tenant")
	}
	if session.User.Role != models.RoleUser {
		t.Fatalf("expected a plain user role, got %q", session.User.Role)
	}
}

func TestRegisterRejectsShortPassword(t *testing.T) {
	store := newAuthTestStore()
	svc := newTestService(t, store, Config{OpenRegistration: true})

	_, err := svc.Register(context.Background(), "new@example.com", "short", "")
	requireKind(t, err, app.KindBadRequest)
}

func TestRegisterRejectsDuplicateEmail(t *testing.T) {
	store := newAuthTestStore()
	seedUser(t, store, "correct-horse")
	svc := newTestService(t, store, Config{OpenRegistration: true})

	_, err := svc.Register(context.Background(), "user@example.com", "longenough", "")
	requireKind(t, err, app.KindConflict)
}

func TestUpdateUserOnlySuperAdminGrantsSuperAdmin(t *testing.T) {
	store := newAuthTestStore()
	user := seedUser(t, store, "correct-horse")
	tenant := &models.Tenant{ID: user.TenantID}
	svc := newTestService(t, store, Config{})
	superAdmin := "super_admin"

	admin := authz.Actor{Type: authz.PrincipalUser, ID: uuid.New(), TenantID: tenant.ID, Role: models.RoleAdmin, IsAdmin: true}
	_, err := svc.UpdateUser(context.Background(), admin, tenant, user.ID, UpdateUserRequest{Role: &superAdmin})
	requireKind(t, err, app.KindForbidden)

	root := authz.Actor{Type: authz.PrincipalUser, ID: uuid.New(), TenantID: tenant.ID, Role: models.RoleSuperAdmin, IsSuperAdmin: true}
	updated, err := svc.UpdateUser(context.Background(), root, tenant, user.ID, UpdateUserRequest{Role: &superAdmin})
	if err != nil {
		t.Fatalf("update user: %v", err)
	}
	if updated.Role != models.RoleSuperAdmin {
		t.Fatalf("expected the role to be promoted, got %q", updated.Role)
	}
}

func TestUpdateUserClearsProfileOnlyWhenExplicit(t *testing.T) {
	store := newAuthTestStore()
	user := seedUser(t, store, "correct-horse")
	profileID := uuid.New()
	user.PermissionProfileID = &profileID
	tenant := &models.Tenant{ID: user.TenantID}
	actor := authz.Actor{Type: authz.PrincipalUser, ID: uuid.New(), TenantID: tenant.ID, Role: models.RoleAdmin, IsAdmin: true}
	svc := newTestService(t, store, Config{})

	name := "Renamed"
	untouched, err := svc.UpdateUser(context.Background(), actor, tenant, user.ID, UpdateUserRequest{DisplayName: &name})
	if err != nil {
		t.Fatalf("update user: %v", err)
	}
	if untouched.PermissionProfileID == nil {
		t.Fatal("expected an omitted profile field to leave the profile alone")
	}

	cleared, err := svc.UpdateUser(context.Background(), actor, tenant, user.ID, UpdateUserRequest{ProfileSet: true})
	if err != nil {
		t.Fatalf("update user: %v", err)
	}
	if cleared.PermissionProfileID != nil {
		t.Fatalf("expected an explicit null to clear the profile, got %v", cleared.PermissionProfileID)
	}
}

func TestUpdateUserRejectsProfileFromAnotherTenant(t *testing.T) {
	store := newAuthTestStore()
	user := seedUser(t, store, "correct-horse")
	otherTenant := uuid.New()
	profile := &models.PermissionProfile{ID: uuid.New(), TenantID: &otherTenant, Name: "other"}
	store.profiles[profile.ID] = profile
	tenant := &models.Tenant{ID: user.TenantID}
	actor := authz.Actor{Type: authz.PrincipalUser, ID: uuid.New(), TenantID: tenant.ID, Role: models.RoleAdmin, IsAdmin: true}
	svc := newTestService(t, store, Config{})

	_, err := svc.UpdateUser(context.Background(), actor, tenant, user.ID, UpdateUserRequest{ProfileSet: true, ProfileID: &profile.ID})
	requireKind(t, err, app.KindForbidden)
}

func TestDeleteUserRefusesSelfDeletion(t *testing.T) {
	store := newAuthTestStore()
	user := seedUser(t, store, "correct-horse")
	tenant := &models.Tenant{ID: user.TenantID}
	svc := newTestService(t, store, Config{})

	err := svc.DeleteUser(context.Background(), tenant, user, user.ID)
	requireKind(t, err, app.KindBadRequest)
	if _, found := store.users[user.ID]; !found {
		t.Fatal("expected the user to survive")
	}
}

func TestChangePasswordRevokesEverySession(t *testing.T) {
	store := newAuthTestStore()
	user := seedUser(t, store, "correct-horse")
	svc := newTestService(t, store, Config{})

	if err := svc.ChangePassword(context.Background(), user, "wrong", "longenough"); err == nil {
		t.Fatal("expected the wrong old password to be rejected")
	} else {
		requireKind(t, err, app.KindUnauthorized)
	}

	if err := svc.ChangePassword(context.Background(), user, "correct-horse", "longenough"); err != nil {
		t.Fatalf("change password: %v", err)
	}
	if len(store.revokedAll) != 1 || store.revokedAll[0] != user.ID {
		t.Fatalf("expected every session to be revoked, got %v", store.revokedAll)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(store.users[user.ID].PasswordHash), []byte("longenough")); err != nil {
		t.Fatalf("expected the new password to be stored: %v", err)
	}
}

func TestAcceptInviteCreatesSuperAdmin(t *testing.T) {
	store := newAuthTestStore()
	svc := newTestService(t, store, Config{})

	inv, err := svc.InviteAdmin(context.Background(), " Root@Example.com ", nil)
	if err != nil {
		t.Fatalf("invite: %v", err)
	}
	if inv.Email != "root@example.com" {
		t.Fatalf("expected the email to be normalized, got %q", inv.Email)
	}

	session, err := svc.AcceptInvite(context.Background(), inv.InviteCode, "longenough", "Root")
	if err != nil {
		t.Fatalf("accept invite: %v", err)
	}
	if session.User.Role != models.RoleSuperAdmin {
		t.Fatalf("expected a super admin, got %q", session.User.Role)
	}
	if store.invitations[inv.InviteCode].AcceptedAt == nil {
		t.Fatal("expected the invitation to be marked accepted")
	}

	_, err = svc.AcceptInvite(context.Background(), inv.InviteCode, "longenough", "Root")
	requireKind(t, err, app.KindBadRequest)
}
