package orchestrator_test

// STM-02 (REQ-STM-002, BD-10; INV-07, INV-28, AGENT-07, AGENT-21; PERD 28.2): a pending or an unknown thing is never turned into a success or a failure by a mapping, and the
// investigation's states and the action's states are two vocabularies that never meet. The write runner is driven with random orders of receipts, verdicts and decisions through the
// real Machine and the real table; every double answers a contract value and none fakes a fault. All identifiers are SYNTHETIC.

import (
	"context"
	"errors"
	"testing"

	"pgregory.net/rapid"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/orchestrator"
	"github.com/Noemai-org/noema/services/core/orchestrator/publicstage"
)

// The states that stand for something not yet known or not yet finished. They are the states of the action (a dispatch, a receipt, a verification, a watch) and the one state of an
// unknown decision. Each one stays what it is until a named fact moves it.
var pendingStates = []orchestrator.State{"EXECUTING", "AWAITING_VERIFICATION", "AWAITING_EXTERNAL_OUTCOME", "RECOVERING", "BLOCKED_UNKNOWN"}

// The states that say a question is closed: a success, a refusal, an expiry, or a handoff that ends Noema's part.
var closedStates = []orchestrator.State{"COMPLETED", "REJECTED", "EXPIRED", "ESCALATED"}

var allVerdicts = []contracts.EnumsV1Verdict{
	contracts.EnumsV1VerdictACHIEVED, contracts.EnumsV1VerdictNOTACHIEVED, contracts.EnumsV1VerdictPARTIALLYACHIEVED,
	contracts.EnumsV1VerdictPENDINGEXTERNAL, contracts.EnumsV1VerdictUNKNOWN, contracts.EnumsV1VerdictCOMPENSATED,
}

var allReceipts = []orchestrator.ReceiptClass{orchestrator.ReceiptApplied, orchestrator.ReceiptUnknown, orchestrator.ReceiptVoided, orchestrator.ReceiptBudgetSpent}

var nonAllowOutcomes = []contracts.EnumsV1DecisionOutcome{
	contracts.EnumsV1DecisionOutcomeCLARIFY, contracts.EnumsV1DecisionOutcomeCONFIRM, contracts.EnumsV1DecisionOutcomeWAIT,
	contracts.EnumsV1DecisionOutcomeESCALATE, contracts.EnumsV1DecisionOutcomeREJECT, contracts.EnumsV1DecisionOutcomeUNKNOWN,
}

func inSet(s orchestrator.State, set []orchestrator.State) bool {
	for _, x := range set {
		if x == s {
			return true
		}
	}
	return false
}

// REQ-STM-002: the vocabulary of the investigation and the vocabulary of the action never share a word, so no mapping can pass one for the other.
func TestInvestigationStatesAndActionStatesAreSeparateVocabularies(t *testing.T) {
	tab, err := orchestrator.LoadTable()
	if err != nil {
		t.Fatal(err)
	}
	action := map[string]string{}
	for _, v := range allVerdicts {
		action[string(v)] = "verdict"
	}
	for _, r := range allReceipts {
		action[string(r)] = "receipt class"
	}
	for _, o := range append([]contracts.EnumsV1DecisionOutcome{contracts.EnumsV1DecisionOutcomeALLOW}, nonAllowOutcomes...) {
		action[string(o)] = "decision outcome"
	}
	for _, s := range tab.States() {
		if kind, clash := action[string(s)]; clash {
			t.Errorf("the investigation state %s is also a %s: the two vocabularies must not meet", s, kind)
		}
	}
}

// REQ-STM-002: no pending or unknown state is shown as an answer. The public stage of each is working, needs input or unavailable, never answered.
func TestNoPendingStateIsShownAsAnswered(t *testing.T) {
	for _, s := range pendingStates {
		st, err := publicstage.Of(s)
		if err != nil {
			t.Fatalf("%s: %v", s, err)
		}
		if st == contracts.InvestigationResponseV1PublicStageAnswered {
			t.Errorf("%s is shown as answered", s)
		}
	}
	distinct := map[contracts.InvestigationResponseV1PublicStage][]orchestrator.State{}
	for _, s := range []orchestrator.State{"AWAITING_EXTERNAL_OUTCOME", "BLOCKED_UNKNOWN", "COMPLETED"} {
		st, _ := publicstage.Of(s)
		distinct[st] = append(distinct[st], s)
	}
	if len(distinct) != 3 {
		t.Errorf("a watched outcome, an unknown decision and a completed investigation must have three different public stages, got %v", distinct)
	}
}

