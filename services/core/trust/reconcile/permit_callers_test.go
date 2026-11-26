package reconcile_test

// A659 guard (INV-20): the only production code that may call FreezeGate.Permit or PermitTarget with a bare `frozen` flag is on the list below; every other production caller must go through PermitResult,
// which refuses a waiting result. The test walks every non-test Go file of the module, so a new caller fails here, not in review. Test files build permissions for synthetic worlds and are not scanned.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// allowed lists the production files that may call Permit or PermitTarget, and why. Paths are relative to services/core.
var allowed = map[string]string{
	"trust/intake/steps_t10_t12.go": "T10 asks the freeze gate for one capability on the sealed reconciliation; a waiting result is sealed as INSUFFICIENT_EVIDENCE, which T10 never lets go on",
	"cmd/grantdrill/drill.go":       "the grant drill builds one permission for a synthetic target that is not frozen; it reconciles nothing",
	"trust/reconcile/freeze.go":     "PermitResult and PermitTarget are the gate itself",
}

func TestA659EveryProductionCallerOfPermitIsAccountedFor(t *testing.T) {
	root := filepath.Join("..", "..")
	found := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, perr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if perr != nil {
			return perr
		}
		rel, _ := filepath.Rel(root, path)
		ast.Inspect(f, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok && (sel.Sel.Name == "Permit" || sel.Sel.Name == "PermitTarget") {
					found[filepath.ToSlash(rel)] = true
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for rel := range found {
		if _, ok := allowed[rel]; !ok {
			t.Errorf("%s calls Permit or PermitTarget with a bare frozen flag: use FreezeGate.PermitResult, which refuses a waiting result (A659), or add the file to the list with the reason", rel)
		}
	}
	for rel := range allowed {
		if !found[rel] && rel != "trust/reconcile/freeze.go" {
			t.Errorf("%s is on the list but no longer calls Permit or PermitTarget: remove it", rel)
		}
	}
}
