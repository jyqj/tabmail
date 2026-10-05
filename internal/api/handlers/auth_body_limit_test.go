package handlers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/api/middleware"
	"tabmail/internal/authn"
	"tabmail/internal/models"
	"tabmail/internal/store"
)

// Every fixture is finite and synthetic. No server, database, or live account
// is involved; embedded nil interfaces fail if an unexpected store call occurs.
type authBodyLimitStore struct {
	authStore
	lookupUser                         *models.User
	lookups, rotations, revokes, reads int
	lastEmail, lastHash                string
	rotationSucceeds                   bool
	user                               *models.User
}

func (s *authBodyLimitStore) GetUserByEmail(_ context.Context, email string) (*models.User, error) {
	s.lookups++
	s.lastEmail = email
	return s.lookupUser, nil
}

func (s *authBodyLimitStore) RotateRefreshToken(_ context.Context, hash string, next *models.RefreshToken) (bool, bool, error) {
	s.rotations++
	s.lastHash = hash
	if s.rotationSucceeds {
		next.UserID = s.user.ID
	}
	return s.rotationSucceeds, false, nil
}

func (s *authBodyLimitStore) GetUser(context.Context, uuid.UUID) (*models.User, error) {
	s.reads++
	return s.user, nil
}

func (s *authBodyLimitStore) RevokeRefreshTokenByHash(_ context.Context, hash string) error {
	s.revokes++
	s.lastHash = hash
	return nil
}

func (s *authBodyLimitStore) RevokeUserRefreshTokens(context.Context, uuid.UUID) error {
	s.revokes++
	return nil
}

func (s *authBodyLimitStore) calls() int {
	return s.lookups + s.rotations + s.revokes + s.reads
}

type authBodyLimitReader struct {
	io.Reader
	read, closed, chunk int
}

func (r *authBodyLimitReader) Read(p []byte) (int, error) {
	if r.chunk > 0 && len(p) > r.chunk {
		p = p[:r.chunk]
	}
	n, err := r.Reader.Read(p)
	r.read += n
	return n, err
}

func (r *authBodyLimitReader) Close() error {
	r.closed++
	return nil
}

func newAuthBodyLimitHandler(t *testing.T, st authStore) *AuthHandler {
	t.Helper()
	h := NewAuthHandler(st, "synthetic-secret", uuid.Nil, true, nil, false, zerolog.Nop())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := h.StopContext(ctx); err != nil {
			t.Errorf("stop handler: %v", err)
		}
	})
	return h
}

func authBodyLimitRequest(body string, knownLength bool) (*http.Request, *authBodyLimitReader) {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(body))
	reader := &authBodyLimitReader{Reader: r.Body}
	if !knownLength {
		r.ContentLength = -1
		r.TransferEncoding = []string{"chunked"}
		reader.chunk = 127
	}
	r.Body = reader
	return r, reader
}

func checkAuthBodyLimitRead(t *testing.T, reader *authBodyLimitReader, size int) {
	t.Helper()
	if reader.read > maxAuthBodyBytes+1 || reader.read > size {
		t.Errorf("read %d bytes for %d-byte body; budget is %d plus one overflow byte", reader.read, size, maxAuthBodyBytes)
	}
	if reader.closed != 1 {
		t.Errorf("body closes=%d; want 1", reader.closed)
	}
}

func checkAuthBodyBadRequest(t *testing.T, w *httptest.ResponseRecorder, st *authBodyLimitStore) {
	t.Helper()
	var got envelope
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("invalid response JSON: %v", err)
	}
	if w.Code != http.StatusBadRequest || got.Error == nil || got.Error.Code != "BAD_REQUEST" || got.Error.Message != "invalid request body" {
		t.Errorf("status=%d response=%s; want 400/BAD_REQUEST/invalid request body", w.Code, w.Body.String())
	}
	if st.calls() != 0 || len(w.Result().Cookies()) != 0 {
		t.Errorf("rejected body caused %d store calls or %d cookie effects", st.calls(), len(w.Result().Cookies()))
	}
}

