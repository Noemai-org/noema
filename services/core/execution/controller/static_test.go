package controller_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

func sources(t *testing.T) map[string]*ast.File {
	t.Helper()
	fset := token.NewFileSet()
	out := map[string]*ast.File{}
	files, _ := filepath.Glob("*.go")
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		out[name] = f
	}
	return out
}

// REQ-EXE-006, AGENT-08: there is exactly one Dispatch call, it is not inside a loop, and the recovery
// sweeper and everything else never reach the dispatcher.
func TestThereIsOneDispatchAndNoRetryLoop(t *testing.T) {
	calls := 0
	for name, f := range sources(t) {
		var visit func(n ast.Node, inLoop bool)
		visit = func(n ast.Node, inLoop bool) {
			ast.Inspect(n, func(c ast.Node) bool {
				switch v := c.(type) {
				case *ast.ForStmt:
					visit(v.Body, true)
					return false
				case *ast.RangeStmt:
					visit(v.Body, true)
					return false
				case *ast.CallExpr:
					if sel, ok := v.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Dispatch" {
						calls++
						if inLoop {
							t.Errorf("%s: Dispatch is called inside a loop", name)
						}
						if name != "controller.go" {
							t.Errorf("%s: Dispatch may only be called from controller.go", name)
						}
					}
				}
				return true
			})
		}
		visit(f, false)
	}
	if calls != 1 {
		t.Fatalf("want exactly one Dispatch call, found %d", calls)
	}
	ast.Inspect(sources(t)["recover.go"], func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && (id.Name == "Dispatch" || id.Name == "Out" || id.Name == "Dispatcher" || id.Name == "DispatchRequest") {
			t.Errorf("recover.go must not reach the dispatcher (%s)", id.Name)
		}
		return true
	})
}

// REQ-EXE-005: no code maps a silence class to anything but OUTCOME_UNKNOWN, and the word SUCCEEDED does not
// appear as a state. Only classify.go names the silence classes.
func TestOnlyClassifyMapsSilenceAndNothingSaysSucceeded(t *testing.T) {
	silence := map[string]bool{"TransportTimeout": true, "TransportConnectionReset": true, "TransportServer5xx": true, "TransportNoResponse": true}
	for name, f := range sources(t) {
		ast.Inspect(f, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && silence[id.Name] && name != "classify.go" && name != "types.go" {
				t.Errorf("%s names %s; only classify.go decides what silence means", name, id.Name)
			}
			if lit, ok := n.(*ast.BasicLit); ok && strings.Contains(strings.ToUpper(lit.Value), "SUCCEEDED") {
				t.Errorf("%s: a state named SUCCEEDED; that word belongs to Outcome", name)
			}
			return true
		})
	}
	f := sources(t)["classify.go"]
	ok := false
	ast.Inspect(f, func(n ast.Node) bool {
		cc, isCase := n.(*ast.CaseClause)
		if !isCase {
			return true
		}
		names := map[string]bool{}
		for _, e := range cc.List {
			if id, isID := e.(*ast.Ident); isID {
				names[id.Name] = true
			}
		}
		if names["TransportTimeout"] {
			for _, st := range cc.Body {
				if r, isRet := st.(*ast.ReturnStmt); isRet && len(r.Results) == 1 {
					if id, isID := r.Results[0].(*ast.Ident); isID && id.Name == "StateOutcomeUnknown" {
						ok = true
					}
				}
			}
		}
		return true
	})
	if !ok {
		t.Fatal("the silence classes must return StateOutcomeUnknown in Classify")
	}
}
