package main

// STM-04 refactor guard: the worker's RouteDecision carries only what Trust binds into the route step's parameter hash (case, owner queue). VersionToken is read fresh by Execution at
// dispatch and CreateReceiptID is never needed (the case ID comes from the verified create outcome). REQ-STM-001; INV-03. Source-level check, no reflection. Every value is SYNTHETIC.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestRouteDecisionHasNoVersionTokenOrCreateReceiptID(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "wire_worker_ports.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if !ok || ts.Name.Name != "RouteDecision" {
			return true
		}
		st, ok := ts.Type.(*ast.StructType)
		if !ok {
			t.Fatal("RouteDecision is not a struct")
		}
		found = true
		for _, fl := range st.Fields.List {
			for _, name := range fl.Names {
				if name.Name == "VersionToken" || name.Name == "CreateReceiptID" {
					t.Errorf("RouteDecision still has the field %s", name.Name)
				}
			}
		}
		return false
	})
	if !found {
		t.Fatal("type RouteDecision not found in wire_worker_ports.go")
	}
}
