package architecture

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
)

type routeFact struct {
	Method     string   `json:"method"`
	Path       string   `json:"path"`
	Handler    string   `json:"handler"`
	Middleware []string `json:"middleware"`
	Conditions []string `json:"conditions"`
	Source     string   `json:"source"`
	Line       int      `json:"line"`
	Authority  string   `json:"authority"`
	Resource   string   `json:"resource"`
	Version    string   `json:"version"`
	Audit      string   `json:"audit"`
	Content    string   `json:"content"`
	Evidence   string   `json:"evidence"`
}

// This is deliberately a bounded SOURCE inventory, not runtime authorization
// certification. It follows the current chi composition syntax and records
// conditions without enabling services or calling any handlers.
func collectRouteFacts(root string) ([]routeFact, error) {
	fs := token.NewFileSet()
	sources := map[string]*ast.FuncDecl{}
	files := map[string]string{}
	for _, item := range []struct{ path, name string }{
		{"internal/api/router.go", "NewRouter"},
		{"internal/api/handlers/company_routes.go", "RegisterCompanyRoutes"},
	} {
		tree, err := parser.ParseFile(fs, filepath.Join(root, item.path), nil, 0)
		if err != nil {
			return nil, err
		}
		for _, decl := range tree.Decls {
			if f, ok := decl.(*ast.FuncDecl); ok && f.Name.Name == item.name {
				sources[item.name] = f
				files[item.name] = item.path
			}
		}
		if sources[item.name] == nil {
			return nil, fmt.Errorf("missing composition %s", item.name)
		}
	}
	render := func(n ast.Node) string { var b bytes.Buffer; _ = format.Node(&b, fs, n); return b.String() }
	literal := func(e ast.Expr) (string, error) {
		b, ok := e.(*ast.BasicLit)
		if !ok || b.Kind != token.STRING {
			return "", fmt.Errorf("nonliteral route at %s", fs.Position(e.Pos()))
		}
		return strconv.Unquote(b.Value)
	}
	facts := []routeFact{}
	var walk func(*ast.BlockStmt, string, []string, []string, string) error
	walk = func(block *ast.BlockStmt, prefix string, mids, conds []string, file string) error {
		mids = append([]string{}, mids...)
		for _, stmt := range block.List {
			if s, ok := stmt.(*ast.IfStmt); ok {
				if err := walk(s.Body, prefix, mids, append(append([]string{}, conds...), render(s.Cond)), file); err != nil {
					return err
				}
				if s.Else != nil {
					return fmt.Errorf("new conditional composition needs explicit review: %s", fs.Position(s.Pos()))
				}
				continue
			}
			expr, ok := stmt.(*ast.ExprStmt)
			if !ok {
				continue
			}
			c, ok := expr.X.(*ast.CallExpr)
			if !ok {
				continue
			}
			sel, ok := c.Fun.(*ast.SelectorExpr)
			if !ok {
				continue
			}
			if sel.Sel.Name == "RegisterCompanyRoutes" {
				if err := walk(sources[sel.Sel.Name].Body, prefix, mids, conds, files[sel.Sel.Name]); err != nil {
					return err
				}
				continue
			}
			localMids := append([]string{}, mids...)
			receiver := sel.X
			if w, ok := receiver.(*ast.CallExpr); ok {
				ws, ok := w.Fun.(*ast.SelectorExpr)
				if !ok || ws.Sel.Name != "With" {
					continue
				}
				receiver = ws.X
				for _, m := range w.Args {
					localMids = append(localMids, render(m))
				}
			}
			id, ok := receiver.(*ast.Ident)
			if !ok || id.Name != "r" {
				continue
			}
			switch sel.Sel.Name {
			case "Use":
				for _, m := range c.Args {
					mids = append(mids, render(m))
				}
			case "Route", "Group":
				next := prefix
				if sel.Sel.Name == "Route" {
					if len(c.Args) != 2 {
						return fmt.Errorf("bad Route")
					}
					p, e := literal(c.Args[0])
					if e != nil {
						return e
					}
					next += p
				}
				if len(c.Args) == 0 {
					return fmt.Errorf("empty composition")
				}
				f, ok := c.Args[len(c.Args)-1].(*ast.FuncLit)
				if !ok {
					return fmt.Errorf("indirect composition needs review")
				}
				if e := walk(f.Body, next, localMids, conds, file); e != nil {
					return e
				}
			case "Get", "Post", "Put", "Patch", "Delete", "Head", "Options":
				if len(c.Args) != 2 {
					return fmt.Errorf("unexpected route arity")
				}
				p, e := literal(c.Args[0])
				if e != nil {
					return e
				}
				facts = append(facts, routeFact{Method: strings.ToUpper(sel.Sel.Name), Path: prefix + p, Handler: render(c.Args[1]), Middleware: localMids, Conditions: append([]string{}, conds...), Source: file, Line: fs.Position(c.Pos()).Line})
			default:
				return fmt.Errorf("unhandled router operation %s at %s", sel.Sel.Name, fs.Position(c.Pos()))
			}
		}
		return nil
	}
	if err := walk(sources["NewRouter"].Body, "", nil, nil, files["NewRouter"]); err != nil {
		return nil, err
	}
	// NewRouter returns an outer HTTP wrapper; /ready is not a chi registration.
	// Pin this exception to actual syntax and include it rather than silently
	// claiming chi route declarations enumerate every HTTP entrypoint.
	wrapper := false
	ast.Inspect(sources["NewRouter"], func(n ast.Node) bool {
		if b, ok := n.(*ast.BasicLit); ok && b.Kind == token.STRING {
			s, _ := strconv.Unquote(b.Value)
			if s == "/ready" {
				wrapper = true
			}
		}
		return true
	})
	if !wrapper {
		return nil, fmt.Errorf("health wrapper changed: review inventory")
	}
	for i := range facts {
		if facts[i].Path == "/health" {
			facts[i].Middleware = []string{}
			facts[i].Handler = "NewRouter.outerHealthWrapper"
		}
	}
	facts = append(facts, routeFact{Method: "GET", Path: "/ready", Handler: "NewRouter.outerReadinessWrapper", Middleware: []string{}, Conditions: []string{}, Source: files["NewRouter"], Line: 363})
	sort.Slice(facts, func(i, j int) bool { return facts[i].Method+" "+facts[i].Path < facts[j].Method+" "+facts[j].Path })
	return facts, nil
}

