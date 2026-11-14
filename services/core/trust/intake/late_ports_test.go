package intake_test

import (
	"context"

	"github.com/Noemai-org/noema/services/core/trust/intake"
)

// lateSpy stands in for the T13 to T15 ports in the T1 to T6 tests: it answers like a request that needs nothing more and records every call, so a test can prove that a
// refusal or an unbuilt step stops the evaluation before these ports are reached.
type lateSpy struct{ calls []string }

func (l *lateSpy) Status(context.Context, intake.AdmittedIntent) (intake.Confirmation, error) {
	l.calls = append(l.calls, "Confirmations.Status")
	return intake.Confirmation{Status: intake.ConfirmationNotRequired}, nil
}

func (l *lateSpy) Plan(context.Context, intake.PlanRequest) (intake.Plan, error) {
	l.calls = append(l.calls, "Planner.Plan")
	return intake.Plan{}, intake.ErrPlanInvalid
}

func (l *lateSpy) Record(context.Context, intake.RecordRequest) (intake.RecordResult, error) {
	l.calls = append(l.calls, "Recorder.Record")
	return intake.RecordResult{}, intake.ErrPlanInvalid
}
