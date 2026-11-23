package recorder_test

// TRU-10 / US-06 (A503, A507; BO needs it): the DECISION_MADE event of a decision that names a disposition names it too (disposition_decision_id), so the Record, and the
// reconstruction built from it alone, show which ESCALATE disposition each escalation action decision links, without the decision table. A decision that names none carries no such
// member. REQ-TRU-010, REQ-AUD-003; INV-25. All data is SYNTHETIC.

import (
	"testing"

	"github.com/Noemai-org/noema/services/core/trust/decide"
)

func TestADecisionThatNamesADispositionNamesItInItsDecisionMadeEvent(t *testing.T) {
	w := escalationWorld(t)
	disp := w.disposition(t, 1)
	res, err := w.writeAndCommit(t, w.action(t, decide.Allow, disp, packEscalationCapabilities(t)[0]))
	if err != nil {
		t.Fatal(err)
	}
	var linked, unlinked int
	for _, e := range w.eventsOf(t, "DECISION_MADE") {
		p := payloadOf(t, e)
		switch p["decision_id"] {
		case string(res.Record.DecisionId):
			if p["disposition_decision_id"] != disp {
				t.Errorf("the action decision's DECISION_MADE names the disposition %v, want %s", p["disposition_decision_id"], disp)
			}
			linked++
		case disp:
			if _, ok := p["disposition_decision_id"]; ok {
				t.Errorf("the disposition's own DECISION_MADE names a disposition: %v", p["disposition_decision_id"])
			}
			unlinked++
		}
	}
	if linked != 1 || unlinked != 1 {
		t.Fatalf("%d action and %d disposition DECISION_MADE events, want one each", linked, unlinked)
	}
}

func TestADecisionThatNamesNoDispositionHasNoSuchMemberInItsDecisionMadeEvent(t *testing.T) {
	w := newWorld(t)
	if _, err := w.writeAndCommit(t, w.action(t, decide.Allow, "")); err != nil {
		t.Fatal(err)
	}
	for _, e := range w.eventsOf(t, "DECISION_MADE") {
		if _, ok := payloadOf(t, e)["disposition_decision_id"]; ok {
			t.Fatalf("a decision that names no disposition carries disposition_decision_id in %v", payloadOf(t, e))
		}
	}
}