func compareRouteFacts(actual, documented []routeFact) error {
	indexed := map[string]routeFact{}
	for _, r := range documented {
		key := r.Method + " " + r.Path
		if _, ok := indexed[key]; ok {
			return fmt.Errorf("duplicate matrix row %s", key)
		}
		for _, s := range []string{r.Authority, r.Resource, r.Version, r.Audit, r.Content, r.Evidence} {
			if strings.TrimSpace(s) == "" {
				return fmt.Errorf("incomplete boundary row %s", key)
			}
		}
		indexed[key] = r
	}
	if len(actual) == 0 || len(actual) != len(indexed) {
		return fmt.Errorf("route count drift actual=%d documented=%d", len(actual), len(indexed))
	}
	for _, r := range actual {
		key := r.Method + " " + r.Path
		d, ok := indexed[key]
		if !ok {
			return fmt.Errorf("undocumented route %s", key)
		}
		if d.Handler != r.Handler || d.Source != r.Source || !reflect.DeepEqual(d.Middleware, r.Middleware) || !reflect.DeepEqual(d.Conditions, r.Conditions) {
			return fmt.Errorf("composition/guard changed for %s", key)
		}
		delete(indexed, key)
	}
	return nil
}

func TestR5RouteInventory(t *testing.T) {
	root := filepath.Clean("../..")
	facts, err := collectRouteFacts(root)
	if err != nil {
		t.Fatal(err)
	}
	if out := os.Getenv("TABMAIL_ROUTE_INVENTORY_OUTPUT"); out != "" {
		b, e := json.MarshalIndent(facts, "", "  ")
		if e != nil {
			t.Fatal(e)
		}
		f, e := os.OpenFile(out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			t.Fatal(e)
		}
		_, e = f.Write(append(b, '\n'))
		ce := f.Close()
		if e != nil {
			t.Fatal(e)
		}
		if ce != nil {
			t.Fatal(ce)
		}
	}
	b, err := os.ReadFile(filepath.Join(root, "docs/company-mail/evidence/R5-API-MATRIX.json"))
	if err != nil {
		t.Fatal(err)
	}
	var documented []routeFact
	if err = json.Unmarshal(b, &documented); err != nil {
		t.Fatal(err)
	}
	if err = compareRouteFacts(facts, documented); err != nil {
		t.Fatal(err)
	}
	t.Logf("%d source route declarations and outer-wrapper entries match reviewed matrix", len(facts))
}

func TestR5RouteInventoryRejectsDrift(t *testing.T) {
	r := routeFact{Method: "GET", Path: "/x", Handler: "h", Source: "s", Middleware: []string{"RequireAuth"}, Conditions: []string{}, Authority: "user", Resource: "tenant", Version: "read", Audit: "none", Content: "metadata", Evidence: "source"}
	for _, kind := range []string{"missing", "added", "duplicate", "guard", "handler", "condition", "unexplained"} {
		t.Run(kind, func(t *testing.T) {
			d := r
			actual := []routeFact{r}
			doc := []routeFact{d}
			switch kind {
			case "missing":
				doc = nil
			case "added":
				actual = append(actual, routeFact{Path: "/new"})
			case "duplicate":
				doc = append(doc, d)
			case "guard":
				doc[0].Middleware = []string{}
			case "handler":
				doc[0].Handler = "other"
			case "condition":
				doc[0].Conditions = []string{"disabled"}
			case "unexplained":
				doc[0].Authority = ""
			}
			if compareRouteFacts(actual, doc) == nil {
				t.Fatal("drift accepted")
			}
		})
	}
}
