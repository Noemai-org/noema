package outcomee2e_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// noema#525, REQ-TST-001 (a flaky test is a bug), plan section 12: a harness that starts the mock-case must never choose a port before the process that
// uses it has bound it. "Probe a free port, close it, tell the child to bind it" loses to any other process that binds in between, and the child then dies with
// "listen tcp 127.0.0.1:PORT: bind: address already in use" (reproduced in the PR: about 0.4 percent of binds fail with 64 starters at once). The process must hold its own
// listener from the first instant: it is started on port 0 and the harness reads the address it logged.
//
// This guard parses the test files of both mock-case harnesses (this package and execution/e2e) and fails for a function that listens on port 0, closes the listener
// and returns a value: the shape of a port probe. internal/testpg has the same shape for PostgreSQL (which cannot bind port 0); that is recorded as debt, not guarded here.
func TestNoMockCaseHarnessChoosesAPortBeforeItIsBound(t *testing.T) {
	var found []string
	for _, dir := range []string{".", filepath.Join("..", "..", "execution", "e2e")} {
		files, err := filepath.Glob(filepath.Join(dir, "*_test.go"))
		if err != nil || len(files) == 0 {
			t.Fatalf("no test files in %s (%v): the guard is looking in the wrong place", dir, err)
		}
		fset := token.NewFileSet()
		for _, name := range files {
			src, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			f, err := parser.ParseFile(fset, name, src, 0)
			if err != nil {
				t.Fatal(err)
			}
			for _, d := range f.Decls {
				fn, ok := d.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				if listensOnPortZero(fn) && closesAListener(fn) && returnsAValue(fn) {
					found = append(found, filepath.ToSlash(name)+": "+fn.Name.Name)
				}
			}
		}
	}
	if len(found) > 0 {
		t.Fatalf("a harness probes a port and releases it before the mock-case binds it (start the process on 127.0.0.1:0 and read the address it logs):\n  %s", strings.Join(found, "\n  "))
	}
}

func listensOnPortZero(fn *ast.FuncDecl) bool {
	hit := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) != 2 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Listen" {
			return true
		}
		if lit, ok := call.Args[1].(*ast.BasicLit); ok && lit.Kind == token.STRING && strings.HasSuffix(lit.Value, `:0"`) {
			hit = true
		}
		return true
	})
	return hit
}

func closesAListener(fn *ast.FuncDecl) bool {
	hit := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Close" {
				hit = true
			}
		}
		return true
	})
	return hit
}

func returnsAValue(fn *ast.FuncDecl) bool {
	return fn.Type.Results != nil && len(fn.Type.Results.List) > 0
}
