package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/rs/zerolog"

	"tabmail/internal/api/handlers"
	"tabmail/internal/api/middleware"
	"tabmail/internal/authn"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
	"tabmail/internal/store"
	"tabmail/internal/testpg"
	"tabmail/internal/testutil"
)

func r5HistoricalDeleteEnvelope(t *testing.T, rr *httptest.ResponseRecorder, status int, code, message string) {
	t.Helper()
	if rr.Code != status {
		t.Fatalf("HTTP status=%d want=%d body=%s", rr.Code, status, rr.Body.String())
	}
	var got struct {
		Data  any `json:"data"`
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Error == nil || got.Error.Code != code || got.Error.Message != message || got.Data != nil {
		t.Fatalf("unsafe member-delete envelope: %s", rr.Body.String())
	}
	for _, private := range []string{"employee_offboarding_plans", "draft_creation_receipts", "foreign_key", "23503", "DELETE FROM", "private-driver-detail"} {
		if strings.Contains(rr.Body.String(), private) {
			t.Fatalf("response leaked %q", private)
		}
	}
}

func TestR5MemberHistoricalDeleteSafeErrorMapping(t *testing.T) {
	for _, tc := range []struct {
		name          string
		err           error
		status        int
		code, message string
	}{
		{"domain", store.ErrMemberHasHistoricalIdentity, http.StatusConflict, "CONFLICT", store.ErrMemberHasHistoricalIdentity.Error()},
		{"wrapped-domain", fmt.Errorf("private-driver-detail DELETE FROM employee_offboarding_plans foreign_key 23503: %w", store.ErrMemberHasHistoricalIdentity), http.StatusConflict, "CONFLICT", store.ErrMemberHasHistoricalIdentity.Error()},
		{"raw-driver-stays-internal", &pgconn.PgError{Code: "23503", TableName: "draft_creation_receipts", ConstraintName: "foreign_key", Message: "private-driver-detail"}, http.StatusInternalServerError, "INTERNAL", "internal server error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			port, admin, member := r5HistoricalDeleteDispatchFixture(t, tc.err)
			h := handlers.NewUserAdminHandler(port, zerolog.Nop())
			// Exercise the shipping handler's actual mapping, not a re-created
			// error adapter or access to its unexported implementation helper.
			rr := r5HistoricalDeleteRequest(t, port, h, admin, member.ID)
			r5HistoricalDeleteEnvelope(t, rr, tc.status, tc.code, tc.message)
			if port.guardCalls != 1 || port.legacyCalls != 0 {
				t.Fatal("error mapping case bypassed the guarded deletion port")
			}
		})
	}
}

type r5HistoricalDeleteErrorStore struct {
	*testutil.FakeStore
	err                     error
	guardCalls, legacyCalls int
}

func (s *r5HistoricalDeleteErrorStore) DeleteUserGuarded(_ context.Context, a authz.Actor, tenant, target uuid.UUID) error {
	s.guardCalls++
	if a.Type != authz.PrincipalUser || !a.IsAdmin || a.TenantID != tenant || target == a.ID {
		return errors.New("invalid guarded handler dispatch")
	}
	return s.err
}

func (s *r5HistoricalDeleteErrorStore) DeleteUser(context.Context, uuid.UUID) error {
	s.legacyCalls++
	return errors.New("unguarded deletion must never be used")
}

const r5HistoricalDeleteJWT = "test-only-historical-delete-jwt"

func r5HistoricalDeleteRequest(t *testing.T, s interface {
	GetTenant(context.Context, uuid.UUID) (*models.Tenant, error)
	ResolveAPIKey(context.Context, string) (*models.Tenant, *uuid.UUID, []string, []uuid.UUID, *uuid.UUID, error)
	TouchAPIKey(context.Context, uuid.UUID, string) error
	GetUser(context.Context, uuid.UUID) (*models.User, error)
	EffectivePermission(context.Context, uuid.UUID) (*models.EffectivePermission, error)
}, handler *handlers.UserAdminHandler, caller *models.User, target uuid.UUID) *httptest.ResponseRecorder {
	t.Helper()
	token, err := authn.IssueAccessToken(r5HistoricalDeleteJWT, caller)
	if err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	router.Use(middleware.Auth(s, r5HistoricalDeleteJWT, "00000000-0000-0000-0000-000000000001"))
	router.Use(middleware.RequireAdmin)
	router.Delete("/api/v1/admin/users/{id}", handler.DeleteUserByAdmin)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/users/"+target.String(), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	return rr
}

// External-package tests break handlers -> testpg -> api -> handlers's test
// import cycle while still testing the exported production HTTP handler. The
// fake is confined to injected error witnesses; the PG HTTP top below keeps
// the real guarded store/transaction and durable no-effects/success assertions.
func r5HistoricalDeleteDispatchFixture(t *testing.T, err error) (*r5HistoricalDeleteErrorStore, *models.User, *models.User) {
	t.Helper()
	st := testutil.NewFakeStore()
	tenant := &models.Tenant{ID: uuid.New(), Name: "Historical HTTP dispatch"}
	st.SeedTenant(tenant)
	admin := &models.User{ID: uuid.New(), TenantID: tenant.ID, Email: "admin@dispatch.test", Role: models.RoleAdmin, IsActive: true}
	member := &models.User{ID: uuid.New(), TenantID: tenant.ID, Email: "member@dispatch.test", Role: models.RoleUser, IsActive: true}
	for _, u := range []*models.User{admin, member} {
		if err := st.CreateUser(context.Background(), u); err != nil {
			t.Fatal(err)
		}
	}
	return &r5HistoricalDeleteErrorStore{FakeStore: st, err: err}, admin, member
}

