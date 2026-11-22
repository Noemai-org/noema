package trustport

// STM-01 A456 (interim, relayed, not yet committed), REQ-TRU-002, REQ-NFR-002; INV-03, EC-E16: the Recorder port serves every outcome. A non-ALLOW has no steps and no plan
// hash and is written with the decision event only (a CONFIRM with its request also writes the request's event); an ALLOW needs a plan hash; no outcome but ALLOW carries a
// grant. All data is SYNTHETIC. The fake-port tests run anywhere; the testpg tests run in CI only (pgsodium).

import (
	"errors"
	"math/rand"
	"testing"

	"github.com/Noemai-org/noema/services/core/record/chain"
	"github.com/Noemai-org/noema/services/core/trust/decide"
	"github.com/Noemai-org/noema/services/core/trust/intake"
	"github.com/Noemai-org/noema/services/core/trust/recorder"
)

// rnaRequest is a record request of the given outcome (REJECT, CONFIRM, UNKNOWN), built the way the intake builds it: no steps, no plan hash. withRequest adds the new
// confirmation request a T13 CONFIRM carries.
func rnaRequest(t *testing.T, outcome string, withRequest bool) intake.RecordRequest {
	t.Helper()
	b := recBuilder(t)
	res, ok := decide.Admit(decide.IntentView{Version: 2}).Resolved()
	if !ok {
		t.Fatal("not resolved")
	}
	a := decide.Authority{Technical: decide.Pass, Policy: decide.Pass, Business: decide.Pass}
	var d decide.Decision
	var err error
	switch outcome {
	case "CONFIRM":
		d, err = res.Confirm(a)
	case "REJECT":
		var rc decide.ReasonCode
		if rc, err = decide.Core("DECLINED_BY_APPROVER"); err == nil {
			d, err = res.Reject(a, rc)
		}
	case "UNKNOWN":
		var rc decide.ReasonCode
		if rc, err = decide.Core("INTERNAL_ERROR"); err == nil {
			d, err = res.Unknown(a, rc)
		}
	default:
		t.Fatalf("outcome %q", outcome)
	}
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
	r := intake.RecordRequest{TenantID: recTenant, CorrelationID: "corr_ABC123", PrincipalID: "usr_01J9X3ABCDEFGHJKMNPQRSTVWX", IntentID: "int_01J9X3", IntentHash: recHash(10), IntentVersion: 2,
		Decision: d, Evaluated: b.Sealed(), BasisDigest: digest}
	if withRequest {
		composite, err := d.Basis().CompositePolicyVersion()
		if err != nil {
			t.Fatal(err)
		}
		r.ConfirmationRequest = &intake.NewConfirmationRequest{PrincipalID: "usr_01J9X3ABCDEFGHJKMNPQRSTVWX", IntentHash: recHash(10), CapabilityID: recCap, TargetRef: recTarget, ParamsHash: recHash(13),
			ConfirmationBindingEvidenceHash: recHash(11), PlanHash: recHash(12), PolicyVersion: composite, Requirement: "SELF", RequesterID: "usr_01J9X3ABCDEFGHJKMNPQRSTVWX", ExpiresAt: recEpoch.Unix() + 600}
	}
	return r
}

type rnaCase struct {
	name        string
	outcome     string
	withRequest bool
}

var rnaCases = []rnaCase{{"REJECT", "REJECT", false}, {"CONFIRM with an open request", "CONFIRM", false}, {"CONFIRM that creates a request", "CONFIRM", true}, {"UNKNOWN", "UNKNOWN", false}}

// A non-ALLOW with no steps and no plan hash is accepted and handed to the Writer as it is: no step, no plan hash, no grant, one commit; the decision id comes back (A456).
func TestANonAllowWithNoStepsAndNoPlanHashIsRecordedInOneTransaction(t *testing.T) {
	for _, c := range rnaCases {
		r := rnaRequest(t, c.outcome, c.withRequest)
		w, tx := &recWriter{res: recGrants(0)}, &recTx{}
		if c.withRequest {
			w.res.ConfirmationRequestID = "cfr_01REQ"
		}
		got, err := recAdapter(t, &recBegin{tx: tx}, newRecFacts(), w).Record(recCtx(t), r)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got.DecisionID != "dec_01DEC" || len(got.GrantIDs) != 0 || got.ConfirmationRequestID != w.res.ConfirmationRequestID {
			t.Errorf("%s: result %+v", c.name, got)
		}
		if len(w.got) != 1 || len(w.got[0].Steps) != 0 || w.got[0].ConfirmationEventID != "" || w.got[0].ConfirmationPlanHash != "" || w.got[0].PolicyID != "fin.pol" {
			t.Fatalf("%s: writer got %+v", c.name, w.got)
		}
		if tx.commits != 1 || tx.rollbacks != 0 {
			t.Errorf("%s: commits %d rollbacks %d", c.name, tx.commits, tx.rollbacks)
		}
	}
}

