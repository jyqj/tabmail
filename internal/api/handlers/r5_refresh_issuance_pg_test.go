package handlers_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/api/handlers"
	"tabmail/internal/authn"
	"tabmail/internal/models"
	"tabmail/internal/store/postgres"
)

// No SQL emulates a rotation or account mutation. The wrapper exposes the
// interval after PgStore committed and before Refresh reads the current user.
type r5RefreshIssuancePGStore struct {
	*postgres.PgStore
	afterRotate func()
	readError   error
	nextHash    string
}

func (s *r5RefreshIssuancePGStore) RotateRefreshToken(ctx context.Context, hash string, next *models.RefreshToken) (bool, bool, error) {
	rotated, revoked, err := s.PgStore.RotateRefreshToken(ctx, hash, next)
	if rotated && err == nil {
		s.nextHash = next.TokenHash
		if s.afterRotate != nil {
			s.afterRotate()
		}
	}
	return rotated, revoked, err
}

func (s *r5RefreshIssuancePGStore) GetUser(ctx context.Context, id uuid.UUID) (*models.User, error) {
	if s.readError != nil {
		err := s.readError
		s.readError = nil
		return nil, err
	}
	return s.PgStore.GetUser(ctx, id)
}

func r5RefreshIssuancePGSeed(t *testing.T) (*r5LoginIssuanceFixture, *r5RefreshIssuancePGStore, string, *handlers.AuthHandler) {
	t.Helper()
	f := r5LoginIssuanceSeed(t)
	raw, hash, err := authn.GenerateRefreshToken()
	if err != nil {
		t.Fatal(err)
	}
	if err := f.st.CreateRefreshToken(t.Context(), &models.RefreshToken{UserID: f.user.ID, TokenHash: hash, ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	s := &r5RefreshIssuancePGStore{PgStore: f.st}
	h := handlers.NewAuthHandler(s, r5LoginIssuanceSecret, uuid.Nil, false, nil, true, zerolog.Nop())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := h.StopContext(ctx); err != nil {
			t.Errorf("refresh handler cleanup: %v", err)
		}
	})
	return f, s, raw, h
}

func TestR5RefreshIssuancePostgresRejectsPostCommitAccountChange(t *testing.T) {
	for _, change := range []string{"password", "freeze", "freeze_unfreeze", "role_change", "deleted_user"} {
		t.Run(change, func(t *testing.T) {
			f, s, raw, h := r5RefreshIssuancePGSeed(t)
			s.afterRotate = func() {
				ctx := t.Context()
				switch change {
				case "password":
					if err := f.st.ChangePasswordAtomic(ctx, f.user.ID, f.user.PasswordHash, "synthetic-new-refresh-password"); err != nil {
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
			}
			w := r5RefreshIssuanceCall(h, raw)
			if w.Code != http.StatusUnauthorized || strings.Contains(w.Body.String(), "access_token") {
				if w.Code == http.StatusOK {
					claims := r5RefreshIssuanceAccess(t, w, r5LoginIssuanceSecret)
					current, err := f.st.GetUser(t.Context(), f.user.ID)
					if err != nil || current == nil || claims.SessionVersion != current.SessionVersion {
						t.Fatalf("baseline did not demonstrate a newly usable current-session token: %v", err)
					}
					t.Logf("vulnerable response signed current session %d after committed %s", claims.SessionVersion, change)
				}
				t.Errorf("post-commit account change issued status=%d / access_token=%v", w.Code, strings.Contains(w.Body.String(), "access_token"))
			}
			if c := r5RefreshIssuanceLastCookie(t, w); c.MaxAge >= 0 || c.Value != "" {
				t.Error("rejected post-commit authentication left an active browser cookie")
			}
			var active int
			if err := f.pool.QueryRow(t.Context(), `SELECT count(*) FROM refresh_tokens WHERE user_id=$1 AND revoked_at IS NULL`, f.user.ID).Scan(&active); err != nil {
				t.Fatal(err)
			}
			if active != 0 {
				t.Errorf("rejected refresh left %d active descendants", active)
			}
		})
	}
}

func TestR5RefreshIssuancePostgresPreservesCurrentAndRetry(t *testing.T) {
	for _, mode := range []string{"unchanged", "display_name", "read_failure"} {
		t.Run(mode, func(t *testing.T) {
			f, s, raw, h := r5RefreshIssuancePGSeed(t)
			if mode == "display_name" {
				s.afterRotate = func() {
					name := "Current refresh display name"
					if _, err := f.st.UpdateUserGuarded(t.Context(), f.admin, f.tenant.ID, f.user.ID, models.UserAdminPatch{DisplayName: &name}); err != nil {
						t.Fatal(err)
					}
				}
			}
			if mode == "read_failure" {
				s.readError = errors.New("private post-rotation read diagnostic")
			}
			w := r5RefreshIssuanceCall(h, raw)
			if mode == "read_failure" {
				if w.Code != http.StatusInternalServerError || strings.Contains(w.Body.String(), "access_token") || strings.Contains(w.Body.String(), "private") {
					t.Fatalf("transient read failure returned %d %s", w.Code, w.Body.String())
				}
				cookie := r5RefreshIssuanceLastCookie(t, w)
				if cookie.MaxAge <= 0 || cookie.Value == "" || cookie.Value == raw {
					t.Fatal("committed rotation lost recoverable cookie")
				}
				w = r5RefreshIssuanceCall(h, cookie.Value)
			}
			if w.Code != http.StatusOK {
				t.Fatalf("current refresh status=%d %s", w.Code, w.Body.String())
			}
			claims := r5RefreshIssuanceAccess(t, w, r5LoginIssuanceSecret)
			if claims.UserID != f.user.ID || claims.TenantID != f.tenant.ID || claims.SessionVersion != f.user.SessionVersion {
				t.Fatal("current refresh signed another user, company or session")
			}
			if mode == "display_name" && !strings.Contains(w.Body.String(), "Current refresh display name") {
				t.Fatal("non-credential profile update was lost")
			}
			cookie := r5RefreshIssuanceLastCookie(t, w)
			if cookie.MaxAge <= 0 || authn.HashToken(cookie.Value) != s.nextHash {
				t.Fatal("HTTP descendant does not match committed token")
			}
			if strings.Contains(w.Body.String(), f.user.PasswordHash) || strings.Contains(w.Body.String(), "issuance") {
				t.Fatal("ephemeral authentication proof leaked through the response")
			}
			stored, err := f.st.GetRefreshToken(t.Context(), s.nextHash)
			if err != nil || stored == nil || stored.Issuance != nil || stored.UserID != f.user.ID || stored.RevokedAt != nil {
				t.Fatal("persisted rotation result or ephemeral proof boundary changed")
			}
		})
	}
}

func TestR5RefreshIssuancePostgresRotationFailurePreservesAncestor(t *testing.T) {
	f, _, raw, h := r5RefreshIssuancePGSeed(t)
	if _, err := f.pool.Exec(t.Context(), `ALTER TABLE refresh_tokens ADD CONSTRAINT r5_refresh_reject_new CHECK(false) NOT VALID`); err != nil {
		t.Fatal(err)
	}
	w := r5RefreshIssuanceCall(h, raw)
	if w.Code != http.StatusInternalServerError || len(w.Result().Cookies()) != 0 || strings.Contains(w.Body.String(), "access_token") {
		t.Fatalf("rolled-back rotation changed browser credentials: %d", w.Code)
	}
	old, err := f.st.GetRefreshToken(t.Context(), authn.HashToken(raw))
	if err != nil || old == nil || old.RevokedAt != nil {
		t.Fatal("failed rotation consumed ancestor")
	}
}