type script struct {
	decisions []orchestrator.Decision
	receipts  []orchestrator.ReceiptClass
	verdicts  []contracts.EnumsV1Verdict
	blocked   bool
}

func drawScript(t *rapid.T) script {
	var sc script
	n := rapid.IntRange(1, 4).Draw(t, "decisions")
	for i := 0; i < n; i++ {
		grant := "grt_S" + string(rune('A'+i)) + "A1"
		switch rapid.IntRange(0, 9).Draw(t, "kind") {
		case 0:
			sc.decisions = append(sc.decisions, orchestrator.Decision{ID: "dec_AA11", Outcome: contracts.EnumsV1DecisionOutcomeALLOW, Read: true, IntentRef: "int_AA11"})
		case 1:
			o := rapid.SampledFrom(nonAllowOutcomes).Draw(t, "outcome")
			sc.decisions = append(sc.decisions, orchestrator.Decision{ID: "dec_AA11", Outcome: o, IntentRef: "int_AA11"})
		default:
			sc.decisions = append(sc.decisions, writeDecision(grant))
		}
	}
	m := rapid.IntRange(1, 4).Draw(t, "rounds")
	for i := 0; i < m; i++ {
		sc.receipts = append(sc.receipts, rapid.SampledFrom(allReceipts).Draw(t, "receipt"))
		sc.verdicts = append(sc.verdicts, rapid.SampledFrom(allVerdicts).Draw(t, "verdict"))
	}
	sc.blocked = rapid.Bool().Draw(t, "blocked")
	return sc
}

