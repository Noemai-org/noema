package orchestrator_test

// STM-02 (REQ-STM-002, BD-10): a pending or unknown outcome is never collapsed into a terminal state. Written before any change to the runner, as the property the
// card asks for: random orders of verdicts, receipts and rounds go through RunWrite and the Record of moves is read back. Every identifier is SYNTHETIC.

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"pgregory.net/rapid"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/orchestrator"
)

var terminal = map[orchestrator.State]bool{"COMPLETED": true, "REJECTED": true, "ESCALATED": true, "EXPIRED": true}

// REQ-STM-002: the only verdict that may end in COMPLETED is ACHIEVED; PENDING_EXTERNAL stays AWAITING_EXTERNAL_OUTCOME; UNKNOWN and COMPENSATED stay RECOVERING;
// no run passes through a terminal state on the way to a pending or unknown one; and a verdict that is not ACHIEVED never reaches the explanation.
func TestNoOrderOfVerdictsCollapsesAPendingOrUnknownOutcomeIntoATerminalState(tt *testing.T) {
	retry := []contracts.EnumsV1Verdict{contracts.EnumsV1VerdictNOTACHIEVED, contracts.EnumsV1VerdictPARTIALLYACHIEVED}
	last := []contracts.EnumsV1Verdict{contracts.EnumsV1VerdictACHIEVED, contracts.EnumsV1VerdictPENDINGEXTERNAL, contracts.EnumsV1VerdictUNKNOWN, contracts.EnumsV1VerdictCOMPENSATED}
	rapid.Check(tt, func(t *rapid.T) {
		// One retry at most: PERDMaxDispatchesPerKey is 2, and a second NOT_ACHIEVED ends in EVALUATING with the budget spent, which is a stop and not a collapse.
		rounds := rapid.IntRange(0, int(orchestrator.PERDMaxDispatchesPerKey)-1).Draw(t, "retries")
		w := newWfakes()
		w.decisions, w.receipts, w.verdicts = nil, nil, nil
		for i := 0; i < rounds; i++ {
			w.verdicts = append(w.verdicts, rapid.SampledFrom(retry).Draw(t, "retry verdict"))
		}
		final := rapid.SampledFrom(last).Draw(t, "final verdict")
		w.verdicts = append(w.verdicts, final)
		for i := range w.verdicts {
			g := fmt.Sprintf("grt_%02dAA", i)
			w.decisions = append(w.decisions, writeDecision(g))
			w.receipts = append(w.receipts, orchestrator.Receipt{GrantID: g, Class: rapid.SampledFrom([]orchestrator.ReceiptClass{orchestrator.ReceiptApplied, orchestrator.ReceiptUnknown}).Draw(t, "receipt")})
		}
		res, err := newWrite(tt, w).RunWrite(context.Background(), sess, turn)
		got := res.Investigation.State
		trail := states(w.changes)
		switch final {
		case contracts.EnumsV1VerdictACHIEVED:
			if got != "COMPLETED" || err != nil {
				t.Fatalf("ACHIEVED ended in %s (err %v), trail %v", got, err, trail)
			}
		case contracts.EnumsV1VerdictPENDINGEXTERNAL:
			if got != "AWAITING_EXTERNAL_OUTCOME" || err != nil {
				t.Fatalf("PENDING_EXTERNAL ended in %s (err %v), trail %v", got, err, trail)
			}
		default:
			if got != "RECOVERING" || !(errors.Is(err, orchestrator.ErrOutcomeUnknown) || errors.Is(err, orchestrator.ErrCompensated)) {
				t.Fatalf("%s ended in %s (err %v), trail %v", final, got, err, trail)
			}
		}
		if final != contracts.EnumsV1VerdictACHIEVED {
			for _, s := range trail {
				if terminal[s] || s == "EXPLAINING" {
					t.Fatalf("verdict %s reached %s on the way, trail %v", final, s, trail)
				}
			}
			if n := count(w.calls, "explain"); n != 0 {
				t.Fatalf("verdict %s asked for the explanation %d times", final, n)
			}
		}
	})
}

// The table itself: the only arrows from a pending or unknown state into a terminal one are the listed ones, each on a verified ACHIEVED or a handoff, never on
// a timeout, silence or an unknown (REQ-STM-002).
func TestTheTableLeavesAPendingOrUnknownStateOnlyByTheListedArrows(t *testing.T) {
	tab, err := orchestrator.LoadTable()
	if err != nil {
		t.Fatal(err)
	}
	pending := []orchestrator.State{"AWAITING_EXTERNAL_OUTCOME", "BLOCKED_UNKNOWN", "RECOVERING"}
	allowed := map[orchestrator.Transition]bool{
		{From: "AWAITING_EXTERNAL_OUTCOME", To: "COMPLETED"}: true, // on ACHIEVED
		{From: "BLOCKED_UNKNOWN", To: "ESCALATED"}:           true, // a handoff, not a result
	}
	for _, p := range pending {
		for s := range terminal {
			if tab.Allowed(p, s) && !allowed[orchestrator.Transition{From: p, To: s}] {
				t.Errorf("%s to %s is allowed and is not on the list of arrows that carry a verified result or a handoff", p, s)
			}
		}
	}
}
