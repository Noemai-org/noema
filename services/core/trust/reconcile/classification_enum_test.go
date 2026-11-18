package reconcile_test

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// classificationConstants returns the value of every constant of type Classification that the package declares, read from its own source files (test files excluded), so a
// thirteenth constant added to the package makes the comparison below fail instead of passing silently (EL's note on noema#665).
func classificationConstants(t *testing.T) []string {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool { return !strings.HasSuffix(fi.Name(), "_test.go") }, 0)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, pkg := range pkgs {
		for _, f := range pkg.Files {
			for _, d := range f.Decls {
				gd, ok := d.(*ast.GenDecl)
				if !ok || gd.Tok != token.CONST {
					continue
				}
				for _, sp := range gd.Specs {
					vs := sp.(*ast.ValueSpec)
					id, ok := vs.Type.(*ast.Ident)
					if !ok || id.Name != "Classification" {
						continue
					}
					for _, v := range vs.Values {
						lit, ok := v.(*ast.BasicLit)
						if !ok || lit.Kind != token.STRING {
							t.Fatalf("a Classification constant is not a string literal at %s", fset.Position(v.Pos()))
						}
						s, err := strconv.Unquote(lit.Value)
						if err != nil {
							t.Fatal(err)
						}
						out = append(out, s)
					}
				}
			}
		}
	}
	if len(out) == 0 {
		t.Fatal("no Classification constant found in the package source")
	}
	return out
}

// REQ-RES-001, REQ-API-001; OD-P-10, A272 (the transaction classification is its own closed list, so an Execution Receipt state such as OUTCOME_UNKNOWN cannot be assigned to it):
// the classifications Trust's reconciliation produces are exactly the contract's TransactionClassification.
func TestTheClassificationsOfTheReconcilerAreExactlyTheContractsTransactionClassification(t *testing.T) {
	raw, err := os.ReadFile("../../../../contracts/schemas/enums.v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Defs map[string]struct {
			Enum []string `json:"enum"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	listed := schema.Defs["TransactionClassification"].Enum
	if len(listed) == 0 {
		t.Fatal("enums.v1.json has no closed list TransactionClassification")
	}
	have := classificationConstants(t)
	sort.Strings(have)
	want := append([]string(nil), listed...)
	sort.Strings(want)
	if len(have) != len(want) {
		t.Fatalf("the reconciler has %d classifications, the contract lists %d: %v / %v", len(have), len(want), have, want)
	}
	for i := range have {
		if have[i] != want[i] {
			t.Errorf("classification %d: the reconciler has %s, the contract %s", i, have[i], want[i])
		}
	}
}
