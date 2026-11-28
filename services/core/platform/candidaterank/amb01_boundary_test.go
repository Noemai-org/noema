package candidaterank

// AMB-01 Done-when (plan): "Same inputs give the same order 1,000 times; boundary tests at margin 0.349, 0.35 and 0.351 pass". REQ-AMB-003, INV-21, AGENT-16. All data is SYNTHETIC.
// These are characterisation tests of the merged ranker (TRU-15): they add no behaviour, and each is killed by a hand mutant (see status/AMB-01.md).

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

// marginSet builds two candidates whose exact margin is wantMarginBP: the top has every feature full (10000), the second has the amount and the time exact (5500 together under the
// goodConfig weights), no alias, no direction, and a recipient feature chosen so that its score is 10000 - wantMarginBP. The recipient weight is 2500, so one basis point of score is
// four basis points of the recipient feature.
func marginSet(t *testing.T, wantMarginBP int64) ([]Candidate, Target) {
	t.Helper()
	at := time.Date(2026, 11, 20, 9, 30, 0, 0, time.UTC)
	tgt := Target{HasAmount: true, AmountMinor: 100000, Currency: "NGN", HasWindow: true, From: at.Add(-30 * time.Minute), To: at.Add(30 * time.Minute), ExpectedDirection: "OUTGOING"}
	secondScore := UnitBP - wantMarginBP
	recipientBP := (secondScore - 5500) * 4
	if recipientBP < 0 || recipientBP > UnitBP {
		t.Fatalf("margin %d: the second's recipient feature %d is out of range", wantMarginBP, recipientBP)
	}
	top := Candidate{ID: "cand_top", AmountMinor: 100000, Currency: "NGN", OccurredAt: at, RecipientBP: UnitBP, AliasMatch: true, Direction: "OUTGOING"}
	second := Candidate{ID: "cand_second", AmountMinor: 100000, Currency: "NGN", OccurredAt: at, RecipientBP: recipientBP, Direction: "INCOMING"}
	return []Candidate{second, top}, tgt
}

// REQ-AMB-003: the auto-resolve margin of 0.35 (3500 basis points) is read at 0.349, 0.35 and 0.351: the first does not resolve, the other two do. The set is otherwise clean (the top is
// above the floor, no flag, one candidate above the floor), so the margin is the only thing that decides.
func TestTheMarginIsDecidedAt0349And035And0351(t *testing.T) {
	c := goodConfig()
	cases := []struct {
		name       string
		marginBP   int64
		wantState  State
		wantWhyNot []Why
	}{
		{"0.349 does not resolve", 3490, Ambiguous, []Why{MarginNotMet}},
		{"0.35 resolves", 3500, AutoResolved, nil},
		{"0.351 resolves", 3510, AutoResolved, nil},
	}
	for _, tc := range cases {
		cands, tgt := marginSet(t, tc.marginBP)
		got, err := c.Rank(cands, tgt, Context{})
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got.MarginBP != tc.marginBP || got.TopBP != UnitBP {
			t.Errorf("%s: top %d margin %d, want %d and %d (the set is not the one under test)", tc.name, got.TopBP, got.MarginBP, UnitBP, tc.marginBP)
		}
		if got.State != tc.wantState || fmt.Sprint(got.WhyNot) != fmt.Sprint(tc.wantWhyNot) {
			t.Errorf("%s: state %s why %v, want %s and %v", tc.name, got.State, got.WhyNot, tc.wantState, tc.wantWhyNot)
		}
		if (tc.wantState == AutoResolved) != (got.ResolvedID == "cand_top") || (tc.wantState != AutoResolved && got.ResolvedID != "") {
			t.Errorf("%s: resolved %q for state %s", tc.name, got.ResolvedID, got.State)
		}
	}
}

