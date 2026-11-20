package ownershipport_test

// STM-01 (A475, interim, relayed): the ownership map the composition builds from the pack's ownership data. REQ-TRU-002; INV-14, INV-12. SYNTHETIC.

import (
	"reflect"
	"testing"

	ownershipport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust/ownership"
)

func TestNodesFromKindsGivesOneNodePerEntryAndCopies(t *testing.T) {
	in := map[string]string{ownNode: kind, "fin.intent.second": otherKind}
	got := ownershipport.NodesFromKinds(in)
	want := map[string]ownershipport.Node{ownNode: {ResourceKind: kind}, "fin.intent.second": {ResourceKind: otherKind}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("NodesFromKinds = %v; want %v", got, want)
	}
	in[ownNode] = "x.changed"
	if got[ownNode].ResourceKind != kind {
		t.Fatal("a later edit of the input reached the result")
	}
}

func TestNodesFromKindsOfNothingIsNothing(t *testing.T) {
	if got := ownershipport.NodesFromKinds(nil); len(got) != 0 {
		t.Fatalf("NodesFromKinds(nil) = %v", got)
	}
	if _, err := ownershipport.New(ownershipport.Config{TenantID: tenant, Reader: &store{}, Nodes: ownershipport.NodesFromKinds(nil)}); err == nil {
		t.Fatal("an empty map must not build an adapter (the adapter refuses no node)")
	}
}

func TestNodesFromKindsKeepsEveryEntry(t *testing.T) {
	in := map[string]string{"a.one": kind, "a.two": otherKind, "a.three": kind}
	if got := ownershipport.NodesFromKinds(in); len(got) != 3 || got["a.two"].ResourceKind != otherKind || got["a.three"].ResourceKind != kind {
		t.Fatalf("NodesFromKinds = %v", got)
	}
}
