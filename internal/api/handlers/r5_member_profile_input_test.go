package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
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
	"tabmail/internal/authn"
	"tabmail/internal/authz"
	"tabmail/internal/models"
	"tabmail/internal/testutil"
)

// The real JWT middleware and member handler run before this instrumented
// persistence boundary. Invalid text has the existing PostgreSQL VARCHAR(255)
// / NUL failure behavior; no database-concurrency claim is made here.
type r5MemberProfileInputStore struct {
	*testutil.FakeStore
	commands int
	failure  error
}

func (s *r5MemberProfileInputStore) UpdateUserGuarded(ctx context.Context, a authz.Actor, tenant, target uuid.UUID, patch models.UserAdminPatch) (*models.User, error) {
	s.commands++
	if s.failure != nil {
		return nil, s.failure
	}
	if patch.DisplayName != nil && (strings.ContainsRune(*patch.DisplayName, 0) || utf8.RuneCountInString(*patch.DisplayName) > 255) {
		return nil, errors.New("synthetic database text constraint")
	}
	return s.FakeStore.UpdateUserGuarded(ctx, a, tenant, target, patch)
}

type r5MemberProfileInputFixture struct {
	store  *r5MemberProfileInputStore
	admin  *models.User
	target *models.User
	secret string
}

func r5MemberProfileInputSeed(t *testing.T) *r5MemberProfileInputFixture {
	t.Helper()
	f := &r5MemberProfileInputFixture{store: &r5MemberProfileInputStore{FakeStore: testutil.NewFakeStore()}, secret: "synthetic-member-profile-jwt"}
	tenant := &models.Tenant{ID: uuid.New(), Name: "Synthetic profile company", PlanID: uuid.New()}
	if err := f.store.CreateTenant(t.Context(), tenant); err != nil {
		t.Fatal(err)
	}
	f.admin = &models.User{ID: uuid.New(), TenantID: tenant.ID, Email: "admin@fixture.test", DisplayName: "Administrator", Role: models.RoleAdmin, IsActive: true}
	f.target = &models.User{ID: uuid.New(), TenantID: tenant.ID, Email: "employee@fixture.test", DisplayName: "Original employee", Role: models.RoleUser, IsActive: true}
	for _, u := range []*models.User{f.admin, f.target} {
		if err := f.store.CreateUser(t.Context(), u); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

type r5MemberProfileInputReader struct {
	io.Reader
	read, closed int
	streamed     bool
}

func (r *r5MemberProfileInputReader) Read(p []byte) (int, error) {
	if r.streamed && len(p) > 37 {
		p = p[:37]
	}
	n, err := r.Reader.Read(p)
	r.read += n
	return n, err
}

func (r *r5MemberProfileInputReader) Close() error { r.closed++; return nil }

func (f *r5MemberProfileInputFixture) request(t *testing.T, body string, streamed bool) (*httptest.ResponseRecorder, *r5MemberProfileInputReader) {
	t.Helper()
	token, err := authn.IssueAccessToken(f.secret, f.admin)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/admin/users/"+f.target.ID.String(), strings.NewReader(body))
	reader := &r5MemberProfileInputReader{Reader: req.Body, streamed: streamed}
	req.Body = reader
	if streamed {
		req.ContentLength = -1
		req.TransferEncoding = []string{"chunked"}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	router := chi.NewRouter()
	router.Use(middleware.Auth(f.store, f.secret, f.admin.TenantID.String()))
	router.Use(middleware.RequireAdmin)
	router.Patch("/api/v1/admin/users/{id}", NewUserAdminHandler(f.store, zerolog.Nop()).UpdateUserByAdmin)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w, reader
}

func r5MemberProfileInputJSON(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func (f *r5MemberProfileInputFixture) unchanged(t *testing.T) {
	t.Helper()
	u, err := f.store.GetUser(t.Context(), f.target.ID)
	if err != nil || !reflect.DeepEqual(u, f.target) || f.store.commands != 0 {
		t.Fatalf("invalid input reached member mutation: commands=%d user=%#v err=%v", f.store.commands, u, err)
	}
}

func TestR5MemberProfileInputRejectsInvalidTextBeforeCommand(t *testing.T) {
	for _, tc := range []struct{ name, text string }{
		{"ascii_256", strings.Repeat("a", 256)},
		{"unicode_256", strings.Repeat("界", 256)},
		{"nul", "employee\x00name"},
	} {
		for _, mixed := range []bool{false, true} {
			name := tc.name + "/name_only"
			if mixed {
				name = tc.name + "/combined_lifecycle_update"
			}
			t.Run(name, func(t *testing.T) {
				f := r5MemberProfileInputSeed(t)
				input := map[string]any{"display_name": tc.text}
				if mixed {
					input["is_active"], input["role"] = false, "admin"
				}
				w, reader := f.request(t, r5MemberProfileInputJSON(t, input), false)
				if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), `"code":"BAD_REQUEST"`) {
					t.Errorf("invalid display name status=%d body=%s", w.Code, w.Body.String())
				}
				if reader.closed != 1 {
					t.Errorf("request body closed %d times", reader.closed)
				}
				f.unchanged(t)
			})
		}
	}
}

func TestR5MemberProfileInputBoundsCompleteBody(t *testing.T) {
	valid := `{"display_name":"Updated employee"}`
	for _, streamed := range []bool{false, true} {
		mode := "known_length"
		if streamed {
			mode = "streamed"
		}
		for _, tc := range []struct {
			name, body string
			valid      bool
		}{
			{"at_limit", valid + strings.Repeat(" ", maxAuthBodyBytes-len(valid)), true},
			{"one_over", valid + strings.Repeat(" ", maxAuthBodyBytes+1-len(valid)), false},
			{"large_whitespace", valid + strings.Repeat(" ", 4*maxAuthBodyBytes), false},
			{"oversized_field", r5MemberProfileInputJSON(t, map[string]string{"display_name": strings.Repeat("a", 3*maxAuthBodyBytes)}), false},
			{"trailing_document", valid + ` {}`, false},
			{"unknown_field", `{"display_name":"Updated","unexpected":true}`, false},
			{"malformed", `{"display_name":"Updated"`, false},
			{"null_object", `null`, false},
		} {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				f := r5MemberProfileInputSeed(t)
				w, reader := f.request(t, tc.body, streamed)
				if reader.read > maxAuthBodyBytes+1 || reader.closed != 1 {
					t.Errorf("profile body exceeded bounded decoder ownership: bytes=%d closed=%d", reader.read, reader.closed)
				}
				if tc.valid {
					u, err := f.store.GetUser(t.Context(), f.target.ID)
					if w.Code != http.StatusOK || f.store.commands != 1 || err != nil || u.DisplayName != "Updated employee" || u.Role != f.target.Role || u.IsActive != f.target.IsActive {
						t.Fatalf("valid at-limit profile rejected: status=%d user=%#v err=%v", w.Code, u, err)
					}
				} else {
					if w.Code != http.StatusBadRequest {
						t.Errorf("invalid profile body status=%d", w.Code)
					}
					f.unchanged(t)
				}
			})
		}
	}
}

