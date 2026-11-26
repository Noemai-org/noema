package runnableport_test

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

// INV-14: the package names no pack ID: no capability, node or graph (the pack's IDs start with its own pack ID and a dot).
func TestNoSourceFileNamesAnIDOfThePack(t *testing.T) {
	var p struct {
		PackID string `json:"pack_id"`
	}
	b, err := os.ReadFile(filepath.Join(packsDir(), packFile))
	if err != nil {
		t.Fatal(err)
	}
	if err := jsonUnmarshal(b, &p); err != nil || p.PackID == "" {
		t.Fatalf("pack_id: %v", err)
	}
	prefix := p.PackID + "."
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		af, err := parser.ParseFile(token.NewFileSet(), f, src, parser.ParseComments)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(af, func(n ast.Node) bool {
			if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				if s, err := strconv.Unquote(lit.Value); err == nil && strings.Contains(s, prefix) {
					t.Errorf("%s names %q", f, s)
				}
			}
			return true
		})
		if strings.Contains(string(src), prefix) {
			t.Errorf("%s names an ID of the pack", f)
		}
	}
}
