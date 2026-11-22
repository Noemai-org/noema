package trustport

// TRU-10, ruling A503 (interim; A507, A508): an ESCALATE disposition is its own Trust decision (no step, no grant) recorded first through the Recorder, and each escalation action
// decision links it. The intake's RecordRequest carries the optional DispositionDecisionID and this adapter hands it to the Writer as it is; the Writer's own rules (an existing
// same-tenant ESCALATE decision, ErrDispositionNotValid) are not repeated here. REQ-TRU-010; INV-25, EC-E16. All data is SYNTHETIC. The fake-port tests run anywhere; the testpg test
// runs in CI only (pgsodium).

import (
	"testing"

	"github.com/Noemai-org/noema/services/core/record/chain"
	"github.com/Noemai-org/noema/services/core/trust/decide"
	"github.com/Noemai-org/noema/services/core/trust/intake"
	"github.com/Noemai-org/noema/services/core/trust/recorder"
)

// The request the Writer gets names the disposition the intake's request names, and names none when the request names none.
func TestRecordHandsTheWriterTheDispositionDecisionIDOfTheRequest(t *testing.T) {
	for name, disposition := range map[string]string{"a disposition": "dec_01DISP", "none": ""} {
		t.Run(name, func(t *testing.T) {
			r := recRequest(t, 1)
			r.DispositionDecisionID = disposition
			w := &recWriter{res: recGrants(1)}
			if _, err := recAdapter(t, &recBegin{tx: &recTx{}}, newRecFacts(), w).Record(recCtx(t), r); err != nil {
				t.Fatal(err)
			}
			if len(w.got) != 1 || w.got[0].DispositionDecisionID != disposition {
				t.Fatalf("the Writer got %d requests, the first naming %q, want one naming %q", len(w.got), disp(w.got), disposition)
			}
		})
	}
}

// A decision with no step (a read ALLOW) passes it too: the adapter does not decide who needs one, the Writer does (A503: the Writer's rule stays).
func TestRecordPassesTheDispositionOfADecisionWithNoStepsToo(t *testing.T) {
	r := recRequest(t, 0)
	r.DispositionDecisionID = "dec_01DISP"
	w := &recWriter{res: recGrants(0)}
	if _, err := recAdapter(t, &recBegin{tx: &recTx{}}, newRecFacts(), w).Record(recCtx(t), r); err != nil {
		t.Fatal(err)
	}
	if len(w.got) != 1 || w.got[0].DispositionDecisionID != "dec_01DISP" {
		t.Fatalf("the Writer got %d requests, the first naming %q, want one naming dec_01DISP", len(w.got), disp(w.got))
	}
}

// disp is the disposition the first request names, for a failure message that does not print the whole request.
func disp(got []recorder.Request) string {
	if len(got) == 0 {
		return ""
	}
	return got[0].DispositionDecisionID
}

// escalateDisposition is a record request of an ESCALATE decision with no step and no plan hash, as the escalation path records its disposition (A503); n makes its input distinct.
func escalateDisposition(t *testing.T, n byte) intake.RecordRequest {
	t.Helper()
	r := rnaRequest(t, "REJECT", false)
	b := recBuilder(t)
	reason, err := decide.Core("NOT_AUTHORIZED")
	if err != nil {
		t.Fatal(err)
	}
	u, ok := decide.Admit(decide.IntentView{Version: 2, MaterialFlags: []string{"X"}}).Unresolved()
	if !ok {
		t.Fatal("not unresolved")
	}
	d, err := u.Escalate(reason)
	if err != nil {
		t.Fatal(err)
	}
	if d, err = b.Attach(d); err != nil {
		t.Fatal(err)
	}
	digest, err := b.Digest()
	if err != nil {
		t.Fatal(err)
	}
	r.Decision, r.Evaluated, r.BasisDigest, r.IntentHash = d, b.Sealed(), digest, recHash(n)
	return r
}

// The real path: the disposition is recorded first through the adapter and the real Writer, which returns its decision ID; an action decision that names it is stored with the link,
// and the chain verifies (A503: no INSERT, no seeded row).
func TestAnActionDecisionIsStoredWithTheDispositionRecordedFirstThroughTheAdapter(t *testing.T) {
	w := newRecWorld(t)
	disp, err := w.a.Record(recCtx(t), escalateDisposition(t, 30))
	if err != nil || disp.DecisionID == "" || len(disp.GrantIDs) != 0 {
		t.Fatalf("the disposition: %+v, %v; want a decision ID and no grant", disp, err)
	}
	r := recRequest(t, 1)
	r.DispositionDecisionID = disp.DecisionID
	act, err := w.a.Record(recCtx(t), r)
	if err != nil || act.DecisionID == "" || len(act.GrantIDs) != 1 {
		t.Fatalf("the action decision: %+v, %v", act, err)
	}
	var linked string
	if err := w.admin.QueryRow(recCtx(t), `SELECT disposition_decision_id FROM decision_record WHERE decision_id = $1`, act.DecisionID).Scan(&linked); err != nil {
		t.Fatal(err)
	}
	if linked != disp.DecisionID {
		t.Fatalf("the stored action decision links %q, want the disposition %q", linked, disp.DecisionID)
	}
	if n := w.count(t, `SELECT count(*) FROM decision_record WHERE disposition_decision_id IS NULL AND outcome = 'ALLOW'`); n != 0 {
		t.Fatalf("%d ALLOW decisions with no disposition", n)
	}
	if n, err := chain.Verify(recCtx(t), w.admin, recTenant); err != nil || n == 0 {
		t.Fatalf("chain verify: %d %v", n, err)
	}
}
