package trustpg2_test

// A552 (interim, relayed, not yet committed): the confirmation store no longer creates requests. A request exists only through the Recorder's CONFIRM decision, in the same transaction
// (a re-request after an expiry is a new CONFIRM decision). No non-test file of the package may declare a creator of confirmation_request rows: no method named Open or Create, and no
// INSERT INTO confirmation_request.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTheStoreHasNoCreatorOfConfirmationRequests(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("no source files: %v", err)
	}
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(src), "INSERT INTO confirmation_request") {
			t.Errorf("%s inserts into confirmation_request: only the Writer creates a request", name)
		}
		f, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok && fn.Recv != nil && (fn.Name.Name == "Open" || fn.Name.Name == "Create") {
				t.Errorf("%s declares %s: the store has no creator of requests", name, fn.Name.Name)
			}
		}
	}
}
