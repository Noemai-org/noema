package adapters_test

// ENV-05 (REQ-ENV-001, REQ-ENV-006; INV-21): the current state of each lifecycle of a payout, folded from its transition log. Delta v1.3 section 3 says the log is read oldest first and a
// state at a point is "fold the transitions and take toState"; Tushe's response of 7 Oct 2026 section 4.1 says a payout carries three lifecycles that are written independently and "does not
// synchronise" them. So each lifecycle is folded on its own and no state of one is read from another. State strings stay Tushe's own, raw (A310). SYNTHETIC (AGENT-25).

import (
	"errors"
	"testing"

	"pgregory.net/rapid"

	"github.com/Noemai-org/noema/services/core/adapters"
)

func TestEachLifecycleTakesTheToStateOfItsLastTransitionAndNeverAnothersState(t *testing.T) {
	lifecycles := []string{"status", "settlementStatus", "arrivalStatus"}
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(1, 25).Draw(t, "rows")
		var items []adapters.PayoutTransition
		want := map[string]string{}
		for i := 0; i < n; i++ {
			lc := rapid.SampledFrom(lifecycles).Draw(t, "lifecycle")
			to := rapid.StringMatching(`[a-z_]{1,12}`).Draw(t, "to")
			items = append(items, adapters.PayoutTransition{Seq: int64(i + 1), Lifecycle: lc, ToState: to})
			want[lc] = to
		}
		got, err := adapters.LifecycleStates(adapters.TransitionPage{Items: items})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != len(want) {
			t.Fatalf("got %v, want %v: a lifecycle with no row must have no state, never a state read from another", got, want)
		}
		for lc, to := range want {
			if got[lc] != to {
				t.Fatalf("lifecycle %s: got %q, want the last toState %q", lc, got[lc], to)
			}
		}
	})
}

func TestACompletedAndNotArrivedPayoutKeepsBothStates(t *testing.T) {
	// the 7 Oct response: "completed + not_arrived is a legal, reachable, queryable state": debited but not credited. Neither lifecycle overwrites or implies the other, in either order.
	for name, items := range map[string][]adapters.PayoutTransition{
		"arrival first": {
			{Seq: 1, Lifecycle: "arrivalStatus", ToState: "not_arrived"},
			{Seq: 2, Lifecycle: "status", FromState: "processing", ToState: "completed"},
		},
		"status first": {
			{Seq: 1, Lifecycle: "status", ToState: "completed"},
			{Seq: 2, Lifecycle: "arrivalStatus", ToState: "not_arrived"},
		},
	} {
		got, err := adapters.LifecycleStates(adapters.TransitionPage{Items: items})
		if err != nil || got["status"] != "completed" || got["arrivalStatus"] != "not_arrived" || len(got) != 2 {
			t.Errorf("%s: got %v, %v; want status completed and arrivalStatus not_arrived", name, got, err)
		}
	}
	// a completed payout whose log has no arrival row has no arrival state: it is not read as arrived, and not as anything else
	got, err := adapters.LifecycleStates(adapters.TransitionPage{Items: []adapters.PayoutTransition{{Seq: 1, Lifecycle: "status", ToState: "completed"}}})
	if _, has := got["arrivalStatus"]; err != nil || has || got["status"] != "completed" {
		t.Errorf("got %v, %v; want only status completed", got, err)
	}
}

func TestALogThatIsNotOldestFirstOrNamesNoLifecycleOrStateIsRefusedNotFolded(t *testing.T) {
	bad := map[string][]adapters.PayoutTransition{
		"repeated seq": {{Seq: 1, Lifecycle: "status", ToState: "a"}, {Seq: 1, Lifecycle: "status", ToState: "b"}},
		"lower seq":    {{Seq: 2, Lifecycle: "status", ToState: "a"}, {Seq: 1, Lifecycle: "status", ToState: "b"}},
		"no lifecycle": {{Seq: 1, Lifecycle: "", ToState: "a"}},
		"no to-state":  {{Seq: 1, Lifecycle: "status", ToState: ""}},
		"zero seq":     {{Seq: 0, Lifecycle: "status", ToState: "a"}},
		"negative seq": {{Seq: -3, Lifecycle: "status", ToState: "a"}},
	}
	for name, items := range bad {
		got, err := adapters.LifecycleStates(adapters.TransitionPage{Items: items})
		var re *adapters.ReadError
		if err == nil || !errors.As(err, &re) || re.Failure != adapters.ReadMalformed || got != nil {
			t.Errorf("%s: got %v, %v; want a MALFORMED read error and no states", name, got, err)
		}
	}
	if got, err := adapters.LifecycleStates(adapters.TransitionPage{}); err != nil || len(got) != 0 {
		t.Errorf("an empty log: got %v, %v; want no states and no error (the caller decides what no log means)", got, err)
	}
}
