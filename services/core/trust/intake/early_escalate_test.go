package intake

// A663 (interim, relayed; A581, A590, A574, A577, A578; REQ-TRU-010, REQ-RCV-003; INV-25, INV-21, EC-E16): an ESCALATE reached before the evidence and the policy were evaluated has an
// incomplete basis. The intake records it after the evaluation accepted it, with the turn of the request, so that the Writer can keep it in the null-basis form; a request with no turn leaves
// nothing recorded and ends in UNKNOWN, as before. An ESCALATE with a complete basis carries no turn: it is recorded as it always was. All data is SYNTHETIC.

import (
	"context"
	"testing"

	"github.com/Noemai-org/noema/services/core/trust/decide"
)

const turnSynth = "0b8f3c1e-5d2a-4e7b-9c41-6a1d2f3e4b50"

// escalatedEarly is an evaluation whose T2 ends in ESCALATE over a builder that has read no evidence and no policy.
func escalatedEarly(t *testing.T, f *f13, turn string) Result {
	t.Helper()
	ev := newEval13(t, f)
	ev.bb = freshBuilder(t, false, false)
	ev.req.TurnID = turn
	steps := ev.lateSteps()
	steps[decide.T2Session-1] = func(context.Context) decide.StepResult {
		r, err := ev.resolved()
		if err != nil {
			return decide.FailStep(err)
		}
		return escalateCore(r, "NO_RUNNABLE_CAPABILITY", passed())
	}
	return ev.run(tctx(t), steps)
}

func TestA663AnEarlyEscalateIsRecordedWithTheTurnAndComesBackWithItsDecisionID(t *testing.T) {
	f := newFake13()
	f.rec = RecordResult{DecisionID: "dec_synth0007"}
	res := escalatedEarly(t, f, turnSynth)
	if res.Decision.Outcome() != decide.Escalate || res.DecisionID != "dec_synth0007" || len(res.GrantIDs) != 0 {
		t.Fatalf("%s ID %q grants %v, want the recorded ESCALATE and no grant", res.Decision.Outcome(), res.DecisionID, res.GrantIDs)
	}
	if len(f.recReqs) != 1 {
		t.Fatalf("Record called %d times, want once", len(f.recReqs))
	}
	r := f.recReqs[0]
	if r.TurnID != turnSynth || r.PrincipalID != t13Principal || len(r.Steps) != 0 || r.PlanHash != "" || r.ConfirmationRequest != nil {
		t.Errorf("the record request %+v: want the turn, the principal, no step, no plan and no request", r)
	}
}

func TestA663AnEarlyEscalateWithNoTurnIsNotRecordedAndIsUnknown(t *testing.T) {
	f := newFake13()
	f.rec = RecordResult{DecisionID: "dec_synth0007"}
	res := escalatedEarly(t, f, "")
	if res.Decision.Outcome() != decide.Unknown || res.DecisionID != "" || len(f.recReqs) != 0 {
		t.Errorf("%s ID %q, Record called %d times; want UNKNOWN, no ID and no Record call", res.Decision.Outcome(), res.DecisionID, len(f.recReqs))
	}
}

// An ESCALATE with a complete basis names no turn: the Writer takes a turn only on an incomplete basis.
func TestA663AnEscalateWithACompleteBasisCarriesNoTurn(t *testing.T) {
	f := newFake13()
	f.rec = RecordResult{DecisionID: "dec_synth0007"}
	ev := newEval13(t, f)
	ev.req.TurnID = turnSynth
	steps := ev.lateSteps()
	steps[decide.T12Clocks-1] = func(context.Context) decide.StepResult {
		r, err := ev.resolved()
		if err != nil {
			return decide.FailStep(err)
		}
		return escalateCore(r, "NO_RUNNABLE_CAPABILITY", passed())
	}
	res := ev.run(tctx(t), steps)
	if res.Decision.Outcome() != decide.Escalate || len(f.recReqs) != 1 || f.recReqs[0].TurnID != "" {
		t.Errorf("%s, %d Record calls, turn %q; want the ESCALATE recorded with no turn", res.Decision.Outcome(), len(f.recReqs), f.recReqs[0].TurnID)
	}
}
