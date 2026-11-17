// Package workflowstest is test support for workflows (TEN-05, PERD 34.5; ruling A256 allows the Temporal test suite for it). It binds Noema's simulated clock to the Temporal test
// environment's time: when the test environment skips time to fire a timer, the simulated clock follows, so an activity that reads the clock interface sees the instant workflow
// code sees. The simulated clock applies only in tests and replays; nothing in production imports this package.
//
// May import: the standard library, go.temporal.io/sdk/testsuite, platform/clock.
//
// Must never: be imported by production code, read the wall clock, or move the simulated clock backwards.
package workflowstest

import (
	"go.temporal.io/sdk/testsuite"

	"github.com/Noemai-org/noema/services/core/platform/clock"
)

// BindClock makes sim follow env's test time: each time a timer fires, sim is advanced to env.Now() (never backwards: clock.Manual ignores a negative step). The caller starts sim
// at the instant it gave env.SetStartTime.
func BindClock(env *testsuite.TestWorkflowEnvironment, sim *clock.Manual) {
	env.SetOnTimerFiredListener(func(string) {
		if d := env.Now().Sub(sim.Now()); d > 0 {
			sim.Advance(d)
		}
	})
}
