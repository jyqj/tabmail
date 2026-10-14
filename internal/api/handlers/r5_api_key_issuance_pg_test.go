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

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"tabmail/internal/api/handlers"
	"tabmail/internal/api/middleware"
	"tabmail/internal/authn"
	"tabmail/internal/authz"
	"tabmail/internal/models"
	"tabmail/internal/store/postgres"
	"tabmail/internal/testpg"
)

const advanceAPIKeySecret = "synthetic-api-key-issuance-jwt"

type advanceAPIKeyFixture struct {
	st      *postgres.PgStore
	pool    *pgxpool.Pool
	tenant  *models.Tenant
	user    *models.User
	manager authz.Actor
	profile *models.PermissionProfile
	zone    *models.DomainZone
	global  bool
}

func advanceAPIKeySeed(t *testing.T, role models.UserRole, crossHome bool) *advanceAPIKeyFixture {
	t.Helper()
	st, pool, _ := testpg.NewPostgres(t)
	f := &advanceAPIKeyFixture{st: st, pool: pool, global: crossHome}
	f.tenant = &models.Tenant{Name: "Synthetic key company", PlanID: uuid.MustParse("00000000-0000-0000-0000-000000000002")}
	if err := st.CreateTenant(t.Context(), f.tenant); err != nil {
		t.Fatal(err)
	}
	home := f.tenant.ID
	if crossHome {
		homeTenant := &models.Tenant{Name: "Synthetic issuer home", PlanID: f.tenant.PlanID}
		if err := st.CreateTenant(t.Context(), homeTenant); err != nil {
			t.Fatal(err)
		}
		home = homeTenant.ID
	}
	f.profile = &models.PermissionProfile{TenantID: &home, Name: "Synthetic key rights", CanSend: true, CanCreateAPIKeys: true}
	if err := st.CreatePermissionProfile(t.Context(), f.profile); err != nil {
		t.Fatal(err)
	}
	f.user = &models.User{TenantID: home, Email: "issuer@fixture.test", DisplayName: "Synthetic issuer", Role: role, IsActive: true, PasswordHash: "synthetic-unused-password-hash", PermissionProfileID: &f.profile.ID}
	if err := st.CreateUser(t.Context(), f.user); err != nil {
		t.Fatal(err)
	}
	manager := &models.User{TenantID: home, Email: "manager@fixture.test", DisplayName: "Synthetic manager", Role: models.RoleSuperAdmin, IsActive: true, PasswordHash: "synthetic-unused-password-hash"}
	if err := st.CreateUser(t.Context(), manager); err != nil {
		t.Fatal(err)
	}
	f.manager = authz.Actor{Type: authz.PrincipalUser, ID: manager.ID, TenantID: home, Role: models.RoleSuperAdmin, IsSuperAdmin: true}
	f.zone = &models.DomainZone{TenantID: f.tenant.ID, Domain: "keys.fixture.test", IsVerified: true, MXVerified: true}
	if err := st.CreateZone(t.Context(), f.zone); err != nil {
		t.Fatal(err)
	}
	return f
}

type advanceAPIKeyGate struct {
	entered, resume chan struct{}
	once            sync.Once
}

func (g *advanceAPIKeyGate) release() { g.once.Do(func() { close(g.resume) }) }

func (f *advanceAPIKeyFixture) router(t *testing.T, gate *advanceAPIKeyGate) http.Handler {
	t.Helper()
	state := middleware.NewAuthState(nil)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := state.StopContext(ctx); err != nil {
			t.Errorf("owned key authentication cleanup: %v", err)
		}
	})
	h := handlers.NewAdminHandler(f.st, nil, models.SMTPPolicy{}, nil, nil, zerolog.Nop())
	r := chi.NewRouter()
	r.Use(middleware.Auth(f.st, advanceAPIKeySecret, f.tenant.ID.String(), state))
	r.Use(middleware.PermissionLoader(f.st))
	if gate != nil {
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Pause after real JWT/account/permission loading, before the
				// unmodified production handler and PostgreSQL command execute.
				close(gate.entered)
				select {
				case <-gate.resume:
				case <-r.Context().Done():
				}
				next.ServeHTTP(w, r)
			})
		})
	}
	r.With(middleware.RequireAuth).Post("/api/v1/keys", h.UserCreateAPIKey)
	r.With(middleware.RequireSuperAdmin).Post("/api/v1/admin/tenants/{id}/keys", h.CreateAPIKey)
	return r
}

