package intake

// A680 (interim, relayed; A574, A581, A590; REQ-TRU-002, REQ-TRU-007; INV-16, INV-21, INV-25; EC-E16): a refusal at T1 to T9 is a REJECT reached before the evidence and the policy were
// evaluated. When the request names a well-formed turn, the evaluation records it, through the Recorder, before it is returned, in the null-basis form keyed by the turn, and the
// result carries the decision ID. A request that names no turn is refused as before and records nothing. A Record that fails ends in UNKNOWN INTERNAL_ERROR, never a REJECT nobody
// recorded. A REJECT with a complete basis (T11) and the declined approval of T13 are not this path. All data is SYNTHETIC.

import (
	"context"
	"errors"
	"testing"

	"github.com/Noemai-org/noema/services/core/trust/decide"
)

// refusedAtT4 is an evaluation whose T4 refuses (a prohibited capability) over a builder that has read no evidence and no policy.
func refusedAtT4(t *testing.T, f *f13, turn string) Result {
	t.Helper()
	ev := newEval13(t, f)
	ev.bb = freshBuilder(t, false, false)
	ev.req.TurnID = turn
	steps := ev.lateSteps()
	steps[decide.T4Capability-1] = func(context.Context) decide.StepResult {
		d, err := decide.FailAtGate(decide.T4Capability, ev.version)
		if err != nil {
			return decide.FailStep(err)
		}
		return decide.Stop(d)
	}
	return ev.run(tctx(t), steps)
}

func TestA680AnEarlyRefusalIsRecordedWithTheTurnAndComesBackWithItsDecisionID(t *testing.T) {
	f := newFake13()
	f.rec = RecordResult{DecisionID: "dec_synth0008"}
	res := refusedAtT4(t, f, turnSynth)
	if res.Decision.Outcome() != decide.Reject || res.DecisionID != "dec_synth0008" || len(res.GrantIDs) != 0 {
		t.Fatalf("%s ID %q grants %v, want the recorded REJECT and no grant", res.Decision.Outcome(), res.DecisionID, res.GrantIDs)
	}
	if len(f.recReqs) != 1 {
		t.Fatalf("Record called %d times, want once", len(f.recReqs))
	}
	r := f.recReqs[0]
	if r.Decision.Outcome() != decide.Reject || r.TurnID != turnSynth || r.PrincipalID != t13Principal || len(r.Steps) != 0 || r.PlanHash != "" || r.ConfirmationRequest != nil {
		t.Errorf("the record request %+v: want a REJECT with the turn, the principal, no step, no plan and no request", r)
	}
}

func TestA680AnEarlyRefusalWithNoTurnIsNotRecordedAndStaysARejection(t *testing.T) {
	f := newFake13()
	f.rec = RecordResult{DecisionID: "dec_synth0008"}
	res := refusedAtT4(t, f, "")
	if res.Decision.Outcome() != decide.Reject || res.DecisionID != "" || len(f.recReqs) != 0 {
		t.Errorf("%s ID %q, Record called %d times; want REJECT, no ID and no Record call", res.Decision.Outcome(), res.DecisionID, len(f.recReqs))
	}
}

func TestA680ARefusalThatCannotBeRecordedIsUnknownNeverARejectionNobodyRecorded(t *testing.T) {
	f := newFake13()
	f.recErr = errors.New("synthetic: the Record is unavailable")
	res := refusedAtT4(t, f, turnSynth)
	if res.Decision.Outcome() != decide.Unknown || res.DecisionID != "" {
		t.Errorf("%s ID %q, want UNKNOWN and no ID", res.Decision.Outcome(), res.DecisionID)
	}
}
