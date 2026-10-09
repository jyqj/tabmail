package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"tabmail/internal/api/middleware"
	"tabmail/internal/models"
	"tabmail/internal/testutil"
)

// Run the production policy handler, application service, JWT authentication
// and platform-administrator gate over the stateful persistence boundary.
// These tests do not claim PostgreSQL transaction or SMTP network coverage.
type r5SMTPPolicyIntentStore struct {
	*testutil.FakeStore
	writes, audits, invalidations int
	failure                       error
}

func (s *r5SMTPPolicyIntentStore) UpsertSMTPPolicy(ctx context.Context, p *models.SMTPPolicy) error {
	s.writes++
	if s.failure != nil {
		return s.failure
	}
	return s.FakeStore.UpsertSMTPPolicy(ctx, p)
}

func (s *r5SMTPPolicyIntentStore) InsertAudit(ctx context.Context, entry *models.AuditEntry) error {
	s.audits++
	return s.FakeStore.InsertAudit(ctx, entry)
}

func (s *r5SMTPPolicyIntentStore) InvalidateSMTPPolicy() { s.invalidations++ }

func r5SMTPPolicyCompleteInput() map[string]any {
	return map[string]any{
		"default_accept": true, "default_store": true,
		"accept_domains":        []string{"accept.fixture.test"},
		"reject_domains":        []string{"reject.fixture.test"},
		"store_domains":         []string{"store.fixture.test"},
		"discard_domains":       []string{"discard.fixture.test"},
		"reject_origin_domains": []string{"origin.fixture.test"},
	}
}

