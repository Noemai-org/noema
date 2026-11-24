package main

// STM-01 lane 2 (A537, interim, relayed, not yet committed): the composition builds the Evidence adapter (T9) from the pack's trust profiles (PackProfiles over the verified pack bytes),
// the tenants' own sealed-snapshot reads, the injected clock and the Reconciler's own facts (the rule set and the mapping main already builds: one mapping, no second). REQ-STM-001,
// REQ-TRU-001, REQ-TRU-002, REQ-CTX-003, REQ-CTX-004; INV-11, INV-14, INV-16, INV-19; AGENT-13, AGENT-16. Nothing is dialled by the build; the one test that reads a snapshot needs
// PostgreSQL with pgsodium and runs in CI only. SYNTHETIC.

import (
	"errors"
	"strings"
	"testing"

	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
)

func TestEvidenceIsBuiltFromThePackProfilesAndTheUnderstandingParts(t *testing.T) {
	got, err := buildTrustInputs(tiIn(t))
	if err != nil {
		t.Fatal(err)
	}
	if got.Evidence == nil || tiHas(got.Gaps, "Evidence") {
		t.Fatalf("Evidence is not built (gaps %v)", got.Gaps)
	}
	if a := got.fill(TrustAdapters{}); a.Evidence == nil {
		t.Fatal("fill did not give the composition the Evidence adapter")
	}
}

// Each part Evidence needs, absent, makes it a named gap and never an adapter built over a default; the process starts (A469). The rule set and the mapping are Understanding's parts.
func TestEvidenceWithAPartMissingIsANamedGap(t *testing.T) {
	cases := map[string]func(*trustInputsIn){
		"no rule set":         func(in *trustInputsIn) { in.RuleSet = nil },
		"no mapping":          func(in *trustInputsIn) { in.Mapping = nil },
		"no payload source":   func(in *trustInputsIn) { in.Payloads = nil },
		"no injection screen": func(in *trustInputsIn) { in.Screen.Screen = nil },
		"no clock":            func(in *trustInputsIn) { in.Clock = nil },
		"no verified pack":    func(in *trustInputsIn) { in.Verified.Pack = nil },
	}
	for name, mut := range cases {
		in := tiIn(t)
		mut(&in)
		got, err := buildTrustInputs(in)
		if err != nil {
			t.Errorf("%s: a gap is not an error: %v", name, err)
			continue
		}
		if got.Evidence != nil || !tiHas(got.Gaps, "Evidence") {
			t.Errorf("%s: Evidence %v, gaps %v; want none and a named gap", name, got.Evidence, got.Gaps)
		}
	}
}

// A value that is present and invalid stops the process, and the error names no content of the pack (INV-16, AGENT-13, AGENT-16).
func TestEvidenceWithAnInvalidInputIsAnErrorThatCarriesNoPackContent(t *testing.T) {
	cases := map[string]func(*trustInputsIn){
		"a pack file that does not decode": func(in *trustInputsIn) { in.Verified.Pack = []byte(`{"intent_nodes":"SYNTHETIC-LEAK-7"}`) },
		"a timeout of zero":                func(in *trustInputsIn) { in.Timeout = 0 },
		"a served tenant with no login":    func(in *trustInputsIn) { in.Tenants = append(in.Tenants, "ten_cc") },
	}
	for name, mut := range cases {
		in := tiIn(t)
		mut(&in)
		got, err := buildTrustInputs(in)
		if err == nil {
			t.Errorf("%s: built %v with no error", name, got.Evidence)
			continue
		}
		if strings.Contains(err.Error(), "SYNTHETIC-LEAK-7") {
			t.Errorf("%s: the error carries pack content: %q", name, err)
		}
	}
}

// CI only (needs PostgreSQL with pgsodium): through the composed adapter, a snapshot that does not exist is ErrSnapshotNotFound for a served tenant, and a tenant with no login is an
// error and never a snapshot of another tenant's (INV-11, INV-19).
func TestEvidenceOverRealLoginsFindsNoSnapshotAndRefusesAnUnservedTenant(t *testing.T) {
	in := tiIn(t)
	in.Pools = tiDBPools(t, "ten_aa", "ten_bb")
	got, err := buildTrustInputs(in)
	if err != nil || got.Evidence == nil {
		t.Fatalf("built %v, %v", got.Evidence, err)
	}
	if _, err := got.Evidence.SnapshotFreshness(tiDeadline(t), "ten_aa", "evs_aa11"); !errors.Is(err, trustport.ErrSnapshotNotFound) {
		t.Errorf("got %v; want ErrSnapshotNotFound", err)
	}
	if _, err := got.Evidence.SnapshotFreshness(tiDeadline(t), "ten_zz", "evs_aa11"); err == nil || errors.Is(err, trustport.ErrSnapshotNotFound) {
		t.Errorf("got %v; want an error that is not 'not found' for a tenant with no login", err)
	}
}
