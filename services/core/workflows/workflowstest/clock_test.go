package workflowstest_test

// TEN-05 (PERD §34.5, G0-N; ruling A256 allows the Temporal test suite for it): Noema's simulated clock applies only in tests and replays, and it advances together with Temporal test
// time. The test environment skips time when a workflow sleeps; BindClock makes the simulated clock (clock.Manual, labelled SYNTHETIC) follow that time, so an activity that reads
// the clock interface sees the same instant as workflow code. Workflow code reads workflow.Now and never the wall clock (INV-30, INV-21). All data is SYNTHETIC.

import (
	"context"
	"go/parser"
	"go/token"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/workflows/workflowstest"
)

var start = time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC) // SYNTHETIC

// stamps are what the activity saw on the simulated clock and what the workflow saw as its own time, at each step.
type stamps struct{ Activity, Workflow []time.Time }

// reader is the activity: it reads the clock interface, as every Noema activity does.
type reader struct {
	c    clock.Clock
	seen []time.Time
}

func (r *reader) Read(context.Context) (time.Time, error) {
	now := r.c.Now()
	r.seen = append(r.seen, now)
	return now, nil
}

// sleepsAndReads sleeps for each duration, then reads the simulated clock through an activity and records the workflow's own time.
func sleepsAndReads(ctx workflow.Context, sleeps []time.Duration) (stamps, error) {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{StartToCloseTimeout: time.Minute})
	var out stamps
	for _, d := range sleeps {
		if err := workflow.Sleep(ctx, d); err != nil {
			return out, err
		}
		var at time.Time
		if err := workflow.ExecuteActivity(ctx, "Read").Get(ctx, &at); err != nil {
			return out, err
		}
		out.Activity = append(out.Activity, at.UTC())
		out.Workflow = append(out.Workflow, workflow.Now(ctx).UTC())
	}
	return out, nil
}

func newEnv(t *testing.T) (*testsuite.TestWorkflowEnvironment, *clock.Manual, *reader) {
	t.Helper()
	var suite testsuite.WorkflowTestSuite
	suite.SetLogger(log.NewStructuredLogger(slog.New(slog.DiscardHandler)))
	env := suite.NewTestWorkflowEnvironment()
	env.SetStartTime(start)
	sim := clock.NewManual(start)
	rd := &reader{c: sim}
	env.RegisterWorkflow(sleepsAndReads)
	env.RegisterActivityWithOptions(rd.Read, activity.RegisterOptions{Name: "Read"})
	return env, sim, rd
}

func run(t *testing.T, env *testsuite.TestWorkflowEnvironment, sleeps ...time.Duration) stamps {
	t.Helper()
	env.ExecuteWorkflow(sleepsAndReads, sleeps)
	if !env.IsWorkflowCompleted() {
		t.Fatal("the workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatal(err)
	}
	var out stamps
	if err := env.GetWorkflowResult(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

// TEN-05: after each sleep the simulated clock, read by an activity, is the instant Temporal's test time reached, and equals the workflow's own time.
func TestTheSimulatedClockAdvancesWithTemporalTestTime(t *testing.T) {
	env, sim, _ := newEnv(t)
	workflowstest.BindClock(env, sim)
	got := run(t, env, 36*time.Hour, 90*time.Minute, 24*time.Hour)
	want := []time.Time{start.Add(36 * time.Hour), start.Add(37*time.Hour + 30*time.Minute), start.Add(61*time.Hour + 30*time.Minute)}
	if len(got.Activity) != 3 || len(got.Workflow) != 3 {
		t.Fatalf("stamps %+v", got)
	}
	for i := range want {
		if !got.Activity[i].Equal(want[i]) || !got.Workflow[i].Equal(want[i]) {
			t.Errorf("step %d: the activity saw %v and the workflow %v, want both %v", i, got.Activity[i], got.Workflow[i], want[i])
		}
	}
	if !sim.Now().Equal(want[2]) {
		t.Errorf("the simulated clock ends at %v, want %v", sim.Now(), want[2])
	}
}

// The control: without the bridge the simulated clock does not move while Temporal's test time does, so the test above can fail (it detects a clock that stays behind).
func TestWithoutTheBridgeTheSimulatedClockStaysBehind(t *testing.T) {
	env, sim, _ := newEnv(t)
	got := run(t, env, 36*time.Hour)
	if !got.Activity[0].Equal(start) || got.Workflow[0].Equal(start) {
		t.Errorf("the unbridged clock read %v and the workflow %v: the clock should have stayed at %v while the workflow moved on", got.Activity[0], got.Workflow[0], start)
	}
	if !sim.Now().Equal(start) {
		t.Errorf("the simulated clock moved by itself to %v", sim.Now())
	}
}

// The clock only follows forward: a bridge never sets the simulated clock back (time never runs backwards), and a simulated clock already ahead is left where it is.
func TestTheBridgeNeverMovesTheClockBackwards(t *testing.T) {
	env, sim, _ := newEnv(t)
	ahead := start.Add(100 * time.Hour)
	sim.Advance(100 * time.Hour)
	workflowstest.BindClock(env, sim)
	run(t, env, time.Hour)
	if sim.Now().Before(ahead) {
		t.Errorf("the simulated clock went back to %v from %v", sim.Now(), ahead)
	}
}

// TEN-05 "only in tests and replays": no production source file builds the simulated clock. Every file that calls clock.NewManual outside the clock package must be a test file or
// sit in a test-support package named *test (this one) or a replay package.
func TestNoProductionCodeBuildsTheSimulatedClock(t *testing.T) {
	root := filepath.Join("..", "..")
	var bad []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel := filepath.ToSlash(path)
		if strings.Contains(rel, "platform/clock/") || strings.Contains(rel, "test/") || strings.Contains(rel, "/replay") {
			return nil
		}
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		if _, perr := parser.ParseFile(token.NewFileSet(), path, src, parser.SkipObjectResolution); perr != nil {
			return nil
		}
		if strings.Contains(string(src), "NewManual(") {
			bad = append(bad, rel)
		}
		return nil
	})
	if len(bad) != 0 {
		t.Errorf("production code builds the simulated clock (it applies only in tests and replays): %v", bad)
	}
}
