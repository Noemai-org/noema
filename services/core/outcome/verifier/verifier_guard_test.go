package verifier_test

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/outcome/verifier"
)

// Redelivery, corrupt input and the unbound-variable rule (split from verifier_test.go to stay under the review size trigger).

// Duplicate delivery (the outbox is at least once): a second Verify for the same verification returns the stored verdict, reads
// nothing and writes nothing. A concurrent writer that wins the race is the same case.
func TestARepeatedVerificationReturnsTheStoredVerdictAndDoesNothingMore(t *testing.T) {
	w := newWorld(t, func(w *world) { w.read.byKey, w.read.byTx = []step{complete()}, []step{complete()} })
	first, _ := w.decided(t)
	reads := w.read.reads()
	again, err := w.verify(t)
	if err != nil || again.VerdictID != first.VerdictID || again.Verdict != first.Verdict || !again.Replayed {
		t.Fatalf("second delivery: %+v %v, first %+v", again, err, first)
	}
	if w.read.reads() != reads || len(w.store.writes) != 1 {
		t.Fatalf("a repeated delivery read %d more and wrote %d verdicts", w.read.reads()-reads, len(w.store.writes))
	}
}

func TestAWriterThatLosesTheRaceReturnsTheWinnersVerdict(t *testing.T) {
	w := newWorld(t, func(w *world) {
		w.store.found = map[string]verifier.Found{tenant + "/vrf_0001": {Result: verifier.Result{VerdictID: "ver_winner", Verdict: verifier.Achieved, Reason: verifier.ReasonPredicateTrue}, ReceiptID: rcpt}}
	})
	// The first lookup must miss and the insert must lose: the race is between the lookup and the insert.
	hidden := w.store.found
	w.store.found = nil
	w.store.race = hidden
	res, err := w.verify(t)
	if err != nil || res.VerdictID != "ver_winner" || !res.Replayed {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestAVerificationNeedsAnIDAndAnIDIsNotReusedForAnotherReceipt(t *testing.T) {
	w := newWorld(t, func(w *world) { w.req.VerificationID = "" })
	if _, err := w.verify(t); err == nil || w.read.reads() != 0 || len(w.store.writes) != 0 {
		t.Fatalf("a verification with no ID was accepted: %v", err)
	}
	w = newWorld(t, func(w *world) {
		w.store.found = map[string]verifier.Found{tenant + "/vrf_0001": {Result: verifier.Result{VerdictID: "ver_old"}, ReceiptID: "rcp_other"}}
	})
	if _, err := w.verify(t); err == nil || w.read.reads() != 0 {
		t.Fatalf("an ID used for another receipt was accepted: %v", err)
	}
}

// A receipt state that is not an execution state is corrupt data: no read, no verdict.
func TestAReceiptInAnUnknownStateIsRefused(t *testing.T) {
	w := newWorld(t, func(w *world) { w.store.receipt.State = "NOT_SENT" })
	if _, err := w.verify(t); err == nil || w.read.reads() != 0 || len(w.store.writes) != 0 {
		t.Fatalf("an unknown receipt state was verified: %v", err)
	}
}

func TestANegativeSettleWindowIsRefused(t *testing.T) {
	w := newWorld(t, func(w *world) { w.req.Definition.SettleWindowSeconds = -1 })
	if _, err := w.verify(t); !errors.Is(err, verifier.ErrDefinition) || w.read.reads() != 0 {
		t.Fatalf("err = %v", err)
	}
}

// The unbound-variable check is the compiler's, not a scan of the text: a variable named only inside a string literal is not
// a reference, and a missing binding is found whichever other variables are bound.
func TestUnboundVariablesAreFoundByTheCompilerNotByTextScan(t *testing.T) {
	t.Run("one of two bindings missing", func(t *testing.T) {
		w := newWorld(t, func(w *world) { delete(w.req.Bindings, "subject_resource") })
		res, _ := w.decided(t)
		want(t, res, verifier.Unknown, verifier.ReasonPredicateUnknown)
		if w.read.reads() != 0 {
			t.Fatal("a verdict that cannot follow from the bindings was read for")
		}
	})
	t.Run("a variable name only inside a string literal is no reference", func(t *testing.T) {
		w := newWorld(t, func(w *world) {
			w.req.Definition.SuccessPredicate = "exists(case where case.subject == subject_resource and case.owner_queue == 'intent_id') or exists(case where case.id == case.id)"
			delete(w.req.Bindings, "intent_id")
		})
		res, _ := w.decided(t)
		want(t, res, verifier.Achieved, verifier.ReasonPredicateTrue)
	})
}

// The settle window is converted to a time.Duration. A schema-valid value above MaxInt64 nanoseconds would wrap negative and let
// a verdict through without waiting; it is refused, and the largest value that fits is accepted.
func TestASettleWindowThatOverflowsADurationIsRefused(t *testing.T) {
	const largest = math.MaxInt64 / int64(time.Second)
	for _, secs := range []int64{largest + 1, 1<<53 - 1, math.MaxInt64} {
		w := newWorld(t, func(w *world) { w.req.Definition.SettleWindowSeconds = secs })
		if _, err := w.verify(t); !errors.Is(err, verifier.ErrDefinition) || w.read.reads() != 0 || len(w.store.writes) != 0 {
			t.Fatalf("settle window %d: err = %v, reads %d", secs, err, w.read.reads())
		}
	}
	w := newWorld(t, func(w *world) { w.req.Definition.SettleWindowSeconds = largest })
	var open *verifier.ErrSettleWindowOpen
	if _, err := w.verify(t); !errors.As(err, &open) {
		t.Fatalf("the largest window that fits must be accepted and still open, got %v", err)
	}
}