// REQ-AMB-003: the same reading at the 0.70 floor: 0.699 is below it, 0.70 and 0.701 reach it. A single candidate has a margin of its whole score (the second is 0), so the floor is the
// only rule that decides here.
func TestTheFloorIsDecidedAt0699And070And0701(t *testing.T) {
	c := goodConfig()
	at := time.Date(2026, 11, 20, 9, 30, 0, 0, time.UTC)
	tgt := Target{HasAmount: true, AmountMinor: 100000, Currency: "NGN", HasWindow: true, From: at.Add(-30 * time.Minute), To: at.Add(30 * time.Minute)}
	for _, tc := range []struct {
		name      string
		score     int64
		wantState State
		wantWhy   []Why
	}{
		{"0.699 is below the floor", 6990, Ambiguous, []Why{BelowFloor}},
		{"0.70 reaches the floor", 7000, AutoResolved, nil},
		{"0.701 reaches the floor", 7010, AutoResolved, nil},
	} {
		// amount and time exact (5500) plus the recipient feature that makes up the rest: four basis points of the feature for each of score.
		cand := Candidate{ID: "cand_one", AmountMinor: 100000, Currency: "NGN", OccurredAt: at, RecipientBP: (tc.score - 5500) * 4}
		got, err := c.Rank([]Candidate{cand}, tgt, Context{})
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got.TopBP != tc.score || got.State != tc.wantState || fmt.Sprint(got.WhyNot) != fmt.Sprint(tc.wantWhy) {
			t.Errorf("%s: top %d state %s why %v, want %d %s %v", tc.name, got.TopBP, got.State, got.WhyNot, tc.score, tc.wantState, tc.wantWhy)
		}
	}
}

// REQ-AMB-003: more than the pack's four candidates above the floor is TOO_MANY_CANDIDATES (an escalation) even when one of them leads by a wide margin; four are not too many.
func TestMoreThanFourAboveTheFloorIsTooManyWhateverTheLead(t *testing.T) {
	c := goodConfig()
	at := time.Date(2026, 11, 20, 9, 30, 0, 0, time.UTC)
	tgt := Target{HasAmount: true, AmountMinor: 100000, Currency: "NGN", HasWindow: true, From: at.Add(-30 * time.Minute), To: at.Add(30 * time.Minute)}
	build := func(n int) []Candidate {
		out := []Candidate{{ID: "cand_lead", AmountMinor: 100000, Currency: "NGN", OccurredAt: at, RecipientBP: UnitBP, AliasMatch: true, Direction: "OUTGOING"}}
		for i := 1; i < n; i++ { // each follower is exactly at the floor (7000): 5500 plus a recipient feature of 6000
			out = append(out, Candidate{ID: fmt.Sprintf("cand_f%d", i), AmountMinor: 100000, Currency: "NGN", OccurredAt: at.Add(time.Duration(i) * time.Second), RecipientBP: 6000})
		}
		return out
	}
	four, err := c.Rank(build(4), tgt, Context{})
	if err != nil {
		t.Fatal(err)
	}
	five, err := c.Rank(build(5), tgt, Context{})
	if err != nil {
		t.Fatal(err)
	}
	if four.AboveFloor != 4 || four.State == TooManyCandidates {
		t.Errorf("four above the floor: %d above, state %s: not too many", four.AboveFloor, four.State)
	}
	if five.AboveFloor != 5 || five.State != TooManyCandidates || five.ResolvedID != "" {
		t.Errorf("five above the floor: %d above, state %s, resolved %q: want TOO_MANY_CANDIDATES and no resolved id", five.AboveFloor, five.State, five.ResolvedID)
	}
}

