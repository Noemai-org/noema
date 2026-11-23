package intake

// A531 (interim, relayed, not yet committed; REQ-TRU-004, REQ-RCV-003; INV-03, INV-16): when no authorized capability is runnable now, the first evaluation of a write ends at T12, where
// an ESCALATE may end it, with the explicit result NO_RUNNABLE_CAPABILITY, and never with PLAN_INVALID or a WAIT. The disposition stays ESCALATE when the pack routes the reason to a
// handoff (the orchestrator raises the ops alert on the reason, so Trust has no alert port); it is UNKNOWN when no handoff is routed, when the port is not set or when it fails; and it
// is never a grant. The T14 PLAN_INVALID of noema#896 stays as the backstop for a plan request that reaches it with an empty set. All data is SYNTHETIC.

import (
	"context"
	"testing"

	"github.com/Noemai-org/noema/services/core/trust/decide"
)

const noRunnableCode = "NO_RUNNABLE_CAPABILITY"

type handoffFake struct {
	routed bool
	err    error
	asked  []HandoffQuery
	panic  bool
}

func (h *handoffFake) Routed(_ context.Context, q HandoffQuery) (bool, error) {
	h.asked = append(h.asked, q)
	if h.panic {
		panic("injected panic in Handoffs.Routed")
	}
	return h.routed, h.err
}

// noRunnableEv is a write at T12 whose authorized set is the intent's own capability; the ports are the ones given (an interface, so that none stays none).
func noRunnableEv(t *testing.T, runnable Runnable, handoffs Handoffs) (*fake, *evaluation) {
	t.Helper()
	f := &fake{dispatch: DispatchCount{Used: 0, Limit: 1}, authorized: []string{tWriteCap}}
	ev := newEv(t, f).withCapability(tWriteCap)
	ev.intent.TargetRef, ev.intent.CorrelationID = "txn_synth0001", "corr_synth0001"
	ev.late.needsWrite = true
	ev.snapshotID = tSnapshot
	ev.svc.Runnable, ev.svc.Handoffs = runnable, handoffs
	return f, ev
}

// With a routed handoff the evaluation ends at T12 in ESCALATE NO_RUNNABLE_CAPABILITY, all three layers passed; the handoff was asked once for this reason and node; the ledger is not asked
// (nothing can be dispatched), and no grant exists to be issued.
func TestA531NothingRunnableWithAHandoffEscalatesWithTheExplicitReason(t *testing.T) {
	for name, ids := range map[string][]string{"none runnable": nil, "only a capability that is not authorized": {"fin.cap.not_authorized"}} {
		h := &handoffFake{routed: true}
		f, ev := noRunnableEv(t, &runFake{ids: ids}, h)
		d, trace := run(t, ev, decide.T12Clocks)
		stopped(t, name, d, trace, decide.T12Clocks, decide.Escalate, []string{noRunnableCode}, escalateAtT12)
		if len(h.asked) != 1 || h.asked[0] != (HandoffQuery{TenantID: tTenant, IntentNode: tNode, ReasonCode: noRunnableCode}) {
			t.Errorf("%s: the handoff was asked %+v, want once for %s on the intent's node", name, h.asked, noRunnableCode)
		}
		if len(f.gotDispatch) != 0 {
			t.Errorf("%s: the ledger was asked %+v after an escalation with nothing to dispatch", name, f.gotDispatch)
		}
	}
}

// No handoff, no port, a failing port and a panicking port are UNKNOWN: never an ESCALATE nobody can take, never a pass.
func TestA531NothingRunnableWithNoHandoffIsUnknown(t *testing.T) {
	cases := map[string]func() (Handoffs, *handoffFake){
		"the pack routes no handoff": func() (Handoffs, *handoffFake) { h := &handoffFake{routed: false}; return h, h },
		"the port fails":             func() (Handoffs, *handoffFake) { h := &handoffFake{routed: true, err: errInjected}; return h, h },
		"the port panics":            func() (Handoffs, *handoffFake) { h := &handoffFake{panic: true}; return h, h },
		"no port":                    func() (Handoffs, *handoffFake) { return nil, nil },
	}
	for name, mk := range cases {
		h, _ := mk()
		_, ev := noRunnableEv(t, &runFake{}, h)
		d, trace := run(t, ev, decide.T12Clocks)
		failedClosed(t, name, d, trace, decide.T12Clocks)
	}
}

// Something runnable and authorized goes on without asking the handoff; a continuation and a read never ask the Runnable port here at all.
func TestA531ARunnableCapabilityGoesOnAndNothingIsAskedOfTheHandoff(t *testing.T) {
	h := &handoffFake{routed: true}
	_, ev := noRunnableEv(t, &runFake{ids: []string{tWriteCap}}, h)
	d, trace := run(t, ev, decide.T12Clocks)
	if d.Outcome() == decide.Escalate || len(h.asked) != 0 || trace[len(trace)-1].Step == decide.T12Clocks && trace[len(trace)-1].Result == decide.StepFailed {
		t.Errorf("a runnable capability: %s %v, handoff asked %d times; want the evaluation to go on and no ask", d.Outcome(), reasonList(d), len(h.asked))
	}

	r := &runFake{}
	h = &handoffFake{routed: true}
	_, ev = noRunnableEv(t, r, h)
	ev.late.needsWrite = false
	if d, _ := run(t, ev, decide.T12Clocks); d.Outcome() == decide.Escalate || len(r.asked) != 0 || len(h.asked) != 0 {
		t.Errorf("a read: %s, Runnable asked %d times, handoff %d times; want neither", d.Outcome(), len(r.asked), len(h.asked))
	}
}

// The reason is a core code of the closed enum: Trust can build it (the contract carries it too, tested in tools/contracts).
func TestA531TheReasonIsACoreCode(t *testing.T) {
	if _, err := decide.Core(noRunnableCode); err != nil {
		t.Fatalf("%s is not a core reason code: %v", noRunnableCode, err)
	}
}
