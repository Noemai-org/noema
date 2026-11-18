package proxy

import (
	"errors"
	"io"
	"testing"

	"github.com/Noemai-org/noema/benchmark/hostility_proxy/internal/plan"
	"github.com/Noemai-org/noema/benchmark/hostility_proxy/internal/routes"
)

// A480 (noema#786) and A492 (noema#855 follow-up): the request log's generation comes from an entropy source that the SERVER owns (Options.Entropy, crypto/rand when nil), so a failing source is passed
// in by the test and no package-level variable is ever replaced: nothing a test does here is visible to another server or another test. A process that cannot get randomness still cannot make a
// safe claim about the log, so it REFUSES: New returns an error, and a reset (a plan load or a delete) is refused with nothing changed. SYNTHETIC (AGENT-25).

// failing is an entropy source that always fails; ones is a working one with a known output.
type failing struct{}

func (failing) Read([]byte) (int, error) { return 0, errors.New("no entropy") }

type ones struct{}

func (ones) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0xff
	}
	return len(p), nil
}

var _ io.Reader = failing{}

func TestNoRandomnessIsAnErrorForTheGenerationAndForANewLog(t *testing.T) {
	if _, err := newGeneration(failing{}); !errors.Is(err, errNoRandomness) {
		t.Fatalf("newGeneration: %v, want errNoRandomness", err)
	}
	if l, err := newRequestLog(0, failing{}); !errors.Is(err, errNoRandomness) || l != nil {
		t.Fatalf("newRequestLog: %v, %v; want nil and errNoRandomness", l, err)
	}
}

func TestTheErrorTextIsFixedAndHoldsNothingOfTheSource(t *testing.T) {
	_, err := newGeneration(failing{})
	if err == nil || err.Error() != errNoRandomness.Error() {
		t.Fatalf("the error is %v; want the fixed text only", err)
	}
}

func TestAShortReadIsNoRandomnessToo(t *testing.T) {
	if _, err := newGeneration(io.LimitReader(ones{}, 8)); !errors.Is(err, errNoRandomness) {
		t.Fatalf("a source that gives 8 of 16 bytes: %v, want errNoRandomness", err)
	}
}

func TestANilSourceIsTheSystemsAndGivesA32CharacterGeneration(t *testing.T) {
	g, err := newGeneration(nil)
	if err != nil || len(g) != 32 {
		t.Fatalf("generation %q, error %v; want 32 characters", g, err)
	}
	h, _ := newGeneration(nil)
	if g == h {
		t.Fatal("two generations from the system's source were equal")
	}
}

func TestAServerOwnsItsSourceSoTwoServersDoNotShareOne(t *testing.T) {
	// One server has a failing source and one a working one at the same time: nothing global is involved, so each does what its own source says.
	a, err := newRequestLog(0, ones{})
	if err != nil {
		t.Fatal(err)
	}
	bad := &Server{engine: plan.NewEngine(), clock: newClockHolder(), trace: &trace{}, reqlog: a, stale: newStale(), held: newHeld(), entropy: failing{}}
	good := &Server{engine: plan.NewEngine(), clock: newClockHolder(), trace: &trace{}, reqlog: a, stale: newStale(), held: newHeld(), entropy: ones{}}
	if err := bad.clearRun(); !errors.Is(err, errNoRandomness) {
		t.Fatalf("the server with the failing source: %v, want errNoRandomness", err)
	}
	if err := good.clearRun(); err != nil {
		t.Fatalf("the server with the working source, at the same time: %v", err)
	}
}

func TestAResetThatCannotGetAGenerationChangesNothing(t *testing.T) {
	l, err := newRequestLog(0, ones{})
	if err != nil {
		t.Fatal(err)
	}
	l.add(RequestEntry{Target: "t", Operation: "o", Method: "GET", Path: "/p"})
	before, entries, _, _ := l.snapshot("", "")
	s := &Server{engine: plan.NewEngine(), clock: newClockHolder(), trace: &trace{}, reqlog: l, stale: newStale(), held: newHeld(), entropy: failing{}}
	if err := s.clearRun(); !errors.Is(err, errNoRandomness) {
		t.Fatalf("clearRun: %v, want errNoRandomness", err)
	}
	after, kept, _, _ := l.snapshot("", "")
	if after != before || len(kept) != len(entries) {
		t.Fatalf("a refused reset changed the log: generation %q to %q, %d entries to %d", before, after, len(entries), len(kept))
	}
}

func TestAResetWithRandomnessGivesANewGeneration(t *testing.T) {
	l, err := newRequestLog(0, nil)
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

func TestNewRefusesWithAFailingSourceInsteadOfPanicking(t *testing.T) {
	if _, err := New(Options{Routes: &routes.Table{}, Entropy: failing{}}); !errors.Is(err, errNoRandomness) {
		t.Fatalf("New with a failing source: %v, want errNoRandomness", err)
	}
}
