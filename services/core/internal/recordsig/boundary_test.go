package recordsig_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// A184, the Security Reviewer's note 6 on noema#536: recordsig.AppendUnguarded appends a Record event without the payload digit-run guard, and testpg opens throw-away
// databases. Both are test aids. No non-test file of the module may import either of them (an arch-lint rule would say the same; this test is the check until one does).
func TestNoProductionFileImportsTheTestAids(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	forbidden := []string{
		`"github.com/Noemai-org/noema/services/core/internal/recordsig"`,
		`"github.com/Noemai-org/noema/services/core/internal/testpg"`,
	}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if strings.HasPrefix(filepath.ToSlash(rel), "internal/recordsig/") || strings.HasPrefix(filepath.ToSlash(rel), "internal/testpg/") {
			return nil
		}
		f, perr := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if perr != nil {
			return perr
		}
		for _, imp := range f.Imports {
			for _, bad := range forbidden {
				if imp.Path.Value == bad {
					t.Errorf("%s imports %s: a test aid, never production code", filepath.ToSlash(rel), bad)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
