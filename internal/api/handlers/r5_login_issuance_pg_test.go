package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"golang.org/x/crypto/bcrypt"
	"tabmail/internal/api/handlers"
	"tabmail/internal/authn"
	"tabmail/internal/authz"
	"tabmail/internal/models"
	"tabmail/internal/store/postgres"
	"tabmail/internal/testpg"
)

const r5LoginIssuancePassword = "synthetic-login-before-change"
const r5LoginIssuanceSecret = "synthetic-login-issuance-jwt"

type r5LoginIssuanceFixture struct {
	st       *postgres.PgStore
	pool     *pgxpool.Pool
	tenant   *models.Tenant
	user     *models.User
	admin    authz.Actor
	password string
}

func r5LoginIssuanceSeed(t *testing.T) *r5LoginIssuanceFixture {
	t.Helper()
	if os.Getenv("TABMAIL_TEST_DB_DSN") == "" {
		t.Fatal("owned PostgreSQL DSN is required for login issuance regression")
	}
	st, pool, _ := testpg.NewPostgres(t)
	ctx := t.Context()
	f := &r5LoginIssuanceFixture{st: st, pool: pool, password: r5LoginIssuancePassword}
	f.tenant = &models.Tenant{Name: "Synthetic login issuer", PlanID: uuid.MustParse("00000000-0000-0000-0000-000000000002")}
	if err := st.CreateTenant(ctx, f.tenant); err != nil {
		t.Fatal(err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(f.password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	f.user = &models.User{TenantID: f.tenant.ID, Email: "login-issuer@fixture.test", DisplayName: "Synthetic employee", Role: models.RoleUser, IsActive: true, PasswordHash: string(hash)}
	if err := st.CreateUser(ctx, f.user); err != nil {
		t.Fatal(err)
	}
	admin := &models.User{TenantID: f.tenant.ID, Email: "login-admin@fixture.test", DisplayName: "Synthetic administrator", Role: models.RoleAdmin, IsActive: true, PasswordHash: string(hash)}
	if err := st.CreateUser(ctx, admin); err != nil {
		t.Fatal(err)
	}
	f.admin = authz.Actor{Type: authz.PrincipalUser, ID: admin.ID, TenantID: f.tenant.ID, Role: models.RoleAdmin, IsAdmin: true}
	return f
}

// Only the boundary after the real account read is paused. Authentication,
// bcrypt, session issuance, all account commands and rotation stay production.
type r5LoginReadGate struct {
	*postgres.PgStore
	read, resume chan struct{}
}

func (s *r5LoginReadGate) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	u, err := s.PgStore.GetUserByEmail(ctx, email)
	if err != nil {
		return nil, err
	}
	close(s.read)
	select {
	case <-s.resume:
		return u, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func r5LoginIssuanceHandler(t *testing.T, f *r5LoginIssuanceFixture, gate *r5LoginReadGate) *handlers.AuthHandler {
	t.Helper()
	if gate == nil {
		gate = &r5LoginReadGate{PgStore: f.st, read: make(chan struct{}), resume: make(chan struct{})}
		close(gate.resume)
	}
	h := handlers.NewAuthHandler(gate, r5LoginIssuanceSecret, uuid.Nil, false, nil, false, zerolog.Nop())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := h.StopContext(ctx); err != nil {
			t.Errorf("login background cleanup: %v", err)
		}
	})
	return h
}

func r5LoginIssuanceRequest(f *r5LoginIssuanceFixture) *http.Request {
	body, _ := json.Marshal(map[string]string{"email": f.user.Email, "password": f.password})
	return httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(string(body)))
}

func r5LoginIssuanceWait(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("login issuance barrier not reached")
	}
}

