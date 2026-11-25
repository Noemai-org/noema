package main

// STM-01 lane 2 (A537, A469, A332, interim, relayed, not yet committed): two of the six Trust inputs of this lane cannot be built honestly from what is on main, and the composition
// names them rather than invent an input (AGENT-16, AGENT-20). Each is a gap even when every part the builder takes is present, and each leaves its TrustAdapters field nil so that
// /readyz names it ("trust.<Name>"; fixed text, no value).
//
//   - Writes: NOT a gap any more (A684 step 1, interim, relayed, not yet committed). The admitted intent carries the slot values since noema#913 and the production WriteRule over the
//     pack's `write_when` is on main (noema#935), so compose_writes.go builds it; compose_writes_test.go pins that. It is a gap only when its own parts are absent.
//   - Reconciler: the pack carries neither the reason code a classification escalates with nor the capabilities allowed under a freeze (reconciler README, "Pack data").
//   - Clocks: the routing of a clock phase to an escalation reason has no source in the pack (intakeadapters README), the anchor events have no entity or field mapping, and the pack's
//     trust limits (the at-risk fraction) are not pinned by the manifest.
//
// REQ-STM-001, REQ-TRU-002, REQ-TRU-007; INV-01, INV-03, INV-14, INV-19; AGENT-16. SYNTHETIC.

import (
	"slices"
	"testing"
)

func TestReconcilerAndClocksAreNamedGapsWhateverIsGiven(t *testing.T) {
	got, err := buildTrustInputs(tiInWithLists(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Reconciler", "Clocks"} {
		if !tiHas(got.Gaps, name) {
			t.Errorf("%s is not a named gap (gaps %v)", name, got.Gaps)
		}
	}
	a := got.fill(TrustAdapters{})
	if a.Reconciler != nil || a.Clocks != nil {
		t.Fatalf("fill gave a Reconciler or a Clocks: %+v", a)
	}
	unset := unsetAdapters(a)
	for _, name := range []string{"Reconciler", "Clocks"} {
		if !tiHas(unset, name) {
			t.Errorf("/readyz would not name trust.%s (unset %v)", name, unset)
		}
	}
}

// The gaps are named in the order of the TrustAdapters struct, and a component that was built is never in the list.
func TestTheGapsAreInTheOrderOfTheAdapterStructAndExcludeWhatIsBuilt(t *testing.T) {
	got, err := buildTrustInputs(tiInWithLists(t))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Reconciler", "Clocks"}
	if !slices.Equal(got.Gaps, want) {
		t.Fatalf("gaps %v; want %v (Writes, Ownership, Evidence and Policies are built)", got.Gaps, want)
	}
}

// With nothing given every one of the six is a gap, in the order of the struct, and the builder still returns no error: the process starts and /readyz names each.
func TestWithNothingGivenEveryInputIsAGapInStructOrder(t *testing.T) {
	got, err := buildTrustInputs(trustInputsIn{})
	if err != nil {
		t.Fatalf("a gap is not an error: %v", err)
	}
	want := []string{"Writes", "Ownership", "Evidence", "Reconciler", "Policies", "Clocks"}
	if !slices.Equal(got.Gaps, want) {
		t.Fatalf("gaps %v; want %v", got.Gaps, want)
	}
	if got.Ownership != nil || got.Evidence != nil || got.Policies != nil {
		t.Fatalf("a port was built from nothing: %+v", got)
	}
}
