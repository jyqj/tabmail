package company

import (
	"strings"
	"testing"

	"tabmail/internal/app"
)

func r5TemplateEncodingBadRequest(t *testing.T, err error, message string) {
	t.Helper()
	got, ok := app.As(err)
	if !ok || got.Kind != app.KindBadRequest || got.Message != message {
		t.Fatalf("error = %v, want bad_request %q", err, message)
	}
}

func TestR5TemplateEncodingBoundarySource(t *testing.T) {
	for _, tc := range []struct {
		name    string
		change  func(*TemplateDraft)
		message string
	}{
		{"subject", func(d *TemplateDraft) { d.Subject = "hello\xff" }, "invalid template subject"},
		{"text", func(d *TemplateDraft) { d.TextBody = "text\xe4\xb8" }, "invalid template body encoding"},
		{"html", func(d *TemplateDraft) { d.HTMLBody = "<p>\x80</p>" }, "invalid template body encoding"},
		{"unused_option", func(d *TemplateDraft) { d.Variables[0].Options = []string{"\xed\xa0\x80"} }, "invalid variable option encoding"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			draft := TemplateDraft{Subject: "hello�", TextBody: "text�", HTMLBody: "<p>�</p>", Variables: []Variable{{Name: "value", Type: "text", MaxLength: 20, Options: []string{"�"}}}}
			if err := ValidateTemplate(draft); err != nil {
				t.Fatalf("valid source control: %v", err)
			}
			subject, text, html, err := Render(draft, nil, "Employee", "Company", "sender@example.test")
			if err != nil || subject != draft.Subject || text != draft.TextBody || html != draft.HTMLBody {
				t.Fatalf("valid source control: subject=%q text=%q html=%q err=%v", subject, text, html, err)
			}
			tc.change(&draft)
			r5TemplateEncodingBadRequest(t, ValidateTemplate(draft), tc.message)
			subject, text, html, err = Render(draft, nil, "Employee", "Company", "sender@example.test")
			r5TemplateEncodingBadRequest(t, err, tc.message)
			if subject != "" || text != "" || html != "" {
				t.Fatal("invalid source returned partial output")
			}
		})
	}
}

func TestR5TemplateEncodingBoundaryIdentity(t *testing.T) {
	for _, field := range []string{"employee_name", "company_name", "sender_address"} {
		for _, use := range []string{"referenced", "unreferenced"} {
			t.Run(field+"/"+use, func(t *testing.T) {
				draft := TemplateDraft{Subject: "hello", TextBody: "text", HTMLBody: "<p>text</p>"}
				if use == "referenced" {
					draft.TextBody = "{{." + field + "}}"
					draft.HTMLBody = "<p>{{." + field + "}}</p>"
				}
				identities := map[string]string{"employee_name": "Employee", "company_name": "Company", "sender_address": "sender@example.test"}
				identity := identities[field]
				identities[field] = identity + "�"
				subject, text, html, err := Render(draft, nil, identities["employee_name"], identities["company_name"], identities["sender_address"])
				wantText, wantHTML := "text", "<p>text</p>"
				if use == "referenced" {
					wantText, wantHTML = identities[field], "<p>"+identities[field]+"</p>"
				}
				if err != nil || subject != "hello" || text != wantText || html != wantHTML {
					t.Fatalf("valid identity control: subject=%q text=%q html=%q err=%v", subject, text, html, err)
				}
				identities[field] = identity + "\xff"
				subject, text, html, err = Render(draft, nil, identities["employee_name"], identities["company_name"], identities["sender_address"])
				r5TemplateEncodingBadRequest(t, err, "invalid template identity encoding")
				if subject != "" || text != "" || html != "" {
					t.Fatal("invalid identity returned partial output")
				}
			})
		}
	}
}

func TestR5TemplateEncodingBoundaryClientValue(t *testing.T) {
	draft := TemplateDraft{Subject: "hello", TextBody: "{{.value}}", Variables: []Variable{{Name: "value", Type: "text", MaxLength: 20}}}
	values := map[string]string{"value": "�"}
	subject, text, html, err := Render(draft, values, "Employee", "Company", "sender@example.test")
	if err != nil || subject != "hello" || text != "�" || html != "" {
		t.Fatalf("valid client-value control: subject=%q text=%q html=%q err=%v", subject, text, html, err)
	}
	values["value"] = "\xff"
	subject, text, html, err = Render(draft, values, "Employee", "Company", "sender@example.test")
	// Preserve the existing client-variable error, including malformed UTF-8.
	r5TemplateEncodingBadRequest(t, err, "oversized variable: value")
	if subject != "" || text != "" || html != "" {
		t.Fatal("invalid client value returned partial output")
	}
}