func (f *advanceAPIKeyFixture) request(t *testing.T, ctx context.Context) *http.Request {
	t.Helper()
	path := "/api/v1/keys"
	if f.global {
		path = "/api/v1/admin/tenants/" + f.tenant.ID.String() + "/keys"
	}
	raw, err := json.Marshal(map[string]any{"label": "Synthetic key", "scopes": []string{"send:write"}, "allowed_zone_ids": []uuid.UUID{f.zone.ID}})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(raw))).WithContext(ctx)
	token, err := authn.IssueAccessToken(advanceAPIKeySecret, f.user)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	return req
}

func advanceAPIKeyWait(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("API key issuance barrier was not reached")
	}
}

func (f *advanceAPIKeyFixture) counts(t *testing.T) [3]int {
	t.Helper()
	var counts [3]int
	for i, query := range []string{
		`SELECT count(*) FROM tenant_api_keys WHERE tenant_id=$1`,
		`SELECT count(*) FROM tenant_api_key_usage u JOIN tenant_api_keys k ON k.id=u.api_key_id WHERE k.tenant_id=$1`,
		`SELECT count(*) FROM audit_log WHERE tenant_id=$1 AND action='api_key.create'`,
	} {
		if err := f.pool.QueryRow(t.Context(), query, f.tenant.ID).Scan(&counts[i]); err != nil {
			t.Fatal(err)
		}
	}
	return counts
}

func (f *advanceAPIKeyFixture) rejected(t *testing.T, w *httptest.ResponseRecorder, status int) {
	t.Helper()
	if w.Code != status || f.counts(t) != [3]int{} || strings.Contains(w.Body.String(), `"key":`) {
		t.Errorf("stale or failed API-key issuance escaped: status=%d want=%d counts=%v secret_returned=%t", w.Code, status, f.counts(t), strings.Contains(w.Body.String(), `"key":`))
	}
	if strings.Contains(w.Body.String(), "synthetic-private") || strings.Contains(w.Body.String(), "23514") {
		t.Error("database diagnostic leaked into key response")
	}
}

