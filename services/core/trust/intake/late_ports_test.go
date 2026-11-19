package intake_test

import (
	"context"

	"github.com/Noemai-org/noema/services/core/trust/intake"
)

// lateSpy stands in for the T13 to T15 ports in the T1 to T6 tests: it answers like a request that needs nothing more and records every call, so a test can prove that a
// refusal or an unbuilt step stops the evaluation before these ports are reached.
type lateSpy struct {
	calls []string
	// allow makes the ports answer like a request that has nothing left to refuse: no confirmation needed, a plan with its hash and no step (a read, ruling A205 Q3), and a
	// Decision Record that was written. Without it the Planner refuses, so nothing after T14 runs and nothing is written.
	allow bool
}

func (l *lateSpy) Status(context.Context, intake.AdmittedIntent, intake.FreshBinding) (intake.Confirmation, error) {
	l.calls = append(l.calls, "Confirmations.Status")
	return intake.Confirmation{Status: intake.ConfirmationNotRequired}, nil
}

func (l *lateSpy) Plan(context.Context, intake.PlanRequest) (intake.Plan, error) {
	l.calls = append(l.calls, "Planner.Plan")
	if l.allow {
		return intake.Plan{Hash: "sha256:" + hex64()}, nil
	}
	return intake.Plan{}, intake.ErrPlanInvalid
}

func (l *lateSpy) Record(context.Context, intake.RecordRequest) (intake.RecordResult, error) {
	l.calls = append(l.calls, "Recorder.Record")
	if l.allow {
		return intake.RecordResult{DecisionID: "dec_synth0001"}, nil
	}
	return intake.RecordResult{}, intake.ErrPlanInvalid
}

func (l *lateSpy) Count(context.Context, intake.DispatchKey) (intake.DispatchCount, error) {
	l.calls = append(l.calls, "Dispatches.Count")
	return intake.DispatchCount{Used: 0, Limit: 1}, nil
}
