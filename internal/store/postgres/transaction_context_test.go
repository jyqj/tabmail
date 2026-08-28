package postgres

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Repository methods must resolve their executor through db(ctx). A direct
// pool call silently escapes an application Unit of Work, allowing an asset
// row to commit even when its audit or outbox insert later rolls back.
func TestRepositoryMethodsDoNotBypassTransactionContext(t *testing.T) {
	_, current, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	dir := filepath.Dir(current)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || name == "postgres.go" {
			continue
		}
		path := filepath.Join(dir, name)
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			method, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || !isDBMethod(method.Sel.Name) {
				return true
			}
			if selectorEndsWithPool(method.X) {
				line := fset.Position(node.Pos()).Line
				t.Errorf("%s:%d bypasses db(ctx) with a direct pool.%s call", name, line, method.Sel.Name)
			}
			return true
		})
	}
}

func isDBMethod(name string) bool {
	switch name {
	case "Exec", "Query", "QueryRow", "Begin":
		return true
	default:
		return false
	}
}

func selectorEndsWithPool(expr ast.Expr) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "pool"
}