func TestR5MemberProfileInputPreservesExplicitAndOmittedFields(t *testing.T) {
	for _, tc := range []struct{ name, text string }{
		{"ascii_255", strings.Repeat("a", 255)},
		{"unicode_255", strings.Repeat("界", 255)},
		{"emoji_255", strings.Repeat("🦊", 255)},
		{"empty", ""},
		{"whitespace_unchanged", " \tEmployee name\n "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := r5MemberProfileInputSeed(t)
			body := r5MemberProfileInputJSON(t, map[string]string{"display_name": tc.text})
			w, _ := f.request(t, body, false)
			u, err := f.store.GetUser(t.Context(), f.target.ID)
			if w.Code != http.StatusOK || f.store.commands != 1 || err != nil || u.DisplayName != tc.text || u.Role != f.target.Role || u.IsActive != f.target.IsActive {
				t.Fatalf("valid profile value changed: status=%d user=%#v err=%v", w.Code, u, err)
			}
			var result struct {
				Data struct {
					DisplayName string `json:"display_name"`
				} `json:"data"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || result.Data.DisplayName != tc.text || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("profile response changed: %#v err=%v", result, err)
			}
		})
	}
	for _, body := range []string{`{}`, `{"display_name":null,"role":null,"is_active":null}`} {
		t.Run(body, func(t *testing.T) {
			f := r5MemberProfileInputSeed(t)
			w, _ := f.request(t, body, false)
			u, err := f.store.GetUser(t.Context(), f.target.ID)
			if w.Code != http.StatusOK || f.store.commands != 1 || err != nil || u.DisplayName != f.target.DisplayName || u.Role != f.target.Role || u.IsActive != f.target.IsActive {
				t.Fatalf("omitted/null field semantics changed: status=%d user=%#v err=%v", w.Code, u, err)
			}
		})
	}
	t.Run("explicit_false_and_role", func(t *testing.T) {
		f := r5MemberProfileInputSeed(t)
		w, _ := f.request(t, `{"is_active":false,"role":"admin","display_name":"Changed"}`, false)
		u, err := f.store.GetUser(t.Context(), f.target.ID)
		if w.Code != http.StatusOK || f.store.commands != 1 || err != nil || u.IsActive || u.Role != models.RoleAdmin || u.DisplayName != "Changed" {
			t.Fatalf("explicit profile command changed: status=%d user=%#v err=%v", w.Code, u, err)
		}
	})
}

func TestR5MemberProfileInputKeepsExistingDenials(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"legacy_profile_null", `{"permission_profile_id":null}`, http.StatusConflict},
		{"legacy_profile_uuid", `{"permission_profile_id":"11111111-1111-4111-8111-111111111111"}`, http.StatusConflict},
		{"invalid_role", `{"role":"unknown"}`, http.StatusBadRequest},
		{"super_admin_promotion", `{"role":"super_admin"}`, http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := r5MemberProfileInputSeed(t)
			w, _ := f.request(t, tc.body, false)
			if w.Code != tc.status {
				t.Errorf("existing denial status=%d want=%d", w.Code, tc.status)
			}
			f.unchanged(t)
		})
	}
	t.Run("foreign_target", func(t *testing.T) {
		f := r5MemberProfileInputSeed(t)
		f.target.TenantID = uuid.New()
		if err := f.store.UpdateUser(t.Context(), f.target); err != nil {
			t.Fatal(err)
		}
		w, _ := f.request(t, `{"display_name":"Changed"}`, false)
		if w.Code != http.StatusNotFound || f.store.commands != 0 {
			t.Fatalf("foreign member exposed: status=%d commands=%d", w.Code, f.store.commands)
		}
	})
	t.Run("store_failure", func(t *testing.T) {
		f := r5MemberProfileInputSeed(t)
		f.store.failure = errors.New("synthetic private persistence failure")
		w, _ := f.request(t, `{"display_name":"Changed"}`, false)
		if w.Code != http.StatusInternalServerError || f.store.commands != 1 || strings.Contains(w.Body.String(), "synthetic") {
			t.Fatalf("persistence failure was not preserved and redacted: status=%d body=%s", w.Code, w.Body.String())
		}
	})
}
