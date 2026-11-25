package trustport_test

// TRU-12, ruling A625 (interim, relayed, not yet committed; BD-05, INV-12): Trust never reads the investigation. The orchestrator reads it with its own login and hands the session
// over as typed data, so no file of trust/ and no Trust-side file of this package may import the investigations package or name its table. Source-level check. SYNTHETIC.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const investigationsPkg = "github.com/Noemai-org/noema/services/core/orchestrator/investigations"

func TestNoFileOfTrustImportsTheInvestigations(t *testing.T) { // A625, BD-05
	root := "../../../trust"
	seen := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		seen++
		f, perr := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if perr != nil {
			return perr
		}
		for _, imp := range f.Imports {
			if p, _ := strconv.Unquote(imp.Path.Value); strings.HasPrefix(p, investigationsPkg) {
				t.Errorf("%s imports %s: Trust must never read the investigation (BD-05)", path, p)
			}
		}
		return nil
	})
	if err != nil || seen == 0 {
		t.Fatalf("walked %d files of trust/, err %v", seen, err)
	}
}

// The Trust-side reader of the route scope (routescopes.go) neither imports the investigations package nor names the investigation table in any query.
func TestTheTrustSideScopeReaderNeitherImportsNorQueriesTheInvestigation(t *testing.T) { // A625, BD-05
	f, err := parser.ParseFile(token.NewFileSet(), "routescopes.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, imp := range f.Imports {
		if p, _ := strconv.Unquote(imp.Path.Value); strings.HasPrefix(p, investigationsPkg) {
			t.Errorf("routescopes.go imports %s", p)
		}
	}
	queries := 0
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		s, _ := strconv.Unquote(lit.Value)
		if strings.Contains(strings.ToUpper(s), "SELECT ") {
			queries++
			if strings.Contains(strings.ToLower(s), "investigation") {
				t.Errorf("routescopes.go names the investigation in a query: %q", s)
			}
		}
		return true
	})
	if queries == 0 {
		t.Fatal("routescopes.go holds no SELECT: the guard has nothing to check")
	}
}
