package registry_test

// REQ-CAP-002 acceptance: "a static scan SHALL find no symbol or HTTP route matching the prohibited operations in
// adapters". The operation names are read from the pack's own prohibited capabilities, so a ninth prohibited capability
// is covered without editing this test. BD-08, INV-05.

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

// squash lower-cases a name and drops every separator, so transfer_funds, TransferFunds and "transfer-funds" match.
func squash(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func operationNames(t testing.TB) map[string]string {
	t.Helper()
	r := loadFintech(t)
	out := map[string]string{}
	for _, id := range r.Prohibited() {
		op := id[strings.LastIndex(id, ".")+1:]
		out[squash(op)] = id
	}
	return out
}

// hits reports every identifier or string literal in src that squashes to contain a prohibited operation name.
func hits(t testing.TB, filename string, src any, ops map[string]string) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filename, src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", filename, err)
	}
	var found []string
	note := func(pos token.Pos, what, text string) {
		sq := squash(text)
		for op, id := range ops {
			if strings.Contains(sq, op) {
				found = append(found, fset.Position(pos).String()+": "+what+" "+strconv.Quote(text)+" names "+id)
			}
		}
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.Ident:
			note(x.Pos(), "identifier", x.Name)
		case *ast.BasicLit:
			if x.Kind == token.STRING {
				if s, err := strconv.Unquote(x.Value); err == nil {
					note(x.Pos(), "string", s)
				}
			}
		}
		return true
	})
	return found
}

// The scan reads every non-test Go file under services/core except the registry itself (which holds no operation names:
// it reads them as pack data) and the generated contract mirrors (types only).
func TestNoSymbolOrRouteNamesAProhibitedOperation(t *testing.T) {
	ops := operationNames(t)
	root := filepath.Join("..", "..")
	var all []string
	scanned := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || strings.HasSuffix(path, "_gen.go") {
			return nil
		}
		scanned++
		all = append(all, hits(t, path, nil, ops)...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if scanned < 20 {
		t.Fatalf("the scan read only %d files; the root is wrong", scanned)
	}
	for _, h := range all {
		t.Error(h)
	}
}

// The scan has teeth: planted code in each shape is found. (A scan that cannot fail proves nothing.)
func TestTheScanFindsPlantedCode(t *testing.T) {
	ops := operationNames(t)
	planted := map[string]string{
		"a function":   "package a\nfunc TransferFunds() {}\n",
		"a method":     "package a\ntype C struct{}\nfunc (C) initiate_refund() {}\n",
		"a route":      "package a\nvar r = \"/v1/cases/{id}/reverse-transaction\"\n",
		"a capability": "package a\nconst id = \"fin.cap.adjust_balance\"\n",
		"a field":      "package a\ntype T struct{ EditOrDeleteEvidence bool }\n",
	}
	for name, src := range planted {
		if got := hits(t, name+".go", src, ops); len(got) == 0 {
			t.Errorf("%s: planted code was not found", name)
		}
	}
	if got := hits(t, "ok.go", "package a\nfunc LookupAccount() {}\nvar s = \"/v1/cases\"\n", ops); len(got) != 0 {
		t.Errorf("an innocent file was flagged: %v", got)
	}
}
