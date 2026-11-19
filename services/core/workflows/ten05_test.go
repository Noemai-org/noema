package workflows_test

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"github.com/Noemai-org/noema/services/core/platform/clock"
)

// TEN-05 (G0-N; PERD 34.5, REQ-ENV-005, INV-21): Tushe time is controlled per object; Noema's simulated clock applies only in tests and replays, and advances together with Temporal
// test time. This is the Noema half: a workflow sleeps 48 hours in the Temporal test environment, which skips the time; the simulated Noema clock (clock.Manual, labelled SYNTHETIC)
// is advanced to the environment's time whenever a timer fires, and an activity reads it through the clock interface, never the wall clock. Before the sleep the activity sees the
// start; after it, start plus 48 hours, equal to the environment's own time; and no real time passes. No test double replaces a failure mode: the timer is the SDK's own.

const ten05Sleep = 48 * time.Hour

type ten05Observation struct{ Before, After time.Time }

type ten05Activities struct{ Clock clock.Clock }

func (a *ten05Activities) Observe(context.Context) (time.Time, error) { return a.Clock.Now(), nil }

func ten05Workflow(ctx workflow.Context) (ten05Observation, error) {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{StartToCloseTimeout: time.Minute})
	var a *ten05Activities
	var out ten05Observation
	if err := workflow.ExecuteActivity(ctx, a.Observe).Get(ctx, &out.Before); err != nil {
		return out, err
	}
	if err := workflow.Sleep(ctx, ten05Sleep); err != nil {
		return out, err
	}
	err := workflow.ExecuteActivity(ctx, a.Observe).Get(ctx, &out.After)
	return out, err
}

func TestTEN05TheSimulatedClockAdvancesWithTemporalTestTime(t *testing.T) {
	t.Cleanup(func() {
		t.Logf("TEN_RESULT id=TEN-05 seed=none clock=SYNTHETIC skipped=%s pass=%t", ten05Sleep, !t.Failed())
	})
	start := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC) // SYNTHETIC
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.SetStartTime(start)
	sim := clock.NewManual(start)
	env.RegisterWorkflow(ten05Workflow)
	env.RegisterActivity(&ten05Activities{Clock: sim})

	fired := 0
	env.SetOnTimerFiredListener(func(string) {
		fired++
		sim.Advance(env.Now().Sub(sim.Now())) // the simulated clock follows the environment's time, forwards only
	})
	wall := clock.System{} // the wall clock, only to show that no real time passed
	began := wall.Now()
	env.ExecuteWorkflow(ten05Workflow)
	if !env.IsWorkflowCompleted() {
		t.Fatal("the workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatal(err)
	}
	var got ten05Observation
	if err := env.GetWorkflowResult(&got); err != nil {
		t.Fatal(err)
	}
	if !got.Before.Equal(start) {
		t.Errorf("before the sleep the activity read %s, want the start %s", got.Before, start)
	}
	if want := start.Add(ten05Sleep); !got.After.Equal(want) {
		t.Errorf("after the sleep the activity read %s, want %s (start plus %s)", got.After, want, ten05Sleep)
	}
	if !got.After.Equal(env.Now()) {
		t.Errorf("the simulated clock reads %s, Temporal test time is %s", got.After, env.Now())
	}
	if fired != 1 {
		t.Errorf("%d timers fired, want 1", fired)
	}
	if real := wall.Now().Sub(began); real > 10*time.Second {
		t.Errorf("%s of real time passed: the test environment did not skip the %s", real, ten05Sleep)
	}
}

// The simulated clock applies only in tests and replays: no production file outside the clock package builds a Manual clock or names the type (the real wiring is clock.System,
// and `time.Now` outside platform/clock is a lint failure).
func TestTEN05TheSimulatedClockIsBuiltOnlyInTestFiles(t *testing.T) {
	root := filepath.Join("..")
	var seen int
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || strings.Contains(filepath.ToSlash(path), "platform/clock/") {
			return nil
		}
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		seen++
		if strings.Contains(string(src), "clock.NewManual") || strings.Contains(string(src), "clock.Manual") {
			t.Errorf("%s builds or names the simulated clock in non-test code", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if seen < 50 {
		t.Fatalf("only %d production files were scanned: the walk is not reading the module", seen)
	}
}