// INV-21, REQ-AMB-003: the same inputs give the same order 1,000 times. Each run gives the rows in another order (a stride permutation, no random source), with ties in score at the top and
// below it so that the tie-breaks (the newer time, then the ID) are what fix the order. Every run must give the same resolution, the same order and the same bytes of the audit payload.
func TestTheSameInputsGiveTheSameOrderAThousandTimes(t *testing.T) {
	c := goodConfig()
	at := time.Date(2026, 11, 20, 9, 30, 0, 0, time.UTC)
	tgt := Target{HasAmount: true, AmountMinor: 100000, Currency: "NGN", HasWindow: true, From: at.Add(-30 * time.Minute), To: at.Add(30 * time.Minute), ExpectedDirection: "OUTGOING"}
	base := []Candidate{
		{ID: "cand_a", AmountMinor: 100000, Currency: "NGN", OccurredAt: at, RecipientBP: 9000, Direction: "OUTGOING"},
		{ID: "cand_b", AmountMinor: 100000, Currency: "NGN", OccurredAt: at, RecipientBP: 9000, Direction: "OUTGOING"},                  // ties cand_a on score and time: the ID breaks it
		{ID: "cand_c", AmountMinor: 100000, Currency: "NGN", OccurredAt: at.Add(time.Minute), RecipientBP: 9000, Direction: "OUTGOING"}, // ties on score, newer: first
		{ID: "cand_d", AmountMinor: 101000, Currency: "NGN", OccurredAt: at.Add(-time.Hour), RecipientBP: 3000, AliasMatch: true},
		{ID: "cand_e", AmountMinor: 90000, Currency: "NGN", OccurredAt: at.Add(3 * time.Hour), RecipientBP: 100},
		{ID: "cand_f", AmountMinor: 100000, Currency: "USD", OccurredAt: at, RecipientBP: 5000, Direction: "OUTGOING"},
		{ID: "cand_g", AmountMinor: 100000, Currency: "NGN", OccurredAt: at.Add(-time.Minute), RecipientBP: 9000, Direction: "OUTGOING"},
	}
	n := len(base)
	strides := []int{1, 2, 3, 4, 5, 6} // coprime to 7: each is a permutation
	var first Resolution
	var firstJSON []byte
	for i := 0; i < 1000; i++ {
		stride := strides[i%len(strides)]
		in := make([]Candidate, n)
		for j := 0; j < n; j++ {
			in[j] = base[(i+j*stride)%n]
		}
		got, err := c.Rank(in, tgt, Context{})
		if err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
		raw, err := json.Marshal(got.AuditPayload())
		if err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
		if i == 0 {
			first, firstJSON = got, raw
			continue
		}
		if got.State != first.State || got.TopBP != first.TopBP || got.SecondBP != first.SecondBP || got.MarginBP != first.MarginBP || got.AboveFloor != first.AboveFloor || got.ResolvedID != first.ResolvedID || len(got.Ranked) != len(first.Ranked) {
			t.Fatalf("run %d: the resolution differs: %+v vs %+v", i, got, first)
		}
		for k := range got.Ranked {
			if got.Ranked[k] != first.Ranked[k] {
				t.Fatalf("run %d: rank %d is %+v, the first run had %+v", i, k, got.Ranked[k], first.Ranked[k])
			}
		}
		if string(raw) != string(firstJSON) {
			t.Fatalf("run %d: the audit payload differs:\n%s\n%s", i, raw, firstJSON)
		}
	}
	// The fixed order the ties give: score, then the newer time, then the ID (REQ-AMB-004).
	want := []string{"cand_c", "cand_a", "cand_b", "cand_g"}
	for k, id := range want {
		if first.Ranked[k].ID != id {
			t.Errorf("rank %d is %s, want %s (score, then the newer time, then the ID)", k, first.Ranked[k].ID, id)
		}
	}
}

// exactConfig has weights with no common factor, so that a score can sit one scaled unit (a ten-thousandth of a basis point) either side of a threshold. A566: the margin, the floor and
// the tie are decided on the exact sums, to the last unit.
func exactConfig() Config {
	return Config{
		Weights:  Weights{AmountBP: 3500, RecipientBP: 1, TemporalBP: 3499, RelationshipBP: 1500, TypeDirectionBP: 1500},
		FloorBP:  7000,
		MarginBP: 3500, MaxAboveFloor: 4, AmountToleranceBP: 100, TimeToleranceSeconds: 7200, MaxRaw: 20,
	}
}

