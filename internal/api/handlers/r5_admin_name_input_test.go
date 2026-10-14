package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/api/middleware"
	"tabmail/internal/models"
	"tabmail/internal/testutil"
)

// Exercise the production HTTP handlers, application service, JWT lookup and
// administrator gate. The stateful fake models the existing VARCHAR(64/255)
// and PostgreSQL text NUL boundaries; this is not a live PostgreSQL test.
type r5AdminNameStore struct {
	*testutil.FakeStore
	reads, writes, audits int
	persistedName         string
	failure               error
	planID                uuid.UUID
}

func (s *r5AdminNameStore) GetPlan(ctx context.Context, id uuid.UUID) (*models.Plan, error) {
	s.reads++
	return s.FakeStore.GetPlan(ctx, id)
}

func (s *r5AdminNameStore) writeName(name string, maxCharacters int, write func() error) error {
	s.writes++
	if s.failure != nil {
		return s.failure
	}
	if strings.ContainsRune(name, 0) || utf8.RuneCountInString(name) > maxCharacters {
		return errors.New("private PostgreSQL text boundary diagnostic")
	}
	if err := write(); err != nil {
		return err
	}
	s.persistedName = name
	return nil
}

func (s *r5AdminNameStore) CreateTenant(ctx context.Context, tenant *models.Tenant) error {
	return s.writeName(tenant.Name, 255, func() error { return s.FakeStore.CreateTenant(ctx, tenant) })
}

func (s *r5AdminNameStore) CreatePlan(ctx context.Context, plan *models.Plan) error {
	return s.writeName(plan.Name, 64, func() error { return s.FakeStore.CreatePlan(ctx, plan) })
}

func (s *r5AdminNameStore) UpdatePlan(ctx context.Context, plan *models.Plan) error {
	return s.writeName(plan.Name, 64, func() error { return s.FakeStore.UpdatePlan(ctx, plan) })
}

func (s *r5AdminNameStore) InsertAudit(ctx context.Context, entry *models.AuditEntry) error {
	s.audits++
	return s.FakeStore.InsertAudit(ctx, entry)
}

type r5AdminNameSnapshot struct {
	plans   []*models.Plan
	tenants []*models.Tenant
}