// The request a CONFIRM creates reaches the Writer value for value (the Writer mints the id and the binding hash), and rides on a CONFIRM only.
func TestTheConfirmationRequestOfAConfirmReachesTheWriterUnchanged(t *testing.T) {
	r := rnaRequest(t, "CONFIRM", true)
	w := &recWriter{res: recGrants(0)}
	w.res.ConfirmationRequestID = "cfr_01REQ"
	if _, err := recAdapter(t, &recBegin{tx: &recTx{}}, newRecFacts(), w).Record(recCtx(t), r); err != nil {
		t.Fatal(err)
	}
	q, c := w.got[0].ConfirmationRequest, r.ConfirmationRequest
	if q == nil || *q != (recorder.NewConfirmationRequest{PrincipalID: c.PrincipalID, IntentHash: c.IntentHash, CapabilityID: c.CapabilityID, TargetRef: c.TargetRef, ParamsHash: c.ParamsHash,
		ConfirmationBindingEvidenceHash: c.ConfirmationBindingEvidenceHash, PlanHash: c.PlanHash, PolicyVersion: c.PolicyVersion, Requirement: c.Requirement, RequesterID: c.RequesterID, ExpiresAt: c.ExpiresAt}) {
		t.Fatalf("writer got %+v, want %+v", q, c)
	}
	for _, other := range []string{"REJECT", "UNKNOWN"} {
		r := rnaRequest(t, other, false)
		r.ConfirmationRequest = c
		b, w := &recBegin{tx: &recTx{}}, &recWriter{res: recGrants(0)}
		if _, err := recAdapter(t, b, newRecFacts(), w).Record(recCtx(t), r); !errors.Is(err, ErrRecordRequest) {
			t.Errorf("%s with a request: err %v", other, err)
		}
		if len(b.tenants) != 0 || len(w.got) != 0 {
			t.Errorf("%s with a request reached the database", other)
		}
	}
}

// INV-03: a grant step on any outcome but ALLOW is refused before the transaction, and so is a plan hash on one (a non-ALLOW has no plan).
func TestAGrantOrAPlanHashOnANonAllowIsRefusedBeforeAnyTransaction(t *testing.T) {
	step := intake.RecordStep{PlanStep: 0, CapabilityID: recCap, TargetRef: recTarget, ParamsHash: recHash(2)}
	for _, c := range rnaCases {
		for name, mutate := range map[string]func(*intake.RecordRequest){
			"a step":            func(r *intake.RecordRequest) { r.Steps = []intake.RecordStep{step} },
			"a step and a plan": func(r *intake.RecordRequest) { r.Steps, r.PlanHash = []intake.RecordStep{step}, recHash(4) },
			"a plan hash":       func(r *intake.RecordRequest) { r.PlanHash = recHash(4) },
		} {
			r := rnaRequest(t, c.outcome, c.withRequest)
			mutate(&r)
			b, w := &recBegin{tx: &recTx{}}, &recWriter{res: recGrants(0)}
			if _, err := recAdapter(t, b, newRecFacts(), w).Record(recCtx(t), r); !errors.Is(err, ErrRecordRequest) {
				t.Errorf("%s with %s: err %v", c.name, name, err)
			}
			if len(b.tenants) != 0 || len(w.got) != 0 {
				t.Errorf("%s with %s reached the database", c.name, name)
			}
		}
	}
}

// An ALLOW still needs its plan hash, for a read (no steps, the hash present: A205 Q3) as for a write: without one it is refused before the transaction.
func TestAnAllowWithoutAPlanHashIsStillRefused(t *testing.T) {
	for _, n := range []int{0, 1, 2} {
		r := recRequest(t, n)
		r.PlanHash = ""
		b, w := &recBegin{tx: &recTx{}}, &recWriter{res: recGrants(n)}
		if _, err := recAdapter(t, b, newRecFacts(), w).Record(recCtx(t), r); !errors.Is(err, ErrRecordRequest) {
			t.Errorf("%d steps: err %v", n, err)
		}
		if len(b.tenants) != 0 || len(w.got) != 0 {
			t.Errorf("%d steps: reached the database", n)
		}
	}
}

