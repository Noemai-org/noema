package recorder_test

// TRU-10 (A680, A574, A581, A590; interim, relayed; REQ-TRU-001, REQ-TRU-002, REQ-TRU-007; INV-03, INV-16, INV-21, INV-25): a refusal at T1 to T9 is a REJECT reached before the evidence and the
// policy were evaluated, so its basis is incomplete. The Writer records it in the null-basis form, with no step and no grant, keyed by the turn and the verified principal like an early
// ESCALATE. Any other early outcome with a turn is still refused. Real PostgreSQL. All data is SYNTHETIC.

import (
	"errors"
	"testing"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/trust/decide"
	"github.com/Noemai-org/noema/services/core/trust/recorder"
)

// earlyGate is the decision a failed gate step produces (T4: a prohibited capability is a REJECT; T3: writes disabled is an UNKNOWN) over a builder that has read nothing.
func earlyGate(t *testing.T, step decide.GateStep) (decide.Decision, *decide.BasisBuilder) {
	t.Helper()
	b, err := decide.NewBasisBuilder("fin.pack@1.0.0", epoch.Unix())
	if err != nil {
		t.Fatal(err)
	}
	d, err := decide.FailAtGate(step, 2)
	if err != nil {
		t.Fatal(err)
	}
	if d, err = b.Attach(d); err != nil {
		t.Fatal(err)
	}
	return d, b
}

func gateRequest(t *testing.T, step decide.GateStep, turn, principal string) recorder.Request {
	t.Helper()
	d, b := earlyGate(t, step)
	r := request(t, d, b)
	r.TurnID, r.TurnPrincipalID = turn, principal
	return r
}

func (w *world) rejectRows(t *testing.T) int {
	return w.count(t, `SELECT count(*) FROM decision_record WHERE outcome = 'REJECT' AND jsonb_typeof(json -> 'evidence_snapshot_id') = 'null'
		AND jsonb_typeof(json -> 'policy_version') = 'null' AND jsonb_typeof(json -> 'evidence_freshness') = 'null'`)
}

func TestAnEarlyRejectIsRecordedWithTheBasisNullAndNoGrant(t *testing.T) {
	w := newWorld(t)
	res, err := w.write(t, gateRequest(t, decide.T4Capability, turnA, principalA), true)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	rec := res.Record
	if rec.Outcome != contracts.EnumsV1DecisionOutcomeREJECT || rec.EvidenceSnapshotId != nil || rec.PolicyVersion != nil || rec.EvidenceFreshness != nil || len(res.Grants) != 0 {
		t.Fatalf("record %+v grants %d: want a REJECT with the three basis members null and no grant", rec, len(res.Grants))
	}
	if n := w.rejectRows(t); n != 1 {
		t.Errorf("%d stored null-basis REJECT rows, want 1", n)
	}
	if got := w.events(t); len(got) != 1 {
		t.Errorf("events %v, want one DECISION_MADE", got)
	}
}

func TestAReplayOfTheSameRefusalReturnsTheStoredRecordAndANewTurnRecordsAnother(t *testing.T) {
	w := newWorld(t)
	first, err := w.write(t, gateRequest(t, decide.T4Capability, turnA, principalA), true)
	if err != nil {
		t.Fatal(err)
	}
	again, err := w.write(t, gateRequest(t, decide.T4Capability, turnA, principalA), true)
	if err != nil || again.Record.DecisionId != first.Record.DecisionId {
		t.Fatalf("the replay: %v %s, want the stored %s", err, again.Record.DecisionId, first.Record.DecisionId)
	}
	other, err := w.write(t, gateRequest(t, decide.T4Capability, turnB, principalA), true)
	if err != nil || other.Record.DecisionId == first.Record.DecisionId {
		t.Fatalf("a new turn: %v %s, want a new record", err, other.Record.DecisionId)
	}
	if n := w.rejectRows(t); n != 2 {
		t.Errorf("%d rows, want 2", n)
	}
}

func TestAnEarlyRejectNeedsItsTurnAndPrincipal(t *testing.T) {
	w := newWorld(t)
	for name, r := range map[string]recorder.Request{
		"no turn":      gateRequest(t, decide.T4Capability, "", ""),
		"no principal": gateRequest(t, decide.T4Capability, turnA, ""),
		"bad turn":     gateRequest(t, decide.T4Capability, "TURN-1", principalA),
	} {
		if _, err := w.write(t, r, true); err == nil {
			t.Errorf("%s: a null-basis REJECT was stored", name)
		}
	}
	if n := w.rejectRows(t); n != 0 {
		t.Errorf("%d rows, want none", n)
	}
}

// Only a REJECT and an ESCALATE are early decisions: an UNKNOWN (T3, writes disabled) with a turn is still refused.
func TestAnEarlyUnknownWithATurnIsStillRefused(t *testing.T) {
	w := newWorld(t)
	_, err := w.write(t, gateRequest(t, decide.T3WritesEnabled, turnA, principalA), true)
	if !errors.Is(err, recorder.ErrNoEvaluatedBasis) && !errors.Is(err, recorder.ErrRequest) {
		t.Fatalf("got %v, want a refusal", err)
	}
}
