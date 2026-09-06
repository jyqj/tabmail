package enterprise

import (
	"bytes"
	"fmt"
	htmltemplate "html/template"
	"regexp"
	"strings"
	texttemplate "text/template"
	"unicode/utf8"

	"tabmail/internal/app"
	"tabmail/internal/sanitize"
)

var variableName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,47}$`)
var placeholder = regexp.MustCompile(`{{\s*\.([a-z][a-z0-9_]{0,47})\s*}}`)

const maxTemplateBytes = 256 * 1024

// Only scalar placeholders are allowed: no loops, functions, includes, or code.
// HTML is contextually escaped before the normal email sanitizer is applied.
func ValidateTemplate(t Template) error {
	if len(t.Name) == 0 || len(t.Name) > 120 || len(t.Subject) == 0 || len(t.Subject) > 998 || strings.ContainsAny(t.Subject, "\r\n") {
		return app.BadRequest("invalid template name or subject")
	}
	if t.TextBody == "" && t.HTMLBody == "" {
		return app.BadRequest("template body required")
	}
	if len(t.TextBody)+len(t.HTMLBody) > maxTemplateBytes || len(t.Variables) > 32 || len(t.MailboxIDs) == 0 || len(t.MailboxIDs) > 100 {
		return app.BadRequest("template size or scope limit exceeded")
	}
	for k, n := range t.Variables {
		if !variableName.MatchString(k) || k == "employee_name" || k == "company_name" || n < 1 || n > 4000 {
			return app.BadRequest("invalid template variable")
		}
	}
	for _, body := range []string{t.Subject, t.TextBody, t.HTMLBody} {
		for _, m := range placeholder.FindAllStringSubmatch(body, -1) {
			if _, ok := t.Variables[m[1]]; !ok && m[1] != "employee_name" && m[1] != "company_name" {
				return app.BadRequest("undeclared variable: " + m[1])
			}
		}
		rest := placeholder.ReplaceAllString(body, "")
		if strings.Contains(rest, "{{") || strings.Contains(rest, "}}") {
			return app.BadRequest("only {{.variable}} placeholders are supported")
		}
	}
	// Execute once to catch contextual HTML template errors before saving.
	sample := map[string]string{}
	for k := range t.Variables {
		sample[k] = "x"
	}
	_, _, _, err := render(t, sample, "Employee", "Company")
	return err
}
func Render(t Template, values map[string]string, employee, company string) (string, string, string, error) {
	if err := ValidateTemplate(t); err != nil {
		return "", "", "", err
	}
	return render(t, values, employee, company)
}
func render(t Template, values map[string]string, employee, company string) (string, string, string, error) {
	data := map[string]string{"employee_name": employee, "company_name": company}
	for k, v := range values {
		n, ok := t.Variables[k]
		if !ok || !utf8.ValidString(v) || utf8.RuneCountInString(v) > n {
			return "", "", "", app.BadRequest("unknown or oversized variable: " + k)
		}
		data[k] = v
	}
	for k := range t.Variables {
		if strings.TrimSpace(data[k]) == "" {
			return "", "", "", app.BadRequest("missing variable: " + k)
		}
	}
	execText := func(body string) (string, error) {
		tpl, err := texttemplate.New("mail").Option("missingkey=error").Parse(body)
		if err != nil {
			return "", err
		}
		b := limitedTemplateBuffer{limit: maxTemplateBytes}
		err = tpl.Execute(&b, data)
		return b.String(), err
	}
	subject, err := execText(t.Subject)
	if err != nil || len(subject) > 998 || strings.ContainsAny(subject, "\r\n") {
		return "", "", "", app.BadRequest("invalid rendered subject")
	}
	text, err := execText(t.TextBody)
	if err != nil {
		return "", "", "", app.BadRequest("invalid text template")
	}
	tpl, err := htmltemplate.New("mail").Option("missingkey=error").Parse(t.HTMLBody)
	if err != nil {
		return "", "", "", app.BadRequest("invalid HTML template")
	}
	b := limitedTemplateBuffer{limit: maxTemplateBytes}
	if err = tpl.Execute(&b, data); err != nil {
		return "", "", "", app.BadRequest(fmt.Sprintf("invalid HTML context: %v", err))
	}
	html, err := sanitize.HTML(b.String())
	if err != nil {
		return "", "", "", app.BadRequest("HTML sanitization failed")
	}
	if len(text)+len(html) > maxTemplateBytes {
		return "", "", "", app.BadRequest("rendered email is too large")
	}
	return subject, text, html, nil
}

// Bound expansion while rendering, before allocating an oversized intermediate.
type limitedTemplateBuffer struct {
	buffer bytes.Buffer
	limit  int
}

func (b *limitedTemplateBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.buffer.Len() {
		return 0, app.BadRequest("template expansion limit exceeded")
	}
	return b.buffer.Write(p)
}
func (b *limitedTemplateBuffer) String() string { return b.buffer.String() }
