package verifier_test

// TEN-02, verdict half (internal, task ENV-14; PERD 34.5 [REC]): "A replayed scenario gives identical Trust decisions and
// verdicts." The Trust half is services/core/trust/decide/ten02_test.go.
//
// WHAT THIS SHOWS: six scenarios, each ending in a different verdict, are replayed 64 times in a seeded order, concurrently,
// each time on a freshly built world (its own reader script, store, signer and simulated clock). Every replay must give the
// same verdict, reason, predicate result, evidence reads (query, attempt, status, item count), recovery request, watch and
// verification time as the expectation that is committed below, which states what each scenario must conclude (INV-21,
// REQ-VER-001). Generated identifiers (verdict, evidence, recovery request IDs) are ULIDs and differ by design; they are the
// only fields left out. The signed document is not compared for the same reason: it carries the verdict ID.
//
// WHAT THIS DOES NOT SHOW: a replay against the real mock-case or a real database. The reads are scripted by this package's
// test reader (decision logic, including answers mock-case cannot produce); fault evidence comes from the end-to-end tests with
// real FaultPlans. TEN-02 therefore covers the decision, not the wire.

import (
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/outcome/verifier"
)

const (
	ten02VerdictSeed    = 20261103 // SYNTHETIC. The seed of the replay order; nothing else in this test is random.
	ten02VerdictReplays = 64
)

// ten02Outcome is what a verification concluded, without its generated identifiers.
type ten02Outcome struct {
	Verdict    verifier.Verdict
	Reason     verifier.Reason
	Predicate  string
	Reads      string
	Recovery   bool
	Watch      bool
	VerifiedAt time.Time
}

type ten02Scenario struct {
	name   string
	mutate func(*world)
	want   ten02Outcome
}

func ten02Scenarios() []ten02Scenario {
	absent := func(w *world) { w.read.byKey, w.read.byTx = []step{complete()}, []step{complete()} }
	verifiedAt := t0.Add(30 * time.Second) // the end of the settle window; the reads cost no simulated time when they succeed
	return []ten02Scenario{
		{"effect present", func(*world) {}, ten02Outcome{Verdict: verifier.Achieved, Reason: verifier.ReasonPredicateTrue, Predicate: "true",
			Reads: "BY_KEY/1/1/OK/1;BY_TRANSACTION/1/1/OK/1", VerifiedAt: verifiedAt}},
		{"effect verifiably absent", absent, ten02Outcome{Verdict: verifier.NotAchieved, Reason: verifier.ReasonVerifiedAbsent, Predicate: "false",
			Reads: "BY_KEY/1/1/OK/0;BY_TRANSACTION/1/1/OK/0", Recovery: true, VerifiedAt: verifiedAt}},
		{"acknowledged but absent", func(w *world) { absent(w); w.store.receipt.State = "ACKNOWLEDGED" }, ten02Outcome{Verdict: verifier.NotAchieved,
			Reason: verifier.ReasonPostconditionUnchecked, Predicate: "false", Reads: "BY_KEY/1/1/OK/0;BY_TRANSACTION/1/1/OK/0", Recovery: true, VerifiedAt: verifiedAt}},
		{"incomplete page", func(w *world) { w.read.byKey = []step{{page: verifier.CasePage{Complete: false}}} }, ten02Outcome{Verdict: verifier.Unknown,
			Reason: verifier.ReasonIncompleteRead, Predicate: "nil", Reads: "BY_KEY/1/1/OK/0", Recovery: true, VerifiedAt: verifiedAt}},
		{"malformed read", func(w *world) { w.read.byKey = []step{{err: &verifier.ReadError{Kind: verifier.ReadMalformed}}} }, ten02Outcome{Verdict: verifier.Unknown,
			Reason: verifier.ReasonMalformedRead, Predicate: "nil", Reads: "BY_KEY/1/1/MALFORMED/0", Recovery: true, VerifiedAt: verifiedAt}},
		{"reads refused", func(w *world) { w.read.byKey = []step{{err: &verifier.ReadError{Kind: verifier.ReadRefused}}} }, ten02Outcome{Verdict: verifier.Unknown,
			Reason: verifier.ReasonReadRefused, Predicate: "nil", Reads: "BY_KEY/1/1/REFUSED/0", Recovery: true, VerifiedAt: verifiedAt}},
	}
}

func ten02Observe(w *world, res verifier.Result, wr verifier.Write) ten02Outcome {
	o := ten02Outcome{Verdict: res.Verdict, Reason: res.Reason, Predicate: "nil", Recovery: wr.RecoveryRequestID != "", Watch: wr.Watch != nil, VerifiedAt: wr.Verdict.VerifiedAt}
	if wr.Verdict.PredicateResult != nil {
		o.Predicate = fmt.Sprintf("%t", *wr.Verdict.PredicateResult)
	}
	for i, r := range wr.Reads {
		if i > 0 {
			o.Reads += ";"
		}
		o.Reads += fmt.Sprintf("%s/%d/%d/%s/%d", r.Query, r.Attempt, r.Page, r.Status, r.Items)
	}
	return o
}

func TestTEN02ReplayedScenariosGiveIdenticalVerdicts(t *testing.T) {
	defer func() {
		t.Logf("TEN_RESULT id=TEN-02 part=outcome-verdicts seed=%d replays=%d scenarios=%d pass=%t", ten02VerdictSeed, ten02VerdictReplays, len(ten02Scenarios()), !t.Failed())
	}()
	scenarios := ten02Scenarios()

	type job struct {
		scenario int
		replay   int
	}
	var jobs []job
	for s := range scenarios {
		for r := 0; r < ten02VerdictReplays; r++ {
			jobs = append(jobs, job{s, r})
		}
	}
	rng := rand.New(rand.NewSource(ten02VerdictSeed))
	rng.Shuffle(len(jobs), func(i, j int) { jobs[i], jobs[j] = jobs[j], jobs[i] })

	var mu sync.Mutex
	got := make([][]ten02Outcome, len(scenarios))
	t.Run("replays", func(t *testing.T) {
		for _, j := range jobs {
			j := j
			t.Run(fmt.Sprintf("%s/%d", scenarios[j.scenario].name, j.replay), func(t *testing.T) {
				t.Parallel()
				w := newWorld(t, scenarios[j.scenario].mutate)
				res, wr := w.decided(t)
				o := ten02Observe(w, res, wr)
				mu.Lock()
				got[j.scenario] = append(got[j.scenario], o)
				mu.Unlock()
			})
		}
	})

	for s, sc := range scenarios {
		if len(got[s]) != ten02VerdictReplays {
			t.Fatalf("%s: %d replays finished, want %d", sc.name, len(got[s]), ten02VerdictReplays)
		}
		for i, o := range got[s] {
			if o != sc.want {
				t.Fatalf("%s: replay %d gave %+v, want %+v", sc.name, i, o, sc.want)
			}
		}
	}
}

// A mutant that makes a verdict depend on something other than the evidence must be seen by this test: the same world built
// twice with a different receipt state and the same absent target gives different verdicts, so the comparison above is able to
// tell two outcomes apart.
func TestTEN02TheComparisonCanTellTwoVerdictsApart(t *testing.T) {
	sc := ten02Scenarios()
	a, b := newWorld(t, sc[1].mutate), newWorld(t, sc[2].mutate)
	ra, wa := a.decided(t)
	rb, wb := b.decided(t)
	if ten02Observe(a, ra, wa) == ten02Observe(b, rb, wb) {
		t.Fatal("two scenarios with different conclusions compare equal: the replay comparison is blind")
	}
}
