package interpreter_test

// INP-01, slice 3 (A320, A404, A712; REQ-INP-004, REQ-INT-004; PUS-08; INV-02): the contradiction flag of a follow-up is the core's, not the model's. With a Prior and the pack's flag configured, the
// flag is on the saved intent exactly when a slot the prior had resolved now holds a different value (the amount "50k, not 20k"); a model's flag where nothing differs is removed, and a slot the follow-up
// does not name is not a contradiction (it is asked for, A712). With no Prior, or no flag configured, nothing is added or removed. No database: spies, as in interpreter_test.go. All data is SYNTHETIC.

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"pgregory.net/rapid"

	"github.com/Noemai-org/noema/services/core/understanding/intentstore"
	"github.com/Noemai-org/noema/services/core/understanding/interpreter"
)

const flag = "CONTRADICTS_PRIOR_TURN"

func amount(minor int64, unit string) map[string]any {
	return map[string]any{"type": "Quantity", "value": minor, "unit": unit, "confidence": 9500}
}

func withSlots(slots map[string]any, flags ...string) func(map[string]any) {
	return func(m map[string]any) {
		have := m["slots"].(map[string]any)
		for k, v := range slots {
			have[k] = v
		}
		if flags != nil {
			l := make([]any, len(flags))
			for i, f := range flags {
				l[i] = f
			}
			m["ambiguity_flags"] = l
		}
	}
}

// priorWith is the first turn, run through the real gate, with the given slots added.
func priorWith(t *testing.T, slots map[string]any) *intentstore.Admitted {
	t.Helper()
	w := newWorld(t, proposal(withSlots(slots)))
	if _, err := w.in.Interpret(context.Background(), input()); err != nil {
		t.Fatal(err)
	}
	p := w.save.saved[0]
	return &p
}

func followUpWith(t *testing.T, prior *intentstore.Admitted, configured string, model []byte) []string {
	t.Helper()
	w := newWorldWithFlag(t, model, configured)
	in := input()
	in.Prior = prior
	if _, err := w.in.Interpret(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Flags []string `json:"ambiguity_flags"`
	}
	if err := json.Unmarshal(w.save.saved[0].JSON, &doc); err != nil {
		t.Fatal(err)
	}
	return doc.Flags
}

func newWorldWithFlag(t *testing.T, out []byte, configured string) *world {
	t.Helper()
	w := newWorld(t, out)
	gate := mustGate(t)
	var err error
	w.in, err = interpreter.New(interpreter.Config{Compiler: w.comp, Gate: gate, Recorder: w.rec, Saver: w.save, ContradictionFlag: configured})
	if err != nil {
		t.Fatal(err)
	}
	return w
}


// PUS-08: "it was 50k, not 20k".
func TestAnAmountThatDiffersFromTheOneThePriorHeldRaisesTheFlag(t *testing.T) {
	prior := priorWith(t, map[string]any{"amount": amount(2_000_000, "NGN")})
	got := followUpWith(t, prior, flag, proposal(withSlots(map[string]any{"amount": amount(5_000_000, "NGN")})))
	if !slices.Contains(got, flag) {
		t.Errorf("flags %v: a changed amount must raise %s", got, flag)
	}
}

func TestTheSameAmountRaisesNoFlagAndTheModelsFlagIsRemoved(t *testing.T) {
	prior := priorWith(t, map[string]any{"amount": amount(2_000_000, "NGN")})
	got := followUpWith(t, prior, flag, proposal(withSlots(map[string]any{"amount": amount(2_000_000, "NGN")}, flag)))
	if slices.Contains(got, flag) {
		t.Errorf("flags %v: the model's flag is advisory and nothing differs", got)
	}
}

func TestASlotTheFollowUpDoesNotNameIsNotAContradiction(t *testing.T) {
	prior := priorWith(t, map[string]any{"amount": amount(2_000_000, "NGN")})
	got := followUpWith(t, prior, flag, proposal(nil))
	if slices.Contains(got, flag) {
		t.Errorf("flags %v: a slot left out is asked for, not contradicted (A712)", got)
	}
}

