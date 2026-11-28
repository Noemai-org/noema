package recorder_test

// A534 (interim, relayed, not yet committed; REQ-TRU-010, INV-25): the disposition rule applies only to a decision whose steps include a capability of the injected escalation set. A write
// outside the escalation plan (the complaint write, `log_complaint_reference`, a node of another graph) is an ALLOW with a grant and no disposition, and is accepted. A decision with a step of
// the escalation plan and no disposition is refused; a decision with a disposition and a step outside the plan is refused (a disposition authorises the plan's steps and nothing else). The
// set is the pack's, read as data. All data is SYNTHETIC.

import (
	"errors"
	"testing"

	"github.com/Noemai-org/noema/services/core/trust/decide"
	"github.com/Noemai-org/noema/services/core/trust/recorder"
)

// The complaint write is the case of the Founder's order: an ALLOW with a grant and no disposition is accepted, the grant is issued and no disposition is stored; the same for another
// write the pack does not put in the escalation plan.
func TestAComplaintWriteWithAGrantAndNoDispositionIsAccepted(t *testing.T) {
	w := escalationWorld(t)
	for _, c := range []string{capComplaint, capOther} {
		res, err := w.writeAndCommit(t, w.action(t, decide.Allow, "", c))
		if err != nil || len(res.Grants) != 1 {
			t.Fatalf("%s with no disposition: %+v %v; want an ALLOW with one grant", c, res, err)
		}
		if _, linked := w.storedDisposition(t, string(res.Record.DecisionId)); linked {
			t.Errorf("%s: a disposition is stored on a decision that named none", c)
		}
	}
}

// One step of the escalation plan is enough to need the disposition, whatever else the decision carries; with the disposition, the step outside the plan is refused.
func TestADecisionWithAStepOfTheEscalationPlanNeedsItsDispositionWhateverElseItCarries(t *testing.T) {
	w := escalationWorld(t)
	disp := w.disposition(t, 1)
	before := w.counts(t)
	if _, err := w.writeAndCommit(t, w.action(t, decide.Allow, "", capComplaint, capability)); !errors.Is(err, recorder.ErrDispositionRequired) {
		t.Errorf("an escalation step among others, no disposition: %v, want ErrDispositionRequired", err)
	}
	if _, err := w.writeAndCommit(t, w.action(t, decide.Allow, disp, capComplaint, capability)); !errors.Is(err, recorder.ErrStepOutsideEscalation) {
		t.Errorf("a complaint write beside an escalation step, with the disposition: %v, want ErrStepOutsideEscalation", err)
	}
	if after := w.counts(t); after != before {
		t.Errorf("the refusals changed the rows from %+v to %+v", before, after)
	}
}

// The set is the one the writer is given: with the complaint write put in the escalation set, it needs a disposition; with the empty set (the zero value) no write does.
func TestTheRescopeFollowsTheInjectedSetAndTheEmptySetNeedsNone(t *testing.T) {
	w := escalationWorld(t)
	w.w.Escalation = recorder.NewEscalationSet(capComplaint)
	if _, err := w.writeAndCommit(t, w.action(t, decide.Allow, "", capComplaint)); !errors.Is(err, recorder.ErrDispositionRequired) {
		t.Errorf("a capability of the injected set, no disposition: %v, want ErrDispositionRequired", err)
	}
	w.w.Escalation = recorder.EscalationSet{}
	if res, err := w.writeAndCommit(t, w.action(t, decide.Allow, "", capability)); err != nil || len(res.Grants) != 1 {
		t.Errorf("with the empty set nothing is an escalation step: %+v %v; want an ALLOW with one grant", res, err)
	}
}
