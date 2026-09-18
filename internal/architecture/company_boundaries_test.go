package architecture

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// These checks enforce dependency direction, not line counts or file size.
// They run with go test ./... and do not need PostgreSQL or a frontend runtime.
func TestCompanyMailDependencyDirection(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	for _, dir := range []string{"internal/app/companymail", "internal/api/handlers"} {
		entries, e := os.ReadDir(filepath.Join(root, dir))
		if e != nil {
			t.Fatal(e)
		}
		for _, entry := range entries {
			name := entry.Name()
			if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			if dir == "internal/api/handlers" && name != "company_mail.go" {
				continue
			}
			tree, e := parser.ParseFile(token.NewFileSet(), filepath.Join(root, dir, name), nil, 0)
			if e != nil {
				t.Fatal(e)
			}
			for _, imp := range tree.Imports {
				path, e := strconv.Unquote(imp.Path.Value)
				if e != nil {
					t.Fatal(e)
				}
				if dir == "internal/app/companymail" {
					if path == "net/http" || strings.Contains(path, "/api/") || strings.HasPrefix(path, "tabmail/internal/store") {
						t.Errorf("application service imports transport/storage adapter %s", path)
					}
				} else if strings.Contains(path, "enmime") || strings.HasPrefix(path, "tabmail/internal/store") {
					t.Errorf("mail transport owns parsing/storage: %s", path)
				}
			}
			if dir == "internal/api/handlers" {
				ast.Inspect(tree, func(node ast.Node) bool {
					s, ok := node.(*ast.StructType)
					if !ok {
						return true
					}
					for _, field := range s.Fields.List {
						p, ok := field.Type.(*ast.StarExpr)
						if !ok {
							continue
						}
						if id, ok := p.X.(*ast.Ident); ok && strings.HasSuffix(id.Name, "Handler") {
							t.Errorf("handler depends on another handler: %s", id.Name)
						}
					}
					return true
				})
			}
		}
	}
}
