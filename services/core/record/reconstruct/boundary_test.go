package reconstruct_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// REQ-AUD-003, A184 as relayed (noema#492, the Security Reviewer's point 1): the reconstruction, with its `events` array, is for audit and ops only. Who may call it is the
// handler's and the authoriser's business (the tests above: a customer or an approver is the not-found shape). Who may IMPORT it is this test's: no package that serves a
// customer, an approver, the benchmark arm or the UI may build a reconstruction body, because the events array holds what no other role may read. A new importer is a
// decision for the Security Reviewer, and a line here with the reason.
var mayImportReconstruct = map[string]string{
	"cmd/core":         "the composition root mounts the route for ops and audit behind the session check and the role gate",
	"record/admission": "admission uses the reader types only (TenantDBs, TxBeginner), never a reconstruction body",
}

func TestOnlyTheCompositionRootAndAdmissionImportTheReconstruction(t *testing.T) {
	const imported = `"github.com/Noemai-org/noema/services/core/record/reconstruct"`
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, perr := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if perr != nil {
			return perr
		}
		for _, imp := range f.Imports {
			if imp.Path.Value == imported {
				rel, _ := filepath.Rel(root, filepath.Dir(path))
				found[filepath.ToSlash(rel)] = true
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for dir := range found {
		if dir != "record/reconstruct" {
			got = append(got, dir)
		}
	}
	sort.Strings(got)
	for _, dir := range got {
		if _, ok := mayImportReconstruct[dir]; !ok {
			t.Errorf("%s imports the reconstruction: only %v may (REQ-AUD-003); the events array is audit and ops only", dir, keys(mayImportReconstruct))
		}
	}
	for dir := range mayImportReconstruct {
		if !found[dir] {
			t.Errorf("%s is listed as an importer but is not: remove the line, so the list is exact", dir)
		}
	}
}

func keys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
