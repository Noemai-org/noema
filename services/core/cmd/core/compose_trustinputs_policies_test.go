package main

// STM-01 lane 2 (A537, interim, relayed, not yet committed): the composition builds the Policies adapter (T11) from the pack's policy book (LoadPolicyBook over the verified pack bytes),
// the tenants' own publication reads, the admission time of the stored intent and the fact source over the sealed snapshot's lists (the same mapping as the Reconciler's). REQ-STM-001,
// REQ-TRU-004, REQ-TRU-005, REQ-TRU-006, REQ-TRU-007; INV-09, INV-14, INV-16, INV-19; AGENT-13, AGENT-16. The list entities and their scope prefixes are the evidence read's convention,
// which no committed ruling gives (intakesources README: "EL must agree"); the test passes SYNTHETIC ones through the seam and the production seam stays empty, so Policies is then a named
// gap. The one test that reads a database needs PostgreSQL with pgsodium and runs in CI only. SYNTHETIC.

import (
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust/intakesources"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

// tiLists is a SYNTHETIC convention for the two list entities of the pack's policy: the source and the scope prefix of the item that holds each list.
func tiLists() map[string]intakesources.ListSpec {
	return map[string]intakesources.ListSpec{
		"case":          {Source: "synthetic.source.cases", ScopePrefix: "synthetic.case:"},
		"complaint_ref": {Source: "synthetic.source.cases", ScopePrefix: "synthetic.complaint:"},
	}
}

func tiInWithLists(t *testing.T) trustInputsIn {
	t.Helper()
	in := tiIn(t)
	in.ListEntities = tiLists()
	return in
}

func TestPoliciesIsBuiltFromThePolicyBookAndTheFactSource(t *testing.T) {
	got, err := buildTrustInputs(tiInWithLists(t))
	if err != nil {
		t.Fatal(err)
	}
	if got.Policies == nil || tiHas(got.Gaps, "Policies") {
		t.Fatalf("Policies is not built (gaps %v)", got.Gaps)
	}
	if a := got.fill(TrustAdapters{}); a.Policies == nil {
		t.Fatal("fill did not give the composition the Policies adapter")
	}
}

// Each part Policies needs, absent, makes it a named gap and never an adapter that answers without facts (INV-19): with no list entity the facts of a rule that reads a list cannot be had.
func TestPoliciesWithAPartMissingIsANamedGap(t *testing.T) {
	cases := map[string]func(*trustInputsIn){
		"no list entity":      func(in *trustInputsIn) { in.ListEntities = nil },
		"no mapping":          func(in *trustInputsIn) { in.Mapping = nil },
		"no payload source":   func(in *trustInputsIn) { in.Payloads = nil },
		"no injection screen": func(in *trustInputsIn) { in.Screen.Alert = nil },
		"no verified pack":    func(in *trustInputsIn) { in.Verified.Pack = nil },
	}
	for name, mut := range cases {
		in := tiInWithLists(t)
		mut(&in)
		got, err := buildTrustInputs(in)
		if err != nil {
			t.Errorf("%s: a gap is not an error: %v", name, err)
			continue
		}
		if got.Policies != nil || !tiHas(got.Gaps, "Policies") {
			t.Errorf("%s: Policies %v, gaps %v; want none and a named gap", name, got.Policies, got.Gaps)
		}
	}
}

// A pack whose policy cannot be loaded, an overlapping list convention and a timeout of zero stop the process, and the error carries no pack content (INV-16, AGENT-13, AGENT-16).
func TestPoliciesWithAnInvalidInputIsAnErrorThatCarriesNoPackContent(t *testing.T) {
	cases := map[string]func(*trustInputsIn){
		"a pack with no policy": func(in *trustInputsIn) { in.Verified.Pack = []byte(`{"intent_nodes":[],"SYNTHETIC-LEAK-8":1}`) },
		"two list prefixes that overlap": func(in *trustInputsIn) {
			in.ListEntities["complaint_ref"] = intakesources.ListSpec{Source: "synthetic.source.cases", ScopePrefix: "synthetic.case:x"}
		},
		"a timeout of zero": func(in *trustInputsIn) { in.Timeout = 0 },
	}
	for name, mut := range cases {
		in := tiInWithLists(t)
		mut(&in)
		got, err := buildTrustInputs(in)
		if err == nil {
			t.Errorf("%s: built %v with no error", name, got.Policies)
			continue
		}
		if strings.Contains(err.Error(), "SYNTHETIC-LEAK-8") {
			t.Errorf("%s: the error carries pack content: %q", name, err)
		}
	}
}

// CI only (needs PostgreSQL with pgsodium): through the composed adapter an intent that is not stored gives an error and never a permit, and a tenant with no login is an error (INV-19, INV-11).
func TestPoliciesOverRealLoginsNeverPermitsWhatItCannotRead(t *testing.T) {
	in := tiInWithLists(t)
	in.Pools = tiDBPools(t, "ten_aa", "ten_bb")
	got, err := buildTrustInputs(in)
	if err != nil || got.Policies == nil {
		t.Fatalf("built %v, %v", got.Policies, err)
	}
	node, _ := tiOwnedNode(t)
	q := intake.PolicyQuery{TenantID: "ten_aa", IntentID: "int_synth01", IntentVersion: 1, IntentNode: node, CapabilityID: "fin.cap.create_escalation_case", SnapshotID: "evs_aa11", Now: 1_800_000_000}
	if ev, err := got.Policies.Evaluate(tiDeadline(t), q); err == nil {
		t.Errorf("got %+v and no error for an intent that is not stored", ev)
	}
	q.TenantID = "ten_zz"
	if ev, err := got.Policies.Evaluate(tiDeadline(t), q); err == nil {
		t.Errorf("got %+v and no error for a tenant with no login", ev)
	}
}
