package company

import (
	"strings"
	"testing"

	"tabmail/internal/app"
)

func TestR5TemplateOptionsRejectUnusableChoices(t *testing.T) {
	for _, tc := range []struct {
		name, kind, good, bad string
		required              bool
	}{
		{"email_display_name", "email", "user@example.test", "User <user@example.test>", false},
		{"email_invalid", "email", "user@example.test", "not-an-email", false},
		{"integer_decimal", "integer", "42", "1.2", false},
		{"integer_overflow", "integer", "42", "9223372036854775808", false},
		{"date_invalid_day", "date", "2026-10-07", "2026-02-31", false},
		{"date_invalid_format", "date", "2026-10-07", "07/10/2026", false},
		{"url_scheme", "url", "https://example.test", "javascript:alert(1)", false},
		{"url_missing_host", "url", "https://example.test", "https:///path", false},
		{"url_credentials", "url", "https://example.test", "https://user:secret@example.test", false},
		{"text_required_empty", "text", "good", "", true},
		{"text_required_whitespace", "text", "good", " \t\n", true},
		{"email_required_empty", "email", "user@example.test", "", true},
		{"email_required_whitespace", "email", "user@example.test", " ", true},
		{"integer_required_empty", "integer", "42", "", true},
		{"integer_required_whitespace", "integer", "42", "\t", true},
		{"date_required_empty", "date", "2026-10-07", "", true},
		{"date_required_whitespace", "date", "2026-10-07", "\n", true},
		{"url_required_empty", "url", "https://example.test", "", true},
		{"url_required_whitespace", "url", "https://example.test", "\u3000", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			draft := TemplateDraft{Subject: "Choices", TextBody: "{{.value}}", Variables: []Variable{{Name: "value", Type: tc.kind, Required: tc.required, MaxLength: 100, Options: []string{tc.good, tc.bad}}}}
			if err := ValidateTemplate(draft); err == nil {
				t.Error("template publication accepted an unusable declared choice")
			} else if e, ok := app.As(err); !ok || e.Kind != app.KindBadRequest {
				t.Errorf("validation error = %v, want bad request", err)
			}
			// An unusable definition is rejected consistently even when the
			// current preview/send happened to select its other, valid choice.
			subject, text, html, err := Render(draft, map[string]string{"value": tc.good}, "Employee", "Company", "sender@example.test")
			if err == nil || subject != "" || text != "" || html != "" {
				t.Errorf("invalid definition produced output: subject=%q text=%q html=%q err=%v", subject, text, html, err)
			}
		})
	}
}

func TestR5TemplateOptionsEveryValidChoiceRenders(t *testing.T) {
	for _, tc := range []struct {
		name, kind string
		required   bool
		options    []string
	}{
		{"text", "text", true, []string{"Alice", "中文", "line one\nline two"}},
		{"email", "email", true, []string{"user@example.test", "user+tag@example.test"}},
		{"integer", "integer", true, []string{"0", "+42", "-42", "9223372036854775807", "-9223372036854775808"}},
		{"date", "date", true, []string{"2026-10-07", "2024-02-29"}},
		{"url", "url", true, []string{"http://example.test/path", "https://example.test/path?q=1#part"}},
		{"optional_text", "text", false, []string{"", " ", "中文"}},
		{"optional_email", "email", false, []string{"", "user@example.test"}},
		{"optional_integer", "integer", false, []string{"", "0"}},
		{"optional_date", "date", false, []string{"", "2026-10-07"}},
		{"optional_url", "url", false, []string{"", "https://example.test"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			draft := TemplateDraft{Subject: "Choices", TextBody: "{{.value}}", Variables: []Variable{{Name: "value", Type: tc.kind, Required: tc.required, MaxLength: 100, Options: tc.options}}}
			if err := ValidateTemplate(draft); err != nil {
				t.Fatalf("valid template rejected: %v", err)
			}
			for _, option := range tc.options {
				subject, text, html, err := Render(draft, map[string]string{"value": option}, "Employee", "Company", "sender@example.test")
				if err != nil || subject != "Choices" || text != option || html != "" {
					t.Errorf("choice %q rendered (%q, %q, %q, %v)", option, subject, text, html, err)
				}
			}
			if !tc.required {
				if _, text, _, err := Render(draft, nil, "Employee", "Company", "sender@example.test"); err != nil || text != "" {
					t.Errorf("omitted optional choice rendered %q: %v", text, err)
				}
			}
		})
	}
}

func TestR5TemplateOptionsPreserveScalarBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, option string
		maxLength    int
		wantError    string
	}{
		{"unicode_at_limit", "中文", 2, ""},
		{"unicode_over_limit", "中文字", 2, "oversized variable option"},
		{"invalid_utf8", "\xff", 2, "invalid variable option encoding"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			draft := TemplateDraft{Subject: "Choices", TextBody: "{{.value}}", Variables: []Variable{{Name: "value", Type: "text", Required: true, MaxLength: tc.maxLength, Options: []string{tc.option}}}}
			err := ValidateTemplate(draft)
			if tc.wantError == "" {
				if err != nil {
					t.Fatal(err)
				}
				if _, text, _, err := Render(draft, map[string]string{"value": tc.option}, "Employee", "Company", "sender@example.test"); err != nil || text != tc.option {
					t.Fatalf("valid bounded Unicode changed: %q %v", text, err)
				}
			} else if e, ok := app.As(err); !ok || e.Kind != app.KindBadRequest || e.Message != tc.wantError {
				t.Fatalf("error = %v, want %q", err, tc.wantError)
			}
		})
	}
}

func TestR5TemplateOptionsRemainDataAndEnforceMembership(t *testing.T) {
	option := "<script>alert(1)</script>{{.employee_name}}"
	draft := TemplateDraft{Subject: "Choices", TextBody: "{{.value}}", HTMLBody: "<p>{{.value}}</p>", Variables: []Variable{{Name: "value", Type: "text", MaxLength: 100, Options: []string{option}}}}
	if err := ValidateTemplate(draft); err != nil {
		t.Fatal(err)
	}
	subject, text, html, err := Render(draft, map[string]string{"value": option}, "Employee", "Company", "sender@example.test")
	if err != nil || subject != "Choices" || text != option || !strings.Contains(html, "&lt;script&gt;") || !strings.Contains(html, "{{.employee_name}}") || strings.Contains(html, "<script>") {
		t.Fatalf("option was not safely treated as data: %q %q %q %v", subject, text, html, err)
	}
	if _, _, _, err := Render(draft, map[string]string{"value": "not an allowed option"}, "Employee", "Company", "sender@example.test"); err == nil {
		t.Fatal("out-of-enum value accepted")
	}
	// Optional omission remains allowed even when the enum has no empty item.
	if _, text, _, err := Render(draft, nil, "Employee", "Company", "sender@example.test"); err != nil || text != "" {
		t.Fatalf("optional omission = %q, %v", text, err)
	}
}

func TestR5TemplateOptionsDoNotBypassRenderedSubjectSafety(t *testing.T) {
	option := "value\r\nBcc: hidden@example.test"
	draft := TemplateDraft{Subject: "{{.value}}", TextBody: "body", Variables: []Variable{{Name: "value", Type: "text", Required: true, MaxLength: 100, Options: []string{option}}}}
	// Newlines are valid text data (for body variables), but never valid in a
	// rendered subject. Publication choice typing must not weaken that gate.
	subject, text, html, err := Render(draft, map[string]string{"value": option}, "Employee", "Company", "sender@example.test")
	if e, ok := app.As(err); !ok || e.Kind != app.KindBadRequest || e.Message != "invalid rendered subject" || subject != "" || text != "" || html != "" {
		t.Fatalf("header injection gate changed: %q %q %q %v", subject, text, html, err)
	}
}
