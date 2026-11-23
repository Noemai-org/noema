package recovery

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// STM-05, A490, INV-01, INV-03: recovery resumes a write; it never decides one. No file of this package imports Trust (any package under .../trust or .../portadapters/trust), so Trust
// cannot be called during recovery. The Candidate carries the decision Trust recorded earlier; it is read, never re-made.
func TestRecoveryImportsNoTrustPackage(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("no source files: %v", err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		if _, err := os.Stat(f); err != nil {
			t.Fatal(err)
		}
		file, err := parser.ParseFile(token.NewFileSet(), f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		for _, imp := range file.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if strings.Contains(p, "/trust/") || strings.HasSuffix(p, "/trust") || strings.Contains(p, "portadapters/trust") {
				t.Errorf("%s imports %s: recovery must never call Trust", f, p)
			}
		}
	}
}
