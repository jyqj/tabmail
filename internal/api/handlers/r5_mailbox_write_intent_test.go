package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
)

// Exercise the shipping HTTP handlers. The repository spy represents an
// existing mailbox with a policy/grant; an omitted field must never reach its
// full replacement command, even when the observed revision is valid.
type finishMailboxIntentRepo struct {
	company.MailboxAdminService
	calls    int
	policy   *string
	grant    models.MailboxGrant
	revision int64
	err      error
}

func (s *finishMailboxIntentRepo) SetWorkMailboxSendPolicy(_ context.Context, _ authz.Actor, _ uuid.UUID, policy *string, revision int64) error {
	s.calls++
	s.policy, s.revision = policy, revision
	return s.err
}

func (s *finishMailboxIntentRepo) SetWorkGrant(_ context.Context, _ authz.Actor, grant models.MailboxGrant, revision int64) error {
	s.calls++
	s.grant, s.revision = grant, revision
	return s.err
}

func finishMailboxIntentRequest(t *testing.T, endpoint, body string, repo *finishMailboxIntentRepo) *httptest.ResponseRecorder {
	t.Helper()
	h := NewMailboxAdminHandler(repo, zerolog.Nop())
	router := chi.NewRouter()
	router.Put("/mailboxes/{id}/send-policy", h.MailboxSendPolicy)
	router.Put("/mailboxes/{id}/grants", h.Grant)
	req := httptest.NewRequest(http.MethodPut, "/mailboxes/22222222-2222-4222-8222-222222222222/"+endpoint, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestR5MailboxSendPolicyExplicitIntent(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		policy     *string
	}{
		{"omitted-policy", `{"revision":7}`, 400, nil},
		{"empty-body", `{}`, 400, nil},
		{"null-body", `null`, 400, nil},
		{"explicit-inherit", `{"revision":7,"send_policy":null}`, 200, nil},
		{"empty-inherit", `{"revision":7,"send_policy":""}`, 200, new(string)},
		{"disabled", `{"revision":7,"send_policy":"disabled"}`, 200, finishString("disabled")},
		{"template", `{"revision":7,"send_policy":"template_required"}`, 200, finishString("template_required")},
		{"free", `{"revision":7,"send_policy":"free"}`, 200, finishString("free")},
		{"wrong-type", `{"revision":7,"send_policy":false}`, 400, nil},
		{"missing-revision", `{"send_policy":null}`, 400, nil},
		{"zero-revision", `{"revision":0,"send_policy":null}`, 400, nil},
		{"unknown-field", `{"revision":7,"send_policy":null,"unexpected":true}`, 400, nil},
		{"trailing-document", `{"revision":7,"send_policy":null} {}`, 400, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &finishMailboxIntentRepo{}
			w := finishMailboxIntentRequest(t, "send-policy", tc.body, repo)
			if w.Code != tc.status {
				t.Errorf("status=%d want=%d body=%s", w.Code, tc.status, w.Body.String())
			}
			if tc.status != 200 {
				if repo.calls != 0 {
					t.Errorf("invalid intent reached %d replacement commands", repo.calls)
				}
				return
			}
			if repo.calls != 1 || repo.revision != 7 || (repo.policy == nil) != (tc.policy == nil) || repo.policy != nil && *repo.policy != *tc.policy {
				t.Errorf("explicit policy/revision changed: calls=%d revision=%d policy=%v", repo.calls, repo.revision, repo.policy)
			}
		})
	}
}

func finishString(value string) *string { return &value }

func finishGrantBody() map[string]any {
	return map[string]any{"revision": int64(7), "user_id": "33333333-3333-4333-8333-333333333333", "can_read": true, "can_organize": false, "can_send": true, "template_only": true}
}

func TestR5MailboxGrantExplicitIntent(t *testing.T) {
	for _, field := range []string{"can_read", "can_organize", "can_send", "template_only"} {
		for _, absent := range []string{"omitted", "null"} {
			t.Run(field+"/"+absent, func(t *testing.T) {
				body := finishGrantBody()
				if absent == "omitted" {
					delete(body, field)
				} else {
					body[field] = nil
				}
				raw, err := json.Marshal(body)
				if err != nil {
					t.Fatal(err)
				}
				repo := &finishMailboxIntentRepo{}
				w := finishMailboxIntentRequest(t, "grants", string(raw), repo)
				if w.Code != 400 || repo.calls != 0 {
					t.Errorf("incomplete grant: status=%d writes=%d body=%s", w.Code, repo.calls, w.Body.String())
				}
			})
		}
	}
	for _, grant := range []struct {
		name                           string
		read, organize, send, template bool
	}{
		{"explicit-revoke", false, false, false, false},
		{"reader", true, false, false, false},
		{"organizer", true, true, false, false},
		{"template-sender", false, false, true, true},
		{"free-sender", false, false, true, false},
		{"combined", true, true, true, true},
	} {
		t.Run(grant.name, func(t *testing.T) {
			body := finishGrantBody()
			body["can_read"], body["can_organize"], body["can_send"], body["template_only"] = grant.read, grant.organize, grant.send, grant.template
			raw, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			repo := &finishMailboxIntentRepo{}
			w := finishMailboxIntentRequest(t, "grants", string(raw), repo)
			g := repo.grant
			if w.Code != 200 || repo.calls != 1 || repo.revision != 7 || g.UserID.String() != body["user_id"] || g.CanRead != grant.read || g.CanOrganize != grant.organize || g.CanSend != grant.send || g.TemplateOnly != grant.template {
				t.Errorf("explicit full grant changed: status=%d calls=%d grant=%+v", w.Code, repo.calls, g)
			}
		})
	}
}

func TestR5MailboxWriteIntentKeepsRepositoryRefusals(t *testing.T) {
	for _, endpoint := range []string{"send-policy", "grants"} {
		for _, tc := range []struct {
			name   string
			err    error
			status int
		}{
			{"stale", app.Conflict("reload mailbox"), 409},
			{"scope", app.Forbidden("outside management scope"), 403},
			{"missing", app.NotFound("mailbox not found"), 404},
			{"storage", errors.New("private database diagnostic"), 500},
		} {
			t.Run(endpoint+"/"+tc.name, func(t *testing.T) {
				body := `{"revision":7,"send_policy":null}`
				if endpoint == "grants" {
					raw, err := json.Marshal(finishGrantBody())
					if err != nil {
						t.Fatal(err)
					}
					body = string(raw)
				}
				repo := &finishMailboxIntentRepo{err: tc.err}
				w := finishMailboxIntentRequest(t, endpoint, body, repo)
				if w.Code != tc.status || repo.calls != 1 {
					t.Errorf("status=%d want=%d calls=%d", w.Code, tc.status, repo.calls)
				}
				if strings.Contains(w.Body.String(), "private database") {
					t.Fatal("database diagnostic leaked")
				}
			})
		}
	}
}
