package decide_test

import (
	"errors"
	"math/rand"
	"reflect"
	"strconv"
	"testing"

	"github.com/Noemai-org/noema/services/core/trust/decide"
)

func openClarification() decide.Open {
	return decide.Open{ID: "clr_1", CandidateSetVersion: 2, ExpiresAt: 1000, Round: 1,
		Options:    []decide.Option{{ID: "opt_1", CandidateID: "t1"}, {ID: "opt_2", CandidateID: "t2"}, {ID: "opt_3", CandidateID: "t3"}},
		Candidates: []string{"t1", "t2", "t3"}}
}

func TestBindRejectsStaleOrForeignAnswers(t *testing.T) {
	open := openClarification()
	good := decide.Answer{ClarificationID: "clr_1", CandidateSetVersion: 2, Selected: "opt_1"}
	stale := good
	stale.CandidateSetVersion = 1
	if _, err := decide.Bind(open, stale, 10, 2); !errors.Is(err, decide.ErrStaleCandidateSet) {
		t.Fatalf("old version: %v", err)
	}
	if _, err := decide.Bind(open, good, 1001, 2); !errors.Is(err, decide.ErrStaleCandidateSet) {
		t.Fatalf("expired: %v", err)
	}
	if _, err := decide.Bind(open, good, 1000, 2); err != nil {
		t.Fatalf("an answer at the expiry instant: %v", err)
	}
	foreign := good
	foreign.ClarificationID = "clr_9"
	if _, err := decide.Bind(open, foreign, 10, 2); !errors.Is(err, decide.ErrWrongClarification) {
		t.Fatalf("%v", err)
	}
	for name, a := range map[string]decide.Answer{
		"empty":         {ClarificationID: "clr_1", CandidateSetVersion: 2},
		"unknown":       {ClarificationID: "clr_1", CandidateSetVersion: 2, Selected: "opt_9"},
		"unknown no":    {ClarificationID: "clr_1", CandidateSetVersion: 2, Rejected: []string{"opt_9"}},
		"contradictory": {ClarificationID: "clr_1", CandidateSetVersion: 2, Selected: "opt_1", Rejected: []string{"opt_1"}},
	} {
		if _, err := decide.Bind(open, a, 10, 2); err == nil {
			t.Fatalf("%s answer accepted", name)
		}
	}
}

func TestSelectionResolvesOnlyByExplicitChoice(t *testing.T) {
	open := openClarification()
	b, err := decide.Bind(open, decide.Answer{ClarificationID: "clr_1", CandidateSetVersion: 2, Selected: "opt_2"}, 10, 2)
	if err != nil || b.Kind != decide.BoundSelected || b.CandidateID != "t2" {
		t.Fatalf("%+v %v", b, err)
	}
	// a negation that leaves exactly one candidate is still a question, never a resolution
	b, err = decide.Bind(open, decide.Answer{ClarificationID: "clr_1", CandidateSetVersion: 2, Rejected: []string{"opt_1", "opt_3"}}, 10, 2)
	if err != nil || b.Kind != decide.Narrowed || !reflect.DeepEqual(b.Remaining, []string{"t2"}) || b.CandidateID != "" {
		t.Fatalf("%+v %v", b, err)
	}
	// a negation of everything, and "none of these", go back to the full set
	for _, a := range []decide.Answer{
		{ClarificationID: "clr_1", CandidateSetVersion: 2, Rejected: []string{"opt_1", "opt_2", "opt_3"}},
		{ClarificationID: "clr_1", CandidateSetVersion: 2, None: true},
	} {
		b, err = decide.Bind(open, a, 10, 2)
		if err != nil || b.Kind != decide.NoneOfThese || len(b.Remaining) != 3 {
			t.Fatalf("%+v %v", b, err)
		}
	}
}

func TestRoundLimitEscalates(t *testing.T) {
	open := openClarification()
	open.Round = 2
	b, err := decide.Bind(open, decide.Answer{ClarificationID: "clr_1", CandidateSetVersion: 2, Rejected: []string{"opt_1"}}, 10, 2)
	if err != nil || b.Kind != decide.EscalateRound {
		t.Fatalf("%+v %v", b, err)
	}
	b, err = decide.Bind(open, decide.Answer{ClarificationID: "clr_1", CandidateSetVersion: 2, Selected: "opt_3"}, 10, 2)
	if err != nil || b.Kind != decide.BoundSelected {
		t.Fatalf("a selection in the last round must still resolve: %+v %v", b, err)
	}
}

// Negation narrows monotonically: more constraints give a subset, applying twice changes nothing, and order does not matter.
func TestConstraintsNarrowMonotonically(t *testing.T) {
	r := rand.New(rand.NewSource(3))
	subset := func(a, b []string) bool {
		in := map[string]bool{}
		for _, x := range b {
			in[x] = true
		}
		for _, x := range a {
			if !in[x] {
				return false
			}
		}
		return true
	}
	for i := 0; i < 3000; i++ {
		var ids []string
		for k := 0; k < 1+r.Intn(8); k++ {
			ids = append(ids, "t"+strconv.Itoa(k))
		}
		var cs []decide.Constraint
		prev := ids
		for k := 0; k < r.Intn(6); k++ {
			cs = append(cs, decide.Constraint{ExcludeCandidate: "t" + strconv.Itoa(r.Intn(10))})
			cur := decide.ApplyConstraints(ids, cs)
			if !subset(cur, prev) || len(cur) > len(prev) {
				t.Fatalf("a constraint grew the set: %v -> %v", prev, cur)
			}
			prev = cur
		}
		once := decide.ApplyConstraints(ids, cs)
		if twice := decide.ApplyConstraints(once, cs); !reflect.DeepEqual(once, twice) {
			t.Fatal("not idempotent")
		}
		shuffled := append([]decide.Constraint(nil), cs...)
		r.Shuffle(len(shuffled), func(a, b int) { shuffled[a], shuffled[b] = shuffled[b], shuffled[a] })
		if got := decide.ApplyConstraints(ids, shuffled); !reflect.DeepEqual(once, got) {
			t.Fatal("constraint order mattered")
		}
	}
}
