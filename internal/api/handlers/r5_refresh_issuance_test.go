package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/api/handlers"
	"tabmail/internal/authn"
	"tabmail/internal/models"
	"tabmail/internal/testutil"
)

// The adapter performs the real in-memory atomic rotation before exposing the
// already-committed/read boundary. The same file runs on the original source.
type r5RefreshIssuanceUnitStore struct {
	*testutil.FakeStore
	afterRotate func(*models.RefreshToken)
	readError   error
	nextHash    string
}

func (s *r5RefreshIssuanceUnitStore) RotateRefreshToken(ctx context.Context, hash string, next *models.RefreshToken) (bool, bool, error) {
	rotated, revoked, err := s.FakeStore.RotateRefreshToken(ctx, hash, next)
	if rotated && err == nil {
		s.nextHash = next.TokenHash
		if s.afterRotate != nil {
			s.afterRotate(next)
		}
	}
	return rotated, revoked, err
}

func (s *r5RefreshIssuanceUnitStore) GetUser(ctx context.Context, id uuid.UUID) (*models.User, error) {
	if s.readError != nil {
		err := s.readError
		s.readError = nil
		return nil, err
	}
	return s.FakeStore.GetUser(ctx, id)
}

func r5RefreshIssuanceUnitSeed(t *testing.T) (*r5RefreshIssuanceUnitStore, *models.User, string, *handlers.AuthHandler) {
	t.Helper()
	s := &r5RefreshIssuanceUnitStore{FakeStore: testutil.NewFakeStore()}
	u := &models.User{ID: uuid.New(), TenantID: uuid.New(), Email: "refresh@fixture.test", PasswordHash: "synthetic-password-hash", Role: models.RoleUser, IsActive: true, SessionVersion: 7}
	if err := s.CreateUser(t.Context(), u); err != nil {
		t.Fatal(err)
	}
	raw, hash, err := authn.GenerateRefreshToken()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateRefreshToken(t.Context(), &models.RefreshToken{UserID: u.ID, TokenHash: hash, ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	h := handlers.NewAuthHandler(s, "r5-refresh-unit-secret", uuid.Nil, false, nil, true, zerolog.Nop())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := h.StopContext(ctx); err != nil {
			t.Errorf("refresh handler cleanup: %v", err)
		}
	})
	return s, u, raw, h
}

func r5RefreshIssuanceCall(h *handlers.AuthHandler, raw string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	r.AddCookie(&http.Cookie{Name: handlers.RefreshCookieName, Value: raw})
	w := httptest.NewRecorder()
	h.Refresh(w, r)
	return w
}

func r5RefreshIssuanceLastCookie(t *testing.T, w *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	var found *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == handlers.RefreshCookieName {
			found = c
		}
	}
	if found == nil {
		t.Fatal("refresh response omitted browser cookie state")
	}
	if !found.HttpOnly || !found.Secure || found.Path != "/api/v1/auth" || found.SameSite != http.SameSiteLaxMode {
		t.Fatal("refresh cookie security attributes changed")
	}
	return found
}

