package template

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"tabmail/internal/models"
)

// memStore is a minimal Store for service-level tests. It mirrors the
// FakeStore's GetOutboundTemplate contract (nil, nil when absent).
type memStore struct {
	rows map[string]*models.OutboundTemplate // key = tenantID/name
}

func (m *memStore) GetOutboundTemplate(_ context.Context, tenantID uuid.UUID, name string) (*models.OutboundTemplate, error) {
	return m.rows[tenantID.String()+"/"+name], nil
}

func newServiceWith(row *models.OutboundTemplate) *Service {
	st := &memStore{rows: map[string]*models.OutboundTemplate{}}
	if row != nil {
		st.rows[row.TenantID.String()+"/"+row.Name] = row
	}
	return NewService(st)
}

func TestRenderVariableSubstitution(t *testing.T) {
	svc := newServiceWith(&models.OutboundTemplate{
		Name:        "welcome",
		SubjectTmpl: "Hello {{.Name.Val}}",
		TextTmpl:    "Welcome {{.Name.Val}}, your code is {{.Code.Val}}.",
		HTMLTmpl:    "<p>Welcome {{.Name.Val}}, code {{.Code.Val}}</p>",
	})
	out, err := svc.Render(RenderInput{
		Name: "welcome",
		Vars: map[string]string{"Name": "Alice", "Code": "123456"},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if out.Subject != "Hello Alice" {
		t.Errorf("subject = %q", out.Subject)
	}
	if !strings.Contains(out.TextBody, "Welcome Alice, your code is 123456.") {
		t.Errorf("text = %q", out.TextBody)
	}
	if !strings.Contains(out.HTMLBody, "Welcome Alice, code 123456") {
		t.Errorf("html = %q", out.HTMLBody)
	}
}

func TestRenderHTMLAutoEscapesScriptInjection(t *testing.T) {
	svc := newServiceWith(&models.OutboundTemplate{
		Name:        "xss",
		SubjectTmpl: "{{.Name.Val}}",
		HTMLTmpl:    "<div>{{.Name.Val}}</div>",
	})
	out, err := svc.Render(RenderInput{
		Name: "xss",
		Vars: map[string]string{"Name": `<script>alert(1)</script>`},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(out.HTMLBody, "<script>") {
		t.Errorf("script tag was not escaped: %q", out.HTMLBody)
	}
	if !strings.Contains(out.HTMLBody, "&lt;script&gt;") {
		t.Errorf("expected escaped script tag in %q", out.HTMLBody)
	}
}

func TestRenderVarValueIsLiteralNotTemplateSyntax(t *testing.T) {
	// A var value containing template syntax must be rendered literally,
	// never executed as a directive. This is the template-injection guard:
	// the value flows through .Val as data, not into the template source.
	svc := newServiceWith(&models.OutboundTemplate{
		Name:        "inject",
		SubjectTmpl: "{{.Name.Val}}",
	})
	out, err := svc.Render(RenderInput{
		Name: "inject",
		Vars: map[string]string{
			"Name": "{{printf \"injected\"}}",
		},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if out.Subject != `{{printf "injected"}}` {
		t.Errorf("var value was interpreted as syntax, got %q", out.Subject)
	}
}

func TestRenderStripsCRLFInVarValues(t *testing.T) {
	svc := newServiceWith(&models.OutboundTemplate{
		Name:        "crlf",
		SubjectTmpl: "{{.Name.Val}}",
	})
	out, err := svc.Render(RenderInput{
		Name: "crlf",
		Vars: map[string]string{"Name": "line1\r\nline2\r\nBcc: evil@example.com"},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.ContainsAny(out.Subject, "\r\n") {
		t.Errorf("CRLF not stripped: %q", out.Subject)
	}
	// stripCRLF makes header injection impossible: without CR or LF the
	// "Bcc:" substring is inert body text, never a new header line.
}

func TestRenderUnknownTemplateNameErrors(t *testing.T) {
	svc := newServiceWith(nil)
	_, err := svc.Render(RenderInput{Name: "nope", TenantID: uuid.New()})
	if !errors.Is(err, ErrTemplateNotFound) {
		t.Fatalf("got err=%v, want ErrTemplateNotFound", err)
	}
}

func TestRenderEmptyNameErrors(t *testing.T) {
	svc := newServiceWith(&models.OutboundTemplate{Name: "x"})
	_, err := svc.Render(RenderInput{Name: ""})
	if !errors.Is(err, ErrTemplateNotFound) {
		t.Fatalf("got err=%v, want ErrTemplateNotFound", err)
	}
}

func TestNilStoreServiceReturnsNotFound(t *testing.T) {
	svc := NewService(nil)
	_, err := svc.Render(RenderInput{Name: "x"})
	if !errors.Is(err, ErrTemplateNotFound) {
		t.Fatalf("got err=%v, want ErrTemplateNotFound", err)
	}
}

func TestSyntaxErrorSurfacesAtLoadTime(t *testing.T) {
	// A broken template must fail to render, and the failure must come from
	// load (compile), not from a silent partial render.
	svc := newServiceWith(&models.OutboundTemplate{
		Name:        "broken",
		SubjectTmpl: "Hello {{ .Name ", // unbalanced action
	})
	_, err := svc.Render(RenderInput{Name: "broken"})
	if err == nil {
		t.Fatal("expected syntax error, got nil")
	}
	if !strings.Contains(err.Error(), "compile") && !strings.Contains(err.Error(), "subject") {
		t.Errorf("error does not identify load/compile failure: %v", err)
	}
}

func TestRenderUsesCacheAndInvalidation(t *testing.T) {
	row := &models.OutboundTemplate{
		Name:        "cached",
		SubjectTmpl: "v1 {{.X.Val}}",
	}
	svc := newServiceWith(row)

	out1, err := svc.Render(RenderInput{Name: "cached", Vars: map[string]string{"X": "a"}})
	if err != nil {
		t.Fatalf("render1: %v", err)
	}
	if out1.Subject != "v1 a" {
		t.Fatalf("render1 subject = %q", out1.Subject)
	}

	// Mutate the row in the store; the cached compile should still win.
	row.SubjectTmpl = "v2 {{.X.Val}}"
	out2, err := svc.Render(RenderInput{Name: "cached", Vars: map[string]string{"X": "a"}})
	if err != nil {
		t.Fatalf("render2: %v", err)
	}
	if out2.Subject != "v1 a" {
		t.Fatalf("cache miss: expected cached v1, got %q", out2.Subject)
	}

	// After invalidation the new source is picked up.
	svc.Invalidate(row.TenantID, "cached")
	out3, err := svc.Render(RenderInput{Name: "cached", Vars: map[string]string{"X": "a"}})
	if err != nil {
		t.Fatalf("render3: %v", err)
	}
	if out3.Subject != "v2 a" {
		t.Fatalf("post-invalidation subject = %q, want v2 a", out3.Subject)
	}
}
