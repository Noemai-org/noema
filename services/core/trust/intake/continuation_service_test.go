package intake_test

// TRU-14 (REQ-TRU-002; INV-16, INV-03): the continuation entry on the service. It refuses what it cannot decide as an error or as a decision, and never reaches the
// ports of T13 to T15 for it. Every value is SYNTHETIC.

import (
	"context"
	"errors"
	"testing"

	"github.com/Noemai-org/noema/services/core/trust/decide"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

type runnableStub struct{ asked int }

func (r *runnableStub) Runnable(context.Context, intake.RunnableQuery) ([]string, error) {
	r.asked++
	return []string{"fin.cap.route_to_internal_owner"}, nil
}

func stepRequest(r intake.Runnable) intake.ContinuationRequest {
	return intake.ContinuationRequest{Request: request(), CapabilityID: "fin.cap.route_to_internal_owner", Facts: map[string]string{"case_id": "cas_synth0001"}, Runnable: r}
}

func TestEvaluateStepNeedsADeadlineAndAConfiguredService(t *testing.T) {
	s := newSpy()
	if _, err := s.service().EvaluateStep(context.Background(), stepRequest(&runnableStub{})); !errors.Is(err, intake.ErrNoDeadline) {
		t.Errorf("no deadline: %v, want ErrNoDeadline", err)
	}
	if _, err := (&intake.Service{}).EvaluateStep(ctx(t), stepRequest(&runnableStub{})); !errors.Is(err, intake.ErrConfig) {
		t.Errorf("zero service: %v, want ErrConfig", err)
	}
}

func TestEvaluateStepNeedsTheCapabilityAndTheRunnablePort(t *testing.T) {
	for name, mut := range map[string]func(*intake.ContinuationRequest){
		"no capability":    func(r *intake.ContinuationRequest) { r.CapabilityID = "" },
		"no runnable port": func(r *intake.ContinuationRequest) { r.Runnable = nil },
	} {
		s := newSpy()
		r := stepRequest(&runnableStub{})
		mut(&r)
		if _, err := s.service().EvaluateStep(ctx(t), r); !errors.Is(err, intake.ErrStepInvalid) {
			t.Errorf("%s: %v, want ErrStepInvalid", name, err)
		}
		if len(s.calls) != 0 {
			t.Errorf("%s: a port was reached: %v", name, s.calls)
		}
	}
}

// A malformed intent reference is REJECT INTENT_INVALID at T1 and reads nothing, as for Evaluate.
func TestEvaluateStepWithAMalformedIntentRefIsRejectedAtT1(t *testing.T) {
	s := newSpy()
	r := stepRequest(&runnableStub{})
	r.Request.IntentRef = "not-an-intent-ref"
	res, err := s.service().EvaluateStep(ctx(t), r)
	if err != nil || res.Decision.Outcome() != decide.Reject || len(s.calls) != 0 {
		t.Errorf("%v %s, calls %v; want REJECT and no port reached", err, res.Decision.Outcome(), s.calls)
	}
}

// A continuation is for a write only: an intent that leads to none is UNKNOWN at T3 (fail closed), the runnable port is never asked and nothing is planned or recorded.
func TestEvaluateStepForAnIntentThatLeadsToNoWriteFailsClosedAtT3(t *testing.T) {
	s := newSpy()
	s.needsWrite = false
	run := &runnableStub{}
	res, err := s.service().EvaluateStep(ctx(t), stepRequest(run))
	if err != nil {
		t.Fatal(err)
	}
	if res.Decision.Outcome() != decide.Unknown || res.DecisionID != "" || len(res.GrantIDs) != 0 {
		t.Errorf("%s %q %v, want UNKNOWN and no IDs", res.Decision.Outcome(), res.DecisionID, res.GrantIDs)
	}
	last := res.Trace[len(res.Trace)-1]
	if last.Step != decide.T3WritesEnabled || last.Result != decide.StepFailed {
		t.Errorf("trace ends %v, want T3 FAILED", last)
	}
	if run.asked != 0 || len(s.late.calls) != 0 {
		t.Errorf("the runnable port was asked %d times, late ports %v", run.asked, s.late.calls)
	}
}
