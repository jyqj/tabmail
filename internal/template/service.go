// Package template renders tenant-scoped outbound email templates.
//
// Security invariants:
//   - The HTML part is always rendered through html/template, which
//     contextually auto-escapes (so a <script> in a Var value or a template
//     field is neutralised, never executed).
//   - Var values are injected as data, never concatenated into template source.
//     Each value is passed via a map of varVal wrappers (accessed as .Val),
//     so a value containing "{{printf ...}}" is treated as a literal string
//     and not parsed as a directive.
//   - Carriage returns and line feeds are stripped from Var values before
//     injection to prevent header injection when a rendered value is placed
//     adjacent to a folded header line.
package template

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync"
	htmltemplate "html/template"
	texttemplate "text/template"

	"github.com/google/uuid"

	"tabmail/internal/models"
)

// Store is the subset of the persistence layer the template service needs.
// Implemented by *postgres.PgStore and *testutil.FakeStore.
type Store interface {
	GetOutboundTemplate(ctx context.Context, tenantID uuid.UUID, name string) (*models.OutboundTemplate, error)
}

// Service renders outbound email templates with a per-tenant compile cache.
// A nil Store (NewService with nil) is supported and makes every Render return
// ErrTemplateNotFound — used by the outbound service to feature-gate the
// template path without a separate flag.
type Service struct {
	store Store
	mu    sync.RWMutex
	// cache[tenantID][name] -> compiled template. Entries are invalidated on
	// Update/Delete for the (tenantID, name) pair.
	cache map[uuid.UUID]map[string]*compiled
}

// compiled holds the parsed templates for one outbound_templates row.
type compiled struct {
	id      uuid.UUID
	name    string
	subject *texttemplate.Template
	text    *texttemplate.Template
	html    *htmltemplate.Template
}

// varVal wraps a user-supplied string so the template accesses it as .Val.
// This indirection is what prevents a value from being interpreted as
// template syntax: the value is data, never source.
type varVal struct {
	Val string
}

// RenderInput selects a template by name within the caller's tenant and
// supplies the variables for substitution.
type RenderInput struct {
	TenantID uuid.UUID
	Name     string
	Vars     map[string]string
}

// Rendered is the output of a successful Render.
type Rendered struct {
	Subject  string
	TextBody string
	HTMLBody string
}

// ErrTemplateNotFound is returned when no template with the given name exists
// for the tenant, or when the service was constructed without a store.
var ErrTemplateNotFound = fmt.Errorf("template not found")

// NewService builds a template Service over st. A nil st disables the
// template path (every Render returns ErrTemplateNotFound).
func NewService(st Store) *Service {
	return &Service{store: st, cache: map[uuid.UUID]map[string]*compiled{}}
}

// Render looks up the named tenant template (compiling and caching it on first
// use) and renders its subject/text/html parts with the supplied Vars. The
// HTML part is rendered through html/template for automatic contextual
// escaping; Var values are injected as data (never as template source).
func (s *Service) Render(in RenderInput) (Rendered, error) {
	if s == nil || s.store == nil {
		return Rendered{}, ErrTemplateNotFound
	}
	if in.Name == "" {
		return Rendered{}, ErrTemplateNotFound
	}

	c, err := s.load(in.TenantID, in.Name)
	if err != nil {
		return Rendered{}, err
	}

	data := buildData(in.Vars)

	out := Rendered{}
	if c.subject != nil {
		var buf bytes.Buffer
		if err := c.subject.Execute(&buf, data); err != nil {
			return Rendered{}, fmt.Errorf("render subject template %q: %w", in.Name, err)
		}
		out.Subject = buf.String()
	}
	if c.text != nil {
		var buf bytes.Buffer
		if err := c.text.Execute(&buf, data); err != nil {
			return Rendered{}, fmt.Errorf("render text template %q: %w", in.Name, err)
		}
		out.TextBody = buf.String()
	}
	if c.html != nil {
		var buf bytes.Buffer
		if err := c.html.Execute(&buf, data); err != nil {
			return Rendered{}, fmt.Errorf("render html template %q: %w", in.Name, err)
		}
		out.HTMLBody = buf.String()
	}
	return out, nil
}

// Invalidate drops the cached compiled template for (tenantID, name). Call on
// Update/Delete so subsequent Renders re-read from the store.
func (s *Service) Invalidate(tenantID uuid.UUID, name string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if m := s.cache[tenantID]; m != nil {
		delete(m, name)
	}
}

// load returns the compiled template for (tenantID, name), compiling and
// caching it on first access. Template syntax errors surface here, at load
// time — never deferred to Render.
func (s *Service) load(tenantID uuid.UUID, name string) (*compiled, error) {
	s.mu.RLock()
	if m := s.cache[tenantID]; m != nil {
		if c := m[name]; c != nil {
			s.mu.RUnlock()
			return c, nil
		}
	}
	s.mu.RUnlock()

	row, err := s.store.GetOutboundTemplate(context.Background(), tenantID, name)
	if err != nil {
		return nil, fmt.Errorf("load template %q: %w", name, err)
	}
	if row == nil {
		return nil, ErrTemplateNotFound
	}

	c, err := compile(row)
	if err != nil {
		return nil, fmt.Errorf("compile template %q: %w", name, err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.cache[tenantID]
	if m == nil {
		m = map[string]*compiled{}
		s.cache[tenantID] = m
	}
	m[name] = c
	return c, nil
}

// compile parses the row's subject/text/html sources. The subject and text
// parts use text/template (plain bodies, no markup to escape); the html part
// uses html/template for contextual auto-escaping. Parsing here means a
// syntactically broken template fails at load, not at first render.
func compile(row *models.OutboundTemplate) (*compiled, error) {
	c := &compiled{id: row.ID, name: row.Name}

	if row.SubjectTmpl != "" {
		t, err := texttemplate.New("subject").Option("missingkey=zero").Parse(row.SubjectTmpl)
		if err != nil {
			return nil, fmt.Errorf("subject: %w", err)
		}
		c.subject = t
	}
	if row.TextTmpl != "" {
		t, err := texttemplate.New("text").Option("missingkey=zero").Parse(row.TextTmpl)
		if err != nil {
			return nil, fmt.Errorf("text: %w", err)
		}
		c.text = t
	}
	if row.HTMLTmpl != "" {
		// html/template performs contextual auto-escaping: a value placed in
		// HTML body text is HTML-escaped, so <script> in a Var value is
		// neutralised rather than executed.
		t, err := htmltemplate.New("html").Option("missingkey=zero").Parse(row.HTMLTmpl)
		if err != nil {
			return nil, fmt.Errorf("html: %w", err)
		}
		c.html = t
	}
	return c, nil
}

// buildData turns the caller's Vars map into the data object the templates
// execute against. Each value is wrapped in varVal and accessed as .Val, so
// the value is always data — a value containing "{{...}}" is rendered
// literally, not parsed. CR and LF are stripped from each value to prevent
// header injection when a rendered value flows into a folded header line.
func buildData(vars map[string]string) map[string]varVal {
	if len(vars) == 0 {
		return map[string]varVal{}
	}
	out := make(map[string]varVal, len(vars))
	for k, v := range vars {
		out[k] = varVal{Val: stripCRLF(v)}
	}
	return out
}

// stripCRLF removes carriage returns and line feeds. Used on Var values that
// may end up adjacent to header lines in the rendered MIME, closing the
// header-injection vector at the value boundary.
func stripCRLF(s string) string {
	if !strings.ContainsAny(s, "\r\n") {
		return s
	}
	return strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' {
			return -1
		}
		return r
	}, s)
}
