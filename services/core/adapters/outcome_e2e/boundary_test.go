package outcomee2e_test

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

// DEC-2026-10-26 D1, D2 and Amendment 1: the runtime has no control or truth client. This scans every non-test Go file
// under services/ for a string that names a control or truth path or the bench credential, and for an import of any
// package of this repository outside services/core (the benchmark tree and the hostility proxy among them). Test files
// are the only place the test-only client may live.
func TestNoNonTestFileHoldsAControlOrTruthClient(t *testing.T) {
	root := "../../.." // services/
	forbiddenStrings := []string{"/control/v1", "/truth/v1", "BENCH_TOKEN", "MOCK_CASE_BENCH"}
	const repoModule, coreModule = "github.com/Noemai-org/noema/", "github.com/Noemai-org/noema/services/core/"
	files := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		files++
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly|parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			// Anything else in this repository (the benchmark tree, the hostility proxy, the synthetic services) is out of reach.
			if strings.HasPrefix(p, repoModule) && !strings.HasPrefix(p, coreModule) {
				t.Errorf("%s imports %q, which is outside services/core", path, p)
			}
		}
		full, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		ast.Inspect(full, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			s, _ := strconv.Unquote(lit.Value)
			for _, bad := range forbiddenStrings {
				if strings.Contains(s, bad) {
					t.Errorf("%s holds the string %q", path, bad)
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files < 50 {
		t.Fatalf("scanned only %d files: the walk is not reaching services/", files)
	}
}
