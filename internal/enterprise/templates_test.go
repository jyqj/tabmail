package enterprise

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"tabmail/internal/models"
)

func exampleTemplate() Template {
	return Template{Name: "Welcome", Subject: "Hello {{.name}}", TextBody: "Hi {{.name}} from {{.employee_name}}", HTMLBody: "<p>{{.name}}</p>", Variables: map[string]int{"name": 80}, MailboxIDs: []uuid.UUID{uuid.New()}}
}
func TestTemplateIsScalarAndEscapesVariables(t *testing.T) {
	tpl := exampleTemplate()
	_, _, html, err := Render(tpl, map[string]string{"name": "<script>alert(1)</script>"}, "Employee", "Company")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(html, "<script>") {
		t.Fatal("unescaped HTML")
	}
	for _, v := range []string{"{{range .name}}{{.}}{{end}}", "{{template \"x\"}}", "{{.missing}}", "{{printf \"%s\" .name}}"} {
		tpl = exampleTemplate()
		tpl.TextBody = v
		if err := ValidateTemplate(tpl); err == nil {
			t.Errorf("unsafe expression accepted: %s", v)
		}
	}
}
func TestTemplateRejectsMissingOversizeSystemAndHeaderVariables(t *testing.T) {
	for _, values := range []map[string]string{{}, {"name": strings.Repeat("a", 81)}, {"name": "x", "employee_name": "spoof"}, {"name": "a\r\nBcc: victim@example.test"}} {
		if _, _, _, err := Render(exampleTemplate(), values, "Employee", "Company"); err == nil {
			t.Errorf("accepted invalid variables: %v", values)
		}
	}
	tpl := exampleTemplate()
	tpl.HTMLBody = `<a href="{{.name}}">link</a>`
	_, _, html, err := Render(tpl, map[string]string{"name": "javascript:alert(1)"}, "A", "B")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(html, "javascript:") {
		t.Fatal("unsafe URL")
	}
}
func TestSenderPermissionsAreIndependent(t *testing.T) {
	m := &Member{Active: true, Role: "employee"}
	g := &Grant{Read: true}
	if CheckSender(m, g, false) == nil {
		t.Fatal("read implies send")
	}
	g.Read = false
	g.Send = true
	if err := CheckSender(m, g, false); err != nil {
		t.Fatal("send-only grant rejected", err)
	}
	m.Role = "restricted"
	if CheckSender(m, g, false) == nil {
		t.Fatal("restricted raw send")
	}
	if err := CheckSender(m, g, true); err != nil {
		t.Fatal(err)
	}
	m.Role = "viewer"
	if CheckSender(m, g, true) == nil {
		t.Fatal("viewer send")
	}
	m.Role = "admin"
	m.Active = false
	if CheckSender(m, g, true) == nil {
		t.Fatal("disabled admin send")
	}
}
func TestDigestNormalizesJSONBHeadersAndBindsRecipients(t *testing.T) {
	j := &models.OutboundJob{HeadersJSON: []byte(`{"_to":["a@example.test"],"_cc":["b@example.test"]}`)}
	hash := JobDigest(j)
	j.HeadersJSON = []byte(`{ "_cc": ["b@example.test"], "_to": ["a@example.test"] }`)
	if hash != JobDigest(j) {
		t.Fatal("jsonb serialization changed digest")
	}
	j.RcptTo = []string{"intruder@example.test"}
	if hash == JobDigest(j) {
		t.Fatal("recipients not bound")
	}
}

func TestTemplateExpansionIsBoundedDuringRendering(t *testing.T) {
	tpl := Template{Name: "bounded", Subject: "subject", TextBody: strings.Repeat("{{.value}}", 1000), Variables: map[string]int{"value": 4000}, MailboxIDs: []uuid.UUID{uuid.New()}}
	if err := ValidateTemplate(tpl); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := Render(tpl, map[string]string{"value": strings.Repeat("x", 4000)}, "Employee", "Company"); err == nil {
		t.Fatal("oversized expansion accepted")
	}
	b := limitedTemplateBuffer{limit: 10}
	if _, err := b.Write([]byte("12345678901")); err == nil || b.String() != "" {
		t.Fatal("oversized write allocated output")
	}
}

func TestOneCharacterVariableAndUnknownRole(t *testing.T) {
	tpl := exampleTemplate()
	tpl.Variables["name"] = 1
	if _, _, _, err := Render(tpl, map[string]string{"name": "x"}, "Employee", "Company"); err != nil {
		t.Fatal(err)
	}
	if err := CheckSender(&Member{Active: true, Role: "unknown"}, &Grant{Send: true}, false); err == nil {
		t.Fatal("unknown role defaulted to allowed")
	}
}
