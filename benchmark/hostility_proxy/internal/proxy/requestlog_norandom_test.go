package proxy

import (
	"errors"
	"testing"

	"github.com/Noemai-org/noema/benchmark/hostility_proxy/internal/plan"
)

// A480 (noema#786, relayed): the request log's generation came from crypto/rand and a failure of it was a panic, which the PLT-09 lint baseline (forbidigo ^panic$) finds. A process that cannot get
// randomness still cannot make a safe claim about the log, so it must REFUSE: New returns an error, and a reset (a plan load or a delete) is refused with nothing changed. SYNTHETIC (AGENT-25):
// the failing source is a test double of crypto/rand.Read, which is the only place the generation could fail.

func noEntropy(t *testing.T) {
	t.Helper()
	old := randRead
	randRead = func([]byte) (int, error) { return 0, errors.New("no entropy") }
	t.Cleanup(func() { randRead = old })
}

func TestNoRandomnessIsAnErrorForTheGenerationAndForANewLog(t *testing.T) {
	noEntropy(t)
	if _, err := newGeneration(); !errors.Is(err, errNoRandomness) {
		t.Fatalf("newGeneration: %v, want errNoRandomness", err)
	}
	if l, err := newRequestLog(0); !errors.Is(err, errNoRandomness) || l != nil {
		t.Fatalf("newRequestLog: %v, %v; want nil and errNoRandomness", l, err)
	}
}

func TestTheErrorTextIsFixedAndHoldsNothingOfTheSource(t *testing.T) {
	noEntropy(t)
	_, err := newGeneration()
	if err == nil || err.Error() != errNoRandomness.Error() {
		t.Fatalf("the error is %v; want the fixed text only", err)
	}
}

func TestAResetThatCannotGetAGenerationChangesNothing(t *testing.T) {
	l, err := newRequestLog(0)
	if err != nil {
		t.Fatal(err)
	}
	l.add(RequestEntry{Target: "t", Operation: "o", Method: "GET", Path: "/p"})
	before, entries, _, _ := l.snapshot("", "")
	noEntropy(t)
	s := &Server{engine: plan.NewEngine(), clock: newClockHolder(), trace: &trace{}, reqlog: l, stale: newStale(), held: newHeld()}
	if err := s.clearRun(); !errors.Is(err, errNoRandomness) {
		t.Fatalf("clearRun: %v, want errNoRandomness", err)
	}
	after, kept, _, _ := l.snapshot("", "")
	if after != before || len(kept) != len(entries) {
		t.Fatalf("a refused reset changed the log: generation %q to %q, %d entries to %d", before, after, len(entries), len(kept))
	}
}

func TestAResetWithRandomnessGivesANewGeneration(t *testing.T) {
	l, err := newRequestLog(0)
	if err != nil {
		t.Fatal(err)
	}
	before, _, _, _ := l.snapshot("", "")
	s := &Server{engine: plan.NewEngine(), clock: newClockHolder(), trace: &trace{}, reqlog: l, stale: newStale(), held: newHeld()}
	if err := s.clearRun(); err != nil {
		t.Fatal(err)
	}
	if after, _, _, _ := l.snapshot("", ""); after == before || len(after) != 32 {
		t.Fatalf("generation %q after a reset, was %q; want a new 32-character one", after, before)
	}
}
