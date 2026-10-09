package company

import (
	"strings"
	"testing"

	"tabmail/internal/app"
)

func requireTemplateNULRejection(t *testing.T, err error) {
	t.Helper()
	v, ok := app.As(err)
	if !ok || v.Kind != app.KindBadRequest {
		t.Fatalf("embedded NUL must be a client error, got %v", err)
	}
}

func TestR5TemplateNULDefinition(t *testing.T) {
	for _, field := range []string{"subject", "text", "html", "unused_option"} {
		t.Run(field, func(t *testing.T) {
			draft := TemplateDraft{Subject: "Subject", TextBody: "Body", HTMLBody: "<p>Body</p>", Variables: []Variable{{Name: "value", Type: "text", MaxLength: 40}}}
			switch field {
			case "subject":
				draft.Subject = "before\x00after"
			case "text":
				draft.TextBody = "before\x00after"
			case "html":
				draft.HTMLBody = "<p>before\x00after</p>"
			case "unused_option":
				draft.Variables[0].Options = []string{"before\x00after"}
			}
			requireTemplateNULRejection(t, ValidateTemplate(draft))
			subject, text, html, err := Render(draft, nil, "Employee", "Company", "sender@example.test")
			requireTemplateNULRejection(t, err)
			if subject != "" || text != "" || html != "" {
				t.Fatal("rejected definition released rendered content")
			}
		})
	}
}

func TestR5TemplateNULValues(t *testing.T) {
	for _, field := range []string{"value", "employee_name", "company_name", "sender_address"} {
		for _, referenced := range []bool{false, true} {
			name := field + "/unused"
			if referenced {
				name = field + "/referenced"
			}
			t.Run(name, func(t *testing.T) {
				draft := TemplateDraft{Subject: "Subject", TextBody: "Body", Variables: []Variable{{Name: "value", Type: "text", MaxLength: 40}}}
				if referenced {
					draft.TextBody = "{{." + field + "}}"
					draft.HTMLBody = "<p>{{." + field + "}}</p>"
				}
				values := map[string]string{"value": "Value", "employee_name": "Employee", "company_name": "Company", "sender_address": "sender@example.test"}
				values[field] += "\x00suffix"
				subject, text, html, err := Render(draft, map[string]string{"value": values["value"]}, values["employee_name"], values["company_name"], values["sender_address"])
				requireTemplateNULRejection(t, err)
				if subject != "" || text != "" || html != "" {
					t.Fatal("rejected input released partial content")
				}
			})
		}
	}
}

func TestR5TemplateNULLegalTextControls(t *testing.T) {
	for _, value := range []string{"中文😀", "false", "0", "line one\r\nline two\tend", `literal \u0000`, "replacement �"} {
		t.Run(value, func(t *testing.T) {
			draft := TemplateDraft{Subject: "通知", TextBody: "{{.value}}", HTMLBody: "<p>{{.value}}</p>", Variables: []Variable{{Name: "value", Type: "text", Required: true, MaxLength: 40, Options: []string{value}}}}
			if err := ValidateTemplate(draft); err != nil {
				t.Fatalf("legal definition rejected: %v", err)
			}
			subject, text, html, err := Render(draft, map[string]string{"value": value}, "员工", "公司", "员工@example.test")
			// The existing HTML parser normalizes CRLF; plain text retains it.
			if err != nil || subject != "通知" || text != value || !strings.Contains(html, strings.ReplaceAll(value, "\r\n", "\n")) {
				t.Fatalf("legal text changed: subject=%q text=%q html=%q error=%v", subject, text, html, err)
			}
		})
	}
}
