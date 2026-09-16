package company

import (
	"bytes"
	"fmt"
	htmltemplate "html/template"
	"net/mail"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	texttemplate "text/template"
	"time"
	"unicode/utf8"

	"tabmail/internal/app"
	"tabmail/internal/sanitize"
)

var variableName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,47}$`)
var placeholder = regexp.MustCompile(`{{\s*\.([a-z][a-z0-9_]{0,47})\s*}}`)

const MaxTemplateBytes = 256 * 1024

// Published templates accept scalar placeholders only, not arbitrary Go code,
// loops, functions, includes or dynamic format widths. Variable data is never
// parsed as template source. Identity variables cannot be supplied by clients.
func ValidateTemplate(t TemplateDraft) error {
	if t.Subject == "" || len(t.Subject) > 998 || strings.ContainsAny(t.Subject, "\r\n") {
		return app.BadRequest("invalid template subject")
	}
	if t.TextBody == "" && t.HTMLBody == "" {
		return app.BadRequest("template body required")
	}
	if len(t.TextBody)+len(t.HTMLBody) > MaxTemplateBytes || len(t.Variables) > 32 {
		return app.BadRequest("template size limit exceeded")
	}
	declared := map[string]bool{"employee_name": true, "company_name": true, "sender_address": true}
	for _, v := range t.Variables {
		if !variableName.MatchString(v.Name) || declared[v.Name] || v.MaxLength < 1 || v.MaxLength > 4000 {
			return app.BadRequest("invalid or duplicate template variable")
		}
		switch v.Type {
		case "text", "email", "integer", "date", "url":
		default:
			return app.BadRequest("invalid variable type")
		}
		if len(v.Options) > 100 {
			return app.BadRequest("too many variable options")
		}
		for _, option := range v.Options {
			if utf8.RuneCountInString(option) > v.MaxLength {
				return app.BadRequest("oversized variable option")
			}
		}
		declared[v.Name] = true
	}
	for _, body := range []string{t.Subject, t.TextBody, t.HTMLBody} {
		for _, m := range placeholder.FindAllStringSubmatch(body, -1) {
			if !declared[m[1]] {
				return app.BadRequest("undeclared variable: " + m[1])
			}
		}
		rest := placeholder.ReplaceAllString(body, "")
		if strings.Contains(rest, "{{") || strings.Contains(rest, "}}") {
			return app.BadRequest("only {{.variable}} placeholders are allowed")
		}
	}
	// Catch HTML contextual failures on publish, before a send is queued.
	sample := map[string]string{"employee_name": "Employee", "company_name": "Company", "sender_address": "sender@example.test"}
	for _, v := range t.Variables {
		sample[v.Name] = "x"
	}
	_, _, _, err := renderValidated(t, sample)
	return err
}

func Render(t TemplateDraft, values map[string]string, employee, companyName, sender string) (string, string, string, error) {
	if err := ValidateTemplate(t); err != nil {
		return "", "", "", err
	}
	data := map[string]string{"employee_name": employee, "company_name": companyName, "sender_address": sender}
	allowed := map[string]Variable{}
	for _, v := range t.Variables {
		allowed[v.Name] = v
	}
	for k := range values {
		if _, ok := allowed[k]; !ok {
			return "", "", "", app.BadRequest("unknown or reserved variable: " + k)
		}
	}
	for _, v := range t.Variables {
		value := values[v.Name]
		if !utf8.ValidString(value) || utf8.RuneCountInString(value) > v.MaxLength {
			return "", "", "", app.BadRequest("oversized variable: " + v.Name)
		}
		if v.Required && strings.TrimSpace(value) == "" {
			return "", "", "", app.BadRequest("required variable: " + v.Name)
		}
		if value != "" {
			valid := true
			switch v.Type {
			case "email":
				a, e := mail.ParseAddress(value)
				valid = e == nil && a.Address == value
			case "integer":
				_, e := strconv.ParseInt(value, 10, 64)
				valid = e == nil
			case "date":
				_, e := time.Parse("2006-01-02", value)
				valid = e == nil
			case "url":
				u, e := url.Parse(value)
				valid = e == nil && u.Host != "" && (u.Scheme == "https" || u.Scheme == "http") && u.User == nil
			}
			if !valid {
				return "", "", "", app.BadRequest("invalid variable type: " + v.Name)
			}
			if len(v.Options) > 0 {
				found := false
				for _, o := range v.Options {
					if value == o {
						found = true
					}
				}
				if !found {
					return "", "", "", app.BadRequest("variable outside allowed options: " + v.Name)
				}
			}
		}
		data[v.Name] = value
	}
	return renderValidated(t, data)
}

func renderValidated(t TemplateDraft, data map[string]string) (string, string, string, error) {
	execText := func(body string) (string, error) {
		tpl, err := texttemplate.New("mail").Option("missingkey=error").Parse(body)
		if err != nil {
			return "", err
		}
		b := boundedBuffer{limit: MaxTemplateBytes}
		err = tpl.Execute(&b, data)
		return b.buffer.String(), err
	}
	subject, err := execText(t.Subject)
	if err != nil || len(subject) > 998 || strings.ContainsAny(subject, "\r\n") {
		return "", "", "", app.BadRequest("invalid rendered subject")
	}
	text, err := execText(t.TextBody)
	if err != nil {
		return "", "", "", app.BadRequest("invalid rendered text")
	}
	tpl, err := htmltemplate.New("mail").Option("missingkey=error").Parse(t.HTMLBody)
	if err != nil {
		return "", "", "", app.BadRequest("invalid HTML template")
	}
	b := boundedBuffer{limit: MaxTemplateBytes}
	if err = tpl.Execute(&b, data); err != nil {
		return "", "", "", app.BadRequest(fmt.Sprintf("invalid HTML context: %v", err))
	}
	html, err := sanitize.HTML(b.buffer.String())
	if err != nil {
		return "", "", "", app.BadRequest("HTML sanitization failed")
	}
	if len(text)+len(html) > MaxTemplateBytes {
		return "", "", "", app.BadRequest("template expansion limit exceeded")
	}
	return subject, text, html, nil
}

type boundedBuffer struct {
	buffer bytes.Buffer
	limit  int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.buffer.Len() {
		return 0, app.BadRequest("template expansion limit exceeded")
	}
	return b.buffer.Write(p)
}
