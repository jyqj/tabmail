package company

import (
	"html"
	"strings"
	"testing"

	"tabmail/internal/app"
)

var advanceTemplateEmailCases = []struct {
	name, value string
	valid       bool
}{
	{"ordinary", "user@example.test", true},
	{"plus", "user+tag@example.test", true},
	{"unicode", "用户@例子.测试", true},
	{"address_literal", "user@[127.0.0.1]", true},
	{"quoted_comma", `"ops,team"@example.test`, true},
	{"quoted_at", `"ops@team"@example.test`, true},
	{"quoted_space", `"ops team"@example.test`, true},
	{"quoted_quote", `"ops\"team"@example.test`, true},
	{"quoted_backslash", `"ops\\team"@example.test`, true},
	{"quoted_html", `"<script>"@example.test`, true},
	{"redundant_quotes", `"user"@example.test`, true},
	{"quoted_unicode", `"用户 名"@example.test`, true},
	{"display_name", "User <user@example.test>", false},
	{"empty_display_name", `"" <user@example.test>`, false},
	{"angle_wrapped", "<user@example.test>", false},
	{"group", "users:user@example.test;", false},
	{"list", "a@example.test,b@example.test", false},
	{"comment", "user@example.test (User)", false},
	{"leading_space", " user@example.test", false},
	{"trailing_space", "user@example.test ", false},
	{"domain_space", "user@ example.test", false},
	{"domain_tab", "user@\texample.test", false},
	{"quoted_domain_fws", `"ops@team"@ example.test`, false},
	{"crlf", "\"ops\r\nteam\"@example.test", false},
	{"nul", "user\x00@example.test", false},
	{"missing_domain", "user@", false},
}

func advanceTemplateEmailDraft(options []string) TemplateDraft {
	return TemplateDraft{
		Subject: "Contact", TextBody: "{{.contact}}", HTMLBody: "<p>{{.contact}}</p>\n",
		Variables: []Variable{{Name: "contact", Type: "email", Required: true, MaxLength: 100, Options: options}},
	}
}

func TestAdvanceTemplateEmailVariableKeepsMailboxSpelling(t *testing.T) {
	for _, tc := range advanceTemplateEmailCases {
		t.Run(tc.name, func(t *testing.T) {
			draft := advanceTemplateEmailDraft(nil)
			subject, text, body, err := Render(draft, map[string]string{"contact": tc.value}, "Employee", "Company", "sender@example.test")
			if tc.valid {
				if err != nil || subject != "Contact" || text != tc.value || body != "<p>"+html.EscapeString(tc.value)+"</p>\n" {
					t.Fatalf("valid mailbox variable changed or was rejected: subject=%q text=%q html=%q err=%v", subject, text, body, err)
				}
			} else if problem, ok := app.As(err); !ok || problem.Kind != app.KindBadRequest || subject != "" || text != "" || body != "" {
				t.Fatalf("invalid email variable released content: %q %q %q err=%v", subject, text, body, err)
			}
		})
	}
}

func TestAdvanceTemplateEmailOptionsUseSameMailboxGrammar(t *testing.T) {
	for _, tc := range advanceTemplateEmailCases {
		t.Run(tc.name, func(t *testing.T) {
			draft := advanceTemplateEmailDraft([]string{tc.value})
			err := ValidateTemplate(draft)
			if !tc.valid {
				if problem, ok := app.As(err); !ok || problem.Kind != app.KindBadRequest {
					t.Fatalf("unusable mailbox option accepted: err=%v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("valid quoted mailbox cannot be published: %v", err)
			}
			subject, text, _, err := Render(draft, map[string]string{"contact": tc.value}, "Employee", "Company", "sender@example.test")
			if err != nil || subject != "Contact" || text != tc.value {
				t.Fatalf("published mailbox option cannot render unchanged: %q %q %v", subject, text, err)
			}
		})
	}
}

func TestAdvanceTemplateEmailRetainsRequiredLengthAndEnumRules(t *testing.T) {
	value := `"ops,team"@example.test`
	for _, tc := range []struct {
		name    string
		mutate  func(*TemplateDraft, map[string]string)
		allowed bool
	}{
		{"at_length", func(d *TemplateDraft, _ map[string]string) { d.Variables[0].MaxLength = len(value) }, true},
		{"over_length", func(d *TemplateDraft, _ map[string]string) { d.Variables[0].MaxLength = len(value) - 1 }, false},
		{"outside_enum", func(d *TemplateDraft, _ map[string]string) {
			d.Variables[0].Options = []string{`"other,team"@example.test`}
		}, false},
		{"required_empty", func(_ *TemplateDraft, v map[string]string) { v["contact"] = "" }, false},
		{"optional_omitted", func(d *TemplateDraft, v map[string]string) { d.Variables[0].Required = false; delete(v, "contact") }, true},
		{"reserved_identity", func(_ *TemplateDraft, v map[string]string) { v["sender_address"] = "forged@example.test" }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			draft := advanceTemplateEmailDraft(nil)
			vars := map[string]string{"contact": value}
			tc.mutate(&draft, vars)
			_, text, _, err := Render(draft, vars, "Employee", "Company", "sender@example.test")
			if tc.allowed && (err != nil || text != vars["contact"]) || !tc.allowed && err == nil {
				t.Fatalf("email variable constraint changed: text=%q err=%v", text, err)
			}
			if strings.Contains(text, "forged") {
				t.Fatal("reserved identity was overridden")
			}
		})
	}
}
