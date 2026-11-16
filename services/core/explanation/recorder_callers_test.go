package explanation

// Review of noema#493 (Security Reviewer, note 1; REQ-EXP-002, INV-13): the Record-backed recorder writes `validator_result: PASSED` for whatever text it is given
// within its own bounds, because it does not know the facts or the forbidden claims. So the only code that may call AppendExplanation is the Explainer, which has
// validated the text first; the composition root reaches the recorder only through the Explainer's Recorder interface. A call from anywhere else in the core fails here.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

func TestOnlyTheExplainerCallsAppendExplanation(t *testing.T) {
	const allowed = "explanation/explainer.go"
	fset := token.NewFileSet()
	var callers []string
	err := filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if c, ok := n.(*ast.CallExpr); ok {
				if sel, ok := c.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "AppendExplanation" {
					callers = append(callers, filepath.ToSlash(path))
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range callers {
		if c == "../"+allowed {
			found = true
			continue
		}
		t.Errorf("%s calls AppendExplanation: only the Explainer may, after the validator has passed the text (a recorder cannot tell a validated text from any other)", c)
	}
	if !found {
		t.Errorf("the Explainer (%s) no longer calls AppendExplanation: the walk found %v, so this test is not looking at the code it guards", allowed, callers)
	}
}
