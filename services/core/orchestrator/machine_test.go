package orchestrator_test

// STM-01 / REQ-STM-001: the investigation state machine is the PERD 28.2 table as data; any transition not listed is illegal; every applied command moves the
// version by one and is refused when it was made against another version (optimistic locking); a guard can refuse a listed transition but never allow an
// unlisted one. SoT 45.1 ("every state transition is explicit"). Pure functions: persistence and the Record event are the next slice. Every identifier is SYNTHETIC.

import (
	"errors"
	"os"
	"regexp"
	"sort"
	"testing"

	"pgregory.net/rapid"

	"github.com/Noemai-org/noema/services/core/orchestrator"
)

// The reference reader reads the same file with its own, separate pattern, so a bug in the production reader cannot hide itself.
var (
	rowRe    = regexp.MustCompile(`(?m)^\s*- \{from: ([A-Z_]+), to: ([A-Z_]+),`)
	statesRe = regexp.MustCompile(`(?m)^states: \[([A-Z_, ]+)\]$`)
)

func reference(t testing.TB) (states []orchestrator.State, arrows map[orchestrator.Transition]bool) {
	t.Helper()
	raw, err := os.ReadFile("transitions.yaml")
	if err != nil {
		t.Fatal(err)
	}
	m := statesRe.FindSubmatch(raw)
	if m == nil {
		t.Fatal("no states line")
	}
	for _, s := range regexp.MustCompile(`[A-Z_]+`).FindAllString(string(m[1]), -1) {
		states = append(states, orchestrator.State(s))
	}
	arrows = map[orchestrator.Transition]bool{}
	for _, r := range rowRe.FindAllSubmatch(raw, -1) {
		arrows[orchestrator.Transition{From: orchestrator.State(r[1]), To: orchestrator.State(r[2])}] = true
	}
	if len(states) == 0 || len(arrows) == 0 {
		t.Fatal("the reference reader read nothing")
	}
	return states, arrows
}

func machine(t testing.TB) *orchestrator.Machine {
	t.Helper()
	tab, err := orchestrator.LoadTable()
	if err != nil {
		t.Fatalf("LoadTable: %v", err)
	}
	return &orchestrator.Machine{Table: tab}
}

// REQ-STM-001: the table in Go is the table in the file: the same states in the same order and exactly the same arrows.
func TestTheTableIsTheFile(t *testing.T) {
	states, arrows := reference(t)
	m := machine(t)
	got := m.Table.States()
	if len(got) != len(states) {
		t.Fatalf("states = %v, want %v", got, states)
	}
	for i := range states {
		if got[i] != states[i] {
			t.Fatalf("state %d = %s, want %s", i, got[i], states[i])
		}
	}
	n := 0
	for _, from := range states {
		for _, to := range states {
			if m.Table.Allowed(from, to) != arrows[orchestrator.Transition{From: from, To: to}] {
				t.Errorf("Allowed(%s, %s) = %v, want %v", from, to, m.Table.Allowed(from, to), arrows[orchestrator.Transition{From: from, To: to}])
			}
			if m.Table.Allowed(from, to) {
				n++
			}
		}
	}
	if n != len(arrows) {
		t.Errorf("%d arrows allowed, the file lists %d", n, len(arrows))
	}
	exits := m.Table.Exits("EVALUATING")
	sort.Slice(exits, func(i, j int) bool { return exits[i] < exits[j] })
	if len(exits) != 8 {
		t.Errorf("EVALUATING has %d exits %v, want the 8 of PERD 28.2", len(exits), exits)
	}
}

// REQ-STM-001: every listed transition is applied, and moves the version by exactly one.
func TestEveryListedTransitionIsApplied(t *testing.T) {
	_, arrows := reference(t)
	m := machine(t)
	for a := range arrows {
		inv := orchestrator.Investigation{ID: "inv_01", State: a.From, Version: 7}
		got, ch, err := m.Apply(inv, orchestrator.Command{To: a.To, ExpectedVersion: 7})
		if err != nil {
			t.Fatalf("%s -> %s: %v", a.From, a.To, err)
		}
		if got.State != a.To || got.Version != 8 || got.ID != "inv_01" {
			t.Errorf("%s -> %s: got %+v", a.From, a.To, got)
		}
		if ch != (orchestrator.Change{ID: "inv_01", From: a.From, To: a.To, FromVersion: 7, ToVersion: 8}) {
			t.Errorf("%s -> %s: change %+v", a.From, a.To, ch)
		}
	}
}

