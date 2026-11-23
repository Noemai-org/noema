package main

// STM-01 (A475, interim, relayed, not yet committed): the composition builds the Ownership adapter's nodes only from the pack: `Pack.OwnershipKinds()` through
// `ownershipport.NodesFromKinds`, never a hand-written node or kind. A node missing from that map is answered "owned" by the adapter (no ownership slot), so a list kept by hand
// would silently drop a check. REQ-TRU-002, REQ-TRU-005; INV-12, INV-14, INV-16. The guard reads the non-test files of this package; the production caller (the main.go PR) is
// covered the moment it lands. The checker is proved on synthetic sources below, so it is not vacuous while no caller exists. SYNTHETIC.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const ownershipImportSuffix = "/orchestrator/portadapters/trust/ownership"

// ownershipNodeViolations returns what a source file does that builds ownership nodes any way other than Pack.OwnershipKinds() then ownership.NodesFromKinds.
func ownershipNodeViolations(t testing.TB, name string, src any) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, src, 0)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	local := ""
	for _, im := range f.Imports {
		p, _ := strconv.Unquote(im.Path.Value)
		if strings.HasSuffix(p, ownershipImportSuffix) {
			local = "ownershipport"
			if im.Name != nil {
				local = im.Name.Name
			}
		}
	}
	if local == "" {
		return nil
	}
	pos := func(n ast.Node) string { return fset.Position(n.Pos()).String() }
	isSel := func(e ast.Expr, x, sel string) bool {
		s, ok := e.(*ast.SelectorExpr)
		if !ok || s.Sel.Name != sel {
			return false
		}
		id, ok := s.X.(*ast.Ident)
		return ok && id.Name == x
	}
	isKindsCall := func(e ast.Expr) bool {
		c, ok := e.(*ast.CallExpr)
		if !ok || len(c.Args) != 0 {
			return false
		}
		s, ok := c.Fun.(*ast.SelectorExpr)
		return ok && s.Sel.Name == "OwnershipKinds"
	}
	// Names bound to the result of OwnershipKinds(): `kinds, err := pack.OwnershipKinds()`.
	fromPack := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		if a, ok := n.(*ast.AssignStmt); ok && len(a.Rhs) == 1 && isKindsCall(a.Rhs[0]) {
			if id, ok := a.Lhs[0].(*ast.Ident); ok {
				fromPack[id.Name] = true
			}
		}
		return true
	})
	okValue := func(e ast.Expr) bool {
		c, ok := e.(*ast.CallExpr)
		if !ok || len(c.Args) != 1 || !isSel(c.Fun, local, "NodesFromKinds") {
			return false
		}
		if id, ok := c.Args[0].(*ast.Ident); ok {
			return fromPack[id.Name]
		}
		return isKindsCall(c.Args[0])
	}
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.SelectorExpr:
			if isSel(v, local, "Node") {
				out = append(out, pos(v)+": a hand-written ownership node ("+local+".Node)")
			}
		case *ast.CompositeLit:
			if isSel(v.Type, local, "Config") {
				for _, el := range v.Elts {
					if kv, ok := el.(*ast.KeyValueExpr); ok {
						if k, ok := kv.Key.(*ast.Ident); ok && k.Name == "Nodes" && !okValue(kv.Value) {
							out = append(out, pos(kv)+": Config.Nodes is not NodesFromKinds(Pack.OwnershipKinds())")
						}
					}
				}
			}
		case *ast.AssignStmt:
			for i, l := range v.Lhs {
				switch lv := l.(type) {
				case *ast.SelectorExpr:
					if lv.Sel.Name == "Nodes" && (len(v.Rhs) != len(v.Lhs) || !okValue(v.Rhs[i])) {
						out = append(out, pos(v)+": Nodes is set other than by NodesFromKinds(Pack.OwnershipKinds())")
					}
				case *ast.IndexExpr:
					if id, ok := lv.X.(*ast.Ident); ok && fromPack[id.Name] {
						out = append(out, pos(v)+": the pack's ownership kinds are edited by hand")
					}
				}
			}
		}
		return true
	})
	return out
}

const ownershipGuardHead = `package main
import (
	ownershipport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust/ownership"
)
`

func TestTheCompositionBuildsOwnershipNodesOnlyFromThePack(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("no source files: %v", err)
	}
	checked := 0
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		checked++
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range ownershipNodeViolations(t, name, src) {
			t.Errorf("INV-14, A475: %s", v)
		}
	}
	if checked == 0 {
		t.Fatal("no non-test source file was checked")
	}
}

// The guard is not vacuous: the one allowed path passes, every other way of building the nodes is found.
func TestTheOwnershipCompositionGuardFindsHandBuiltNodes(t *testing.T) {
	good := map[string]string{
		"direct":   `func f(p P) { _ = ownershipport.Config{Nodes: ownershipport.NodesFromKinds(p.OwnershipKinds())} }`,
		"bound":    `func f(p P) { kinds, _ := p.OwnershipKinds(); _ = ownershipport.Config{TenantID: "t", Nodes: ownershipport.NodesFromKinds(kinds)} }`,
		"assigned": `func f(p P, c *ownershipport.Config) { kinds, _ := p.OwnershipKinds(); c.Nodes = ownershipport.NodesFromKinds(kinds) }`,
	}
	for name, body := range good {
		if v := ownershipNodeViolations(t, name+".go", ownershipGuardHead+body); len(v) != 0 {
			t.Errorf("%s: the allowed path was refused: %v", name, v)
		}
	}
	bad := map[string]string{
		"literal node":      `func f() { _ = ownershipport.Config{Nodes: map[string]ownershipport.Node{"a": {ResourceKind: "x.y"}}} }`,
		"node value":        `func f() { _ = ownershipport.Node{ResourceKind: "x.y"} }`,
		"literal map":       `func f(p P) { _ = ownershipport.Config{Nodes: nil} }`,
		"unrelated func":    `func f(m map[string]string) { _ = ownershipport.Config{Nodes: ownershipport.NodesFromKinds(m)} }`,
		"literal kinds":     `func f() { _ = ownershipport.Config{Nodes: ownershipport.NodesFromKinds(map[string]string{"a": "x.y"})} }`,
		"assigned by hand":  `func f(c *ownershipport.Config) { c.Nodes = nil }`,
		"edited pack kinds": `func f(p P) { kinds, _ := p.OwnershipKinds(); kinds["a"] = "x.y"; _ = ownershipport.Config{Nodes: ownershipport.NodesFromKinds(kinds)} }`,
	}
	for name, body := range bad {
		if v := ownershipNodeViolations(t, name+".go", ownershipGuardHead+body); len(v) == 0 {
			t.Errorf("%s: a hand-built ownership node was not found", name)
		}
	}
	aliased := strings.Replace(ownershipGuardHead, "ownershipport ", "own ", 1) + `func f() { _ = own.Node{ResourceKind: "x.y"} }`
	if v := ownershipNodeViolations(t, "alias.go", aliased); len(v) == 0 {
		t.Error("an aliased import hid a hand-written node")
	}
}
