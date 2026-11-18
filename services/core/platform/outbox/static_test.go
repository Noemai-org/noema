package outbox_test

// PLT-08, PERD §36.1 ("transactional outbox, never dual writes"), AGENTS section 5. Structure the compiler does not check: Enqueue joins the caller's transaction and
// never opens, commits or rolls one back; the package imports nothing of the powers and no client of another store. No database.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func parsePackage(t *testing.T) map[string]*ast.File {
	t.Helper()
	paths, err := filepath.Glob("*.go")
	if err != nil || len(paths) == 0 {
		t.Fatalf("no source files: %v", err)
	}
	out := map[string]*ast.File{}
	for _, p := range paths {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), p, nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		out[p] = f
	}
	if len(out) == 0 {
		t.Fatal("the package has no non-test file")
	}
	return out
}

// REQ-VER-005, PERD §56: the event is written in the state change's transaction. Enqueue never owns the transaction, so it can never commit the event alone.
func TestEnqueueNeverBeginsCommitsOrRollsBackATransaction(t *testing.T) {
	found := false
	for name, f := range parsePackage(t) {
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Name.Name != "Enqueue" || fn.Recv != nil {
				continue
			}
			found = true
			ast.Inspect(fn, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
					switch sel.Sel.Name {
					case "Begin", "BeginTx", "Commit", "Rollback", "Acquire", "Close":
						t.Errorf("%s: Enqueue calls %s: it must only join the caller's transaction", name, sel.Sel.Name)
					}
				}
				return true
			})
			if fn.Type.Params.NumFields() < 3 {
				t.Errorf("%s: Enqueue must take the context, the caller's transaction and the event", name)
			}
		}
	}
	if !found {
		t.Fatal("no function Enqueue in the package")
	}
}

// No import of a power, of the model or provider side, or of a store other than Postgres (INV-29, INV-30, AGENTS section 5). The package takes only the standard library, pgx and platform/clock.
func TestThePackageImportsOnlyTheStandardLibraryPgxAndTheClock(t *testing.T) {
	const mod = "github.com/Noemai-org/noema/services/core/"
	for name, f := range parsePackage(t) {
		for _, imp := range f.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			switch {
			case !strings.Contains(strings.SplitN(path, "/", 2)[0], "."): // standard library
			case strings.HasPrefix(path, "github.com/jackc/pgx/v5"):
			case path == mod+"platform/clock":
			default:
				t.Errorf("%s imports %s", name, path)
			}
		}
	}
}

// No print, no panic, no wall clock (AGENTS section 7; the arch lint checks the clock too).
func TestThePackageHasNoPrintPanicOrWallClock(t *testing.T) {
	for name, f := range parsePackage(t) {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			switch fn := call.Fun.(type) {
			case *ast.Ident:
				if fn.Name == "panic" || fn.Name == "print" || fn.Name == "println" {
					t.Errorf("%s: %s", name, fn.Name)
				}
			case *ast.SelectorExpr:
				if x, ok := fn.X.(*ast.Ident); ok && ((x.Name == "fmt" && strings.HasPrefix(fn.Sel.Name, "Print")) || (x.Name == "time" && (fn.Sel.Name == "Now" || fn.Sel.Name == "Since" || fn.Sel.Name == "Until"))) {
					t.Errorf("%s: %s.%s", name, x.Name, fn.Sel.Name)
				}
			}
			return true
		})
	}
}