func r5LoginIssuanceCount(t *testing.T, f *r5LoginIssuanceFixture) int {
	t.Helper()
	var count int
	if err := f.pool.QueryRow(t.Context(), `SELECT count(*) FROM refresh_tokens WHERE user_id=$1`, f.user.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func r5LoginIssuanceRefresh(h *handlers.AuthHandler, token string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	r.AddCookie(&http.Cookie{Name: handlers.RefreshCookieName, Value: token})
	w := httptest.NewRecorder()
	h.Refresh(w, r)
	return w
}

func r5LoginIssuanceCookie(w *httptest.ResponseRecorder) string {
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == handlers.RefreshCookieName && cookie.MaxAge > 0 {
			return cookie.Value
		}
	}
	return ""
}

func TestR5LoginIssuancePostgresRejectsChangedAuthentication(t *testing.T) {
	for _, change := range []string{"password", "freeze", "freeze_unfreeze", "role_change", "deleted_user"} {
		t.Run(change, func(t *testing.T) {
			f := r5LoginIssuanceSeed(t)
			gate := &r5LoginReadGate{PgStore: f.st, read: make(chan struct{}), resume: make(chan struct{})}
			var once sync.Once
			release := func() { once.Do(func() { close(gate.resume) }) }
			h := r5LoginIssuanceHandler(t, f, gate)
			defer release()
			w := httptest.NewRecorder()
			done := make(chan struct{})
			go func() { defer close(done); h.Login(w, r5LoginIssuanceRequest(f)) }()
			r5LoginIssuanceWait(t, gate.read)
			ctx := t.Context()
			switch change {
			case "password":
				if err := f.st.ChangePasswordAtomic(ctx, f.user.ID, f.user.PasswordHash, "synthetic-replaced-hash"); err != nil {
					t.Fatal(err)
				}
			case "freeze", "freeze_unfreeze":
				active := false
				if _, err := f.st.UpdateUserGuarded(ctx, f.admin, f.tenant.ID, f.user.ID, models.UserAdminPatch{IsActive: &active}); err != nil {
					t.Fatal(err)
				}
				if change == "freeze_unfreeze" {
					active = true
					if _, err := f.st.UpdateUserGuarded(ctx, f.admin, f.tenant.ID, f.user.ID, models.UserAdminPatch{IsActive: &active}); err != nil {
						t.Fatal(err)
					}
				}
			case "role_change":
				role := models.RoleAdmin
				if _, err := f.st.UpdateUserGuarded(ctx, f.admin, f.tenant.ID, f.user.ID, models.UserAdminPatch{Role: &role}); err != nil {
					t.Fatal(err)
				}
			case "deleted_user":
				if err := f.st.DeleteUserGuarded(ctx, f.admin, f.tenant.ID, f.user.ID); err != nil {
					t.Fatal(err)
				}
			}
			release()
			r5LoginIssuanceWait(t, done)
			// Join the real background touch before observing durable effects.
			stopCtx, stop := context.WithTimeout(context.Background(), 3*time.Second)
			defer stop()
			if err := h.StopContext(stopCtx); err != nil {
				t.Fatal(err)
			}
			// In the old implementation a password change or freeze/unfreeze
			// can leave a fresh usable refresh family after revocation finished.
			if cookie := r5LoginIssuanceCookie(w); cookie != "" {
				rotated := r5LoginIssuanceRefresh(h, cookie)
				t.Errorf("changed authentication issued a cookie; real refresh status=%d", rotated.Code)
			}
			if w.Code != http.StatusUnauthorized {
				t.Errorf("changed authentication status=%d want=401", w.Code)
			}
			if got := r5LoginIssuanceCount(t, f); got != 0 {
				t.Errorf("changed authentication created %d token rows", got)
			}
			if len(w.Result().Cookies()) != 0 {
				t.Error("rejected login changed browser cookies")
			}
			var lastLogin *time.Time
			if change != "deleted_user" {
				if err := f.pool.QueryRow(ctx, `SELECT last_login_at FROM users WHERE id=$1`, f.user.ID).Scan(&lastLogin); err != nil {
					t.Fatal(err)
				}
				if lastLogin != nil {
					t.Error("rejected login scheduled a login touch")
				}
			}
		})
	}
}

func TestR5LoginIssuancePostgresCurrentSessionAndRevocation(t *testing.T) {
	f := r5LoginIssuanceSeed(t)
	h := r5LoginIssuanceHandler(t, f, nil)
	w := httptest.NewRecorder()
	h.Login(w, r5LoginIssuanceRequest(f))
	if w.Code != http.StatusOK || r5LoginIssuanceCookie(w) == "" || r5LoginIssuanceCount(t, f) != 1 {
		t.Fatalf("current login failed: status=%d", w.Code)
	}
	var response struct {
		Data struct {
			AccessToken string `json:"access_token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	claims, err := authn.VerifyAccessToken(r5LoginIssuanceSecret, response.Data.AccessToken)
	if err != nil || claims.UserID != f.user.ID || claims.SessionVersion != f.user.SessionVersion {
		t.Fatal("current login signed the wrong user/session")
	}
	rotated := r5LoginIssuanceRefresh(h, r5LoginIssuanceCookie(w))
	if rotated.Code != http.StatusOK || r5LoginIssuanceCookie(rotated) == "" {
		t.Fatalf("current refresh failed: status=%d", rotated.Code)
	}
	if err := f.st.ChangePasswordAtomic(t.Context(), f.user.ID, f.user.PasswordHash, "synthetic-after-login-hash"); err != nil {
		t.Fatal(err)
	}
	if got := r5LoginIssuanceRefresh(h, r5LoginIssuanceCookie(rotated)); got.Code != http.StatusUnauthorized {
		t.Errorf("credential change did not revoke already-issued family: status=%d", got.Code)
	}
}

func r5LoginIssuanceBlocked(t *testing.T, f *r5LoginIssuanceFixture, blocker uint32, fragment string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		var found bool
		err := f.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND $1::int=ANY(pg_blocking_pids(pid)) AND query LIKE '%' || $2 || '%')`, blocker, fragment).Scan(&found)
		if err != nil {
			t.Fatal(err)
		}
		if found {
			return
		}
		select {
		case <-tick.C:
		case <-ctx.Done():
			t.Fatal("real login issuer did not reach the expected database lock")
		}
	}
}

func TestR5LoginIssuancePostgresOwnsUserUntilTokenInsert(t *testing.T) {
	f := r5LoginIssuanceSeed(t)
	h := r5LoginIssuanceHandler(t, f, nil)
	ctx := t.Context()
	hold, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer hold.Rollback(context.Background())
	if _, err := hold.Exec(ctx, `LOCK TABLE refresh_tokens IN SHARE MODE`); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { defer close(done); h.Login(w, r5LoginIssuanceRequest(f)) }()
	r5LoginIssuanceBlocked(t, f, hold.Conn().PgConn().PID(), "INSERT INTO refresh_tokens")
	probe, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer probe.Rollback(context.Background())
	_, err = probe.Exec(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE NOWAIT`, f.user.ID)
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "55P03" {
		t.Errorf("login issuer did not protect authenticated user before token insertion: SQLSTATE=%s", r5LoginIssuanceSQLState(err))
	}
	if err := probe.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err := hold.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	r5LoginIssuanceWait(t, done)
	if w.Code != http.StatusOK || r5LoginIssuanceCount(t, f) != 1 {
		t.Errorf("successful issuer lost its normal result: status=%d", w.Code)
	}
}

func r5LoginIssuanceSQLState(err error) string {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return pg.Code
	}
	if err == nil {
		return "none"
	}
	return "non-PG-error"
}

func TestR5LoginIssuancePostgresInsertFailureHasNoCookie(t *testing.T) {
	f := r5LoginIssuanceSeed(t)
	if _, err := f.pool.Exec(t.Context(), `ALTER TABLE refresh_tokens ADD CONSTRAINT r5_login_reject_insert CHECK(false) NOT VALID`); err != nil {
		t.Fatal(err)
	}
	h := r5LoginIssuanceHandler(t, f, nil)
	w := httptest.NewRecorder()
	h.Login(w, r5LoginIssuanceRequest(f))
	if w.Code != http.StatusInternalServerError || len(w.Result().Cookies()) != 0 || r5LoginIssuanceCount(t, f) != 0 {
		t.Fatalf("failed token insert escaped: status=%d", w.Code)
	}
	if strings.Contains(w.Body.String(), "r5_login_reject_insert") {
		t.Error("database failure leaked to authentication response")
	}
}

func TestR5LoginIssuancePostgresCancellationHasNoCookie(t *testing.T) {
	f := r5LoginIssuanceSeed(t)
	gate := &r5LoginReadGate{PgStore: f.st, read: make(chan struct{}), resume: make(chan struct{})}
	h := r5LoginIssuanceHandler(t, f, gate)
	w := httptest.NewRecorder()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); h.Login(w, r5LoginIssuanceRequest(f).WithContext(ctx)) }()
	r5LoginIssuanceWait(t, gate.read)
	cancel()
	r5LoginIssuanceWait(t, done)
	if w.Code != http.StatusInternalServerError || len(w.Result().Cookies()) != 0 || r5LoginIssuanceCount(t, f) != 0 {
		t.Errorf("cancelled login produced session effects: status=%d", w.Code)
	}
}