func TestAuthLoginBodyLimit(t *testing.T) {
	const budget = 64 * 1024
	const suffix = `","password":"synthetic-password"}`
	body := `{"email":"` + strings.Repeat("a", budget+1-len(`{"email":"`)-len(suffix)) + suffix
	for _, knownLength := range []bool{true, false} {
		name := "unknown_length"
		if knownLength {
			name = "known_length"
		}
		t.Run(name, func(t *testing.T) {
			st := &authBodyLimitStore{}
			h := newAuthBodyLimitHandler(t, st)
			r, reader := authBodyLimitRequest(body, knownLength)
			w := httptest.NewRecorder()
			h.Login(w, r)
			checkAuthBodyBadRequest(t, w, st)
			checkAuthBodyLimitRead(t, reader, len(body))
		})
	}
}

// Use real JWT middleware to install the principal for protected handlers, with
// a separate identity-only fake so rejected bodies must make zero auth calls.
type authBodyIdentityStore struct {
	store.Store
	user *models.User
}

func (s authBodyIdentityStore) GetUser(context.Context, uuid.UUID) (*models.User, error) {
	return s.user, nil
}
func (s authBodyIdentityStore) GetTenant(context.Context, uuid.UUID) (*models.Tenant, error) {
	return &models.Tenant{ID: s.user.TenantID}, nil
}

func authBodyProtectedHandler(t *testing.T, h *AuthHandler, fn http.HandlerFunc, r *http.Request) http.Handler {
	t.Helper()
	user := &models.User{ID: uuid.New(), TenantID: uuid.New(), Role: models.RoleUser, IsActive: true}
	token, err := authn.IssueAccessToken("synthetic-secret", user)
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Authorization", "Bearer "+token)
	return middleware.Auth(authBodyIdentityStore{user: user}, "synthetic-secret", "", middleware.NewAuthState(h.background))(fn)
}

func TestAuthBodyLimitAllRoutes(t *testing.T) {
	for _, route := range []struct {
		name, body string
		accepted   int
		protected  bool
	}{
		{"login", `{"email":"a@example.test","password":"synthetic-password"}`, http.StatusUnauthorized, false},
		{"register", `{"email":"a@example.test","password":"synthetic-password","display_name":"Synthetic"}`, http.StatusConflict, false},
		{"refresh", `{"refresh_token":"synthetic-token"}`, http.StatusUnauthorized, false},
		{"logout", `{"refresh_token":"synthetic-token"}`, http.StatusNoContent, true},
		{"change-password", `{"old_password":"synthetic-old","new_password":"synthetic-new"}`, http.StatusForbidden, true},
	} {
		t.Run(route.name, func(t *testing.T) {
			for _, tc := range []struct {
				name, body string
				known      bool
				rejected   bool
			}{
				{"exact_limit", route.body + strings.Repeat(" ", maxAuthBodyBytes-len(route.body)), true, false},
				{"exact_limit_unknown", route.body + strings.Repeat(" ", maxAuthBodyBytes-len(route.body)), false, false},
				{"limit_plus_one", route.body + strings.Repeat(" ", maxAuthBodyBytes+1-len(route.body)), true, true},
				{"trailing_whitespace_unknown", route.body + strings.Repeat(" ", maxAuthBodyBytes+1024-len(route.body)), false, true},
				{"leading_whitespace_unknown", strings.Repeat(" ", maxAuthBodyBytes+1024) + route.body, false, true},
				{"invalid_json", `{"`, true, true},
				{"unknown_field", `{"unknown":"value"}`, true, true},
				{"second_document", route.body + `{}`, true, true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					st := &authBodyLimitStore{}
					if route.name == "register" {
						st.lookupUser = &models.User{}
					}
					h := newAuthBodyLimitHandler(t, st)
					fn := map[string]http.HandlerFunc{"login": h.Login, "register": h.Register, "refresh": h.Refresh, "logout": h.Logout, "change-password": h.ChangePassword}[route.name]
					r, reader := authBodyLimitRequest(tc.body, tc.known)
					r.URL.Path = "/api/v1/auth/" + route.name
					var handler http.Handler = fn
					if route.protected {
						handler = authBodyProtectedHandler(t, h, fn, r)
					}
					w := httptest.NewRecorder()
					handler.ServeHTTP(w, r)
					if tc.rejected {
						checkAuthBodyBadRequest(t, w, st)
					} else if w.Code != route.accepted {
						t.Errorf("status=%d response=%s; want %d", w.Code, w.Body.String(), route.accepted)
					}
					checkAuthBodyLimitRead(t, reader, len(tc.body))
				})
			}
		})
	}
}

