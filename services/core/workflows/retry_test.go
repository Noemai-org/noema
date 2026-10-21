package workflows_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/workflows"
)

// D8 (DEC-2026-10-20, PERD 36.1): every write activity has MaximumAttempts 1, every read activity is bounded.
func TestWriteActivitiesRunOnceAndReadsAreBounded(t *testing.T) {
	if len(workflows.Activities) == 0 {
		t.Fatal("no activities declared")
	}
	for _, a := range workflows.Activities {
		p := a.Policy
		switch a.Kind {
		case workflows.Write:
			if p.MaximumAttempts != 1 || len(p.BackoffSeconds) != 0 {
				t.Errorf("write activity %s: MaximumAttempts %d, want 1 and no backoff", a.Name, p.MaximumAttempts)
			}
		case workflows.Read:
			if p.MaximumAttempts < 2 || len(p.BackoffSeconds) != p.MaximumAttempts {
				t.Errorf("read activity %s: %d attempts with %d backoffs", a.Name, p.MaximumAttempts, len(p.BackoffSeconds))
			}
			for i := 1; i < len(p.BackoffSeconds); i++ {
				if p.BackoffSeconds[i] < p.BackoffSeconds[i-1] {
					t.Errorf("read activity %s: backoff must not shrink", a.Name)
				}
			}
		default:
			t.Errorf("activity %s has no kind", a.Name)
		}
		if p.MaximumAttempts == 0 {
			t.Errorf("%s: MaximumAttempts 0 is unlimited in Temporal", a.Name)
		}
		if a.Why == "" {
			t.Errorf("%s: no source cited", a.Name)
		}
	}
	if a, ok := workflows.PolicyFor("ExecuteGrant"); !ok || a.Kind != workflows.Write {
		t.Fatal("the spend-and-dispatch activity must be declared as a write")
	}
}

// finding is a MaximumAttempts that breaks D8.
type finding struct{ Where, Why string }

// scanRetry parses Go source and reports every MaximumAttempts that is not a literal of at least 1, and every
// one inside an identifier or function whose name contains "Write" that is not exactly 1.
func scanRetry(name, src string) []finding {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, src, 0)
	if err != nil {
		return []finding{{name, err.Error()}}
	}
	var out []finding
	var walk func(n ast.Node, writeCtx bool)
	walk = func(n ast.Node, writeCtx bool) {
		ast.Inspect(n, func(c ast.Node) bool {
			switch v := c.(type) {
			case *ast.FuncDecl:
				if strings.Contains(v.Name.Name, "Write") {
					walk(v.Body, true)
					return false
				}
			case *ast.ValueSpec:
				for _, id := range v.Names {
					if strings.Contains(id.Name, "Write") {
						for _, val := range v.Values {
							walk(val, true)
						}
						return false
					}
				}
			case *ast.KeyValueExpr:
				if id, ok := v.Key.(*ast.Ident); ok && id.Name == "MaximumAttempts" {
					lit, isLit := v.Value.(*ast.BasicLit)
					n := -1
					if isLit && lit.Kind == token.INT {
						n, _ = strconv.Atoi(lit.Value)
					}
					pos := fset.Position(v.Pos()).String()
					switch {
					case n < 1:
						out = append(out, finding{pos, "MaximumAttempts must be an integer literal of at least 1 (0 is unlimited in Temporal)"})
					case writeCtx && n != 1:
						out = append(out, finding{pos, "a write activity must run with MaximumAttempts: 1"})
					}
				}
			}
			return true
		})
	}
	walk(f, false)
	return out
}

// Canaries: the scanner finds what it is meant to find.
func TestTheScannerFindsABrokenRetryPolicy(t *testing.T) {
	bad := map[string]string{
		"write with three attempts": "package p\nfunc ExecuteWriteOptions() any { return struct{ RetryPolicy struct{ MaximumAttempts int } }{} }\nvar WriteOpts = Opts{MaximumAttempts: 3}\n",
		"unlimited":                 "package p\nvar ReadOpts = Opts{MaximumAttempts: 0}\n",
		"computed":                  "package p\nvar n = 2\nvar ReadOpts = Opts{MaximumAttempts: n}\n",
	}
	for name, src := range bad {
		if len(scanRetry(name+".go", src)) == 0 {
			t.Errorf("%s: not found", name)
		}
	}
	good := "package p\nvar WriteOpts = Opts{MaximumAttempts: 1}\nvar ReadOpts = Opts{MaximumAttempts: 3}\n"
	if got := scanRetry("good.go", good); len(got) != 0 {
		t.Errorf("a correct policy was flagged: %v", got)
	}
}

// The scanner runs over all of services/core, so a Temporal option added anywhere is held to D8.
func TestNoMaximumAttemptsBreaksTheRuleAnywhereInCore(t *testing.T) {
	root := filepath.Join("..")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !strings.Contains(string(src), "MaximumAttempts") {
			return nil
		}
		for _, f := range scanRetry(path, string(src)) {
			t.Errorf("%s: %s", f.Where, f.Why)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
