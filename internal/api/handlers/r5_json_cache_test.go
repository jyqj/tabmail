package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
)

func requireJSONNoStore(t *testing.T, rr *httptest.ResponseRecorder, status int) {
	t.Helper()
	if rr.Code != status {
		t.Fatalf("status = %d, want %d", rr.Code, status)
	}
	if rr.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("JSON response cache policy = %q, want no-store", rr.Header().Get("Cache-Control"))
	}
	if rr.Header().Get("Content-Type") != "application/json; charset=utf-8" || !json.Valid(rr.Body.Bytes()) {
		t.Fatal("JSON response encoding changed")
	}
}

// These exercise the actual handlers, authentication middleware and content
// service. Repositories and object bytes are synthetic; no PostgreSQL claim.
func TestR5JSONSensitiveCompanyResponsesDoNotStore(t *testing.T) {
	t.Run("parsed-message", func(t *testing.T) {
		raw := []byte("From: sender@example.test\r\nSubject: fixture\r\nContent-Type: text/plain\r\n\r\nprivate fixture body")
		h, f, repo, _ := sourceHTTPFixture(t, []sourceHTTPStep{{data: raw}})
		rr := doOutboundHandlerRequest(t, f.st, h.Message, http.MethodGet, "/api/v1/company/mailboxes/fixture/messages/fixture", map[string]string{
			"id": repo.message.MailboxID.String(), "message": repo.message.ID.String(),
		}, outboundUserHeaders(t, f.userA))
		requireJSONNoStore(t, rr, http.StatusOK)
		if !strings.Contains(rr.Body.String(), "private fixture body") || strings.Contains(rr.Body.String(), "raw_object_key") {
			t.Fatal("authorized content or protected storage projection changed")
		}
	})
	t.Run("submission-receipt", func(t *testing.T) {
		f := newOutboundAccessFixture(t)
		h := NewCompanyMailHandler(&capabilitiesSubmissionStub{}, nil, nil, zerolog.Nop())
		rr := doOutboundHandlerRequest(t, f.st, h.Submission, http.MethodGet, "/api/v1/company/submissions/fixture", map[string]string{"id": uuid.NewString()}, outboundUserHeaders(t, f.userA))
		requireJSONNoStore(t, rr, http.StatusOK)
		if !strings.Contains(rr.Body.String(), `"status":"needs_attention"`) {
			t.Fatal("receipt status projection changed")
		}
	})
	t.Run("submission-page", func(t *testing.T) {
		f := newOutboundAccessFixture(t)
		h := NewCompanyMailHandler(&capabilitiesSubmissionStub{}, nil, nil, zerolog.Nop())
		rr := doOutboundHandlerRequest(t, f.st, h.Submissions, http.MethodGet, "/api/v1/company/submissions?page=2&per_page=5", nil, outboundUserHeaders(t, f.userA))
		requireJSONNoStore(t, rr, http.StatusOK)
		var body struct {
			Meta meta `json:"meta"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil || body.Meta != (meta{Total: 0, Page: 2, PerPage: 5}) {
			t.Fatal("pagination envelope changed")
		}
	})
}

type r5CacheReceiptFailure struct {
	capabilitiesSubmissionStub
	err error
}

func (s *r5CacheReceiptFailure) GetSubmission(context.Context, authz.Actor, uuid.UUID) (*company.Submission, error) {
	return nil, s.err
}

func TestR5JSONCompanyErrorsDoNotStore(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"forbidden", app.Forbidden("mailbox read permission required"), 403, "FORBIDDEN"},
		{"not-found", app.NotFound("receipt not found"), 404, "NOT_FOUND"},
		{"conflict", app.Conflict("receipt changed; reload"), 409, "CONFLICT"},
		{"internal", app.Internal(errors.New("private database diagnostic")), 500, "INTERNAL"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newOutboundAccessFixture(t)
			h := NewCompanyMailHandler(&r5CacheReceiptFailure{err: tc.err}, nil, nil, zerolog.Nop())
			rr := doOutboundHandlerRequest(t, f.st, h.Submission, http.MethodGet, "/api/v1/company/submissions/fixture", map[string]string{"id": uuid.NewString()}, outboundUserHeaders(t, f.userA))
			requireJSONNoStore(t, rr, tc.status)
			if !strings.Contains(rr.Body.String(), tc.code) || strings.Contains(rr.Body.String(), "private database") {
				t.Fatal("public application error contract changed")
			}
		})
	}
	t.Run("invalid-id", func(t *testing.T) {
		f := newOutboundAccessFixture(t)
		h := NewCompanyMailHandler(nil, nil, nil, zerolog.Nop())
		rr := doOutboundHandlerRequest(t, f.st, h.Submission, http.MethodGet, "/api/v1/company/submissions/invalid", map[string]string{"id": "invalid"}, outboundUserHeaders(t, f.userA))
		requireJSONNoStore(t, rr, http.StatusBadRequest)
	})
}

func TestR5JSONSessionResponsesDoNotStore(t *testing.T) {
	t.Run("current-user", func(t *testing.T) {
		f := newOutboundAccessFixture(t)
		h := newAuthBodyLimitHandler(t, nil)
		rr := doOutboundHandlerRequest(t, f.st, h.Me, http.MethodGet, "/api/v1/auth/me", nil, outboundUserHeaders(t, f.userA))
		requireJSONNoStore(t, rr, http.StatusOK)
		if !strings.Contains(rr.Body.String(), f.userA.Email) || strings.Contains(rr.Body.String(), "password") {
			t.Fatal("session identity projection changed")
		}
	})
	t.Run("absent-user", func(t *testing.T) {
		h := newAuthBodyLimitHandler(t, nil)
		rr := httptest.NewRecorder()
		h.Me(rr, httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil))
		requireJSONNoStore(t, rr, http.StatusUnauthorized)
	})
	for _, valid := range []bool{true, false} {
		t.Run(map[bool]string{true: "rotated-token", false: "rejected-token"}[valid], func(t *testing.T) {
			st := &authBodyLimitStore{rotationSucceeds: valid, user: &models.User{ID: uuid.New(), TenantID: uuid.New(), IsActive: true, Email: "user@example.test"}}
			if valid {
				st.user.PasswordHash = "synthetic-cache-policy-hash"
			}
			h := newAuthBodyLimitHandler(t, st)
			rr := httptest.NewRecorder()
			h.Refresh(rr, httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", strings.NewReader(`{"refresh_token":"synthetic-token"}`)))
			status := http.StatusUnauthorized
			if valid {
				status = http.StatusOK
			}
			requireJSONNoStore(t, rr, status)
			if st.rotations != 1 || len(rr.Result().Cookies()) != 1 {
				t.Fatal("refresh rotation/cookie behavior changed")
			}
			if valid != strings.Contains(rr.Body.String(), `"access_token"`) {
				t.Fatal("access token success/error boundary changed")
			}
		})
	}
}

func TestR5JSONExplicitPolicyAndProjectionStayCompatible(t *testing.T) {
	for _, policy := range []string{"private, no-store", "private, no-store, no-transform", "public, max-age=60"} {
		t.Run(policy, func(t *testing.T) {
			rr := httptest.NewRecorder()
			rr.Header().Set("Cache-Control", policy)
			rr.Header().Set("Location", "/synthetic/result")
			draft := &company.Template{Draft: company.TemplateDraft{}}
			created(rr, draft)
			if rr.Code != http.StatusCreated || rr.Header().Get("Cache-Control") != policy || rr.Header().Get("Location") != "/synthetic/result" {
				t.Fatal("explicit policy, location or created status changed")
			}
			if !strings.Contains(rr.Body.String(), `"variables":[]`) || draft.Draft.Variables != nil {
				t.Fatal("collection projection mutated domain value")
			}
		})
	}
}