func TestAuthBodyLimitMultibyteAndDeclaredLength(t *testing.T) {
	const prefix, suffix = `{"email":"`, `","password":"synthetic-password"}`
	for _, extra := range []int{0, 1} {
		t.Run(map[int]string{0: "exact_byte_limit", 1: "one_byte_over"}[extra], func(t *testing.T) {
			n := maxAuthBodyBytes + extra - len(prefix) - len(suffix)
			email := strings.Repeat("中", n/len("中")) + strings.Repeat("a", n%len("中"))
			body := prefix + email + suffix
			st := &authBodyLimitStore{}
			h := newAuthBodyLimitHandler(t, st)
			r, reader := authBodyLimitRequest(body, false)
			// An inaccurate small declared length must not bypass the actual reader.
			r.ContentLength = 1
			w := httptest.NewRecorder()
			h.Login(w, r)
			if extra > 0 {
				checkAuthBodyBadRequest(t, w, st)
			} else if w.Code != http.StatusUnauthorized || st.lookups != 1 || st.lastEmail != email {
				t.Errorf("exact byte limit changed decoded credentials: status=%d lookups=%d", w.Code, st.lookups)
			}
			checkAuthBodyLimitRead(t, reader, len(body))
		})
	}
}

func TestAuthOptionalBodyCompatibility(t *testing.T) {
	for _, route := range []string{"refresh", "logout"} {
		t.Run(route, func(t *testing.T) {
			for _, tc := range []struct {
				name, body string
				rejected   bool
			}{
				{"empty", "", false},
				{"whitespace", " \r\n\t", false},
				{"whitespace_exact_limit", strings.Repeat(" ", maxAuthBodyBytes), false},
				{"empty_object", `{}`, false},
				{"null", `null`, false},
				{"cookie_preferred", `{"refresh_token":"different-body-token"}`, false},
				{"malformed", `{"`, true},
				{"partially_decoded", `{"refresh_token":"body-token","unknown":true}`, true},
				{"second_document", `{} {}`, true},
				{"oversized_whitespace", strings.Repeat(" ", maxAuthBodyBytes+1024), true},
				{"oversized_token", `{"refresh_token":"` + strings.Repeat("a", maxAuthBodyBytes+1024) + `"}`, true},
				{"oversized_trailer", `{}` + strings.Repeat(" ", maxAuthBodyBytes+1024), true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					st := &authBodyLimitStore{rotationSucceeds: true, user: &models.User{ID: uuid.New(), TenantID: uuid.New(), IsActive: true}}
					h := newAuthBodyLimitHandler(t, st)
					fn := h.Refresh
					wantStatus := http.StatusOK
					if route == "logout" {
						fn, wantStatus = h.Logout, http.StatusNoContent
					}
					r, reader := authBodyLimitRequest(tc.body, false)
					r.AddCookie(&http.Cookie{Name: RefreshCookieName, Value: "synthetic-cookie-token"})
					w := httptest.NewRecorder()
					fn(w, r)
					if tc.rejected {
						checkAuthBodyBadRequest(t, w, st)
					} else if w.Code != wantStatus || st.lastHash != authn.HashToken("synthetic-cookie-token") || len(w.Result().Cookies()) != 1 {
						t.Errorf("cookie-only/preferred behavior changed: status=%d calls=%d cookies=%d", w.Code, st.calls(), len(w.Result().Cookies()))
					}
					checkAuthBodyLimitRead(t, reader, len(tc.body))
				})
			}
		})
	}
}

