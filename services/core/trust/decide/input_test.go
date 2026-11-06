package decide_test

// TRU-09: decision determinism (REQ-TRU-007, INV-21). The same (Intent Object, sealed snapshot, policy versions, pack version, clock)
// give the same decision, and the decision input hash that records those inputs is canonical: it does not depend on the order
// the policy versions are listed in, and it changes when any one input changes. The golden vectors below were computed by an
// independent Python implementation of the construction written in input.go.

import (
	"context"
	"errors"
	"math/rand"
	"reflect"
	"sync"
	"testing"
	"testing/quick"

	"github.com/Noemai-org/noema/services/core/trust/decide"
)

const (
	hashI = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	hashS = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
)

func baseInput() decide.DecisionInput {
	return decide.DecisionInput{
		IntentHash:     hashI,
		SnapshotHash:   hashS,
		PackVersion:    "fin.pack@1.0.0",
		EvaluatedAt:    1_700_000_100,
		PolicyVersions: []decide.PolicyVersionRef{{PolicyID: "fin.pol", Version: "3.1"}},
	}
}

func TestInputHashGoldenVectors(t *testing.T) {
	two := baseInput()
	two.PolicyVersions = []decide.PolicyVersionRef{{PolicyID: "fin.pol", Version: "3.1"}, {PolicyID: "fin.pol", Version: "3.0"}}
	noSnapshot := decide.DecisionInput{IntentHash: hashI, PackVersion: "fin.pack@1.0.0", EvaluatedAt: 1_700_000_100}
	for name, c := range map[string]struct {
		in   decide.DecisionInput
		want string
	}{
		"one policy version":          {baseInput(), "sha256:ba5f2451e90ca4a65cc8c3793ed4b43ab42a616411837a54329fed64a17c7d58"},
		"two policy versions":         {two, "sha256:24e53819bcc0ef7416eb4964c98da2a7ac394df9d10783a66502cb9534829e3f"},
		"refusal before any evidence": {noSnapshot, "sha256:169c7c1af43764d3586d34ea460d2ef0e43afdab4d0c474a1016a61ed9511fd4"},
	} {
		got, err := decide.InputHash(c.in)
		if err != nil || got != c.want {
			t.Errorf("%s: got %q (%v), want %q", name, got, err, c.want)
		}
	}
}

func TestInputHashIgnoresTheOrderAndRepeatsOfPolicyVersions(t *testing.T) {
	a, b := baseInput(), baseInput()
	a.PolicyVersions = []decide.PolicyVersionRef{{PolicyID: "fin.pol", Version: "3.1"}, {PolicyID: "fin.pol", Version: "3.0"}}
	b.PolicyVersions = []decide.PolicyVersionRef{{PolicyID: "fin.pol", Version: "3.0"}, {PolicyID: "fin.pol", Version: "3.1"}, {PolicyID: "fin.pol", Version: "3.0"}}
	ha, _ := decide.InputHash(a)
	hb, _ := decide.InputHash(b)
	if ha == "" || ha != hb {
		t.Fatalf("order or repeats changed the hash: %q vs %q", ha, hb)
	}
}

func TestEveryInputChangesTheHash(t *testing.T) {
	base, _ := decide.InputHash(baseInput())
	mutations := map[string]func(*decide.DecisionInput){
		"intent":         func(in *decide.DecisionInput) { in.IntentHash = hashS },
		"snapshot":       func(in *decide.DecisionInput) { in.SnapshotHash = hashI },
		"no snapshot":    func(in *decide.DecisionInput) { in.SnapshotHash = "" },
		"pack version":   func(in *decide.DecisionInput) { in.PackVersion = "fin.pack@1.0.1" },
		"clock":          func(in *decide.DecisionInput) { in.EvaluatedAt++ },
		"policy id":      func(in *decide.DecisionInput) { in.PolicyVersions[0].PolicyID = "fin.pol2" },
		"policy version": func(in *decide.DecisionInput) { in.PolicyVersions[0].Version = "3.2" },
		"extra version": func(in *decide.DecisionInput) {
			in.PolicyVersions = append(in.PolicyVersions, decide.PolicyVersionRef{PolicyID: "x.pol", Version: "1"})
		},
		"no versions": func(in *decide.DecisionInput) { in.PolicyVersions = nil },
	}
	for name, f := range mutations {
		in := baseInput()
		in.PolicyVersions = append([]decide.PolicyVersionRef(nil), in.PolicyVersions...)
		f(&in)
		got, err := decide.InputHash(in)
		if err != nil || got == base {
			t.Errorf("%s: hash %q (%v) equals the base hash", name, got, err)
		}
	}
	// A length-prefixed construction cannot be confused by moving characters between fields.
	x, y := baseInput(), baseInput()
	x.PackVersion, x.PolicyVersions = "ab", []decide.PolicyVersionRef{{PolicyID: "c", Version: "d"}}
	y.PackVersion, y.PolicyVersions = "a", []decide.PolicyVersionRef{{PolicyID: "bc", Version: "d"}}
	hx, _ := decide.InputHash(x)
	hy, _ := decide.InputHash(y)
	if hx == hy {
		t.Fatal("moving a character between two fields gave the same hash")
	}
}

