package reconcile_test

// RES-02 (REQ-RES-002; INV-20; A642 safer reading; US-14): a contradiction creates a freeze ROW, a Record event CONTRADICTION_FREEZE_SET, and the freeze is lifted only by a Trust-recorded
// resolution or escalation event (CONTRADICTION_FREEZE_CLEARED), never because the classification changed. This package builds the two events' facts and the effective answer; appending
// them to the Record is the Recorder's (a consumer-side port, C1's side). SYNTHETIC data. A642 and the Founder's decision D1 are in DEC-2026-11-24-architect-rulings-a624-a643 and DEC-2026-11-24-founder-decisions-t10-wait-and-921.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Noemai-org/noema/services/core/internal/predicate"
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

func evidence(id, source, entity string, at int64, fields map[string]predicate.Value) reconcile.Evidence {
	return reconcile.Evidence{ID: id, Source: source, Entity: entity, Status: reconcile.Known, EffectiveAt: at, ExpiresAt: 99999, Required: true, Fields: fields}
}

// mixedEvidence is one pair that has expired at now=1200 (switch FAILED against credit CREDITED, window 100 from 1000) and one still inside its window (a posted reversal against a debit that is
// NOT_POSTED, window 600 from 1050).
func mixedEvidence() []reconcile.Evidence {
	return []reconcile.Evidence{
		evidence("ev_switch", "fin.src.switch", "switch", 1000, map[string]predicate.Value{"state": predicate.Enum("FAILED")}),
		evidence("ev_credit", "fin.src.ledger", "credit", 1200, map[string]predicate.Value{"state": predicate.Enum("CREDITED")}),
		evidence("ev_reversal", "fin.src.ledger", "reversal", 1050, map[string]predicate.Value{"state": predicate.Enum("POSTED"), "original_debit_linked": predicate.Bool(true)}),
		evidence("ev_debit", "fin.src.ledger", "debit", 1100, map[string]predicate.Value{"state": predicate.Enum("NOT_POSTED")}),
	}
}

// A677, EL finding 2 on this PR (after #949): a result with one expired pair and one pair still inside its window freezes on the expired pair alone. The row names its label and its
// evidence; the waiting pair is in neither Labels nor Evidence.
func TestRES02AMixedResultFreezesOnTheExpiredPairAlone(t *testing.T) {
	rs := ruleSetWithWindows(t, map[string]int64{"switch_failed_credit_credited": 100, "reversal_conflict": 600})
	res, err := reconcile.Reconcile(rs, mixedEvidence(), 1200)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.InWindow) != 1 || res.InWindow[0].Label != "reversal_conflict" {
		t.Fatalf("the scenario needs one waiting pair, got %+v", res.InWindow)
	}
	set, err := reconcile.NewFreezeSet("ten_a", "txn_a", rs.ID(), res)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(set.Labels, []string{"switch_failed_credit_credited"}) {
		t.Errorf("Labels must hold the expired pair alone, got %v", set.Labels)
	}
	if !reflect.DeepEqual(set.Evidence, []string{"ev_credit", "ev_switch"}) {
		t.Errorf("Evidence must hold the expired pair's alone, got %v", set.Evidence)
	}
}

// EL finding 3: the row always names what disagreed, whatever the matched rule `uses`. Here R4 (the contradiction rule) uses nothing, so Contributing is empty; the row still holds both
// evidence ids of the freezing pair, and nothing of a waiting one.
func TestRES02TheRowNamesWhatDisagreedEvenWhenTheRuleUsesNothing(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "packs", "fintech", "recon_rules", "rules.v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	for _, m := range doc["incompatible"].([]any) {
		e := m.(map[string]any)
		switch e["label"] {
		case "switch_failed_credit_credited":
			e["convergence_window_seconds"] = 100
		case "reversal_conflict":
			e["convergence_window_seconds"] = 600
		}
	}
	for _, r := range doc["rules"].([]any) {
		if e := r.(map[string]any); e["id"] == "R4" {
			e["uses"] = []any{}
		}
	}
	out, _ := json.Marshal(doc)
	rs, err := reconcile.ParseRuleSet(out)
	if err != nil {
		t.Fatal(err)
	}
	res, err := reconcile.Reconcile(rs, mixedEvidence(), 1200)
	if err != nil {
		t.Fatal(err)
	}
	if res.Classification != reconcile.ContradictoryState || len(res.Contributing) != 0 {
		t.Fatalf("the scenario needs a contradiction whose rule uses nothing, got %s contributing=%v", res.Classification, res.Contributing)
	}
	set, err := reconcile.NewFreezeSet("ten_a", "txn_a", rs.ID(), res)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(set.Evidence, []string{"ev_credit", "ev_switch"}) {
		t.Errorf("the row must name both items that disagreed and no waiting one, got %v", set.Evidence)
	}
}

