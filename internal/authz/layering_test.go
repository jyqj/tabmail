package authz_test

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// forbiddenPrefixes are layers authz must never depend on. Authorization is
// policy; pulling in transport or storage inverts the dependency and makes the
// rules impossible to reuse or test outside an HTTP request.
var forbiddenPrefixes = []string{
	"tabmail/internal/api",
	"tabmail/internal/app",
	"tabmail/internal/store",
}

func TestAuthzDoesNotDependOnOuterLayers(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("list package files: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no Go files found in package directory")
	}

	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, spec := range file.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				t.Fatalf("parse import path in %s: %v", name, err)
			}
			for _, prefix := range forbiddenPrefixes {
				if path == prefix || strings.HasPrefix(path, prefix+"/") {
					t.Errorf("%s imports %s; authz must not depend on %s", name, path, prefix)
				}
			}
		}
	}
}
