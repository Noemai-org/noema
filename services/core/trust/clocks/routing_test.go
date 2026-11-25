package clocks_test

// REQ-TRU-006 (PERD 23.5; A632, A640, A644, interim, relayed, not yet committed): the routing priority derives from the phase through the pack's priority routing
// member; priority may be raised, never lowered; a phase with no entry is NORMAL with no queue, and an empty queue means no re-route. The lookup is the consumer-side view of
// the member (EL's PCK-09 table implements it at composition). Deterministic and replayable (INV-21): no clock, no I/O. Every value here is SYNTHETIC.

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/trust/clocks"
)

type table map[string]clocks.Entry

func (t table) Lookup(phase string) (clocks.Entry, bool) { e, ok := t[phase]; return e, ok }

var (
	allPhases     = []clocks.Phase{clocks.NotStarted, clocks.Running, clocks.AtRisk, clocks.Breached}
	allPriorities = []clocks.Priority{"LOW", "NORMAL", "HIGH"}
	syntheticQ    = "SYNTHETIC_QUEUE"
)

func rank(p clocks.Priority) int {
	for i, q := range allPriorities {
		if p == q {
			return i
		}
	}
	return -1
}

func status(p clocks.Phase) clocks.Status {
	return clocks.Status{ClockID: "syn.clock.test_72h", Label: contracts.ClockV1LabelSYNTHETIC, Phase: p}
}

func TestAPhaseWithNoEntryIsNormalWithNoQueueAndNoReroute(t *testing.T) { // REQ-TRU-006, A644
	ev, err := clocks.Prioritise(status(clocks.AtRisk), "", table{})
	if err != nil {
		t.Fatal(err)
	}
	if ev.Priority != "NORMAL" || ev.OwnerQueue != "" || ev.Reroute() || ev.Raised {
		t.Fatalf("%+v reroute=%v", ev, ev.Reroute())
	}
}

func TestAListedPhaseGivesItsPriorityAndQueue(t *testing.T) { // REQ-TRU-006, A644
	tb := table{"AT_RISK": {Priority: "HIGH", OwnerQueue: syntheticQ}}
	ev, err := clocks.Prioritise(status(clocks.AtRisk), "", tb)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Priority != "HIGH" || ev.OwnerQueue != syntheticQ || !ev.Reroute() {
		t.Fatalf("%+v", ev)
	}
	if ev.Status.Phase != clocks.AtRisk {
		t.Fatalf("the status is carried unchanged: %+v", ev.Status)
	}
}

func TestAnEmptyQueueMeansNoReroute(t *testing.T) { // A644: "no re-route", never "route to the empty queue"
	ev, err := clocks.Prioritise(status(clocks.Breached), "", table{"BREACHED": {Priority: "HIGH"}})
	if err != nil {
		t.Fatal(err)
	}
	if ev.Priority != "HIGH" || ev.Reroute() {
		t.Fatalf("%+v", ev)
	}
}

func TestPriorityIsRaisedNeverLowered(t *testing.T) { // PERD 23.5; DOWNGRADE_CASE_PRIORITY is prohibited
	for _, cur := range allPriorities {
		for _, tbl := range allPriorities {
			ev, err := clocks.Prioritise(status(clocks.Running), cur, table{"RUNNING": {Priority: tbl, OwnerQueue: syntheticQ}})
			if err != nil {
				t.Fatal(err)
			}
			want := cur
			if rank(tbl) > rank(cur) {
				want = tbl
			}
			if ev.Priority != want {
				t.Errorf("current %s, table %s: got %s, want %s", cur, tbl, ev.Priority, want)
			}
			if ev.Raised != (rank(tbl) > rank(cur)) {
				t.Errorf("current %s, table %s: raised=%v", cur, tbl, ev.Raised)
			}
		}
	}
}

func TestAnUnknownPriorityOrQueueFromTheTableOrTheCaseIsRefused(t *testing.T) { // INV-16: nothing is guessed
	bad := []struct {
		name string
		cur  clocks.Priority
		e    clocks.Entry
	}{
		{"table priority", "", clocks.Entry{Priority: "URGENT", OwnerQueue: syntheticQ}},
		{"empty table priority", "", clocks.Entry{Priority: "", OwnerQueue: syntheticQ}},
		{"lower-case table priority", "", clocks.Entry{Priority: "high", OwnerQueue: syntheticQ}},
		{"queue shape", "", clocks.Entry{Priority: "HIGH", OwnerQueue: "bad queue\n"}},
		{"case priority", "URGENT", clocks.Entry{Priority: "HIGH", OwnerQueue: syntheticQ}},
	}
	for _, c := range bad {
		ev, err := clocks.Prioritise(status(clocks.Running), c.cur, table{"RUNNING": c.e})
		if !errors.Is(err, clocks.ErrInvalid) || !reflect.DeepEqual(ev, clocks.Evaluation{}) {
			t.Errorf("%s: got %+v, %v; want ErrInvalid and the zero value", c.name, ev, err)
		}
	}
}