// C1 note 2, EL finding 5: a freeze that an ESCALATION cleared, then a second contradiction inside its window: the target is not frozen while it waits (the recorded freeze is cleared and
// the result waits) and is frozen at the deadline, by a new freeze row, never by a clearing.
func TestRES02AClearedFreezeThenASecondContradictionInsideAWindow(t *testing.T) {
	const window = 600
	rs := ruleSetWith(t, "switch_failed_credit_credited", win(window))
	ev := pair() // switch FAILED at 1000, credit CREDITED at 1200; deadline 1600
	cleared, err := reconcile.NewFreezeCleared("ten_a", "txn_a", reconcile.ClearedByEscalation, "dec_01ABCDEFGHJKMNPQRSTVWXYZ00")
	if err != nil || cleared.By != reconcile.ClearedByEscalation {
		t.Fatalf("the escalation clears the first freeze: %+v %v", cleared, err)
	}
	for _, now := range []int64{1200, 1599} {
		res, err := reconcile.Reconcile(rs, ev, now)
		if err != nil {
			t.Fatal(err)
		}
		if reconcile.EffectiveFrozen(res.Frozen(), reconcile.FreezeCleared) {
			t.Errorf("now=%d: waiting inside the window after a cleared freeze must not be frozen", now)
		}
		if _, err := reconcile.NewFreezeSet("ten_a", "txn_a", rs.ID(), res); !errors.Is(err, reconcile.ErrNotContradictory) {
			t.Errorf("now=%d: a waiting result makes no freeze row: %v", now, err)
		}
	}
	res, err := reconcile.Reconcile(rs, ev, 1600)
	if err != nil {
		t.Fatal(err)
	}
	if !reconcile.EffectiveFrozen(res.Frozen(), reconcile.FreezeCleared) {
		t.Error("at the deadline the second contradiction freezes, whatever was cleared before")
	}
	if _, err := reconcile.NewFreezeSet("ten_a", "txn_a", rs.ID(), res); err != nil {
		t.Errorf("at the deadline a new freeze row is made: %v", err)
	}
}

type freezeReader struct {
	rec reconcile.RecordedFreeze
	err error
}

func (f freezeReader) RecordedFreeze(context.Context, string, string) (reconcile.RecordedFreeze, error) {
	return f.rec, f.err
}

// SR condition 2: an error is FreezeUnknown whatever the reader returned with it, so a caller cannot read an error as "no freeze". Without an error the answer stands, except an
// out-of-range value, which is FreezeUnknown too.
func TestRES02ReadFreezeReadsAnyErrorAsUnknown(t *testing.T) {
	boom := errors.New("record unavailable")
	for name, tc := range map[string]struct {
		r    reconcile.FreezeReader
		want reconcile.RecordedFreeze
	}{
		"none with an error":     {freezeReader{reconcile.FreezeNone, boom}, reconcile.FreezeUnknown},
		"cleared with an error":  {freezeReader{reconcile.FreezeCleared, boom}, reconcile.FreezeUnknown},
		"recorded with an error": {freezeReader{reconcile.FreezeRecorded, boom}, reconcile.FreezeUnknown},
		"none":                   {freezeReader{reconcile.FreezeNone, nil}, reconcile.FreezeNone},
		"recorded":               {freezeReader{reconcile.FreezeRecorded, nil}, reconcile.FreezeRecorded},
		"cleared":                {freezeReader{reconcile.FreezeCleared, nil}, reconcile.FreezeCleared},
		"unknown":                {freezeReader{reconcile.FreezeUnknown, nil}, reconcile.FreezeUnknown},
		"out of range":           {freezeReader{reconcile.RecordedFreeze(99), nil}, reconcile.FreezeUnknown},
		"no reader":              {nil, reconcile.FreezeUnknown},
	} {
		if got := reconcile.ReadFreeze(context.Background(), tc.r, "ten_a", "txn_a"); got != tc.want {
			t.Errorf("%s: got %v want %v", name, got, tc.want)
		}
	}
	if !reconcile.EffectiveFrozen(false, reconcile.ReadFreeze(context.Background(), freezeReader{reconcile.FreezeNone, boom}, "ten_a", "txn_a")) {
		t.Error("a reader error must end in a frozen target")
	}
}

// SR condition 1 and EL finding 1, the part this package can check without the Record: a decision id has the dec_ prefix, something after it and no control character. That the decision
// exists, is the same tenant's, is a RESOLUTION or ESCALATE outcome and names this target is the Recorder's (status/RES-02.md lists it).
func TestRES02ADecisionIDHasTheShapeOfADecisionID(t *testing.T) {
	for _, bad := range []string{"01ABCDEFGHJKMNPQRSTVWXYZ00", "dec_", "dec", "DEC_01ABC", "dec_01\nABC", "dec_01\x00ABC", "dec_01\tABC", "dec_01\x7fABC", " dec_01ABC", "dec_01ABC\r"} {
		if _, err := reconcile.NewFreezeCleared("ten_a", "txn_a", reconcile.ClearedByResolution, bad); !errors.Is(err, reconcile.ErrNotCleared) {
			t.Errorf("%q: want ErrNotCleared, got %v", bad, err)
		}
	}
	if _, err := reconcile.NewFreezeCleared("ten_a", "txn_a", reconcile.ClearedByResolution, "dec_01ABCDEFGHJKMNPQRSTVWXYZ00"); err != nil {
		t.Errorf("a well-formed decision id is accepted: %v", err)
	}
}
