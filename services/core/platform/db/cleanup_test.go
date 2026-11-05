package db

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/platform/clock"
)

// Review of noema#107 (Codex, major), applied to this package (PLT-06): the cleanup of a request (a rollback, the release of a
// lock) must still be tried when the request's own deadline has passed, and must never run without a limit. The only place this
// package detaches a context from its parent's cancellation is cleanupContext, which gives it a deadline of its own.

func TestWithoutCancelIsUsedOnlyInsideTheBoundedHelper(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("no source files: %v", err)
	}
	fset := token.NewFileSet()
	var found []string
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		file, err := parser.ParseFile(fset, f, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range file.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "WithoutCancel" && fn.Name.Name != "cleanupContext" {
					found = append(found, fset.Position(call.Pos()).String()+" in "+fn.Name.Name)
				}
				return true
			})
		}
	}
	if len(found) > 0 {
		t.Fatalf("context.WithoutCancel outside cleanupContext, so with no limit: %s", strings.Join(found, "; "))
	}
}

func TestCleanupContextOutlivesACancelledRequestButIsBounded(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	cancel()
	c, done := cleanupContext(parent)
	if err := c.Err(); err != nil {
		t.Fatalf("the cleanup context is already over (%v): the cleanup could never run", err)
	}
	dl, ok := c.Deadline()
	if !ok {
		t.Fatal("the cleanup context has no deadline")
	}
	if left := dl.Sub(clock.System{}.Now()); left > 10*time.Second || left <= 0 {
		t.Fatalf("the cleanup may run for %v; it must be a few seconds", left)
	}
	done()
	if c.Err() == nil {
		t.Fatal("releasing the cleanup context did not end it")
	}
}