func TestADifferentUnitOrTimeWindowOrResolvedResourceRaisesTheFlag(t *testing.T) {
	window := func(a, b string) map[string]any {
		return map[string]any{"type": "TimeRef", "surface_form": "yesterday", "window": []any{a, b}}
	}
	cases := map[string]struct{ prior, next map[string]any }{
		"unit":   {map[string]any{"amount": amount(2_000_000, "NGN")}, map[string]any{"amount": amount(2_000_000, "USD")}},
		"window": {map[string]any{"when": window("2026-10-01T05:00:00Z", "2026-10-01T09:00:00Z")}, map[string]any{"when": window("2026-10-02T05:00:00Z", "2026-10-02T09:00:00Z")}},
		"resource": {map[string]any{"subject_resource": map[string]any{"type": "Resource", "status": "RESOLVED", "resolved_ref": "fin.transaction:TX1"}},
			map[string]any{"subject_resource": map[string]any{"type": "Resource", "status": "RESOLVED", "resolved_ref": "fin.transaction:TX2"}}},
	}
	for name, c := range cases {
		prior := priorWith(t, c.prior)
		if got := followUpWith(t, prior, flag, proposal(withSlots(c.next))); !slices.Contains(got, flag) {
			t.Errorf("%s: flags %v, want %s", name, got, flag)
		}
	}
}

func TestAResolutionThePriorLackedIsNotAContradiction(t *testing.T) {
	unresolved := map[string]any{"type": "Resource", "status": "UNRESOLVED", "candidates_ref": nil}
	resolved := map[string]any{"type": "Resource", "status": "RESOLVED", "resolved_ref": "fin.transaction:TX2"}
	prior := priorWith(t, map[string]any{"subject_resource": unresolved})
	if got := followUpWith(t, prior, flag, proposal(withSlots(map[string]any{"subject_resource": resolved}))); slices.Contains(got, flag) {
		t.Errorf("flags %v: a slot the prior had not resolved cannot be contradicted", got)
	}
}

func TestOtherFlagsStayAndTheListStaysSortedAndWithoutADuplicate(t *testing.T) {
	prior := priorWith(t, map[string]any{"amount": amount(2_000_000, "NGN")})
	got := followUpWith(t, prior, flag, proposal(withSlots(map[string]any{"amount": amount(5_000_000, "NGN")}, "Z_OTHER", flag, "A_OTHER")))
	want := []string{"A_OTHER", flag, "Z_OTHER"}
	if !slices.Equal(got, want) {
		t.Errorf("flags %v, want %v", got, want)
	}
}

func TestWithNoPriorOrNoConfiguredFlagTheModelsFlagsStand(t *testing.T) {
	w := newWorldWithFlag(t, proposal(withSlots(map[string]any{"amount": amount(1, "NGN")}, flag)), flag)
	if _, err := w.in.Interpret(context.Background(), input()); err != nil { // a first turn: no prior
		t.Fatal(err)
	}
	if !slices.Contains(flagsOf(t, w.save.saved[0]), flag) {
		t.Error("a first turn lost the model's flag")
	}
	prior := priorWith(t, map[string]any{"amount": amount(2_000_000, "NGN")})
	if got := followUpWith(t, prior, "", proposal(withSlots(map[string]any{"amount": amount(5_000_000, "NGN")}))); slices.Contains(got, flag) {
		t.Errorf("flags %v: with no flag configured the core adds none", got)
	}
}

func flagsOf(t *testing.T, a intentstore.Admitted) []string {
	t.Helper()
	var doc struct {
		Flags []string `json:"ambiguity_flags"`
	}
	if err := json.Unmarshal(a.JSON, &doc); err != nil {
		t.Fatal(err)
	}
	return doc.Flags
}

func TestPropertyTheFlagIsRaisedExactlyWhenTheAmountDiffers(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		before := rapid.Int64Range(1, 1_000_000_000).Draw(rt, "before")
		after := rapid.OneOf(rapid.Just(before), rapid.Int64Range(1, 1_000_000_000)).Draw(rt, "after")
		modelFlag := rapid.Bool().Draw(rt, "the model raised it")
		prior := priorWith(t, map[string]any{"amount": amount(before, "NGN")})
		var flags []string
		if modelFlag {
			flags = []string{flag}
		}
		got := followUpWith(t, prior, flag, proposal(withSlots(map[string]any{"amount": amount(after, "NGN")}, flags...)))
		if raised := slices.Contains(got, flag); raised != (before != after) {
			rt.Fatalf("before %d after %d: raised %v", before, after, raised)
		}
	})
}
