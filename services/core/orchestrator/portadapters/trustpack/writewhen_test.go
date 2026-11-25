package trustpack_test

// TRU-02 (A543, A364, A551, interim, relayed, not yet committed: ONE read-only accessor of the intent nodes' `write_when`). REQ-TRU-004; INV-14, INV-16, INV-01. The expectation is
// read from the real fintech pack by code that does not use the package under test. SYNTHETIC.

import (
	"errors"
	"reflect"
	"testing"

	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trustpack"
)

type wwSlot struct {
	Name       string   `json:"name"`
	Type       string   `json:"type"`
	Required   bool     `json:"required"`
	Vocabulary []string `json:"vocabulary"`
}

type wwNode struct {
	ID        string   `json:"node_id"`
	WriteWhen *string  `json:"write_when"`
	Slots     []wwSlot `json:"slots"`
}

func readWWNodes(t testing.TB) []wwNode {
	t.Helper()
	var raw struct {
		Nodes []wwNode `json:"intent_nodes"`
	}
	if err := jsonUnmarshal(readPackFile(t, packFileName), &raw); err != nil {
		t.Fatal(err)
	}
	return raw.Nodes
}

// Every intent node of the pack has an entry with the predicate the pack file carries ("" when it has none) and its slot declarations, in order.
func TestWriteWhensIsTheNodesWriteWhenAndSlotsAsThePackFileWritesThem(t *testing.T) {
	got, err := loadReal(t).WriteWhens()
	if err != nil {
		t.Fatal(err)
	}
	nodes := readWWNodes(t)
	if len(got) != len(nodes) {
		t.Fatalf("WriteWhens has %d nodes, the pack file has %d", len(got), len(nodes))
	}
	withPredicate, without := 0, 0
	for _, n := range nodes {
		want := trustpack.WriteWhen{}
		if n.WriteWhen != nil {
			want.Predicate = *n.WriteWhen
			withPredicate++
		} else {
			without++
		}
		for _, s := range n.Slots {
			want.Slots = append(want.Slots, trustpack.WriteSlot{Name: s.Name, Type: s.Type, Required: s.Required, Vocabulary: s.Vocabulary})
		}
		if g, ok := got[n.ID]; !ok || !reflect.DeepEqual(g, want) {
			t.Fatalf("WriteWhens[%q] = %+v, %v; want %+v", n.ID, g, ok, want)
		}
	}
	if withPredicate == 0 || without == 0 {
		t.Fatalf("the pack must give one node with a write_when and one without (%d, %d): the test is vacuous otherwise", withPredicate, without)
	}
}

// The answer is a copy: changing it changes nothing the pack holds (the pack is never exposed).
func TestWriteWhensReturnsACopy(t *testing.T) {
	p := loadReal(t)
	first, err := p.WriteWhens()
	if err != nil {
		t.Fatal(err)
	}
	for id, w := range first {
		w.Predicate = "false"
		for i := range w.Slots {
			w.Slots[i].Name = "mutated"
			if len(w.Slots[i].Vocabulary) > 0 {
				w.Slots[i].Vocabulary[0] = "MUTATED"
			}
		}
		first[id] = w
	}
	delete(first, readWWNodes(t)[0].ID)
	second, err := p.WriteWhens()
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := loadReal(t).WriteWhens()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(second, fresh) {
		t.Fatal("changing the answer changed what the pack holds")
	}
}

// INV-16: a nil or zero Pack answers nothing, and a pack that fails to load is no Pack, so it yields none.
func TestAPackThatFailsVerificationYieldsNoWriteWhen(t *testing.T) {
	var nilPack *trustpack.Pack
	for name, p := range map[string]*trustpack.Pack{"nil": nilPack, "zero": {}} {
		if got, err := p.WriteWhens(); !errors.Is(err, trustpack.ErrConfig) || got != nil {
			t.Fatalf("%s Pack: WriteWhens = %v, %v; want nil and ErrConfig", name, got, err)
		}
	}
	m := files(t)
	m[packFileName] = edit(t, m[packFileName], func(p map[string]any) {
		ns := p["intent_nodes"].([]any)
		p["intent_nodes"] = append(ns, ns[0])
	})
	p, err := loadFiles(t, m)
	if !errors.Is(err, trustpack.ErrPack) || p != nil {
		t.Fatalf("LoadPack(a repeated intent node) = %v, %v; want a nil Pack and ErrPack", p, err)
	}
	if got, err := p.WriteWhens(); !errors.Is(err, trustpack.ErrConfig) || got != nil {
		t.Fatalf("the Pack of a failed load: WriteWhens = %v, %v; want nil and ErrConfig", got, err)
	}
}
