package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"tabmail/internal/authn"
	"tabmail/internal/models"
)

// The final persistence ports count attempts and retain successful synthetic
// rows. Their text rejection models the existing VARCHAR(255)/NUL constraints;
// this is an HTTP/application regression, not a live PostgreSQL transaction test.
type registrationTextStore struct {
	authStore
	lookups, tenantCalls, userCalls, tokenCalls int
	lookupEmail                                 string
	tenant                                      *models.Tenant
	user                                        *models.User
	token                                       *models.RefreshToken
	failAt                                      string
}

func registrationTextDBError(value string) error {
	if strings.ContainsRune(value, 0) || utf8.RuneCountInString(value) > 255 {
		return errors.New("synthetic PostgreSQL text constraint")
	}
	return nil
}
func (s *registrationTextStore) GetUserByEmail(_ context.Context, email string) (*models.User, error) {
	s.lookups++
	s.lookupEmail = email
	if s.failAt == "lookup" {
		return nil, errors.New("synthetic lookup failure")
	}
	if s.failAt == "duplicate" {
		return &models.User{ID: uuid.New()}, nil
	}
	return nil, nil
}
func (s *registrationTextStore) CreateTenant(_ context.Context, tenant *models.Tenant) error {
	s.tenantCalls++
	if s.failAt == "tenant" {
		return errors.New("synthetic tenant failure")
	}
	if err := registrationTextDBError(tenant.Name); err != nil {
		return err
	}
	tenant.ID = uuid.New()
	copy := *tenant
	s.tenant = &copy
	return nil
}
func (s *registrationTextStore) CreateUser(_ context.Context, user *models.User) error {
	s.userCalls++
	if s.failAt == "user" {
		return errors.New("synthetic user failure")
	}
	if err := registrationTextDBError(user.Email); err != nil {
		return err
	}
	if err := registrationTextDBError(user.DisplayName); err != nil {
		return err
	}
	user.ID = uuid.New()
	copy := *user
	s.user = &copy
	return nil
}
func (s *registrationTextStore) CreateRefreshToken(_ context.Context, token *models.RefreshToken) error {
	s.tokenCalls++
	if s.failAt == "token" {
		return errors.New("synthetic token failure")
	}
	copy := *token
	s.token = &copy
	return nil
}
func registrationTextRequest(t *testing.T, h *AuthHandler, email, name string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]string{"email": email, "display_name": name, "password": "synthetic-password"})
	if err != nil {
		t.Fatal(err)
	}
	if len(body) >= maxAuthBodyBytes {
		t.Fatal("text fixture must fit existing body budget")
	}
	r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", strings.NewReader(string(body)))
	w := httptest.NewRecorder()
	h.Register(w, r)
	return w
}
func TestContinueRegistrationPersistentTextRejectsBeforeStorage(t *testing.T) {
	for _, tc := range []struct{ name, email, display string }{
		{"email-ascii-256", strings.Repeat("a", 256), "ok"},
		{"email-unicode-256", strings.Repeat("中", 256), "ok"},
		{"email-emoji-256", strings.Repeat("😀", 256), "ok"},
		{"email-normalized-still-over", " \t" + strings.Repeat("A", 256) + "\n", "ok"},
		{"email-nul-prefix", "\x00user@example.test", "ok"},
		{"email-nul-middle", "user\x00@example.test", "ok"},
		{"email-nul-suffix", "user@example.test\x00", "ok"},
		{"display-ascii-256", "user@example.test", strings.Repeat("a", 256)},
		{"display-unicode-256", "user@example.test", strings.Repeat("中", 256)},
		{"display-emoji-256", "user@example.test", strings.Repeat("😀", 256)},
		{"display-combining-256", "user@example.test", strings.Repeat("e\u0301", 128)},
		{"display-normalized-still-over", "user@example.test", " \t" + strings.Repeat("中", 256) + "\n"},
		{"display-nul-prefix", "user@example.test", "\x00name"},
		{"display-nul-middle", "user@example.test", "na\x00me"},
		{"display-nul-suffix", "user@example.test", "name\x00"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := &registrationTextStore{}
			h := newAuthBodyLimitHandler(t, st)
			w := registrationTextRequest(t, h, tc.email, tc.display)
			var response envelope
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if w.Code != http.StatusBadRequest || response.Error == nil || response.Error.Code != "BAD_REQUEST" {
				t.Errorf("invalid persisted text status=%d body=%s", w.Code, w.Body.String())
			}
			if st.lookups != 0 || st.tenantCalls != 0 || st.userCalls != 0 || st.tokenCalls != 0 || st.tenant != nil || st.user != nil || st.token != nil || len(w.Result().Cookies()) != 0 {
				t.Errorf("invalid text crossed storage: lookups=%d tenants=%d users=%d tokens=%d tenant_retained=%v cookies=%d", st.lookups, st.tenantCalls, st.userCalls, st.tokenCalls, st.tenant != nil, len(w.Result().Cookies()))
			}
		})
	}
}
func TestContinueRegistrationPersistentTextPreservesValidIdentity(t *testing.T) {
	for _, tc := range []struct{ name, email, display, wantEmail, wantDisplay string }{
		{"ordinary", "user@example.test", "Name", "user@example.test", "Name"},
		{"normalized", " \tUSER@Example.TEST\n", " \tName\u2003", "user@example.test", "Name"},
		{"display-ascii-255", "user@example.test", strings.Repeat("a", 255), "user@example.test", strings.Repeat("a", 255)},
		{"display-unicode-255", "user@example.test", strings.Repeat("中", 255), "user@example.test", strings.Repeat("中", 255)},
		{"display-emoji-255", "user@example.test", strings.Repeat("😀", 255), "user@example.test", strings.Repeat("😀", 255)},
		{"display-combining-255", "user@example.test", strings.Repeat("e\u0301", 127) + "e", "user@example.test", strings.Repeat("e\u0301", 127) + "e"},
		{"trim-before-length", "user@example.test", " " + strings.Repeat("中", 255) + " ", "user@example.test", strings.Repeat("中", 255)},
		{"email-ascii-255", strings.Repeat("A", 242) + "@example.test", "Name", strings.Repeat("a", 242) + "@example.test", "Name"},
		{"email-emoji-255", strings.Repeat("😀", 242) + "@example.test", "Name", strings.Repeat("😀", 242) + "@example.test", "Name"},
		{"default-display", "User@Example.TEST", " \t\n", "user@example.test", "user"},
		{"default-display-long-local", strings.Repeat("中", 242) + "@example.test", "", strings.Repeat("中", 242) + "@example.test", strings.Repeat("中", 242)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := &registrationTextStore{}
			h := newAuthBodyLimitHandler(t, st)
			w := registrationTextRequest(t, h, tc.email, tc.display)
			if w.Code != http.StatusCreated || st.lookups != 1 || st.tenantCalls != 1 || st.userCalls != 1 || st.tokenCalls != 1 || st.tenant == nil || st.user == nil || st.token == nil {
				t.Fatalf("valid text rejected: status=%d calls=%d/%d/%d/%d", w.Code, st.lookups, st.tenantCalls, st.userCalls, st.tokenCalls)
			}
			if st.lookupEmail != tc.wantEmail || st.tenant.Name != tc.wantEmail || st.user.Email != tc.wantEmail || st.user.DisplayName != tc.wantDisplay || st.user.TenantID != st.tenant.ID {
				t.Fatal("normalized persistent identity changed")
			}
			if err := bcrypt.CompareHashAndPassword([]byte(st.user.PasswordHash), []byte("synthetic-password")); err != nil {
				t.Fatal(err)
			}
			var response struct {
				Data struct {
					AccessToken string `json:"access_token"`
					User        struct {
						Email       string `json:"email"`
						DisplayName string `json:"display_name"`
					} `json:"user"`
				} `json:"data"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			claims, err := authn.VerifyAccessToken("synthetic-secret", response.Data.AccessToken)
			cookies := w.Result().Cookies()
			if err != nil || claims.UserID != st.user.ID || claims.TenantID != st.tenant.ID || response.Data.User.Email != tc.wantEmail || response.Data.User.DisplayName != tc.wantDisplay || len(cookies) != 1 || cookies[0].Name != RefreshCookieName || st.token.TokenHash != authn.HashToken(cookies[0].Value) || st.token.UserID != st.user.ID {
				t.Fatal("real response/access token/refresh pairing changed")
			}
		})
	}
}
func TestContinueRegistrationPersistentTextKeepsFailureBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name                                    string
		status, lookups, tenants, users, tokens int
	}{
		{"lookup", 500, 1, 0, 0, 0}, {"duplicate", 409, 1, 0, 0, 0}, {"tenant", 500, 1, 1, 0, 0}, {"user", 500, 1, 1, 1, 0}, {"token", 500, 1, 1, 1, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := &registrationTextStore{failAt: tc.name}
			h := newAuthBodyLimitHandler(t, st)
			w := registrationTextRequest(t, h, "user@example.test", "Name")
			if w.Code != tc.status || st.lookups != tc.lookups || st.tenantCalls != tc.tenants || st.userCalls != tc.users || st.tokenCalls != tc.tokens || len(w.Result().Cookies()) != 0 {
				t.Errorf("store error boundary changed: status=%d calls=%d/%d/%d/%d", w.Code, st.lookups, st.tenantCalls, st.userCalls, st.tokenCalls)
			}
		})
	}
}