func r5SMTPPolicyJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func r5SMTPPolicyIntentRequest(t *testing.T, body, role string, failure error) (*httptest.ResponseRecorder, *r5SMTPPolicyIntentStore, *models.SMTPPolicy) {
	t.Helper()
	f := newOutboundAccessFixture(t)
	st := &r5SMTPPolicyIntentStore{FakeStore: f.st, failure: failure}
	initial := &models.SMTPPolicy{
		DefaultAccept: true, DefaultStore: true,
		AcceptDomains: []string{"old-accept.fixture.test"}, RejectDomains: []string{"old-reject.fixture.test"},
		StoreDomains: []string{"old-store.fixture.test"}, DiscardDomains: []string{"old-discard.fixture.test"},
		RejectOriginDomains: []string{"old-origin.fixture.test"},
	}
	if err := st.FakeStore.UpsertSMTPPolicy(t.Context(), initial); err != nil {
		t.Fatal(err)
	}
	before, err := st.FakeStore.GetSMTPPolicy(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	h := NewAdminHandler(st, nil, models.SMTPPolicy{}, &stubSettings{}, st, zerolog.Nop())
	user := f.platformAdmin
	if role == "tenant" {
		user = f.tenantAdmin
	} else if role == "member" {
		user = f.userA
	}
	r := httptest.NewRequest(http.MethodPatch, "/api/v1/admin/policy", strings.NewReader(body))
	for key, value := range outboundUserHeaders(t, user) {
		r.Header.Set(key, value)
	}
	state := middleware.NewAuthState(nil)
	t.Cleanup(func() {
		if err := state.StopContext(context.Background()); err != nil {
			t.Error(err)
		}
	})
	router := chi.NewRouter()
	router.Use(middleware.Auth(st, outboundTestJWTSecret, publicTenantIDForTests, state))
	router.With(middleware.RequireSuperAdmin).Patch("/api/v1/admin/policy", h.UpdateSMTPPolicy)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	return w, st, before
}

func r5SMTPPolicyAssertRejected(t *testing.T, body string) {
	t.Helper()
	w, st, before := r5SMTPPolicyIntentRequest(t, body, "super", nil)
	if w.Code != http.StatusBadRequest {
		t.Errorf("incomplete or malformed replacement status=%d, want=400", w.Code)
	}
	after, err := st.FakeStore.GetSMTPPolicy(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if st.writes != 0 || st.audits != 0 || st.invalidations != 0 || !reflect.DeepEqual(before, after) {
		t.Errorf("rejected policy request changed state: writes=%d audits=%d invalidations=%d policy_changed=%t", st.writes, st.audits, st.invalidations, !reflect.DeepEqual(before, after))
	}
}

func TestR5SMTPPolicyReplacementRequiresExplicitFields(t *testing.T) {
	t.Run("whole_null", func(t *testing.T) { r5SMTPPolicyAssertRejected(t, "null") })
	t.Run("empty_object", func(t *testing.T) { r5SMTPPolicyAssertRejected(t, "{}") })
	for _, field := range []string{"default_accept", "default_store", "accept_domains", "reject_domains", "store_domains", "discard_domains", "reject_origin_domains"} {
		for _, absent := range []string{"omitted", "null"} {
			t.Run(field+"/"+absent, func(t *testing.T) {
				input := r5SMTPPolicyCompleteInput()
				if absent == "omitted" {
					delete(input, field)
				} else {
					input[field] = nil
				}
				r5SMTPPolicyAssertRejected(t, r5SMTPPolicyJSON(t, input))
			})
		}
	}
}

func TestR5SMTPPolicyReplacementRejectsMalformedValues(t *testing.T) {
	for _, field := range []string{"accept_domains", "reject_domains", "store_domains", "discard_domains", "reject_origin_domains"} {
		t.Run(field+"/null_element", func(t *testing.T) {
			input := r5SMTPPolicyCompleteInput()
			input[field] = []any{"valid.fixture.test", nil}
			r5SMTPPolicyAssertRejected(t, r5SMTPPolicyJSON(t, input))
		})
	}
	for _, tc := range []struct {
		name, field string
		value       any
	}{
		{"string_bool", "default_accept", "false"},
		{"integer_bool", "default_store", 0},
		{"scalar_list", "reject_domains", "reject.fixture.test"},
		{"object_list", "store_domains", map[string]string{}},
		{"unknown_field", "unknown", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := r5SMTPPolicyCompleteInput()
			input[tc.field] = tc.value
			r5SMTPPolicyAssertRejected(t, r5SMTPPolicyJSON(t, input))
		})
	}
	t.Run("trailing_document", func(t *testing.T) {
		r5SMTPPolicyAssertRejected(t, r5SMTPPolicyJSON(t, r5SMTPPolicyCompleteInput())+" null")
	})
	t.Run("top_level_array", func(t *testing.T) { r5SMTPPolicyAssertRejected(t, "[]") })
}

func TestR5SMTPPolicyReplacementPreservesExplicitValues(t *testing.T) {
	for _, mode := range []string{"complete_lists", "explicit_reject_discard", "explicit_accept_store", "explicit_accept_discard", "legacy_response_timestamp"} {
		t.Run(mode, func(t *testing.T) {
			input := r5SMTPPolicyCompleteInput()
			if strings.HasPrefix(mode, "explicit_") {
				for _, field := range []string{"accept_domains", "reject_domains", "store_domains", "discard_domains", "reject_origin_domains"} {
					input[field] = []string{}
				}
				input["default_accept"] = mode != "explicit_reject_discard"
				input["default_store"] = mode == "explicit_accept_store"
			}
			if mode == "legacy_response_timestamp" {
				input["updated_at"] = "2001-02-03T04:05:06Z"
			}
			body := r5SMTPPolicyJSON(t, input)
			var want models.SMTPPolicy
			if err := json.Unmarshal([]byte(body), &want); err != nil {
				t.Fatal(err)
			}
			w, st, _ := r5SMTPPolicyIntentRequest(t, body, "super", nil)
			if w.Code != http.StatusOK || st.writes != 1 || st.audits != 1 || st.invalidations != 1 {
				t.Fatalf("explicit replacement failed: status=%d writes=%d audits=%d invalidations=%d body=%s", w.Code, st.writes, st.audits, st.invalidations, w.Body.String())
			}
			got, err := st.FakeStore.GetSMTPPolicy(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if got.DefaultAccept != want.DefaultAccept || got.DefaultStore != want.DefaultStore ||
				!slices.Equal(got.AcceptDomains, want.AcceptDomains) || !slices.Equal(got.RejectDomains, want.RejectDomains) ||
				!slices.Equal(got.StoreDomains, want.StoreDomains) || !slices.Equal(got.DiscardDomains, want.DiscardDomains) ||
				!slices.Equal(got.RejectOriginDomains, want.RejectOriginDomains) || got.UpdatedAt.IsZero() || got.UpdatedAt.Year() == 2001 {
				t.Errorf("explicit replacement values or server timestamp changed: got=%+v", got)
			}
		})
	}
}

func TestR5SMTPPolicyReplacementAuthorizationAndStorageFailure(t *testing.T) {
	for _, tc := range []struct {
		name, role string
		status     int
		failure    error
	}{
		{"tenant_admin", "tenant", http.StatusForbidden, nil},
		{"member", "member", http.StatusForbidden, nil},
		{"storage_error", "super", http.StatusInternalServerError, errors.New("private policy persistence diagnostic")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, st, before := r5SMTPPolicyIntentRequest(t, r5SMTPPolicyJSON(t, r5SMTPPolicyCompleteInput()), tc.role, tc.failure)
			after, err := st.FakeStore.GetSMTPPolicy(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			wantWrites := 0
			if tc.failure != nil {
				wantWrites = 1
				if strings.Contains(w.Body.String(), tc.failure.Error()) {
					t.Error("internal storage diagnostic leaked in response")
				}
			}
			if w.Code != tc.status || st.writes != wantWrites || st.audits != 0 || st.invalidations != 0 || !reflect.DeepEqual(before, after) {
				t.Errorf("authorization/storage boundary changed: status=%d writes=%d audits=%d invalidations=%d policy_changed=%t", w.Code, st.writes, st.audits, st.invalidations, !reflect.DeepEqual(before, after))
			}
		})
	}
}
