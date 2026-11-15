package verifier_test

import (
	"errors"
	"os"
	"reflect"
	"testing"

	"github.com/Noemai-org/noema/services/core/outcome/predicate"
	"github.com/Noemai-org/noema/services/core/outcome/verifier"
)

// TST-05 (ruling A30, A40; noema#252), RESIST S9: the third kill point, "after verify, before the note". It sits in Outcome, between the verdict
// being stored and the verdict being handed on to the caller that writes the explanation or note. Production leaves the hook nil. All data SYNTHETIC.

func TestTheOnlyBoundaryIsAfterTheVerdictIsStored(t *testing.T) {
	if got, want := verifier.Points(), []verifier.Point{verifier.PointAfterVerdictStored}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Points() = %v, want %v", got, want)
	}
	if verifier.PointAfterVerdictStored.String() != "AFTER_VERDICT_STORED" {
		t.Fatalf("name = %q", verifier.PointAfterVerdictStored.String())
	}
}

// The hook runs once, after the Store holds the verdict (so a crash there leaves a stored verdict) and not before.
func TestTheBoundaryRunsOnceAfterTheVerdictIsStored(t *testing.T) {
	var seen []verifier.Point
	var storedAtTheTime int
	w := newWorld(t)
	w.boundary = func(p verifier.Point) error {
		seen = append(seen, p)
		storedAtTheTime = len(w.store.writes)
		return nil
	}
	w.build(t, mustPack(t))
	res, err := w.verify(t)
	if err != nil {
		t.Fatal(err)
	}
	want(t, res, verifier.Achieved, verifier.ReasonPredicateTrue)
	if !reflect.DeepEqual(seen, []verifier.Point{verifier.PointAfterVerdictStored}) || storedAtTheTime != 1 {
		t.Fatalf("hook calls = %v with %d verdicts stored at the call, want one call after one stored verdict", seen, storedAtTheTime)
	}
}

// A crash after the verdict is stored: Verify returns the crash and no result, the verdict stands, and a redelivery of the verification finds that
// verdict and stores nothing new (INV-04: one verdict per verification).
func TestACrashAfterTheVerdictIsStoredKeepsItAndARedeliveryFindsIt(t *testing.T) {
	w := newWorld(t)
	w.boundary = func(verifier.Point) error { return verifier.ErrCrash }
	w.build(t, mustPack(t))
	res, err := w.verify(t)
	if !errors.Is(err, verifier.ErrCrash) || res != (verifier.Result{}) {
		t.Fatalf("Verify = %+v, %v; want the crash and no result", res, err)
	}
	if len(w.store.writes) != 1 {
		t.Fatalf("stored %d verdicts after the crash, want the 1 that was committed", len(w.store.writes))
	}
	stored := w.store.writes[0].Verdict.VerdictID
	w.boundary = nil
	w.build(t, mustPack(t))
	again, err := w.verify(t)
	if err != nil {
		t.Fatal(err)
	}
	if again.VerdictID != stored || len(w.store.writes) != 1 {
		t.Fatalf("redelivery gave verdict %q and %d stored; want the stored %q and still 1", again.VerdictID, len(w.store.writes), stored)
	}
}

// Replaying a stored verdict stores nothing, so there is no boundary to cross.
func TestReplayingAStoredVerdictDoesNotCrossTheBoundary(t *testing.T) {
	w := newWorld(t)
	if _, err := w.verify(t); err != nil {
		t.Fatal(err)
	}
	calls := 0
	w.boundary = func(verifier.Point) error { calls++; return nil }
	w.build(t, mustPack(t))
	if _, err := w.verify(t); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("the boundary ran %d times on a replay", calls)
	}
}

// Any hook error, not only ErrCrash, stops Verify at that point: the hook decides, not the verifier.
func TestAnErrorFromTheHookIsReturned(t *testing.T) {
	boom := errors.New("synthetic: hook failed")
	w := newWorld(t)
	w.boundary = func(verifier.Point) error { return boom }
	w.build(t, mustPack(t))
	if _, err := w.verify(t); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the hook's", err)
	}
}

func mustPack(t *testing.T) predicate.Pack {
	t.Helper()
	raw, err := os.ReadFile("../../../../packs/fintech_v1.json")
	if err != nil {
		t.Fatal(err)
	}
	pack, err := predicate.LoadPack(raw)
	if err != nil {
		t.Fatal(err)
	}
	return pack
}
