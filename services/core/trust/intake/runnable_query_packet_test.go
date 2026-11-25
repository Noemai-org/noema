package intake

// TRU-14 (REQ-TRU-004, REQ-AG-001; INV-03, INV-16; A694, interim, relayed, not yet committed): RunnableQuery carries the evidence packet hash, optional, from the same value that
// reaches the plan request of the same step. A continuation carries its own packet; a first evaluation has none to carry and the field stays empty; nothing invents one. All data is SYNTHETIC.

import (
	"testing"

	"github.com/Noemai-org/noema/services/core/trust/decide"
)

// A continuation asks the Runnable port with the packet it was given, the one the plan request carries for the same step.
func TestA694AContinuationsRunnableQueryCarriesItsEvidencePacket(t *testing.T) {
	f, p, r := stepFake()
	ev := stepEval13(t, f, p, r, capRoute)
	res := ev.run(tctx(t), ev.policySteps())
	if res.Decision.Outcome() != decide.Allow {
		t.Fatalf("outcome %s, want ALLOW", res.Decision.Outcome())
	}
	if len(r.asked) != 1 || r.asked[0].EvidencePacket != tPacketRef {
		t.Fatalf("the runnable port was asked %+v, want once with EvidencePacket %q", r.asked, tPacketRef)
	}
	if len(f.planReq) != 1 || f.planReq[0].EvidencePacket != r.asked[0].EvidencePacket {
		t.Errorf("the plan request packet %+v and the runnable query packet %q differ: one source of truth", f.planReq, r.asked[0].EvidencePacket)
	}
}

// A continuation with no packet asks with none: optional means empty, never a default.
func TestA694AContinuationWithNoPacketAsksWithNone(t *testing.T) {
	f, p, r := stepFake()
	ev := stepEval13(t, f, p, r, capRoute)
	ev.step.packet = ""
	ev.run(tctx(t), ev.policySteps())
	if len(r.asked) != 1 || r.asked[0].EvidencePacket != "" {
		t.Fatalf("the runnable port was asked %+v, want once with an empty EvidencePacket", r.asked)
	}
}

// A first evaluation has no verified result to carry: its query leaves the packet empty, whatever the snapshot, and invents nothing.
func TestA694AFirstEvaluationsRunnableQueryHasNoEvidencePacket(t *testing.T) {
	r := &runFake{ids: []string{tWriteCap}}
	_, ev := noRunnableEv(t, r, &handoffFake{routed: true})
	run(t, ev, decide.T12Clocks)
	if len(r.asked) != 1 {
		t.Fatalf("the runnable port was asked %d times, want once", len(r.asked))
	}
	if r.asked[0].EvidencePacket != "" || r.asked[0].SnapshotID != tSnapshot {
		t.Errorf("the first evaluation's query %+v, want the snapshot %q and an empty EvidencePacket", r.asked[0], tSnapshot)
	}
}
