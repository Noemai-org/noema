package recorder_test

// TRU-10, ruling A503 and the Founder's order of 22 Nov 2026 (interim): steps if and only if a disposition. A decision with steps names its ESCALATE disposition (ErrDispositionRequired);
// a decision with no step names none (ErrDispositionWithoutSteps): the link belongs to an escalation action decision, which has steps, and the disposition decision itself has no step and
// no disposition. The refusal comes before anything is written. REQ-TRU-010; INV-25, EC-E16. All data is SYNTHETIC.

import (
	"errors"
	"testing"

	"github.com/Noemai-org/noema/services/core/trust/decide"
	"github.com/Noemai-org/noema/services/core/trust/recorder"
)

func TestADecisionWithNoStepsThatNamesADispositionIsRefusedAndNothingIsWritten(t *testing.T) {
	w := escalationWorld(t)
	disp := w.disposition(t, 1)
	before := w.counts(t)
	for name, outcome := range map[string]decide.Outcome{"a read ALLOW": decide.Allow, "a REJECT": decide.Reject, "a WAIT": decide.Wait} {
		t.Run(name, func(t *testing.T) {
			if _, err := w.writeAndCommit(t, w.action(t, outcome, disp)); !errors.Is(err, recorder.ErrDispositionWithoutSteps) {
				t.Fatalf("err %v, want ErrDispositionWithoutSteps", err)
			}
			if after := w.counts(t); after != before {
				t.Fatalf("the refusal changed the rows from %+v to %+v", before, after)
			}
		})
	}
}

func TestTheDispositionDecisionItselfHasNoStepAndNoDispositionAndIsWritten(t *testing.T) {
	w := escalationWorld(t)
	if disp := w.disposition(t, 1); disp == "" {
		t.Fatal("the disposition was not written")
	}
}