func TestR5MemberHistoricalDeleteHandlerDispatch(t *testing.T) {
	port, admin, member := r5HistoricalDeleteDispatchFixture(t, fmt.Errorf("private-driver-detail employee_offboarding_plans: %w", store.ErrMemberHasHistoricalIdentity))
	h := handlers.NewUserAdminHandler(port, zerolog.Nop())
	rr := r5HistoricalDeleteRequest(t, port, h, admin, member.ID)
	r5HistoricalDeleteEnvelope(t, rr, http.StatusConflict, "CONFLICT", store.ErrMemberHasHistoricalIdentity.Error())
	if port.guardCalls != 1 || port.legacyCalls != 0 {
		t.Fatal("member handler bypassed the guarded deletion port")
	}
}

func TestR5MemberHistoricalDeleteHTTPPostgresNoEffects(t *testing.T) {
	for _, name := range []string{"target-plan", "successor-plan", "creation-tombstone", "unreferenced-success"} {
		t.Run(name, func(t *testing.T) {
			if os.Getenv("TABMAIL_TEST_DB_DSN") == "" {
				t.Fatal("historical member HTTP acceptance requires owned TABMAIL_TEST_DB_DSN; no skip")
			}
			ctx := context.Background()
			st, pool, _ := testpg.NewPostgres(t)
			tenant := &models.Tenant{Name: "Historical HTTP", PlanID: uuid.MustParse("00000000-0000-0000-0000-000000000002")}
			if err := st.CreateTenant(ctx, tenant); err != nil {
				t.Fatal(err)
			}
			admin := &models.User{TenantID: tenant.ID, Email: "admin@http-historical.test", Role: models.RoleAdmin, IsActive: true, PasswordHash: "test-only"}
			member := &models.User{TenantID: tenant.ID, Email: "member@http-historical.test", Role: models.RoleUser, IsActive: true, PasswordHash: "test-only"}
			successor := &models.User{TenantID: tenant.ID, Email: "successor@http-historical.test", Role: models.RoleUser, IsActive: true, PasswordHash: "test-only"}
			for _, u := range []*models.User{admin, member, successor} {
				if err := st.CreateUser(ctx, u); err != nil {
					t.Fatal(err)
				}
			}
			v := admin.SessionVersion
			actor := authz.Actor{Type: authz.PrincipalUser, ID: admin.ID, TenantID: tenant.ID, Role: admin.Role, IsAdmin: true, SessionVersion: &v}
			target := member.ID
			switch name {
			case "target-plan", "successor-plan":
				if _, err := st.PreviewOffboarding(ctx, actor, member.ID, successor.ID, company.OffboardingOptions{Drafts: "seal"}, "Historical HTTP refusal evidence"); err != nil {
					t.Fatal(err)
				}
				if name == "successor-plan" {
					target = successor.ID
				}
			case "creation-tombstone":
				// Contentless receipt may outlive both draft and original mailbox.
				if _, err := pool.Exec(ctx, `INSERT INTO draft_creation_receipts(id,tenant_id,user_id,mailbox_id,payload_hash) VALUES($1,$2,$3,$4,'legacy')`, uuid.New(), tenant.ID, target, uuid.New()); err != nil {
					t.Fatal(err)
				}
			}
			if err := st.CreateAPIKey(ctx, &models.TenantAPIKey{TenantID: tenant.ID, OwnerUserID: &target, KeyHash: company.Hash(uuid.NewString()), KeyPrefix: "test", Scopes: []string{"send:write"}}); err != nil {
				t.Fatal(err)
			}
			snapshot := func() string {
				var state string
				if err := pool.QueryRow(ctx, `SELECT jsonb_build_object(
 'users',(SELECT jsonb_agg(to_jsonb(r) ORDER BY r.id) FROM users r),
 'keys',(SELECT jsonb_agg(to_jsonb(r) ORDER BY r.id) FROM tenant_api_keys r),
 'plans',(SELECT jsonb_agg(to_jsonb(r) ORDER BY r.id) FROM employee_offboarding_plans r),
 'receipts',(SELECT jsonb_agg(to_jsonb(r) ORDER BY r.id) FROM draft_creation_receipts r),
 'audit',(SELECT jsonb_agg(to_jsonb(r) ORDER BY r.id) FROM audit_log r))::text`).Scan(&state); err != nil {
					t.Fatal(err)
				}
				return state
			}
			before := snapshot()
			rr := r5HistoricalDeleteRequest(t, st, handlers.NewUserAdminHandler(st, zerolog.Nop()), admin, target)
			if name == "unreferenced-success" {
				if rr.Code != http.StatusNoContent || rr.Body.Len() != 0 {
					t.Fatalf("legitimate deletion status=%d body=%s", rr.Code, rr.Body.String())
				}
				var users, keys, audits int
				if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM users WHERE id=$1),(SELECT count(*) FROM tenant_api_keys WHERE owner_user_id=$1),(SELECT count(*) FROM audit_log WHERE action='user.delete' AND resource_id=$1)`, target).Scan(&users, &keys, &audits); err != nil {
					t.Fatal(err)
				}
				if users != 0 || keys != 0 || audits != 1 {
					t.Fatal("HTTP guarded success missing atomic deletion/audit")
				}
				return
			}
			r5HistoricalDeleteEnvelope(t, rr, http.StatusConflict, "CONFLICT", store.ErrMemberHasHistoricalIdentity.Error())
			if snapshot() != before {
				t.Fatal("HTTP historical refusal changed user/key/receipt/plan/audit")
			}
		})
	}
}