// REQ-STM-002, INV-07, AGENT-21: over random orders of decisions, receipts and verdicts, a run is closed as COMPLETED only after EXPLAINING, and a write is explained only after an
// ACHIEVED verdict; an unknown receipt always goes to verification and never to a close; PENDING_EXTERNAL ends the run in AWAITING_EXTERNAL_OUTCOME; UNKNOWN and COMPENSATED end it in
// RECOVERING with nothing dispatched afterwards. A pending state is never the cause of a close.
func TestNoRandomOrderOfReceiptsAndVerdictsCollapsesAPendingOrUnknownStateIntoAClosedOne(t *testing.T) {
	outer := t
	reached := map[string]int{} // vacuity guard: what the random runs actually covered
	rapid.Check(t, func(t *rapid.T) {
		sc := drawScript(t)
		w := newWfakes()
		w.expl = orchestrator.Explanation{Text: "Done.", Blocked: sc.blocked}
		w.decisions = sc.decisions
		w.receipts = nil
		for _, c := range sc.receipts {
			w.receipts = append(w.receipts, orchestrator.Receipt{Class: c})
		}
		w.verdicts = sc.verdicts
		res, err := newWrite(outer, w).RunWrite(context.Background(), sess, turn)
		trail := states(res.Changes)
		var seenVerdicts []contracts.EnumsV1Verdict
		for i := 0; i < w.nVerify; i++ {
			seenVerdicts = append(seenVerdicts, pick(sc.verdicts, i))
		}
		for i := 1; i < len(trail); i++ {
			prev, cur := trail[i-1], trail[i]
			if inSet(prev, pendingStates) && inSet(cur, closedStates) && !(prev == "BLOCKED_UNKNOWN" && cur == "ESCALATED") && !(prev == "AWAITING_EXTERNAL_OUTCOME" && cur == "COMPLETED") {
				t.Fatalf("trail %v: %s was followed by %s", trail, prev, cur)
			}
			if prev == "EXECUTING" && cur != "AWAITING_VERIFICATION" {
				t.Fatalf("trail %v: a dispatched write must wait for verification, got %s", trail, cur)
			}
			if cur == "COMPLETED" && prev != "EXPLAINING" {
				t.Fatalf("trail %v: COMPLETED without EXPLAINING before it", trail)
			}
			if cur == "EXPLAINING" && prev == "AWAITING_VERIFICATION" && (len(seenVerdicts) == 0 || seenVerdicts[len(seenVerdicts)-1] != contracts.EnumsV1VerdictACHIEVED) {
				t.Fatalf("trail %v: a write was explained after verdicts %v", trail, seenVerdicts)
			}
		}
		final := res.Investigation.State
		if verified := w.nExec - countVoided(w); verified != w.nVerify {
			t.Fatalf("%d dispatched writes (receipts that are not voided) and %d verifications: a receipt of any class, an unknown one included, is always verified (trail %v)", verified, w.nVerify, trail)
		}
		if err != nil && !errors.Is(err, orchestrator.ErrOutcomeUnknown) && !errors.Is(err, orchestrator.ErrCompensated) && !errors.Is(err, orchestrator.ErrRecoveryBudgetSpent) {
			t.Fatalf("a scripted run failed for a reason that is no outcome of the script: %v (trail %v)", err, trail)
		}
		reached["final "+string(final)]++
		for _, v := range seenVerdicts {
			reached["verdict "+string(v)]++
		}
		if len(seenVerdicts) > 0 && w.nExec > 0 {
			last := seenVerdicts[len(seenVerdicts)-1]
			switch {
			case final == "AWAITING_EXTERNAL_OUTCOME" && last != contracts.EnumsV1VerdictPENDINGEXTERNAL:
				t.Fatalf("final %s after the last verdict %s", final, last)
			case last == contracts.EnumsV1VerdictPENDINGEXTERNAL && containsState(trail, "AWAITING_VERIFICATION") && final != "AWAITING_EXTERNAL_OUTCOME" && err == nil:
				t.Fatalf("PENDING_EXTERNAL ended in %s, want AWAITING_EXTERNAL_OUTCOME", final)
			case (last == contracts.EnumsV1VerdictUNKNOWN || last == contracts.EnumsV1VerdictCOMPENSATED) && final != "RECOVERING":
				t.Fatalf("the last verdict %s ended in %s, want RECOVERING", last, final)
			case last == contracts.EnumsV1VerdictUNKNOWN && !errors.Is(err, orchestrator.ErrOutcomeUnknown):
				t.Fatalf("UNKNOWN ended with %v, want ErrOutcomeUnknown", err)
			case last == contracts.EnumsV1VerdictCOMPENSATED && !errors.Is(err, orchestrator.ErrCompensated):
				t.Fatalf("COMPENSATED ended with %v, want ErrCompensated", err)
			}
			// A close after a verdict that is not ACHIEVED is possible only through a fresh evaluation whose decision is a read: Trust decided again, the runner did not.
			if final == "COMPLETED" && last != contracts.EnumsV1VerdictACHIEVED {
				if d := pick(sc.decisions, w.nDecide-1); d.Outcome != contracts.EnumsV1DecisionOutcomeALLOW || !d.Read {
					t.Fatalf("COMPLETED after the last verdict %s and a last decision %+v that is not a read", last, d)
				}
			}
		}
		if (final == "RECOVERING" || final == "AWAITING_EXTERNAL_OUTCOME") && count(w.calls, "execute") > w.nVerify+countVoided(w) {
			t.Fatalf("a write was dispatched again after it was left pending: calls %v", w.calls)
		}
	})
	for _, k := range []string{"verdict ACHIEVED", "verdict NOT_ACHIEVED", "verdict PARTIALLY_ACHIEVED", "verdict PENDING_EXTERNAL", "verdict UNKNOWN", "verdict COMPENSATED",
		"final COMPLETED", "final AWAITING_EXTERNAL_OUTCOME", "final RECOVERING"} {
		if reached[k] == 0 {
			t.Errorf("the random runs never reached %q: the property would pass without testing it (%v)", k, reached)
		}
	}
}

func containsState(trail []orchestrator.State, s orchestrator.State) bool {
	return inSet(s, trail)
}

func countVoided(w *wfakes) int {
	n := 0
	for i := 0; i < w.nExec; i++ {
		if c := pick(w.receipts, i).Class; c == orchestrator.ReceiptVoided || c == orchestrator.ReceiptBudgetSpent {
			n++
		}
	}
	return n
}