// A Writer that answers a non-ALLOW with a grant is not committed: the shape of the answer is checked against no steps.
func TestAGrantInTheAnswerToANonAllowIsRolledBack(t *testing.T) {
	for _, c := range rnaCases {
		r := rnaRequest(t, c.outcome, c.withRequest)
		w, tx := &recWriter{res: recGrants(1)}, &recTx{}
		if _, err := recAdapter(t, &recBegin{tx: tx}, newRecFacts(), w).Record(recCtx(t), r); !errors.Is(err, ErrBadAnswer) {
			t.Errorf("%s: err %v", c.name, err)
		}
		if tx.commits != 0 || tx.rollbacks != 1 {
			t.Errorf("%s: commits %d rollbacks %d", c.name, tx.commits, tx.rollbacks)
		}
	}
}

// REQ-CTX-004, INV-17: for a non-ALLOW without steps the sealed facts need no step source call, and whatever the request's own strings are, the evidence hash is the snapshot's.
func TestSealedFactsOfANonAllowNeedNoStepFactsAndTakeNothingFromTheRequest(t *testing.T) {
	rng := rand.New(rand.NewSource(20261120))
	junk := func() string { return recHash(byte(rng.Intn(256))) }
	for i := 0; i < 100; i++ {
		c := rnaCases[rng.Intn(len(rnaCases))]
		rd, st := &sfxReader{snap: sfxSnapshot(t)}, &sfxSteps{}
		r := rnaRequest(t, c.outcome, c.withRequest)
		r.IntentHash, r.ConfirmationBindingEvidenceHash = junk(), junk()
		if r.ConfirmationRequest != nil {
			r.ConfirmationRequest.ConfirmationBindingEvidenceHash, r.ConfirmationRequest.PlanHash = junk(), junk()
		}
		got, err := sfxFacts(t, rd, st).Facts(recCtx(t), r)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got.EvidenceSetHash != rd.snap.EvidenceSetHash || len(got.Steps) != 0 || len(st.steps) != 0 {
			t.Fatalf("round %d %s: facts %+v, step calls %d", i, c.name, got, len(st.steps))
		}
	}
}

// ---- real PostgreSQL (CI only) ----

// A REJECT, an UNKNOWN and a CONFIRM with an open request write the decision and its one event, and no grant; the id comes back; the chain verifies (EC-E16).
func TestRecordOfANonAllowWritesTheDecisionAndItsEventOnly(t *testing.T) {
	for _, c := range rnaCases[:2] {
		w := newRecWorld(t)
		got, err := w.a.Record(recCtx(t), rnaRequest(t, c.outcome, c.withRequest))
		if err != nil || got.DecisionID == "" || len(got.GrantIDs) != 0 || got.ConfirmationRequestID != "" {
			t.Fatalf("%s: %+v %v", c.name, got, err)
		}
		if rec, gr, ev := w.rows(t); rec != 1 || gr != 0 || ev != 1 {
			t.Fatalf("%s rows: %d records, %d grants, %d events", c.name, rec, gr, ev)
		}
		if n, err := chain.Verify(recCtx(t), w.admin, recTenant); err != nil || n != 1 {
			t.Fatalf("%s chain verify: %d %v", c.name, n, err)
		}
	}
	w := newRecWorld(t)
	if got, err := w.a.Record(recCtx(t), rnaRequest(t, "UNKNOWN", false)); err != nil || got.DecisionID == "" || len(got.GrantIDs) != 0 {
		t.Fatalf("UNKNOWN: %+v %v", got, err)
	}
	if rec, gr, ev := w.rows(t); rec != 1 || gr != 0 || ev != 1 {
		t.Fatalf("UNKNOWN rows: %d records, %d grants, %d events", rec, gr, ev)
	}
}

// A CONFIRM that creates a request writes the decision, the request's event and no grant in one transaction, and names the request it minted.
func TestRecordOfAConfirmThatCreatesARequestWritesBothEventsTogether(t *testing.T) {
	w := newRecWorld(t)
	got, err := w.a.Record(recCtx(t), rnaRequest(t, "CONFIRM", true))
	if err != nil || got.DecisionID == "" || len(got.GrantIDs) != 0 || got.ConfirmationRequestID == "" {
		t.Fatalf("%+v %v", got, err)
	}
	if rec, gr, ev := w.rows(t); rec != 1 || gr != 0 || ev != 2 {
		t.Fatalf("rows: %d records, %d grants, %d events", rec, gr, ev)
	}
	if n, err := chain.Verify(recCtx(t), w.admin, recTenant); err != nil || n != 2 {
		t.Fatalf("chain verify: %d %v", n, err)
	}
}
