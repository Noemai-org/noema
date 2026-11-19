package trustpack_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// packPrefixes are the dotted prefixes the pack's own IDs start with ("<pack_id>."), read from the pack.
func packPrefixes(t testing.TB) []string {
	t.Helper()
	var p struct {
		PackID string `json:"pack_id"`
	}
	if err := jsonUnmarshal(readPackFile(t, packFileName), &p); err != nil || p.PackID == "" {
		t.Fatalf("pack_id: %v", err)
	}
	return []string{p.PackID + "."}
}

// scanSource returns the string literals of the Go source that start with, or hold at a word start, one of the prefixes.
func scanSource(t testing.TB, src string, prefixes []string) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "x.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		s, err := strconv.Unquote(lit.Value)
		if err != nil {
			return true
		}
		for _, p := range prefixes {
			if strings.HasPrefix(s, p) || strings.Contains(s, " "+p) {
				out = append(out, s)
			}
		}
		return true
	})
	return out
}

func scanNonTestSource(t testing.TB, prefixes []string) []string {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range scanSource(t, string(b), prefixes) {
			out = append(out, f+": "+s)
		}
	}
	sort.Strings(out)
	return out
}