// REQ-STM-001, PERD 28.2 "Any transition not listed is illegal": every pair of states that is not an arrow (a negative test for each prohibited transition,
// including a state to itself and every exit of a terminal state) is refused with the typed error, changes nothing, and reports no change.
func TestEveryProhibitedTransitionIsRefused(t *testing.T) {
	states, arrows := reference(t)
	m := machine(t)
	refused := 0
	for _, from := range states {
		for _, to := range states {
			if arrows[orchestrator.Transition{From: from, To: to}] {
				continue
			}
			inv := orchestrator.Investigation{ID: "inv_01", State: from, Version: 3}
			got, ch, err := m.Apply(inv, orchestrator.Command{To: to, ExpectedVersion: 3})
			var ill *orchestrator.IllegalTransitionError
			if !errors.As(err, &ill) || ill.From != from || ill.To != to {
				t.Fatalf("%s -> %s: err = %v, want an IllegalTransitionError naming both states", from, to, err)
			}
			if got != inv || ch != (orchestrator.Change{}) {
				t.Fatalf("%s -> %s: the refused command changed something: %+v %+v", from, to, got, ch)
			}
			refused++
		}
	}
	if want := len(states)*len(states) - len(arrows); refused != want {
		t.Fatalf("refused %d pairs, want %d", refused, want)
	}
}

// A state the table does not know is neither a source nor a target.
func TestUnknownStatesAreRefused(t *testing.T) {
	m := machine(t)
	for _, c := range []struct{ from, to orchestrator.State }{{"RECEIVED", "NO_SUCH_STATE"}, {"NO_SUCH_STATE", "RECEIVED"}, {"", "RECEIVED"}, {"RECEIVED", ""}} {
		inv := orchestrator.Investigation{ID: "inv_01", State: c.from, Version: 1}
		got, _, err := m.Apply(inv, orchestrator.Command{To: c.to, ExpectedVersion: 1})
		var ill *orchestrator.IllegalTransitionError
		if !errors.As(err, &ill) || got != inv {
			t.Errorf("%q -> %q: err = %v, got %+v", c.from, c.to, err, got)
		}
	}
}

// REQ-STM-001: optimistic locking on (investigation_id, state_version). A command made against another version is refused and changes nothing, even when
// the transition is listed; the caller reads again.
func TestACommandAgainstAnotherVersionIsRefused(t *testing.T) {
	m := machine(t)
	inv := orchestrator.Investigation{ID: "inv_01", State: "RECEIVED", Version: 5}
	for _, v := range []int64{4, 6, 0, -1} {
		got, ch, err := m.Apply(inv, orchestrator.Command{To: "INTERPRETING", ExpectedVersion: v})
		if !errors.Is(err, orchestrator.ErrStaleVersion) || got != inv || ch != (orchestrator.Change{}) {
			t.Errorf("expected version %d: err = %v, got %+v", v, err, got)
		}
	}
	// A listed command at the current version is applied once; replaying it at the old version is then stale.
	got, _, err := m.Apply(inv, orchestrator.Command{To: "INTERPRETING", ExpectedVersion: 5})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.Apply(inv, orchestrator.Command{To: "INTERPRETING", ExpectedVersion: 5}); err != nil {
		t.Fatalf("Apply is pure: the same inputs give the same result, got %v", err)
	}
	if _, _, err := m.Apply(got, orchestrator.Command{To: "EVALUATING", ExpectedVersion: 5}); !errors.Is(err, orchestrator.ErrStaleVersion) {
		t.Fatalf("a command made before the first one landed = %v, want ErrStaleVersion", err)
	}
}

