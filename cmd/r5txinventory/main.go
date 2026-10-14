// r5txinventory extracts syntax evidence only; it does not prove runtime SQL or lock behavior.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type Call struct {
	Line    int    `json:"line"`
	Expr    string `json:"expr"`
	Name    string `json:"name"`
	SQLExpr string `json:"sql_expr,omitempty"`
}
type Literal struct {
	Line  int    `json:"line"`
	Value string `json:"value"`
}
type Function struct {
	ID       string    `json:"id"`
	File     string    `json:"file"`
	Name     string    `json:"name"`
	Receiver string    `json:"receiver"`
	Line     int       `json:"line"`
	End      int       `json:"end"`
	SHA      string    `json:"sha256"`
	Calls    []Call    `json:"calls"`
	Strings  []Literal `json:"strings"`
	Params   string    `json:"params"`
}
type File struct {
	Path    string    `json:"path"`
	SHA     string    `json:"sha256"`
	Strings []Literal `json:"strings"`
}
type Output struct {
	Files     []File     `json:"files"`
	Functions []Function `json:"functions"`
}

func hash(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func main() {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	fs := token.NewFileSet()
	out := Output{Files: []File{}, Functions: []Function{}}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			rel, e := filepath.Rel(root, path)
			if e != nil {
				return e
			}
			if rel == "." {
				return nil
			}
			top := strings.Split(filepath.ToSlash(rel), "/")[0]
			if (top != "internal" && top != "cmd") || filepath.ToSlash(rel) == "cmd/r5txinventory" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, e := filepath.Rel(root, path)
		if e != nil {
			return e
		}
		src, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		f, e := parser.ParseFile(fs, path, src, 0)
		if e != nil {
			return e
		}
		render := func(n ast.Node) string { var b bytes.Buffer; _ = printer.Fprint(&b, fs, n); return b.String() }
		literals := func(n ast.Node) []Literal {
			result := []Literal{}
			ast.Inspect(n, func(n ast.Node) bool {
				if x, ok := n.(*ast.BasicLit); ok && x.Kind == token.STRING {
					v, e := strconv.Unquote(x.Value)
					if e == nil {
						result = append(result, Literal{fs.Position(x.Pos()).Line, v})
					}
				}
				return true
			})
			return result
		}
		out.Files = append(out.Files, File{rel, hash(src), literals(f)})
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			recv := ""
			if fn.Recv != nil {
				recv = render(fn.Recv.List[0].Type)
			}
			calls := []Call{}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if x, ok := n.(*ast.CallExpr); ok {
					name := ""
					switch y := x.Fun.(type) {
					case *ast.Ident:
						name = y.Name
					case *ast.SelectorExpr:
						name = y.Sel.Name
					}
					sqlExpr := ""
					if (name == "Exec" || name == "Query" || name == "QueryRow" || name == "ExecContext" || name == "QueryContext" || name == "QueryRowContext") && len(x.Args) > 1 {
						sqlExpr = render(x.Args[1])
					}
					calls = append(calls, Call{fs.Position(x.Pos()).Line, render(x.Fun), name, sqlExpr})
				}
				return true
			})
			a, b := fs.Position(fn.Pos()), fs.Position(fn.End())
			out.Functions = append(out.Functions, Function{rel + ":" + recv + ":" + fn.Name.Name, rel, fn.Name.Name, recv, a.Line, b.Line, hash(src[a.Offset:b.Offset]), calls, literals(fn.Body), render(fn.Type)})
		}
		return nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	sort.Slice(out.Files, func(i, j int) bool { return out.Files[i].Path < out.Files[j].Path })
	sort.Slice(out.Functions, func(i, j int) bool { return out.Functions[i].ID < out.Functions[j].ID })
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		panic(err)
	}
}
