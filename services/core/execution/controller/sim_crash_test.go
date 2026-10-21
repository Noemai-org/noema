package controller_test

import (
	"time"

	"github.com/Noemai-org/noema/services/core/execution/controller"
)

// CrashResult is what remained after a process died at one boundary and the system recovered.
type CrashResult struct {
	Point             controller.Point
	Mode              FaultMode
	Crashed           bool
	SpentAtCrash      bool // the ledger had spent the grant when the process died
	DispatchesAtCrash int
	EffectsAtCrash    int
	Final             Snapshot
	FinalEffects      int
	Run               *Run
}

// RunCrash kills one execution of one grant at the given boundary (the process dies: only committed state
// survives), with the network applying `mode` to the first dispatch, then lets the system recover as the
// protocol says: the sweeper writes OUTCOME_UNKNOWN for a spent attempt without a receipt, a stand-in for
// Outcome reads the target's truth and, if the effect is absent, a stand-in for Trust issues a new grant.
func RunCrash(point controller.Point, mode FaultMode) CrashResult {
	run := &Run{Seed: uint64(point)*100 + uint64(mode)}
	w := NewWorld(secret)
	run.World = w
	first := true
	w.SetFaults(func() FaultMode {
		if first {
			first = false
			return mode
		}
		return FaultNone
	})
	now := w.Clock.Now()
	g := w.IssueGrant(1, now.Add(-time.Minute), now.Add(10*time.Minute))
	w.Store(g)
	key := keyOf(w)

	crashed := false
	c := controllerFor(w, func(p controller.Point) error {
		if p == point {
			return controller.ErrCrash
		}
		return nil
	})
	ctx, cancel := deadline()
	_, err := c.Execute(ctx, TenantID, g.Grant.GrantID)
	cancel()
	crashed = err == controller.ErrCrash

	atCrash := w.Snapshot()
	res := CrashResult{Point: point, Mode: mode, Crashed: crashed, SpentAtCrash: atCrash.Spent[g.Grant.GrantID], DispatchesAtCrash: len(atCrash.Dispatches),
		EffectsAtCrash: atCrash.Effects[key], Run: run}
	checkSafety(run, atCrash, grantValid, false)

	// The grant is presented again, as a client or workflow would after a crash. A spent grant is a replay.
	ctx, cancel = deadline()
	out, _ := controllerFor(w, nil).Execute(ctx, TenantID, g.Grant.GrantID)
	cancel()
	if res.SpentAtCrash && !out.Replayed {
		run.fail("a spent grant presented again must be a replay, got %+v", out)
	}
	converge(run, w, key, g)
	res.Final = w.Snapshot()
	res.FinalEffects = w.Truth(key)
	checkSafety(run, res.Final, grantValid, false)
	return res
}

// Modes lists every network fault mode.
func Modes() []FaultMode {
	out := make([]FaultMode, faultCount)
	for i := range out {
		out[i] = FaultMode(i)
	}
	return out
}
