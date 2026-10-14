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

// These requests execute the signed-JWT middleware, shipping template Preview,
// and real Render validator. Only the authority/identity repository is synthetic.
func TestR5TemplateNULPreviewHTTP(t *testing.T) {
	for _, field := range []string{"subject", "text", "html", "option", "value", "employee", "valid_unicode"} {
		t.Run(field, func(t *testing.T) {
			f := newOutboundAccessFixture(t)
			user := f.platformAdmin
			user.DisplayName = "员工"
			if field == "employee" {
				user.DisplayName += "\x00private-suffix"
			}
			if err := f.st.CreateUser(context.Background(), user); err != nil {
				t.Fatal(err)
			}
			repo := &r5CrossHomePreviewRepo{store: f.st, mailbox: models.Mailbox{ID: uuid.New(), TenantID: f.tenantID, FullAddress: "selected@company.test"}}
			draft := company.TemplateDraft{Subject: "通知", TextBody: "{{.value}}", HTMLBody: "<p>{{.value}}</p>", Variables: []company.Variable{{Name: "value", Type: "text", MaxLength: 40}}}
			in := templates.PreviewInput{Mailbox: repo.mailbox.ID, Draft: &draft, Vars: map[string]string{"value": "中文😀"}}
			switch field {
			case "subject":
				draft.Subject += "\x00private-suffix"
			case "text":
				draft.TextBody += "\x00private-suffix"
			case "html":
				draft.HTMLBody += "\x00private-suffix"
			case "option":
				draft.Variables[0].Options = []string{"中文😀", "\x00private-suffix"}
			case "value":
				in.Vars["value"] += "\x00private-suffix"
			}
			body, err := json.Marshal(in)
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodPost, "/api/v1/company/templates/preview", bytes.NewReader(body))
			for key, value := range outboundUserHeaders(t, user) {
				req.Header.Set(key, value)
			}
			req.Header.Set("X-Tenant-ID", f.tenantID.String())
			h := NewCompanyTemplateHandler(repo, repo, zerolog.Nop())
			state := middleware.NewAuthState(nil)
			defer state.StopContext(context.Background())
			w := httptest.NewRecorder()
			middleware.Auth(f.st, outboundTestJWTSecret, publicTenantIDForTests, state)(middleware.RequireAuth(http.HandlerFunc(h.Preview))).ServeHTTP(w, req)
			if field == "valid_unicode" {
				if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "中文😀") {
					t.Fatalf("valid preview rejected: status=%d body=%s", w.Code, w.Body.String())
				}
				return
			}
			if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), `"code":"BAD_REQUEST"`) {
				t.Fatalf("NUL preview status=%d body=%s", w.Code, w.Body.String())
			}
			if strings.Contains(w.Body.String(), "private-suffix") || strings.Contains(w.Body.String(), `"subject"`) {
				t.Fatal("rejected preview exposed rendered input")
			}
		})
	}
}
