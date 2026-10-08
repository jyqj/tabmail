package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/app"
	"tabmail/internal/app/templates"
	"tabmail/internal/authz"
	"tabmail/internal/company"
)

// The active template handlers and service run unchanged. Only their final
// persistence commands are observed; this does not model PG authorization or CAS.
type templateBooleanIntentRepo struct {
	templates.Repository
	retired, enabled        bool
	retireCalls, grantCalls int
	templateID              uuid.UUID
	revision                int
	grant                   company.TemplateGrant
	err                     error
}

func (s *templateBooleanIntentRepo) SetMailTemplateRetired(_ context.Context, _ authz.Actor, id uuid.UUID, revision int, retired bool) error {
	s.retireCalls++
	s.templateID, s.revision = id, revision
	if s.err == nil {
		s.retired = retired
	}
	return s.err
}

func (s *templateBooleanIntentRepo) SetTemplateGrant(_ context.Context, _ authz.Actor, grant company.TemplateGrant, enabled bool) error {
	s.grantCalls++
	s.grant = grant
	if s.err == nil {
		s.enabled = enabled
	}
	return s.err
}

func templateBooleanIntentRequest(t *testing.T, repo *templateBooleanIntentRepo, route, body string, id uuid.UUID) *httptest.ResponseRecorder {
	t.Helper()
	h := NewCompanyTemplateHandler(repo, nil, zerolog.Nop())
	router := chi.NewRouter()
	router.Post("/company/templates/{id}/retire", h.Retire)
	router.Put("/company/templates/{id}/grants", h.TemplateGrant)
	method := http.MethodPost
	if route == "grants" {
		method = http.MethodPut
	}
	r := httptest.NewRequest(method, "/company/templates/"+id.String()+"/"+route, strings.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	return w
}

func TestR5TemplateMutationsRequireExplicitBoolean(t *testing.T) {
	for _, route := range []string{"retire", "grants"} {
		field := "retired"
		prefix := `"revision":7`
		if route == "grants" {
			field = "enabled"
			prefix = fmt.Sprintf(`"mailbox_id":%q,"user_id":%q`, uuid.NewString(), uuid.NewString())
		}
		for _, tc := range []struct{ name, body string }{
			{"omitted", "{" + prefix + "}"},
			{"null", "{" + prefix + fmt.Sprintf(`,%q:null}`, field)},
			{"whole_null", "null"},
			{"empty_object", "{}"},
			{"number", "{" + prefix + fmt.Sprintf(`,%q:0}`, field)},
			{"string", "{" + prefix + fmt.Sprintf(`,%q:"false"}`, field)},
			{"array", "{" + prefix + fmt.Sprintf(`,%q:[]}`, field)},
			{"unknown", "{" + prefix + fmt.Sprintf(`,%q:false,"unknown":true}`, field)},
			{"trailing_document", "{" + prefix + fmt.Sprintf(`,%q:false} null`, field)},
		} {
			t.Run(route+"/"+tc.name, func(t *testing.T) {
				repo := &templateBooleanIntentRepo{retired: true, enabled: true}
				w := templateBooleanIntentRequest(t, repo, route, tc.body, uuid.New())
				var response envelope
				if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
					t.Fatal(err)
				}
				if w.Code != http.StatusBadRequest || response.Error == nil || response.Error.Code != "BAD_REQUEST" || repo.retireCalls != 0 || repo.grantCalls != 0 || !repo.retired || !repo.enabled {
					t.Fatalf("ambiguous template intent caused a mutation: status=%d retire_calls=%d grant_calls=%d retired=%v enabled=%v", w.Code, repo.retireCalls, repo.grantCalls, repo.retired, repo.enabled)
				}
			})
		}
	}
}

func TestR5TemplateMutationsKeepExplicitFalseAndTrue(t *testing.T) {
	for _, route := range []string{"retire", "grants"} {
		for _, value := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%v", route, value), func(t *testing.T) {
				id, mailbox, user := uuid.New(), uuid.New(), uuid.New()
				repo := &templateBooleanIntentRepo{retired: !value, enabled: !value}
				body := fmt.Sprintf(`{"revision":7,"retired":%v}`, value)
				if route == "grants" {
					body = fmt.Sprintf(`{"mailbox_id":%q,"user_id":%q,"enabled":%v}`, mailbox, user, value)
				}
				w := templateBooleanIntentRequest(t, repo, route, body, id)
				if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"updated":true`) {
					t.Fatalf("explicit boolean rejected: status=%d", w.Code)
				}
				if route == "retire" {
					if repo.retireCalls != 1 || repo.grantCalls != 0 || repo.templateID != id || repo.revision != 7 || repo.retired != value {
						t.Fatal("retire command lost its explicit value, identity, or revision")
					}
				} else if repo.grantCalls != 1 || repo.retireCalls != 0 || repo.grant.TemplateID != id || repo.grant.MailboxID != mailbox || repo.grant.UserID != user || repo.enabled != value {
					t.Fatal("grant command lost its explicit value or identity")
				}
			})
		}
	}
}

func TestR5TemplateMutationBooleanKeepsRepositoryFailures(t *testing.T) {
	for _, route := range []string{"retire", "grants"} {
		for _, tc := range []struct {
			name   string
			err    error
			status int
		}{
			{"conflict", app.Conflict("synthetic changed revision"), http.StatusConflict},
			{"forbidden", app.Forbidden("synthetic revoked authority"), http.StatusForbidden},
			{"storage", errors.New("synthetic persistence failure"), http.StatusInternalServerError},
		} {
			t.Run(route+"/"+tc.name, func(t *testing.T) {
				repo := &templateBooleanIntentRepo{retired: true, enabled: true, err: tc.err}
				body := `{"revision":7,"retired":false}`
				if route == "grants" {
					body = fmt.Sprintf(`{"mailbox_id":%q,"user_id":%q,"enabled":false}`, uuid.New(), uuid.New())
				}
				w := templateBooleanIntentRequest(t, repo, route, body, uuid.New())
				if w.Code != tc.status || repo.retireCalls+repo.grantCalls != 1 || !repo.retired || !repo.enabled || strings.Contains(w.Body.String(), `"updated":true`) {
					t.Fatalf("repository failure changed: status=%d calls=%d", w.Code, repo.retireCalls+repo.grantCalls)
				}
			})
		}
	}
}