func TestAuthBodyLimitEmptyAuthenticatedLogout(t *testing.T) {
	st := &authBodyLimitStore{}
	h := newAuthBodyLimitHandler(t, st)
	r, reader := authBodyLimitRequest("", true)
	w := httptest.NewRecorder()
	authBodyProtectedHandler(t, h, h.Logout, r).ServeHTTP(w, r)
	if w.Code != http.StatusNoContent || st.revokes != 1 || st.lastHash != "" || len(w.Result().Cookies()) != 1 {
		t.Errorf("empty authenticated logout changed: status=%d calls=%d", w.Code, st.calls())
	}
	checkAuthBodyLimitRead(t, reader, 0)
}

func TestAuthBodyLimitBodyTokenFallback(t *testing.T) {
	for _, logout := range []bool{false, true} {
		st := &authBodyLimitStore{rotationSucceeds: true, user: &models.User{ID: uuid.New(), TenantID: uuid.New(), IsActive: true}}
		h := newAuthBodyLimitHandler(t, st)
		body := `{"refresh_token":"synthetic-body-token"}`
		r, reader := authBodyLimitRequest(body, true)
		w := httptest.NewRecorder()
		wantStatus := http.StatusOK
		if logout {
			h.Logout(w, r)
			wantStatus = http.StatusNoContent
		} else {
			h.Refresh(w, r)
		}
		if w.Code != wantStatus || st.lastHash != authn.HashToken("synthetic-body-token") || len(w.Result().Cookies()) != 1 {
			t.Errorf("body-token fallback changed: logout=%v status=%d calls=%d", logout, w.Code, st.calls())
		}
		checkAuthBodyLimitRead(t, reader, len(body))
	}
}

func TestAuthBodyLimitEscapedFieldHeadroom(t *testing.T) {
	// VARCHAR(255) counts characters, not bytes. Two maximum-width escaped
	// profile strings plus a fully escaped 72-byte password fit well below the
	// body budget; this does not impose a new per-field credential policy.
	profile := strings.Repeat(`\ud83d\ude00`, 255)
	password := strings.Repeat(`\u0061`, 72)
	body := `{"email":"` + profile + `","display_name":"` + profile + `","password":"` + password + `"}`
	if len(body) >= 7*1024 || len(body) >= maxAuthBodyBytes {
		t.Fatalf("maximum escaped field envelope unexpectedly large: %d", len(body))
	}
	st := &authBodyLimitStore{lookupUser: &models.User{}}
	h := newAuthBodyLimitHandler(t, st)
	r, reader := authBodyLimitRequest(body, false)
	w := httptest.NewRecorder()
	h.Register(w, r)
	if w.Code != http.StatusConflict || st.lookups != 1 || st.lastEmail != strings.Repeat("😀", 255) {
		t.Errorf("escaped fields rejected or altered: status=%d lookups=%d", w.Code, st.lookups)
	}
	checkAuthBodyLimitRead(t, reader, len(body))
}

func TestAuthBodyLimitValidLogin(t *testing.T) {
	st := r5APILoginFixture(t)
	h := newAuthBodyLimitHandler(t, st)
	r := r5APILoginRequest()
	reader := &authBodyLimitReader{Reader: r.Body}
	r.Body = reader
	w := httptest.NewRecorder()
	h.Login(w, r)
	if w.Code != http.StatusOK || st.lookups.Load() != 1 || st.tokens.Load() != 1 || len(w.Result().Cookies()) != 1 {
		t.Errorf("valid login changed: status=%d lookups=%d tokens=%d", w.Code, st.lookups.Load(), st.tokens.Load())
	}
	var got struct {
		Data struct {
			AccessToken string `json:"access_token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || got.Data.AccessToken == "" {
		t.Errorf("missing valid access token: %v", err)
	}
	checkAuthBodyLimitRead(t, reader, int(r.ContentLength))
}
