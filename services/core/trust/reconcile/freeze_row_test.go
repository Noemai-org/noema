package reconcile_test

// RES-02 (REQ-RES-002; INV-20; A642 safer reading; US-14): a contradiction creates a freeze ROW, a Record event CONTRADICTION_FREEZE_SET, and the freeze is lifted only by a Trust-recorded
// resolution or escalation event (CONTRADICTION_FREEZE_CLEARED), never because the classification changed. This package builds the two events' facts and the effective answer; appending
// them to the Record is the Recorder's (a consumer-side port, C1's side). SYNTHETIC data.

import (
	"errors"
	"reflect"
	"testing"

	"github.com/Noemai-org/noema/services/core/trust/reconcile"
)

func contradictory() reconcile.Result {
	return reconcile.Result{Classification: reconcile.ContradictoryState, RuleID: "R4@1", Contributing: []string{"ev_b", "ev_a"},
		Contradictions: []reconcile.Contradiction{{Label: "switch_failed_credit_credited", A: "ev_a", B: "ev_b"}}}
}

// The row names the target, the rule that fired and the evidence, in a fixed order, and only a contradiction makes one.
func TestRES02ContradictionMakesAFreezeRowAndNothingElseDoes(t *testing.T) {
	set, err := reconcile.NewFreezeSet("ten_a", "txn_a", "fin.recon.rules@1", contradictory())
	if err != nil {
		t.Fatal(err)
	}
	want := reconcile.FreezeSet{TenantID: "ten_a", Target: "txn_a", RulesetID: "fin.recon.rules@1", RuleID: "R4@1", Evidence: []string{"ev_a", "ev_b"},
		Labels: []string{"switch_failed_credit_credited"}}
	if !reflect.DeepEqual(set, want) {
		t.Fatalf("got %+v want %+v", set, want)
	}
	for _, c := range []reconcile.Classification{reconcile.SuccessConfirmed, reconcile.InsufficientEvidence, reconcile.EventOrderUncertain, reconcile.ExecutionOutcomeUnknown} {
		r := contradictory()
		r.Classification = c
		if _, err := reconcile.NewFreezeSet("ten_a", "txn_a", "fin.recon.rules@1", r); !errors.Is(err, reconcile.ErrNotContradictory) {
			t.Errorf("%s: want ErrNotContradictory, got %v", c, err)
		}
	}
	for name, args := range map[string][3]string{"tenant": {"", "txn_a", "fin.recon.rules@1"}, "target": {"ten_a", "", "fin.recon.rules@1"}, "ruleset": {"ten_a", "txn_a", ""}} {
		if _, err := reconcile.NewFreezeSet(args[0], args[1], args[2], contradictory()); err == nil {
			t.Errorf("a freeze row without a %s was built", name)
		}
	}
}

// The freeze is lifted only by a recorded resolution or escalation: it names the decision that did it, and a bare "the classification changed" is not one.
func TestRES02OnlyARecordedResolutionOrEscalationClearsTheFreeze(t *testing.T) {
	for _, kind := range []reconcile.ClearedBy{reconcile.ClearedByResolution, reconcile.ClearedByEscalation} {
		c, err := reconcile.NewFreezeCleared("ten_a", "txn_a", kind, "dec_01ABCDEFGHJKMNPQRSTVWXYZ00")
		if err != nil || c.Target != "txn_a" || c.DecisionID != "dec_01ABCDEFGHJKMNPQRSTVWXYZ00" || c.By != kind {
			t.Errorf("%s: got %+v err=%v", kind, c, err)
		}
	}
	for name, a := range map[string]struct {
		by  reconcile.ClearedBy
		dec string
	}{"no decision": {reconcile.ClearedByResolution, ""}, "classification changed": {"CLASSIFICATION_CHANGED", "dec_01ABCDEFGHJKMNPQRSTVWXYZ00"}, "no kind": {"", "dec_01ABCDEFGHJKMNPQRSTVWXYZ00"}} {
		if _, err := reconcile.NewFreezeCleared("ten_a", "txn_a", a.by, a.dec); !errors.Is(err, reconcile.ErrNotCleared) {
			t.Errorf("%s: want ErrNotCleared, got %v", name, err)
		}
	}
}

// The effective freeze is the derived one OR a recorded one that was never cleared: when the evidence changes and the classification is no longer CONTRADICTORY_STATE, the target stays frozen
// until the clearing event is recorded (the safer reading). A reader that cannot answer is frozen too (fail closed).
func TestRES02ARecordedFreezeOutlivesTheClassification(t *testing.T) {
	cases := []struct {
		name    string
		derived bool
		rec     reconcile.RecordedFreeze
		want    bool
	}{
		{"nothing recorded, not contradictory", false, reconcile.FreezeNone, false},
		{"nothing recorded, contradictory now", true, reconcile.FreezeNone, true},
		{"recorded and still contradictory", true, reconcile.FreezeRecorded, true},
		{"recorded, classification converged since", false, reconcile.FreezeRecorded, true},
		{"cleared by a recorded decision, converged", false, reconcile.FreezeCleared, false},
		{"cleared before, contradictory again (a new freeze is needed)", true, reconcile.FreezeCleared, true},
		{"the recorder could not answer", false, reconcile.FreezeUnknown, true},
		{"an out-of-range value", false, reconcile.RecordedFreeze(99), true},
	}
	for _, c := range cases {
		if got := reconcile.EffectiveFrozen(c.derived, c.rec); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

// The gate refuses a non-allowed capability on a target the Record holds frozen, whatever the classification now says; the freeze allow-list still applies (REQ-RES-002).
func TestRES02TheGateFollowsTheEffectiveFreeze(t *testing.T) {
	gate := reconcile.NewFreezeGate([]string{"cap.escalate"})
	frozen := reconcile.EffectiveFrozen(false, reconcile.FreezeRecorded)
	if _, err := gate.PermitTarget(frozen, "cap.other", "txn_a"); !errors.Is(err, reconcile.ErrFrozen) {
		t.Errorf("a write on a recorded freeze must be refused: %v", err)
	}
	if p, err := gate.PermitTarget(frozen, "cap.escalate", "txn_a"); err != nil || !p.Valid() {
		t.Errorf("the allow-listed capability is permitted on a frozen target: %v", err)
	}
	lifted := reconcile.EffectiveFrozen(false, reconcile.FreezeCleared)
	if _, err := gate.PermitTarget(lifted, "cap.other", "txn_a"); err != nil {
		t.Errorf("after the clearing event the target is free: %v", err)
	}
}
