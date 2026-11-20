package trustpack_test

// STM-01 (A475, interim, relayed, not yet committed: one read-only accessor on Pack; the ownership map built from the pack, with a test that fails if a node is missing).
// REQ-TRU-002, REQ-TRU-005; INV-14, INV-12, INV-16. The expectation is read from the real fintech pack by code that does not use the package under test. SYNTHETIC.

import (
	"errors"
	"reflect"
	"sort"
	"testing"

	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trustpack"
)

type ownSlot struct {
	Name         string `json:"name"`
	Type         string `json:"type"`
	ResourceKind string `json:"resource_kind"`
}

type ownNode struct {
	ID           string    `json:"node_id"`
	TrustProfile *string   `json:"trust_profile"`
	Slots        []ownSlot `json:"slots"`
}

type ownProfile struct {
	ID        string `json:"profile_id"`
	Authority struct {
		MustOwn []string `json:"principal_must_own"`
	} `json:"authority"`
}

type ownRaw struct {
	Nodes    []ownNode    `json:"intent_nodes"`
	Profiles []ownProfile `json:"trust_profiles"`
}

// expectedKinds is what the pack file says, read independently: a node's owned slots are its trust profile's principal_must_own; the kind is that slot's resource_kind.
func expectedKinds(t testing.TB, raw []byte) map[string]string {
	t.Helper()
	var r ownRaw
	if err := jsonUnmarshal(raw, &r); err != nil {
		t.Fatal(err)
	}
	owns := map[string][]string{}
	for _, p := range r.Profiles {
		owns[p.ID] = p.Authority.MustOwn
	}
	out := map[string]string{}
	for _, n := range r.Nodes {
		if n.TrustProfile == nil {
			continue
		}
		for _, name := range owns[*n.TrustProfile] {
			for _, s := range n.Slots {
				if s.Name == name {
					out[n.ID] = s.ResourceKind
				}
			}
		}
	}
	return out
}

// missing is the nodes of the pack that need an ownership entry and have none.
func missing(want, got map[string]string) []string {
	var out []string
	for id := range want {
		if _, ok := got[id]; !ok {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

func TestTheRealPackGivesAnOwnershipEntryForEveryNodeThatNeedsOne(t *testing.T) {
	want := expectedKinds(t, readPackFile(t, packFileName))
	if len(want) == 0 {
		t.Fatal("the real pack has no node with an owned slot: the test would be vacuous")
	}
	got, err := loadReal(t).OwnershipKinds()
	if err != nil {
		t.Fatal(err)
	}
	if m := missing(want, got); len(m) != 0 {
		t.Fatalf("intent nodes with no ownership entry: %v", m)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("OwnershipKinds = %v; want %v", got, want)
	}
	for id, k := range got {
		if k == "" {
			t.Errorf("node %s has an empty resource kind", id)
		}
	}
}

// The check is not vacuous: removing any one node from the map is found.
func TestRemovingANodeFromTheMapIsFound(t *testing.T) {
	want := expectedKinds(t, readPackFile(t, packFileName))
	got, err := loadReal(t).OwnershipKinds()
	if err != nil {
		t.Fatal(err)
	}
	for id := range want {
		cut := map[string]string{}
		for k, v := range got {
			if k != id {
				cut[k] = v
			}
		}
		if m := missing(want, cut); !reflect.DeepEqual(m, []string{id}) {
			t.Errorf("without %s the check found %v", id, m)
		}
	}
}

func TestANodeWithNoTrustProfileHasNoOwnershipEntry(t *testing.T) {
	var r ownRaw
	if err := jsonUnmarshal(readPackFile(t, packFileName), &r); err != nil {
		t.Fatal(err)
	}
	got, err := loadReal(t).OwnershipKinds()
	if err != nil {
		t.Fatal(err)
	}
	seen := false
	for _, n := range r.Nodes {
		if n.TrustProfile == nil {
			seen = true
			if _, ok := got[n.ID]; ok {
				t.Errorf("node %s has no trust profile and must have no entry", n.ID)
			}
		}
	}
	if !seen {
		t.Skip("the real pack has no node without a trust profile")
	}
}

// The accessor returns a copy: a caller's edit changes nothing in the pack (the live registry is never exposed).
func TestTheAccessorReturnsCopies(t *testing.T) {
	p := loadReal(t)
	first, err := p.OwnershipKinds()
	if err != nil || len(first) == 0 {
		t.Fatalf("OwnershipKinds = %v, %v", first, err)
	}
	for id := range first {
		first[id] = "x.tampered"
	}
	delete(first, func() string {
		for id := range first {
			return id
		}
		return ""
	}())
	second, err := p.OwnershipKinds()
	if err != nil || !reflect.DeepEqual(second, expectedKinds(t, readPackFile(t, packFileName))) {
		t.Fatalf("a caller's edit reached the pack: %v, %v", second, err)
	}
}

func TestNoPackIsNoOwnership(t *testing.T) {
	var nilPack *trustpack.Pack
	if m, err := nilPack.OwnershipKinds(); !errors.Is(err, trustpack.ErrConfig) || m != nil {
		t.Fatalf("nil pack: %v, %v; want ErrConfig", m, err)
	}
	if m, err := (&trustpack.Pack{}).OwnershipKinds(); !errors.Is(err, trustpack.ErrConfig) || m != nil {
		t.Fatalf("zero pack: %v, %v; want ErrConfig", m, err)
	}
}

// A pack whose ownership data cannot be read soundly gives no map at all (INV-16): the pack still loads (the load rules are unchanged), the accessor refuses.
func TestUnsoundOwnershipDataGivesNoMap(t *testing.T) {
	ownedNode := func(p map[string]any) map[string]any {
		for _, n := range p["intent_nodes"].([]any) {
			if n.(map[string]any)["trust_profile"] != nil {
				return n.(map[string]any)
			}
		}
		t.Fatal("no node with a trust profile")
		return nil
	}
	cases := map[string]func(p map[string]any){
		"the owned slot is not a slot of the node": func(p map[string]any) {
			ownedNode(p)["slots"] = []any{}
		},
		"the owned slot has no resource kind": func(p map[string]any) {
			for _, s := range ownedNode(p)["slots"].([]any) {
				delete(s.(map[string]any), "resource_kind")
			}
		},
		"the owned slot is not a Resource": func(p map[string]any) {
			for _, s := range ownedNode(p)["slots"].([]any) {
				s.(map[string]any)["type"] = "Reference"
			}
		},
		"the trust profile owns two slots of two kinds": func(p map[string]any) {
			n := ownedNode(p)
			second := map[string]any{"name": "second_resource", "type": "Resource", "resource_kind": "x.other", "required": false}
			n["slots"] = append(n["slots"].([]any), second)
			for _, tp := range p["trust_profiles"].([]any) {
				a := tp.(map[string]any)["authority"].(map[string]any)
				a["principal_must_own"] = append(a["principal_must_own"].([]any), "second_resource")
			}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			m := files(t)
			m[packFileName] = edit(t, m[packFileName], mutate)
			p, err := loadFiles(t, m)
			if err != nil {
				t.Fatalf("the pack must still load: %v", err)
			}
			got, err := p.OwnershipKinds()
			if !errors.Is(err, trustpack.ErrPack) || got != nil {
				t.Fatalf("OwnershipKinds = %v, %v; want nil and ErrPack", got, err)
			}
		})
	}
}