func TestANilTableAndAPhaseOutsideTheFourAreRefused(t *testing.T) {
	if _, err := clocks.Prioritise(status(clocks.Running), "", nil); !errors.Is(err, clocks.ErrInvalid) {
		t.Errorf("nil table: %v", err)
	}
	for _, p := range []clocks.Phase{"", "PAUSED", "running"} {
		if _, err := clocks.Prioritise(status(p), "", table{}); !errors.Is(err, clocks.ErrInvalid) {
			t.Errorf("phase %q: %v", p, err)
		}
	}
}

func TestRoutingIsDeterministicAndNeverLowersForAnyInputs(t *testing.T) { // INV-21
	rapid.Check(t, func(t *rapid.T) {
		phase := rapid.SampledFrom(allPhases).Draw(t, "phase")
		cur := rapid.SampledFrom(append([]clocks.Priority{""}, allPriorities...)).Draw(t, "current")
		listed := rapid.Bool().Draw(t, "listed")
		e := clocks.Entry{Priority: rapid.SampledFrom(allPriorities).Draw(t, "priority"), OwnerQueue: rapid.SampledFrom([]string{"", syntheticQ}).Draw(t, "queue")}
		tb := table{}
		if listed {
			tb[string(phase)] = e
		}
		a, err := clocks.Prioritise(status(phase), cur, tb)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := clocks.Prioritise(status(phase), cur, tb)
		if !reflect.DeepEqual(a, b) {
			t.Fatalf("not deterministic: %+v vs %+v", a, b)
		}
		if rank(a.Priority) < rank(cur) || (cur == "" && rank(a.Priority) < 0) {
			t.Fatalf("lowered or unset: current %q, got %q", cur, a.Priority)
		}
		if !listed && (a.OwnerQueue != "" || a.Reroute()) {
			t.Fatalf("an unlisted phase re-routes: %+v", a)
		}
		again, _ := clocks.Prioritise(status(phase), a.Priority, tb) // idempotent: the result is its own floor
		if again.Priority != a.Priority || again.Raised {
			t.Fatalf("not idempotent: %+v then %+v", a, again)
		}
	})
}

func TestEventPayloadCarriesEveryFieldAsIntegersAndStrings(t *testing.T) { // REQ-TRU-006 "SLA_EVALUATED (all fields)"; the Record payload is integers only (PERD 32.1)
	started, deadline, remaining := int64(1_000), int64(1_000)+duration, int64(100)
	ev := clocks.Evaluation{
		Status:     clocks.Status{ClockID: "syn.clock.test_72h", Label: contracts.ClockV1LabelSYNTHETIC, StartedAt: &started, DeadlineAt: &deadline, Elapsed: duration - 100, Remaining: &remaining, Phase: clocks.AtRisk},
		Priority:   "HIGH",
		OwnerQueue: syntheticQ,
		Raised:     true,
	}
	want := map[string]any{
		"clock_id": "syn.clock.test_72h", "label": "SYNTHETIC", "phase": "AT_RISK", "started_at": started, "deadline_at": deadline,
		"elapsed": duration - 100, "remaining": remaining, "priority": "HIGH", "owner_queue": syntheticQ, "priority_raised": true, "reroute": true,
	}
	if got := ev.EventPayload(); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v\nwant %v", got, want)
	}
}

func TestEventPayloadOfAClockNotStartedAssumesNothing(t *testing.T) { // PERD 23.5: a missing anchor is never assumed
	ev, err := clocks.Prioritise(status(clocks.NotStarted), "", table{})
	if err != nil {
		t.Fatal(err)
	}
	got := ev.EventPayload()
	for _, k := range []string{"started_at", "deadline_at", "remaining", "owner_queue"} {
		if _, there := got[k]; there {
			t.Errorf("payload names %s for a clock with no anchor: %v", k, got)
		}
	}
	if got["phase"] != "NOT_STARTED" || got["elapsed"] != int64(0) || got["reroute"] != false || got["priority"] != "NORMAL" {
		t.Errorf("%v", got)
	}
}

func TestTheRoutingCodeReadsNoClockAndDoesNoIO(t *testing.T) { // INV-21, INV-14, AGENT-16
	src, err := os.ReadFile("routing.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	for _, banned := range []string{"time.Now", "\"time\"", "math/rand", "net/http", "database/sql", "pgx"} {
		if strings.Contains(s, banned) {
			t.Errorf("routing.go contains %q", banned)
		}
	}
}