// REQ-STM-001, "the guards are checked in code": a guard can refuse a listed transition, with its reason wrapped; it runs only for a listed transition, so a
// guard can never allow an unlisted one; and a guard sees the investigation and the command as they were.
func TestAGuardRefusesButNeverAllows(t *testing.T) {
	m := machine(t)
	reason := errors.New("no grant")
	calls := 0
	m.Guards = map[orchestrator.Transition]orchestrator.Guard{
		{From: "EVALUATING", To: "PLANNED"}: func(inv orchestrator.Investigation, cmd orchestrator.Command) error {
			calls++
			if inv.State != "EVALUATING" || cmd.To != "PLANNED" {
				t.Errorf("guard saw %+v and %+v", inv, cmd)
			}
			return reason
		},
		{From: "RECEIVED", To: "COMPLETED"}: func(orchestrator.Investigation, orchestrator.Command) error { calls += 100; return nil },
	}
	inv := orchestrator.Investigation{ID: "inv_01", State: "EVALUATING", Version: 2}
	got, ch, err := m.Apply(inv, orchestrator.Command{To: "PLANNED", ExpectedVersion: 2})
	if !errors.Is(err, orchestrator.ErrGuardRefused) || !errors.Is(err, reason) || got != inv || ch != (orchestrator.Change{}) || calls != 1 {
		t.Fatalf("err = %v, got %+v, calls = %d; want the guard's refusal, nothing changed, one call", err, got, calls)
	}
	// RECEIVED -> COMPLETED is not listed: the guard that would say yes is never consulted.
	var ill *orchestrator.IllegalTransitionError
	if _, _, err := m.Apply(orchestrator.Investigation{ID: "inv_01", State: "RECEIVED", Version: 1}, orchestrator.Command{To: "COMPLETED", ExpectedVersion: 1}); !errors.As(err, &ill) || calls != 1 {
		t.Fatalf("err = %v, calls = %d; a guard must not run for an unlisted transition", err, calls)
	}
	// A guard on one arrow does not touch another arrow out of the same state.
	if _, _, err := m.Apply(inv, orchestrator.Command{To: "REJECTED", ExpectedVersion: 2}); err != nil {
		t.Fatalf("EVALUATING -> REJECTED has no guard: %v", err)
	}
}

// REQ-STM-001 acceptance: "a property test (a random command sequence) SHALL find no illegal state reached". Random commands, right and wrong versions, known and
// junk states: every applied step is an arrow of the file and moves the version by one; every refused step changes nothing; the terminal states of the file (no exit)
// are never left.
func TestNoRandomCommandSequenceReachesAnIllegalState(t *testing.T) {
	states, arrows := reference(t)
	m := machine(t)
	hasExit := map[orchestrator.State]bool{}
	for a := range arrows {
		hasExit[a.From] = true
	}
	pick := rapid.Custom(func(t *rapid.T) orchestrator.State {
		if rapid.IntRange(0, 9).Draw(t, "junk") == 0 {
			return orchestrator.State(rapid.StringMatching(`[A-Z_]{0,12}`).Draw(t, "junkState"))
		}
		return rapid.SampledFrom(states).Draw(t, "state")
	})
	rapid.Check(t, func(t *rapid.T) {
		inv := orchestrator.Investigation{ID: "inv_prop", State: states[0], Version: 1}
		steps := rapid.IntRange(1, 60).Draw(t, "steps")
		for i := 0; i < steps; i++ {
			to := pick.Draw(t, "to")
			ev := inv.Version
			if rapid.IntRange(0, 5).Draw(t, "stale") == 0 {
				ev += rapid.SampledFrom([]int64{-1, 1, 2}).Draw(t, "skew")
			}
			before := inv
			got, ch, err := m.Apply(inv, orchestrator.Command{To: to, ExpectedVersion: ev})
			if err != nil {
				if got != before || ch != (orchestrator.Change{}) {
					t.Fatalf("refused %s -> %s changed something", before.State, to)
				}
				continue
			}
			if !arrows[orchestrator.Transition{From: before.State, To: to}] || ev != before.Version {
				t.Fatalf("applied %s -> %s at expected %d (version %d): not a listed arrow or a stale command", before.State, to, ev, before.Version)
			}
			if got.State != to || got.Version != before.Version+1 || ch.FromVersion != before.Version || ch.ToVersion != got.Version {
				t.Fatalf("applied %s -> %s: got %+v, change %+v", before.State, to, got, ch)
			}
			if !hasExit[before.State] {
				t.Fatalf("left %s, which has no exit in the table", before.State)
			}
			inv = got
		}
	})
}