func TestInputHashRefusesAnInvalidInput(t *testing.T) {
	cases := map[string]func(*decide.DecisionInput){
		"intent hash empty":          func(in *decide.DecisionInput) { in.IntentHash = "" },
		"intent hash not sha256:hex": func(in *decide.DecisionInput) { in.IntentHash = "abc" },
		"intent hash upper case": func(in *decide.DecisionInput) {
			in.IntentHash = "sha256:" + "ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789"
		},
		"snapshot hash malformed":      func(in *decide.DecisionInput) { in.SnapshotHash = "sha256:12" },
		"no pack version":              func(in *decide.DecisionInput) { in.PackVersion = "" },
		"no clock reading":             func(in *decide.DecisionInput) { in.EvaluatedAt = 0 },
		"negative clock reading":       func(in *decide.DecisionInput) { in.EvaluatedAt = -5 },
		"policy version without id":    func(in *decide.DecisionInput) { in.PolicyVersions[0].PolicyID = "" },
		"policy version without value": func(in *decide.DecisionInput) { in.PolicyVersions[0].Version = "" },
	}
	for name, f := range cases {
		in := baseInput()
		in.PolicyVersions = append([]decide.PolicyVersionRef(nil), in.PolicyVersions...)
		f(&in)
		if h, err := decide.InputHash(in); !errors.Is(err, decide.ErrInputInvalid) || h != "" {
			t.Errorf("%s: want ErrInputInvalid and no hash, got %q (%v)", name, h, err)
		}
	}
}

// stepsFor builds step functions that depend on the input only (never on time, randomness or map order): the shape the real
// steps must have. Which step decides, and with what, is derived from the input hash.
func stepsFor(in decide.DecisionInput) decide.Steps {
	h, err := decide.InputHash(in)
	if err != nil {
		panic(err)
	}
	var s decide.Steps
	decideAt := int(h[len(h)-1]) % int(decide.T15DecisionRecorded)
	for i := range s {
		step := i
		s[i] = func(context.Context) decide.StepResult {
			switch {
			case step == decideAt && decideAt < int(decide.T4Capability)-1:
				return decide.Stop(decisionFor(decide.Unknown))
			case step == int(decide.T15DecisionRecorded)-1:
				return decide.Stop(decisionFor(decide.Allow))
			}
			return decide.Continue()
		}
	}
	return s
}

// REQ-TRU-007 acceptance and TRU-09 done-when: 10,000 replays of the same inputs give 0 mismatches, including when they run
// concurrently (the race detector sees every shared variable).
func TestReplayOfTheSameInputsGivesIdenticalDecisionsAndTraces(t *testing.T) {
	const replays = 10_000
	in := baseInput()
	first, firstTrace := decide.Evaluate(context.Background(), version, stepsFor(in))
	var wg sync.WaitGroup
	var mu sync.Mutex
	mismatches := 0
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < replays/8; i++ {
				d, tr := decide.Evaluate(context.Background(), version, stepsFor(in))
				if !reflect.DeepEqual(d, first) || !reflect.DeepEqual(tr, firstTrace) {
					mu.Lock()
					mismatches++
					mu.Unlock()
				}
			}
		}()
	}
	wg.Wait()
	if mismatches != 0 {
		t.Fatalf("%d of %d replays differ from the first", mismatches, replays)
	}
}

// Over generated inputs: the hash and the evaluation are pure functions of the input. Two inputs that differ only in the order of
// the policy versions give the same hash and the same decision; the result never depends on which of them was built first.
func TestGeneratedInputsAreDeterministic(t *testing.T) {
	hex := "0123456789abcdef"
	mkHash := func(seed [32]byte) string {
		b := make([]byte, 64)
		for i := range b {
			b[i] = hex[int(seed[i%32])%16]
		}
		return "sha256:" + string(b)
	}
	property := func(i, s [32]byte, at uint32, nVersions uint8, order int64) bool {
		in := decide.DecisionInput{IntentHash: mkHash(i), SnapshotHash: mkHash(s), PackVersion: "fin.pack@1.0.0", EvaluatedAt: int64(at) + 1}
		for k := 0; k < int(nVersions)%6; k++ {
			in.PolicyVersions = append(in.PolicyVersions, decide.PolicyVersionRef{PolicyID: "p" + string(rune('a'+k)), Version: "1"})
		}
		shuffled := in
		shuffled.PolicyVersions = append([]decide.PolicyVersionRef(nil), in.PolicyVersions...)
		rand.New(rand.NewSource(order)).Shuffle(len(shuffled.PolicyVersions), func(a, b int) {
			shuffled.PolicyVersions[a], shuffled.PolicyVersions[b] = shuffled.PolicyVersions[b], shuffled.PolicyVersions[a]
		})
		h1, e1 := decide.InputHash(in)
		h2, e2 := decide.InputHash(shuffled)
		d1, t1 := decide.Evaluate(context.Background(), version, stepsFor(in))
		d2, t2 := decide.Evaluate(context.Background(), version, stepsFor(shuffled))
		return e1 == nil && e2 == nil && h1 == h2 && reflect.DeepEqual(d1, d2) && reflect.DeepEqual(t1, t2)
	}
	if err := quick.Check(property, &quick.Config{MaxCount: 5000, Rand: rand.New(rand.NewSource(20261103))}); err != nil {
		t.Fatal(err)
	}
}
