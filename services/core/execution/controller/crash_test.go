package controller_test

import (
	"testing"

	"github.com/Noemai-org/noema/services/core/execution/controller"
)

// D7, crash-point injection: the process dies at every step boundary of PERD 27.1, under every network fault,
// and after recovery (sweeper, a stand-in for Outcome reading the target, a stand-in for Trust issuing a new
// grant) the target holds exactly one effect. At no moment is there more than one.
func TestExactlyOneEffectForEveryCrashPointAndEveryFault(t *testing.T) {
	for _, p := range controller.Points() {
		for _, m := range Modes() {
			r := RunCrash(p, m)
			name := PointName(p)
			if !r.Crashed {
				// A fault-dependent point (after the response) always exists; every point must be reachable.
				t.Errorf("%s mode %d: the point was never reached", name, m)
			}
			for _, v := range r.Run.Violations {
				t.Errorf("%s mode %d: %s", name, m, v)
			}
			if r.FinalEffects != 1 {
				t.Errorf("%s mode %d: %d effects after recovery, want exactly 1", name, m, r.FinalEffects)
			}
			if r.Final.Pending != 0 {
				t.Errorf("%s mode %d: %d attempts without a receipt after recovery", name, m, r.Final.Pending)
			}
			// What a crash at this boundary must have left behind.
			beforeSpend := p <= controller.PointInSpendBeforeCommit
			if beforeSpend && r.SpentAtCrash {
				t.Errorf("%s: a crash before the spend commits must leave nothing spent", name)
			}
			if !beforeSpend && !r.SpentAtCrash {
				t.Errorf("%s: a crash after the spend commits must leave it spent", name)
			}
			if p < controller.PointAfterSpend && r.DispatchesAtCrash != 0 {
				t.Errorf("%s: nothing may be dispatched before the spend is committed (%d)", name, r.DispatchesAtCrash)
			}
			if p >= controller.PointAfterDispatch && r.DispatchesAtCrash != 1 {
				t.Errorf("%s: want exactly 1 dispatch at the crash, got %d", name, r.DispatchesAtCrash)
			}
			if r.EffectsAtCrash > 1 {
				t.Errorf("%s mode %d: %d effects at the crash", name, m, r.EffectsAtCrash)
			}
		}
	}
}

// EC-E09: a crash between E8 and E11 is recovered by a receipt with OUTCOME_UNKNOWN and never a second dispatch.
func TestTheSweeperNeverRedispatches(t *testing.T) {
	r := RunCrash(controller.PointAfterSpend, FaultNone)
	rc, ok := r.Final.Receipts["grt_1#1"]
	if !ok || rc.State != controller.StateOutcomeUnknown || rc.TransportResult != "SWEEPER_NO_RECEIPT" {
		t.Fatalf("%+v", rc)
	}
	for _, d := range r.Final.Dispatches {
		if d.GrantID == "grt_1" {
			t.Fatal("the crashed grant was never dispatched, so it cannot have a dispatch now")
		}
	}
}