// REQ-AMB-003, A566: the floor is exact to one scaled unit: 69,999,999 of 70,000,000 is below it and 70,000,000 reaches it (the reported score floors to 6999 and 7000).
func TestTheFloorIsExactToTheLastScaledUnit(t *testing.T) {
	c := exactConfig()
	at := time.Date(2026, 11, 20, 9, 30, 0, 0, time.UTC)
	tgt := Target{HasAmount: true, AmountMinor: 100000, Currency: "NGN", HasWindow: true, From: at.Add(-time.Minute), To: at.Add(time.Minute)}
	// amount full (35,000,000) and time full (34,990,000) are 69,990,000; the recipient weight is 1, so a feature of 9999 adds 9,999 and a feature of 10000 adds 10,000.
	for _, tc := range []struct {
		name      string
		recipient int64
		wantBP    int64
		wantState State
		wantWhy   []Why
	}{
		{"one scaled unit below the floor", 9999, 6999, Ambiguous, []Why{BelowFloor}},
		{"exactly the floor", 10000, 7000, AutoResolved, nil},
	} {
		got, err := c.Rank([]Candidate{{ID: "cand_one", AmountMinor: 100000, Currency: "NGN", OccurredAt: at, RecipientBP: tc.recipient}}, tgt, Context{})
		if err != nil {
			t.Fatal(err)
		}
		if got.TopBP != tc.wantBP || got.State != tc.wantState || fmt.Sprint(got.WhyNot) != fmt.Sprint(tc.wantWhy) {
			t.Errorf("%s: top %d state %s why %v, want %d %s %v", tc.name, got.TopBP, got.State, got.WhyNot, tc.wantBP, tc.wantState, tc.wantWhy)
		}
	}
}

// REQ-AMB-003, A566: the margin is exact to one scaled unit: a lead of 34,999,999 of 35,000,000 does not resolve and a lead of exactly 35,000,000 does. The floor is 0 here so that the margin
// alone decides.
func TestTheMarginIsExactToTheLastScaledUnit(t *testing.T) {
	c := exactConfig()
	c.FloorBP = 0
	at := time.Date(2026, 11, 20, 9, 30, 0, 0, time.UTC)
	tgt := Target{HasAmount: true, AmountMinor: 100000, Currency: "NGN"}
	top := Candidate{ID: "cand_top", AmountMinor: 100000, Currency: "NGN", OccurredAt: at}
	for _, tc := range []struct {
		name      string
		recipient int64 // the second's recipient feature: weight 1, so this many scaled units
		wantState State
	}{
		{"a lead one scaled unit short of the margin", 1, Ambiguous},
		{"a lead of exactly the margin", 0, AutoResolved},
	} {
		second := Candidate{ID: "cand_second", AmountMinor: 100000, Currency: "USD", OccurredAt: at, RecipientBP: tc.recipient}
		got, err := c.Rank([]Candidate{top, second}, tgt, Context{})
		if err != nil {
			t.Fatal(err)
		}
		if got.State != tc.wantState {
			t.Errorf("%s: state %s why %v, want %s", tc.name, got.State, got.WhyNot, tc.wantState)
		}
	}
}

// REQ-AMB-003, A583: a tie needs two candidates. One candidate that scores 0 is not tied with the zero the rule gives its missing second: it is short of the margin and the floor, nothing more.
func TestOneCandidateThatScoresZeroIsNotATie(t *testing.T) {
	c := goodConfig()
	at := time.Date(2026, 11, 20, 9, 30, 0, 0, time.UTC)
	got, err := c.Rank([]Candidate{{ID: "cand_zero", OccurredAt: at}}, Target{}, Context{})
	if err != nil {
		t.Fatal(err)
	}
	if got.TopBP != 0 || got.State != Ambiguous || fmt.Sprint(got.WhyNot) != "[MARGIN_NOT_MET BELOW_FLOOR]" {
		t.Errorf("top %d state %s why %v, want 0, AMBIGUOUS and [MARGIN_NOT_MET BELOW_FLOOR]", got.TopBP, got.State, got.WhyNot)
	}
}
