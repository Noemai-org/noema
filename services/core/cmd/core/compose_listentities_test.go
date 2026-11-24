package main

// STM-01 final composition (A545, interim, relayed, not yet committed: "ListEntities needs no new decision; EL builds it from C2's mapping descriptors (ScopeFromDescriptors), the list entities
// of the evidence mapping"). The list entities the Policies adapter reads are the entities the evidence mapping lists as list entities, each with the source its evidence descriptor gives, and
// the scope prefix of the item that holds it is the resource kind the evidence read gives that item (<pack id>.<entity>, then a colon: portread's `ResourceKind + ":" + ref`). The pack
// names no other thing, so the code names none (INV-14). REQ-STM-001, REQ-TRU-006, REQ-CTX-001; INV-14, INV-16, INV-19. SYNTHETIC.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust/intakesources"
)

// leExpected reads the pack and the mapping files independently: an entity is a list entity when a row of the mapping names it with `rows_from`; its source is the descriptor's.
func leExpected(t *testing.T) map[string]intakesources.ListSpec {
	t.Helper()
	v := tiVerified(t)
	var pack struct {
		PackID      string `json:"pack_id"`
		Descriptors []struct {
			Entity string `json:"entity"`
			Source string `json:"source"`
		} `json:"evidence_descriptors"`
	}
	var mp struct {
		Rows []struct {
			Entity   string `json:"entity"`
			Source   string `json:"source"`
			RowsFrom string `json:"rows_from"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(v.Pack, &pack); err != nil || json.Unmarshal(v.EvidenceMapping, &mp) != nil {
		t.Fatal("the real pack or its mapping does not decode")
	}
	out := map[string]intakesources.ListSpec{}
	for _, r := range mp.Rows {
		if r.RowsFrom == "" {
			continue
		}
		for _, d := range pack.Descriptors {
			if d.Entity == r.Entity && d.Source == r.Source {
				out[r.Entity] = intakesources.ListSpec{Source: d.Source, ScopePrefix: pack.PackID + "." + d.Entity + ":"}
			}
		}
	}
	return out
}

func TestTheListEntitiesAreTheMappingsListEntitiesWithTheDescriptorsSource(t *testing.T) {
	want := leExpected(t)
	if len(want) < 2 {
		t.Fatalf("the real mapping lists %d list entities: the test would prove nothing", len(want))
	}
	v := tiVerified(t)
	_, m := tiParts(t, v)
	got, err := listEntitiesFromDescriptors(v.Pack, m)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, %v; want %v", got, err, want)
	}
}

// The scope prefix is the one the evidence read gives the item: the resource kind of the list, a colon, then the reference (portread's ResourceScope). The prefixes of two lists of one
// source do not overlap, or the Policies adapter refuses its configuration.
func TestTheScopePrefixIsTheResourceKindOfTheItemAndNoPrefixOverlapsAnother(t *testing.T) {
	v := tiVerified(t)
	_, m := tiParts(t, v)
	got, err := listEntitiesFromDescriptors(v.Pack, m)
	if err != nil {
		t.Fatal(err)
	}
	for name, spec := range got {
		if !strings.HasSuffix(spec.ScopePrefix, "."+name+":") {
			t.Errorf("%s: prefix %q is not <pack id>.%s:", name, spec.ScopePrefix, name)
		}
		for other, o := range got {
			if other != name && o.Source == spec.Source && strings.HasPrefix(o.ScopePrefix, spec.ScopePrefix) {
				t.Errorf("the prefixes of %s and %s overlap", name, other)
			}
		}
	}
}

// Nothing the mapping does not list is a list entity, and nothing the descriptors do not describe has a source.
func TestAnEntityThatIsNotAListOrHasNoDescriptorIsNotAListEntity(t *testing.T) {
	v := tiVerified(t)
	_, m := tiParts(t, v)
	got, err := listEntitiesFromDescriptors(v.Pack, m)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"transaction", "beneficiary", "account", "switch_event", "settlement_record", "attachment", "synthetic_entity"} {
		if _, ok := got[name]; ok {
			t.Errorf("%s is given as a list entity", name)
		}
	}
}

func TestADescriptorSetThatCannotBeReadIsAnErrorThatNamesNoPackContent(t *testing.T) {
	v := tiVerified(t)
	_, m := tiParts(t, v)
	for name, pack := range map[string][]byte{"not a pack": []byte(`{"pack_id":`), "no descriptor": []byte(`{"pack_id":"synthetic"}`), "no pack id": []byte(`{"evidence_descriptors":[{"entity":"a","source":"s","fields":{"i":{"type":"String"}}}]}`)} {
		got, err := listEntitiesFromDescriptors(pack, m)
		if err == nil || got != nil || strings.Contains(err.Error(), "synthetic") {
			t.Errorf("%s: got %v, %v; want an error with no pack content and no map", name, got, err)
		}
	}
	if got, err := listEntitiesFromDescriptors(v.Pack, nil); err == nil || got != nil {
		t.Errorf("no mapping: got %v, %v; want an error", got, err)
	}
}

// The wiring uses them: with no list convention given, the pack and the Understanding half's mapping are enough for Policies, when its other inputs are given. An explicit one overrides.
func TestTheWiringBuildsPoliciesFromTheDerivedListEntities(t *testing.T) {
	in := twIn(t)
	in.ListEntities = nil
	w, err := wireTrustInputs(in)
	if err != nil {
		t.Fatal(err)
	}
	if w.fill(TrustAdapters{}).Policies == nil {
		t.Errorf("Policies is not built from the derived list entities; unbuilt %v", w.Unbuilt())
	}
	for _, r := range w.Reasons["Policies"] {
		if strings.Contains(r, "list entities") {
			t.Errorf("Policies still lacks the list entities after they were derived")
		}
	}
	in = twIn(t)
	in.Screen = ScreenDeps{}
	w, err = wireTrustInputs(in)
	if err != nil || w.fill(TrustAdapters{}).Policies != nil {
		t.Errorf("Policies is built with no injection screen: %v", err)
	}
}

// The derivation has no source of its own: the file names no id of the pack (INV-14).
func TestTheDerivationNamesNoIdOfThePack(t *testing.T) {
	src, err := os.ReadFile(filepath.Join(".", "compose_listentities.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, banned := range []string{"fin.", "case_system", "complaint_ref", `"case"`} {
		if strings.Contains(string(src), banned) {
			t.Errorf("compose_listentities.go names %q", banned)
		}
	}
}

// Written after the hand mutation check: a repeated entity is refused, and the Policies gap names the list entities only when they are really missing.
func TestARepeatedEntityIsRefused(t *testing.T) {
	_, m := tiParts(t, tiVerified(t))
	dup := []byte(`{"pack_id":"synthetic","evidence_descriptors":[{"entity":"a","source":"s","fields":{"i":{"type":"String"}}},{"entity":"a","source":"s2","fields":{"i":{"type":"String"}}}]}`)
	if got, err := listEntitiesFromDescriptors(dup, m); err == nil || got != nil {
		t.Errorf("got %v, %v; want an error and no map", got, err)
	}
}

func TestThePoliciesGapNamesTheListEntitiesOnlyWhenTheyAreMissing(t *testing.T) {
	has := func(w trustWired) bool {
		for _, r := range w.Reasons["Policies"] {
			if r == needListEntities {
				return true
			}
		}
		return false
	}
	derived := twIn(t)
	derived.ListEntities, derived.Screen = nil, ScreenDeps{}
	w, err := wireTrustInputs(derived)
	if err != nil || len(w.Reasons["Policies"]) == 0 || has(w) {
		t.Errorf("with the entities derived and another part missing: reasons %v, %v; want reasons that do not name the list entities", w.Reasons["Policies"], err)
	}
	none := twIn(t)
	none.ListEntities, none.Understanding.Mapping = nil, nil
	w, err = wireTrustInputs(none)
	if err != nil || !has(w) {
		t.Errorf("with no mapping: reasons %v, %v; want the list entities named", w.Reasons["Policies"], err)
	}
}