func r5RefreshIssuanceAccess(t *testing.T, w *httptest.ResponseRecorder, secret string) *authn.AccessClaims {
	t.Helper()
	var response struct {
		Data struct {
			AccessToken string `json:"access_token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	claims, err := authn.VerifyAccessToken(secret, response.Data.AccessToken)
	if err != nil {
		t.Fatalf("refresh did not issue a verifiable access token: %v", err)
	}
	return claims
}

func TestR5RefreshIssuanceUnitRejectsChangedAuthentication(t *testing.T) {
	for _, change := range []string{"password", "session", "freeze", "freeze_unfreeze", "tenant"} {
		t.Run(change, func(t *testing.T) {
			s, u, raw, h := r5RefreshIssuanceUnitSeed(t)
			s.afterRotate = func(*models.RefreshToken) {
				current := *u
				switch change {
				case "password":
					current.PasswordHash = "synthetic-replacement-hash"
				case "session":
					current.SessionVersion++
				case "freeze":
					current.IsActive = false
					current.SessionVersion++
				case "freeze_unfreeze":
					current.SessionVersion += 2
				case "tenant":
					current.TenantID = uuid.New()
				}
				if err := s.UpdateUser(t.Context(), &current); err != nil {
					t.Fatal(err)
				}
			}
			w := r5RefreshIssuanceCall(h, raw)
			if w.Code != http.StatusUnauthorized || strings.Contains(w.Body.String(), "access_token") {
				t.Errorf("changed authentication produced status=%d / access_token=%v", w.Code, strings.Contains(w.Body.String(), "access_token"))
			}
			if c := r5RefreshIssuanceLastCookie(t, w); c.MaxAge >= 0 || c.Value != "" {
				t.Error("changed authentication left the descendant cookie active")
			}
			next, err := s.GetRefreshToken(t.Context(), s.nextHash)
			if err != nil || next == nil || next.RevokedAt == nil {
				t.Errorf("rejected rotation left a usable descendant: row=%v error=%v", next != nil, err)
			}
		})
	}
}

func TestR5RefreshIssuanceUnitCurrentIdentityAndReadRecovery(t *testing.T) {
	for _, mode := range []string{"unchanged", "display_name", "read_failure"} {
		t.Run(mode, func(t *testing.T) {
			s, u, raw, h := r5RefreshIssuanceUnitSeed(t)
			if mode == "display_name" {
				s.afterRotate = func(*models.RefreshToken) {
					current := *u
					current.DisplayName = "Current display name"
					if err := s.UpdateUser(t.Context(), &current); err != nil {
						t.Fatal(err)
					}
				}
			}
			if mode == "read_failure" {
				s.readError = errors.New("synthetic private read diagnostic")
			}
			w := r5RefreshIssuanceCall(h, raw)
			if mode == "read_failure" {
				if w.Code != http.StatusInternalServerError || strings.Contains(w.Body.String(), "synthetic private") || strings.Contains(w.Body.String(), "access_token") {
					t.Fatalf("committed rotation read failure response=%d %s", w.Code, w.Body.String())
				}
				cookie := r5RefreshIssuanceLastCookie(t, w)
				if cookie.MaxAge <= 0 || cookie.Value == "" || cookie.Value == raw {
					t.Fatal("committed rotation did not preserve recoverable descendant")
				}
				w = r5RefreshIssuanceCall(h, cookie.Value)
			}
			if w.Code != http.StatusOK {
				t.Fatalf("current refresh failed: %d %s", w.Code, w.Body.String())
			}
			claims := r5RefreshIssuanceAccess(t, w, "r5-refresh-unit-secret")
			if claims.UserID != u.ID || claims.TenantID != u.TenantID || claims.SessionVersion != u.SessionVersion {
				t.Fatal("current refresh signed a different user, company, or session")
			}
			if mode == "display_name" && !strings.Contains(w.Body.String(), "Current display name") {
				t.Fatal("unrelated profile update was discarded")
			}
			if strings.Contains(w.Body.String(), "synthetic-password-hash") || strings.Contains(w.Body.String(), "issuance") {
				t.Fatal("rotation proof leaked through the HTTP response")
			}
			persisted, err := s.GetRefreshToken(t.Context(), s.nextHash)
			if err != nil || persisted == nil || persisted.Issuance != nil {
				t.Fatal("ephemeral authentication proof became stored token data")
			}
		})
	}
}

func TestR5RefreshIssuanceUnitMissingProofFailsClosed(t *testing.T) {
	s, _, raw, h := r5RefreshIssuanceUnitSeed(t)
	s.afterRotate = func(next *models.RefreshToken) { next.Issuance = nil }
	w := r5RefreshIssuanceCall(h, raw)
	if w.Code != http.StatusInternalServerError || strings.Contains(w.Body.String(), "access_token") {
		t.Fatalf("unproved rotation issued access: %d %s", w.Code, w.Body.String())
	}
}
