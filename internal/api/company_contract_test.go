package api

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Check the documented company method/path pairs against actual registrations,
// not a second manually maintained list. This does not replace a full OpenAPI validator.
func TestCompanyOpenAPIContractMatchesRouter(t *testing.T) {
	raw, err := openapiSpec.ReadFile("openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	docs := map[string]bool{}
	path := ""
	pathRE := regexp.MustCompile(`^  (/[^:]+):$`)
	methodRE := regexp.MustCompile(`^    (get|post|put|patch|delete):$`)
	for _, line := range strings.Split(string(raw), "\n") {
		if m := pathRE.FindStringSubmatch(line); m != nil {
			path = m[1]
		}
		if m := methodRE.FindStringSubmatch(line); m != nil && strings.HasPrefix(path, "/api/v1/company") {
			docs[strings.ToUpper(m[1])+" "+path] = true
		}
	}
	source, err := os.ReadFile("router.go")
	if err != nil {
		t.Fatal(err)
	}
	f, err := parser.ParseFile(token.NewFileSet(), "router.go", source, 0)
	if err != nil {
		t.Fatal(err)
	}
	routes := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		c, ok := n.(*ast.CallExpr)
		if !ok || len(c.Args) == 0 {
			return true
		}
		sel, ok := c.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		method := strings.ToUpper(sel.Sel.Name)
		if !strings.Contains(" GET POST PUT PATCH DELETE ", " "+method+" ") {
			return true
		}
		literal, ok := c.Args[0].(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			return true
		}
		p, err := strconv.Unquote(literal.Value)
		if err != nil || !strings.HasPrefix(p, "/company") {
			return true
		}
		routes[method+" /api/v1"+p] = true
		return true
	})
	if len(routes) == 0 {
		t.Fatal("no company routes found")
	}
	for r := range routes {
		if !docs[r] {
			t.Errorf("undocumented company endpoint: %s", r)
		}
	}
	for r := range docs {
		if !routes[r] {
			t.Errorf("documented endpoint not registered: %s", r)
		}
	}
	for _, old := range []string{"/api/v1/domains/{id}/grants:", "/api/v1/mailboxes/{mailboxId}/grants:", "/api/v1/send-identities/{id}/grants:"} {
		if strings.Contains(string(raw), old) {
			t.Errorf("removed grant endpoint still documented: %s", old)
		}
	}
}
