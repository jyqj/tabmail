package company

import (
	"strings"
	"testing"
)

func TestTemplateValidationAndDataBoundaries(t *testing.T) {
	base := TemplateDraft{Subject: "Hello {{.customer}}", TextBody: "{{.message}}\n{{.employee_name}}", HTMLBody: "<p>{{.message}}</p>", Variables: []Variable{{Name: "customer", Type: "text", Required: true, MaxLength: 32}, {Name: "message", Type: "text", MaxLength: 200}}}
	if e := ValidateTemplate(base); e != nil {
		t.Fatal(e)
	}
	for name, vars := range map[string]map[string]string{"missing": {}, "identity override": {"customer": "A", "employee_name": "CEO"}, "long": {"customer": strings.Repeat("a", 33)}, "header injection": {"customer": "A\r\nBcc: hidden"}} {
		t.Run(name, func(t *testing.T) {
			if _, _, _, e := Render(base, vars, "Employee", "Company", "sender@company.test"); e == nil {
				t.Fatal("unsafe input accepted")
			}
		})
	}
	_, text, html, e := Render(base, map[string]string{"customer": "A", "message": "<script>alert(1)</script>\n{{printf evil}}"}, "Employee", "Company", "sender@company.test")
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(text, "\n{{printf evil}}") || strings.Contains(html, "<script>") {
		t.Fatal(text, html)
	}
	for _, bad := range []string{"{{printf \"%999999999s\" \"x\"}}", "{{range .message}}x{{end}}", "{{template \"x\" .}}", "{{.unknown}}"} {
		b := base
		b.TextBody = bad
		if ValidateTemplate(b) == nil {
			t.Fatalf("source accepted: %s", bad)
		}
	}
}
func TestTypedTemplateVariablesAndBoundedExpansion(t *testing.T) {
	for kind, value := range map[string]string{"email": "Name <x@company.test>", "url": "javascript:alert(1)", "integer": "1.2", "date": "2026-02-31"} {
		b := TemplateDraft{Subject: "Hello", TextBody: "{{.value}}", Variables: []Variable{{Name: "value", Type: kind, MaxLength: 100}}}
		if _, _, _, e := Render(b, map[string]string{"value": value}, "E", "C", "s@c.test"); e == nil {
			t.Fatal(kind)
		}
	}
	b := TemplateDraft{Subject: "Hello", TextBody: strings.Repeat("{{.value}}", 100), Variables: []Variable{{Name: "value", Type: "text", MaxLength: 4000}}}
	if _, _, _, e := Render(b, map[string]string{"value": strings.Repeat("x", 4000)}, "E", "C", "s@c.test"); e == nil {
		t.Fatal("unbounded output")
	}
}