func (f *advanceAPIKeyFixture) successful(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	var result struct {
		Data struct {
			ID  uuid.UUID `json:"id"`
			Key string    `json:"key"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusCreated || result.Data.ID == uuid.Nil || result.Data.Key == "" || f.counts(t) != [3]int{1, 1, 1} {
		t.Fatalf("normal key issue lost atomic result: status=%d counts=%v", w.Code, f.counts(t))
	}
	tenant, id, _, zones, owner, err := f.st.ResolveAPIKey(t.Context(), result.Data.Key)
	if err != nil || tenant == nil || tenant.ID != f.tenant.ID || id == nil || *id != result.Data.ID || len(zones) != 1 || zones[0] != f.zone.ID {
		t.Fatalf("returned key does not resolve to exact current scope: err=%v", err)
	}
	if f.user.Role == models.RoleUser && (owner == nil || *owner != f.user.ID) || f.user.Role != models.RoleUser && owner != nil {
		t.Fatalf("key ownership semantics changed: role=%s owner=%v", f.user.Role, owner)
	}
}

func TestAdvanceAPIKeyIssuancePostgres(t *testing.T) {
	if os.Getenv("TABMAIL_TEST_DB_DSN") == "" {
		t.Fatal("explicit disposable PostgreSQL DSN required for API key issuance")
	}
	for _, name := range []string{"normal_employee", "normal_admin", "normal_cross_home_super"} {
		t.Run(name, func(t *testing.T) {
			role, crossHome := models.RoleUser, false
			if name == "normal_admin" {
				role = models.RoleAdmin
			} else if name == "normal_cross_home_super" {
				role, crossHome = models.RoleSuperAdmin, true
			}
			f := advanceAPIKeySeed(t, role, crossHome)
			w := httptest.NewRecorder()
			f.router(t, nil).ServeHTTP(w, f.request(t, t.Context()))
			f.successful(t, w)
		})
	}
	for _, name := range []string{"employee_freeze", "employee_freeze_unfreeze", "employee_password", "employee_promoted", "admin_demoted", "cross_home_super_demoted", "permission_revoked", "send_permission_revoked", "zone_permission_revoked"} {
		t.Run(name, func(t *testing.T) {
			role, crossHome := models.RoleUser, false
			if name == "admin_demoted" {
				role = models.RoleAdmin
			} else if name == "cross_home_super_demoted" {
				role, crossHome = models.RoleSuperAdmin, true
			}
			f := advanceAPIKeySeed(t, role, crossHome)
			gate := &advanceAPIKeyGate{entered: make(chan struct{}), resume: make(chan struct{})}
			defer gate.release()
			w, done := httptest.NewRecorder(), make(chan struct{})
			router, req := f.router(t, gate), f.request(t, t.Context())
			go func() { defer close(done); router.ServeHTTP(w, req) }()
			advanceAPIKeyWait(t, gate.entered)
			ctx := t.Context()
			switch name {
			case "employee_freeze", "employee_freeze_unfreeze":
				active := false
				if _, err := f.st.UpdateUserGuarded(ctx, f.manager, f.user.TenantID, f.user.ID, models.UserAdminPatch{IsActive: &active}); err != nil {
					t.Fatal(err)
				}
				if name == "employee_freeze_unfreeze" {
					active = true
					if _, err := f.st.UpdateUserGuarded(ctx, f.manager, f.user.TenantID, f.user.ID, models.UserAdminPatch{IsActive: &active}); err != nil {
						t.Fatal(err)
					}
				}
			case "employee_password":
				if err := f.st.ChangePasswordAtomic(ctx, f.user.ID, f.user.PasswordHash, "synthetic-replaced-hash"); err != nil {
					t.Fatal(err)
				}
			case "employee_promoted", "admin_demoted", "cross_home_super_demoted":
				role := models.RoleUser
				if name == "employee_promoted" {
					role = models.RoleAdmin
				}
				if _, err := f.st.UpdateUserGuarded(ctx, f.manager, f.user.TenantID, f.user.ID, models.UserAdminPatch{Role: &role}); err != nil {
					t.Fatal(err)
				}
			case "permission_revoked":
				allowed := false
				if err := f.st.UpsertUserPermissionOverride(ctx, &models.UserPermissionOverride{UserID: f.user.ID, CanCreateAPIKeys: &allowed}); err != nil {
					t.Fatal(err)
				}
			case "send_permission_revoked":
				f.profile.CanSend = false
				if err := f.st.UpdatePermissionProfile(ctx, f.profile); err != nil {
					t.Fatal(err)
				}
			case "zone_permission_revoked":
				f.profile.AllowedZoneIDs = []uuid.UUID{uuid.New()}
				if err := f.st.UpdatePermissionProfile(ctx, f.profile); err != nil {
					t.Fatal(err)
				}
			}
			gate.release()
			advanceAPIKeyWait(t, done)
			f.rejected(t, w, http.StatusForbidden)
		})
	}
	for _, name := range []string{"required_audit_rollback", "key_insert_rollback", "usage_insert_rollback"} {
		t.Run(name, func(t *testing.T) {
			f := advanceAPIKeySeed(t, models.RoleUser, false)
			query := `ALTER TABLE tenant_api_keys ADD CONSTRAINT advance_key_reject CHECK(false) NOT VALID`
			if name == "usage_insert_rollback" {
				query = `ALTER TABLE tenant_api_key_usage ADD CONSTRAINT advance_usage_reject CHECK(false) NOT VALID`
			} else if name == "required_audit_rollback" {
				query = `CREATE FUNCTION advance_reject_key_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='api_key.create' THEN RAISE EXCEPTION 'synthetic-private key audit failure' USING ERRCODE='23514'; END IF; RETURN NEW; END $$; CREATE TRIGGER advance_key_audit BEFORE INSERT ON audit_log FOR EACH ROW EXECUTE FUNCTION advance_reject_key_audit()`
			}
			if _, err := f.pool.Exec(t.Context(), query); err != nil {
				t.Fatal(err)
			}
			w := httptest.NewRecorder()
			f.router(t, nil).ServeHTTP(w, f.request(t, t.Context()))
			f.rejected(t, w, http.StatusInternalServerError)
		})
	}
	for _, resource := range []string{"current_user", "permission_profile"} {
		t.Run("holds_"+resource, func(t *testing.T) {
			f := advanceAPIKeySeed(t, models.RoleUser, false)
			ctx := t.Context()
			hold, err := f.pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer hold.Rollback(context.Background())
			if _, err := hold.Exec(ctx, `LOCK TABLE tenant_api_keys IN SHARE MODE`); err != nil {
				t.Fatal(err)
			}
			w, done := httptest.NewRecorder(), make(chan struct{})
			router, req := f.router(t, nil), f.request(t, ctx)
			go func() { defer close(done); router.ServeHTTP(w, req) }()
			advanceAPIKeyBlocked(t, f, hold.Conn().PgConn().PID())
			probe, err := f.pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer probe.Rollback(context.Background())
			query, id := `SELECT id FROM users WHERE id=$1 FOR UPDATE NOWAIT`, f.user.ID
			if resource == "permission_profile" {
				query, id = `SELECT id FROM permission_profiles WHERE id=$1 FOR UPDATE NOWAIT`, f.profile.ID
			}
			_, err = probe.Exec(ctx, query, id)
			var pg *pgconn.PgError
			if !errors.As(err, &pg) || pg.Code != "55P03" {
				t.Errorf("API-key issuer did not fence %s before INSERT: SQLSTATE=%s", resource, advanceAPIKeySQLState(err))
			}
			if err := probe.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			if err := hold.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			advanceAPIKeyWait(t, done)
			f.successful(t, w)
		})
	}
	t.Run("cancellation_after_auth", func(t *testing.T) {
		f := advanceAPIKeySeed(t, models.RoleUser, false)
		gate := &advanceAPIKeyGate{entered: make(chan struct{}), resume: make(chan struct{})}
		defer gate.release()
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		w, done := httptest.NewRecorder(), make(chan struct{})
		router, req := f.router(t, gate), f.request(t, ctx)
		go func() { defer close(done); router.ServeHTTP(w, req) }()
		advanceAPIKeyWait(t, gate.entered)
		cancel()
		advanceAPIKeyWait(t, done)
		f.rejected(t, w, http.StatusInternalServerError)
	})
}

func advanceAPIKeyBlocked(t *testing.T, f *advanceAPIKeyFixture, blocker uint32) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	for {
		var found bool
		if err := f.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND $1::int=ANY(pg_blocking_pids(pid)) AND query LIKE '%INSERT INTO tenant_api_keys%')`, int32(blocker)).Scan(&found); err != nil {
			t.Fatal(err)
		}
		if found {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("key INSERT did not reach the observed PostgreSQL lock barrier")
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func advanceAPIKeySQLState(err error) string {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return pg.Code
	}
	if err == nil {
		return "none"
	}
	return "non-PG-error"
}

