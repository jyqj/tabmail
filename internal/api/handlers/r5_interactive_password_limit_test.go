package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"tabmail/internal/api/middleware"
	"tabmail/internal/authn"
	"tabmail/internal/models"
)

// These are the real interactive handlers, bcrypt implementation and JWT
// middleware. Only persistence is a spy; no PostgreSQL transaction is modeled.
type interactivePasswordStore struct {
	authStore
	user                   *models.User
	lookupErr              error
	lookups, changes       int
	touches                atomic.Int32
	tokens                 []*models.RefreshToken
	expectedHash, nextHash string
}

func (s *interactivePasswordStore) GetUserByEmail(context.Context, string) (*models.User, error) {
	s.lookups++
	return s.user, s.lookupErr
}

func (s *interactivePasswordStore) CreateRefreshToken(_ context.Context, token *models.RefreshToken) error {
	s.tokens = append(s.tokens, token)
	return nil
}

func (s *interactivePasswordStore) TouchUserLogin(context.Context, uuid.UUID) error {
	s.touches.Add(1)
	return nil
}

func (s *interactivePasswordStore) ChangePasswordAtomic(_ context.Context, _ uuid.UUID, expected, next string) error {
	s.changes++
	s.expectedHash, s.nextHash = expected, next
	return nil
}

func interactivePasswordUser(t *testing.T, password string) *models.User {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	return &models.User{ID: uuid.New(), TenantID: uuid.New(), Email: "credential-boundary@example.test", Role: models.RoleUser, IsActive: true, PasswordHash: string(hash)}
}

func interactivePasswordRequest(t *testing.T, route, password string) *http.Request {
	t.Helper()
	body := map[string]string{"email": "credential-boundary@example.test", "password": password}
	if route == "change-password" {
		body = map[string]string{"old_password": password, "new_password": "synthetic-new-password"}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return httptest.NewRequest(http.MethodPost, "/api/v1/auth/"+route, bytes.NewReader(raw))
}

func TestR5InteractivePasswordByteLimit(t *testing.T) {
	ascii, chinese, emoji := strings.Repeat("a", 72), strings.Repeat("中", 24), strings.Repeat("😀", 18)
	for _, route := range []string{"login", "change-password"} {
		for _, tc := range []struct {
			name, stored, supplied string
			valid                  bool
		}{
			{"ascii_72", ascii, ascii, true},
			{"ascii_73", ascii, ascii + "x", false},
			{"ascii_long_suffix", ascii, ascii + strings.Repeat("x", 128), false},
			{"chinese_72", chinese, chinese, true},
			{"chinese_73", chinese, chinese + "x", false},
			{"emoji_72", emoji, emoji, true},
			{"emoji_73", emoji, emoji + "x", false},
			{"legacy_short", "short", "short", true},
			{"legacy_short_unicode", "短", "短", true},
			{"legacy_spaces_preserved", "  short  ", "  short  ", true},
			{"wrong_within_limit", ascii, "b" + ascii[1:], false},
			{"empty", ascii, "", false},
		} {
			t.Run(route+"/"+tc.name, func(t *testing.T) {
				st := &interactivePasswordStore{user: interactivePasswordUser(t, tc.stored)}
				h := newAuthBodyLimitHandler(t, st)
				r := interactivePasswordRequest(t, route, tc.supplied)
				w := httptest.NewRecorder()
				if route == "login" {
					h.Login(w, r)
				} else {
					token, err := authn.IssueAccessToken("synthetic-secret", st.user)
					if err != nil {
						t.Fatal(err)
					}
					r.Header.Set("Authorization", "Bearer "+token)
					middleware.Auth(authBodyIdentityStore{user: st.user}, "synthetic-secret", "", middleware.NewAuthState(h.background))(http.HandlerFunc(h.ChangePassword)).ServeHTTP(w, r)
				}
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				if err := h.StopContext(ctx); err != nil {
					t.Fatal(err)
				}
				var result struct {
					Data struct {
						AccessToken string `json:"access_token"`
					} `json:"data"`
					Error *apiErr `json:"error"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				claims, tokenErr := authn.VerifyAccessToken("synthetic-secret", result.Data.AccessToken)
				signedAccess := tokenErr == nil && claims.UserID == st.user.ID
				cookies := w.Result().Cookies()
				if !tc.valid {
					status, code, message := http.StatusUnauthorized, "UNAUTHORIZED", "invalid email or password"
					if route == "change-password" {
						status, code, message = http.StatusForbidden, "INVALID_PASSWORD", "incorrect old password"
					}
					if tc.supplied == "" {
						status, code = http.StatusBadRequest, "BAD_REQUEST"
						message = "email and password are required"
						if route == "change-password" {
							message = "old_password and new_password are required"
						}
					}
					if w.Code != status || result.Error == nil || result.Error.Code != code || result.Error.Message != message || len(st.tokens) != 0 || st.touches.Load() != 0 || st.changes != 0 || len(cookies) != 0 || result.Data.AccessToken != "" {
						t.Fatalf("invalid credential accepted or caused effects: supplied_bytes=%d status=%d tokens=%d touches=%d changes=%d cookies=%d signed_access=%v", len(tc.supplied), w.Code, len(st.tokens), st.touches.Load(), st.changes, len(cookies), signedAccess)
					}
					return
				}
				if w.Code != http.StatusOK || result.Error != nil || len(cookies) != 1 || cookies[0].Name != RefreshCookieName {
					t.Fatalf("valid credential rejected: bytes=%d status=%d cookies=%d", len(tc.supplied), w.Code, len(cookies))
				}
				if route == "login" {
					if !signedAccess || len(st.tokens) != 1 || st.tokens[0].TokenHash != authn.HashToken(cookies[0].Value) || st.touches.Load() != 1 || st.changes != 0 {
						t.Fatal("valid login did not issue the real token pair and login touch")
					}
				} else if st.changes != 1 || st.expectedHash != st.user.PasswordHash || bcrypt.CompareHashAndPassword([]byte(st.nextHash), []byte("synthetic-new-password")) != nil || cookies[0].MaxAge >= 0 || len(st.tokens) != 0 || st.touches.Load() != 0 {
					t.Fatal("valid old credential did not reach atomic change with the expected/new hashes and clear cookie")
				}
			})
		}
	}
}

func TestR5OverlongLoginDoesNotRevealAccountState(t *testing.T) {
	for _, state := range []string{"missing", "disabled", "lookup_failure"} {
		t.Run(state, func(t *testing.T) {
			st := &interactivePasswordStore{}
			if state == "disabled" {
				st.user = interactivePasswordUser(t, strings.Repeat("a", 72))
				st.user.IsActive = false
			} else if state == "lookup_failure" {
				st.lookupErr = errors.New("synthetic lookup failure")
			}
			h := newAuthBodyLimitHandler(t, st)
			w := httptest.NewRecorder()
			h.Login(w, interactivePasswordRequest(t, "login", strings.Repeat("a", 73)))
			var result envelope
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if w.Code != http.StatusUnauthorized || result.Error == nil || result.Error.Code != "UNAUTHORIZED" || result.Error.Message != "invalid email or password" || st.lookups != 0 || len(st.tokens) != 0 || st.touches.Load() != 0 || st.changes != 0 || len(w.Result().Cookies()) != 0 {
				t.Fatalf("overlong credential depended on account lookup: status=%d lookups=%d tokens=%d touches=%d changes=%d cookies=%d", w.Code, st.lookups, len(st.tokens), st.touches.Load(), st.changes, len(w.Result().Cookies()))
			}
		})
	}
}