func TestR5TemplateEncodingBoundaryUnicode(t *testing.T) {
	draft := TemplateDraft{
		Subject: "通知 {{.value}}", TextBody: "{{.employee_name}}|{{.company_name}}|{{.sender_address}}|{{.value}}|�",
		HTMLBody:  "<p>{{.employee_name}}|{{.company_name}}|{{.sender_address}}|{{.value}}|�</p>",
		Variables: []Variable{{Name: "value", Type: "text", Required: true, MaxLength: 2, Options: []string{"中文"}}},
	}
	subject, text, html, err := Render(draft, map[string]string{"value": "中文"}, "张三", "公司", "员工@example.test")
	if err != nil {
		t.Fatal(err)
	}
	want := "张三|公司|员工@example.test|中文|�"
	if subject != "通知 中文" || text != want || html != "<p>"+want+"</p>" {
		t.Fatalf("Unicode changed: subject=%q text=%q html=%q", subject, text, html)
	}
}

func TestR5TemplateEncodingBoundaryBudgets(t *testing.T) {
	for _, tc := range []struct {
		name    string
		draft   TemplateDraft
		values  map[string]string
		wantErr string
	}{
		{"subject_at_byte_limit", TemplateDraft{Subject: strings.Repeat("中", 332) + "ab", TextBody: "text"}, nil, ""},
		{"subject_over_byte_limit", TemplateDraft{Subject: strings.Repeat("中", 333), TextBody: "text"}, nil, "invalid template subject"},
		{"body_at_byte_limit", TemplateDraft{Subject: "hello", TextBody: strings.Repeat("中", MaxTemplateBytes/3) + "x"}, nil, ""},
		{"body_over_byte_limit", TemplateDraft{Subject: "hello", TextBody: strings.Repeat("中", MaxTemplateBytes/3) + "xx"}, nil, "template size limit exceeded"},
		{"escaped_html_within_limit", TemplateDraft{Subject: "hello", HTMLBody: "<p>" + strings.Repeat("{{.value}}", 60) + "</p>", Variables: []Variable{{Name: "value", Type: "text", MaxLength: 1000}}}, map[string]string{"value": strings.Repeat("<", 1000)}, ""},
		{"escaped_html_over_limit", TemplateDraft{Subject: "hello", HTMLBody: "<p>" + strings.Repeat("{{.value}}", 70) + "</p>", Variables: []Variable{{Name: "value", Type: "text", MaxLength: 1000}}}, map[string]string{"value": strings.Repeat("<", 1000)}, "template expansion limit exceeded"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			subject, text, html, err := Render(tc.draft, tc.values, "Employee", "Company", "sender@example.test")
			if tc.wantErr != "" {
				if tc.name == "escaped_html_over_limit" {
					r5TemplateEncodingBadRequest(t, err, "invalid HTML context: template expansion limit exceeded")
				} else {
					r5TemplateEncodingBadRequest(t, err, tc.wantErr)
				}
				if subject != "" || text != "" || html != "" {
					t.Fatal("over-budget render returned partial output")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if subject != tc.draft.Subject || text != tc.draft.TextBody {
				t.Fatal("valid literal bytes changed")
			}
			if tc.name == "escaped_html_within_limit" && html != "<p>"+strings.Repeat("&lt;", 60000)+"</p>" {
				t.Fatal("HTML expansion was not escaped exactly once")
			}
		})
	}
}

func TestR5TemplateEncodingBoundaryReservedIdentity(t *testing.T) {
	for _, field := range []string{"employee_name", "company_name", "sender_address"} {
		draft := TemplateDraft{Subject: "{{." + field + "}}", TextBody: "text"}
		t.Run(field+"/client_override", func(t *testing.T) {
			identities := map[string]string{"employee_name": "Employee", "company_name": "Company", "sender_address": "sender@example.test"}
			subject, text, html, err := Render(draft, nil, identities["employee_name"], identities["company_name"], identities["sender_address"])
			if err != nil || subject != identities[field] || text != "text" || html != "" {
				t.Fatalf("valid reserved-identity control: subject=%q text=%q html=%q err=%v", subject, text, html, err)
			}
			subject, text, html, err = Render(draft, map[string]string{field: "伪造"}, identities["employee_name"], identities["company_name"], identities["sender_address"])
			r5TemplateEncodingBadRequest(t, err, "unknown or reserved variable: "+field)
			if subject != "" || text != "" || html != "" {
				t.Fatal("reserved override returned partial output")
			}
		})
		t.Run(field+"/subject_crlf", func(t *testing.T) {
			identities := map[string]string{"employee_name": "Employee", "company_name": "Company", "sender_address": "sender@example.test"}
			subject, text, html, err := Render(draft, nil, identities["employee_name"], identities["company_name"], identities["sender_address"])
			if err != nil || subject != identities[field] || text != "text" || html != "" {
				t.Fatalf("valid subject control: subject=%q text=%q html=%q err=%v", subject, text, html, err)
			}
			identities[field] += "\r\nBcc: hidden@example.test"
			subject, text, html, err = Render(draft, nil, identities["employee_name"], identities["company_name"], identities["sender_address"])
			r5TemplateEncodingBadRequest(t, err, "invalid rendered subject")
			if subject != "" || text != "" || html != "" {
				t.Fatal("subject CRLF returned partial output")
			}
		})
	}
}