func (s *r5AdminNameStore) snapshot(t *testing.T) r5AdminNameSnapshot {
	t.Helper()
	plans, err := s.FakeStore.ListPlans(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	tenants, err := s.FakeStore.ListTenants(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return r5AdminNameSnapshot{plans: plans, tenants: tenants}
}

func r5AdminNameRequest(t *testing.T, operation string, name any, omitName bool, role string, failure error) (*httptest.ResponseRecorder, *r5AdminNameStore, r5AdminNameSnapshot) {
	t.Helper()
	f := newOutboundAccessFixture(t)
	st := &r5AdminNameStore{FakeStore: f.st, failure: failure}
	plan := &models.Plan{Name: "existing-name-boundary-plan", RetentionHours: 24, DailyQuota: 100}
	if err := st.FakeStore.CreatePlan(t.Context(), plan); err != nil {
		t.Fatal(err)
	}
	st.planID = plan.ID
	before := st.snapshot(t)
	body := map[string]any{"name": name}
	if omitName {
		delete(body, "name")
	}
	if operation == "tenant_create" {
		body["plan_id"] = plan.ID.String()
	} else {
		body["retention_hours"] = 24
		body["daily_quota"] = 100
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	h := NewAdminHandler(st, nil, models.SMTPPolicy{}, &stubSettings{}, nil, zerolog.Nop())
	method, path := http.MethodPost, "/api/v1/admin/tenants"
	if operation == "plan_create" {
		path = "/api/v1/admin/plans"
	} else if operation == "plan_update" {
		method, path = http.MethodPatch, "/api/v1/admin/plans/"+plan.ID.String()
	}
	user := f.platformAdmin
	if role == "tenant" {
		user = f.tenantAdmin
	} else if role == "member" {
		user = f.userA
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	for key, value := range outboundUserHeaders(t, user) {
		req.Header.Set(key, value)
	}
	state := middleware.NewAuthState(nil)
	t.Cleanup(func() {
		if err := state.StopContext(context.Background()); err != nil {
			t.Error(err)
		}
	})
	router := chi.NewRouter()
	router.Use(middleware.Auth(st, outboundTestJWTSecret, publicTenantIDForTests, state))
	router.Group(func(r chi.Router) {
		r.Use(middleware.RequireSuperAdmin)
		r.Post("/api/v1/admin/tenants", h.CreateTenant)
		r.Post("/api/v1/admin/plans", h.CreatePlan)
		r.Patch("/api/v1/admin/plans/{id}", h.UpdatePlan)
	})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w, st, before
}

func TestR5AdminNameRejectsBeforePersistence(t *testing.T) {
	for _, operation := range []string{"tenant_create", "plan_create", "plan_update"} {
		maxCharacters := 64
		if operation == "tenant_create" {
			maxCharacters = 255
		}
		for _, tc := range []struct {
			name  string
			value any
			omit  bool
		}{
			{"empty", "", false},
			{"omitted", nil, true},
			{"null", nil, false},
			{"ascii_whitespace", " \t\r\n ", false},
			{"unicode_whitespace", "\u2003\u3000\t", false},
			{"embedded_nul", "name\x00suffix", false},
			{"ascii_over_limit", strings.Repeat("a", maxCharacters+1), false},
			{"unicode_over_limit", strings.Repeat("界", maxCharacters+1), false},
		} {
			t.Run(operation+"/"+tc.name, func(t *testing.T) {
				w, st, before := r5AdminNameRequest(t, operation, tc.value, tc.omit, "super", nil)
				if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "name") {
					t.Errorf("invalid name status=%d body=%s; want name-specific 400", w.Code, w.Body.String())
				}
				changed := !reflect.DeepEqual(before, st.snapshot(t))
				if st.reads != 0 || st.writes != 0 || st.audits != 0 || changed {
					t.Errorf("invalid name reached persistence: reads=%d writes=%d audits=%d state_changed=%t", st.reads, st.writes, st.audits, changed)
				}
			})
		}
	}
}

func TestR5AdminNamePreservesValidCharacters(t *testing.T) {
	for _, operation := range []string{"tenant_create", "plan_create", "plan_update"} {
		maxCharacters := 64
		if operation == "tenant_create" {
			maxCharacters = 255
		}
		for _, tc := range []struct{ name, value string }{
			{"ordinary", "new-name-boundary"},
			{"ascii_at_limit", strings.Repeat("a", maxCharacters)},
			{"unicode_at_limit", strings.Repeat("界", maxCharacters)},
			{"meaningful_padding", " \t客户 Ω \t "},
		} {
			t.Run(operation+"/"+tc.name, func(t *testing.T) {
				w, st, before := r5AdminNameRequest(t, operation, tc.value, false, "super", nil)
				wantStatus, wantReads := http.StatusCreated, 0
				if operation == "tenant_create" {
					wantReads = 1
				} else if operation == "plan_update" {
					wantStatus = http.StatusOK
				}
				if w.Code != wantStatus || st.reads != wantReads || st.writes != 1 || st.audits != 1 || st.persistedName != tc.value {
					t.Fatalf("valid name changed: status=%d body=%s reads=%d writes=%d audits=%d persisted=%q", w.Code, w.Body.String(), st.reads, st.writes, st.audits, st.persistedName)
				}
				var response struct {
					Data struct {
						Name string `json:"name"`
					} `json:"data"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
					t.Fatal(err)
				}
				if response.Data.Name != tc.value || reflect.DeepEqual(before, st.snapshot(t)) {
					t.Errorf("successful write did not preserve original name: response=%q want=%q", response.Data.Name, tc.value)
				}
			})
		}
	}
}

func TestR5AdminNameAuthorizationAndStorageFailure(t *testing.T) {
	for _, operation := range []string{"tenant_create", "plan_create", "plan_update"} {
		for _, tc := range []struct {
			name, role string
			status     int
			failure    error
		}{
			{"tenant_admin", "tenant", http.StatusForbidden, nil},
			{"member", "member", http.StatusForbidden, nil},
			{"storage_error", "super", http.StatusInternalServerError, errors.New("private name persistence diagnostic")},
		} {
			t.Run(operation+"/"+tc.name, func(t *testing.T) {
				w, st, before := r5AdminNameRequest(t, operation, "new-name-boundary", false, tc.role, tc.failure)
				wantReads, wantWrites := 0, 0
				if tc.failure != nil {
					wantWrites = 1
					if operation == "tenant_create" {
						wantReads = 1
					}
					if strings.Contains(w.Body.String(), tc.failure.Error()) {
						t.Error("private persistence diagnostic leaked to response")
					}
				}
				changed := !reflect.DeepEqual(before, st.snapshot(t))
				if w.Code != tc.status || st.reads != wantReads || st.writes != wantWrites || st.audits != 0 || changed {
					t.Errorf("authorization/storage contract changed: status=%d reads=%d writes=%d audits=%d state_changed=%t", w.Code, st.reads, st.writes, st.audits, changed)
				}
			})
		}
	}
}
