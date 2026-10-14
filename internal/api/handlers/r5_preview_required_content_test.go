package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/api/middleware"
	"tabmail/internal/app/templates"
	"tabmail/internal/company"
	"tabmail/internal/models"
)

func TestR5PreviewRequiredContentHTTP(t *testing.T) {
	for _, tc := range []struct {
		name, subject, text, html, message string
		status                             int
	}{
		{"subject_omitted", "{{.value}}", "private-rendered-body", "", "subject is required", http.StatusBadRequest},
		{"body_sanitized_away", "private-rendered-subject", "", "<script>alert(1)</script>", "text_body or html_body required", http.StatusBadRequest},
		{"html_only", "Subject", "", "<p>Body</p>", "", http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newOutboundAccessFixture(t)
			repo := &r5CrossHomePreviewRepo{store: f.st, mailbox: models.Mailbox{ID: uuid.New(), TenantID: f.platformAdmin.TenantID, FullAddress: "selected@company.test"}}
			draft := company.TemplateDraft{Subject: tc.subject, TextBody: tc.text, HTMLBody: tc.html, Variables: []company.Variable{{Name: "value", Type: "text", MaxLength: 100}}}
			payload, err := json.Marshal(templates.PreviewInput{Mailbox: repo.mailbox.ID, Draft: &draft})
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodPost, "/api/v1/company/templates/preview", bytes.NewReader(payload))
			for key, value := range outboundUserHeaders(t, f.platformAdmin) {
				req.Header.Set(key, value)
			}
			req.Header.Set("X-Tenant-ID", f.platformAdmin.TenantID.String())
			h := NewCompanyTemplateHandler(repo, repo, zerolog.Nop())
			state := middleware.NewAuthState(nil)
			w := httptest.NewRecorder()
			middleware.Auth(f.st, outboundTestJWTSecret, publicTenantIDForTests, state)(middleware.RequireAuth(http.HandlerFunc(h.Preview))).ServeHTTP(w, req)
			if err := state.StopContext(context.Background()); err != nil {
				t.Fatal(err)
			}
			if w.Code != tc.status {
				t.Fatalf("preview HTTP status=%d want=%d body=%s", w.Code, tc.status, w.Body.String())
			}
			if tc.status == http.StatusBadRequest {
				if !strings.Contains(w.Body.String(), tc.message) || strings.Contains(w.Body.String(), "private-rendered") {
					t.Fatalf("invalid preview omitted its error or released partial content: %s", w.Body.String())
				}
			} else {
				var response struct {
					Data templates.Rendered `json:"data"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
					t.Fatal(err)
				}
				if response.Data.Subject != "Subject" || response.Data.TextBody != "" || response.Data.HTMLBody != "<p>Body</p>" {
					t.Fatalf("valid HTML-only preview changed: %+v", response.Data)
				}
			}
		})
	}
}
