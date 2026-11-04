package clocks_test

// REQ-TRU-006 (PERD 23.5): EVALUATE_SLA_STATUS. A missing anchor is NOT_STARTED, never assumed. Phases: NOT_STARTED, RUNNING,
// AT_RISK (remaining below the at-risk fraction, strictly) and BREACHED. Integers only (INV-21); the clock label is carried
// (every clock is labelled SYNTHETIC until validated by counsel, SoT 50.2, AGENT-25). The at-risk fraction is the caller's
// input (from configuration), never a literal here (REQ-OPS-003). All values are SYNTHETIC.

import (
	"errors"
	"math/rand"
	"testing"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/trust/clocks"
)

const (
	hour     = int64(3600)
	duration = 72 * hour
)

var quarter = clocks.Ratio{Num: 1, Den: 4}

func clock(label contracts.ClockV1Label) contracts.ClockV1 {
	return contracts.ClockV1{ClockId: "syn.clock.test_72h", AnchorEvent: "complaint_logged", DurationSeconds: duration, Label: label}
}

func at(anchor, now int64) clocks.Status {
	a := anchor
	st, err := clocks.Evaluate(clock(contracts.ClockV1LabelSYNTHETIC), &a, now, quarter)
	if err != nil {
		panic(err)
	}
	return st
}

func TestAMissingAnchorIsNotStartedAndNothingIsAssumed(t *testing.T) {
	st, err := clocks.Evaluate(clock(contracts.ClockV1LabelSYNTHETIC), nil, 1_000, quarter)
	if err != nil {
		t.Fatal(err)
	}
	if st.Phase != clocks.NotStarted || st.StartedAt != nil || st.DeadlineAt != nil || st.Elapsed != 0 || st.Remaining != nil {
		t.Fatalf("%+v", st)
	}
}

func TestPhasesAroundTheirBoundaries(t *testing.T) {
	const start = int64(1_000_000)
	atRiskFrom := start + duration - duration/4 // remaining == 25% exactly: still RUNNING (strictly below is AT_RISK)
	cases := []struct {
		now       int64
		phase     clocks.Phase
		remaining int64
	}{
		{start, clocks.Running, duration},
		{atRiskFrom - 1, clocks.Running, duration/4 + 1},
		{atRiskFrom, clocks.Running, duration / 4},
		{atRiskFrom + 1, clocks.AtRisk, duration/4 - 1},
		{start + duration - 1, clocks.AtRisk, 1},
		{start + duration, clocks.Breached, 0},
		{start + duration + 5*hour, clocks.Breached, 0},
	}
	for _, c := range cases {
		st := at(start, c.now)
		if st.Phase != c.phase || st.Remaining == nil || *st.Remaining != c.remaining {
			t.Errorf("now=%d: %+v want %s remaining %d", c.now, st, c.phase, c.remaining)
		}
		if st.StartedAt == nil || *st.StartedAt != start || st.DeadlineAt == nil || *st.DeadlineAt != start+duration || st.Elapsed != c.now-start {
			t.Errorf("now=%d: anchor fields %+v", c.now, st)
		}
	}
}

func TestTheLabelIsCarriedAndVerifiedIsNotAssumed(t *testing.T) {
	a := int64(10)
	for _, l := range []contracts.ClockV1Label{contracts.ClockV1LabelSYNTHETIC, contracts.ClockV1LabelVALIDATED} {
		st, err := clocks.Evaluate(clock(l), &a, 20, quarter)
		if err != nil || st.Label != l || st.ClockID != "syn.clock.test_72h" {
			t.Fatalf("%v: %+v %v", l, st, err)
		}
	}
	if _, err := clocks.Evaluate(clock("OTHER"), &a, 20, quarter); !errors.Is(err, clocks.ErrInvalid) {
		t.Fatalf("an unknown label must be refused: %v", err)
	}
}

func TestInputsThatCannotBeEvaluatedAreErrorsNeverGuesses(t *testing.T) {
	a := int64(1_000)
	bad := clock(contracts.ClockV1LabelSYNTHETIC)
	bad.DurationSeconds = 0
	neg := clock(contracts.ClockV1LabelSYNTHETIC)
	neg.DurationSeconds = -1
	huge := clock(contracts.ClockV1LabelSYNTHETIC)
	huge.DurationSeconds = 1 << 62
	for name, run := range map[string]func() error{
		"anchor after now": func() error {
			_, e := clocks.Evaluate(clock(contracts.ClockV1LabelSYNTHETIC), &a, a-1, quarter)
			return e
		},
		"zero duration":      func() error { _, e := clocks.Evaluate(bad, &a, a, quarter); return e },
		"negative duration":  func() error { _, e := clocks.Evaluate(neg, &a, a, quarter); return e },
		"duration overflows": func() error { _, e := clocks.Evaluate(huge, &a, a, quarter); return e },
		"zero denominator": func() error {
			_, e := clocks.Evaluate(clock(contracts.ClockV1LabelSYNTHETIC), &a, a, clocks.Ratio{Num: 1, Den: 0})
			return e
		},
		"fraction above one": func() error {
			_, e := clocks.Evaluate(clock(contracts.ClockV1LabelSYNTHETIC), &a, a, clocks.Ratio{Num: 5, Den: 4})
			return e
		},
		"negative numerator": func() error {
			_, e := clocks.Evaluate(clock(contracts.ClockV1LabelSYNTHETIC), &a, a, clocks.Ratio{Num: -1, Den: 4})
			return e
		},
		"anchor too large": func() error {
			x := int64(1) << 62
			_, e := clocks.Evaluate(clock(contracts.ClockV1LabelSYNTHETIC), &x, x, quarter)
			return e
		},
	} {
		if err := run(); !errors.Is(err, clocks.ErrInvalid) {
			t.Errorf("%s: got %v", name, err)
		}
	}
}

// The phase never moves backwards as time passes, remaining never grows, and elapsed + remaining is the duration until the
// deadline: a seeded property over random anchors and instants.
func TestPhaseIsMonotoneInTime(t *testing.T) {
	rng := rand.New(rand.NewSource(20261104))
	order := map[clocks.Phase]int{clocks.Running: 0, clocks.AtRisk: 1, clocks.Breached: 2}
	for n := 0; n < 5_000; n++ {
		start := rng.Int63n(1 << 40)
		prev, prevRem := -1, duration+1
		last := start
		for step := 0; step < 24; step++ {
			last += rng.Int63n(duration / 8)
			st := at(start, last)
			if order[st.Phase] < prev {
				t.Fatalf("phase went backwards at +%d: %+v", last-start, st)
			}
			if int(*st.Remaining) > prevRem {
				t.Fatalf("remaining grew at +%d: %+v", last-start, st)
			}
			if st.Phase != clocks.Breached && st.Elapsed+*st.Remaining != duration {
				t.Fatalf("elapsed + remaining != duration: %+v", st)
			}
			prev, prevRem = order[st.Phase], int(*st.Remaining)
		}
	}
}
