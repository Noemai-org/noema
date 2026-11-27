package trustport

// A663 (interim, relayed; A581, A590, A574, A577, A578; REQ-TRU-010; INV-03, INV-17, INV-25, EC-E16): the Recorder port serves an ESCALATE reached before the evidence and the policy were
// evaluated. Its basis is incomplete, so there is no sealed snapshot and no policy to take facts from: the facts are not asked, the request carries the turn and the principal to the Writer,
// and no other decision carries either. All data is SYNTHETIC.

import (
	"errors"
	"testing"

	"github.com/Noemai-org/noema/services/core/trust/decide"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

const recTurn = "0b8f3c1e-5d2a-4e7b-9c41-6a1d2f3e4b50"

// recEarlyEscalate is the record request the intake builds for an ESCALATE of an intent no step has read evidence or policy for.
func recEarlyEscalate(t *testing.T, turn string) intake.RecordRequest {
	t.Helper()
	b, err := decide.NewBasisBuilder("fin.pack@1.0.0", recEpoch.Unix())
	if err != nil {
		t.Fatal(err)
	}
	rc, err := decide.Core("NO_RUNNABLE_CAPABILITY")
	if err != nil {
		t.Fatal(err)
	}
	u, ok := decide.Admit(decide.IntentView{Version: 2, MaterialFlags: []string{"X"}}).Unresolved()
	if !ok {
		t.Fatal("resolved")
	}
	d, err := u.Escalate(rc)
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
	return intake.RecordRequest{TenantID: recTenant, CorrelationID: "corr_ABC123", PrincipalID: "usr_01J9X3ABCDEFGHJKMNPQRSTVWX", IntentID: "int_01J9X3", IntentHash: recHash(10), IntentVersion: 2,
		Decision: d, Evaluated: b.Sealed(), BasisDigest: digest, TurnID: turn}
}

func TestA663AnEarlyEscalateReachesTheWriterWithTheTurnAndThePrincipalAndNoFacts(t *testing.T) {
	r := recEarlyEscalate(t, recTurn)
	w, tx, f := &recWriter{res: recGrants(0)}, &recTx{}, newRecFacts()
	got, err := recAdapter(t, &recBegin{tx: tx}, f, w).Record(recCtx(t), r)
	if err != nil {
		t.Fatal(err)
	}
	if got.DecisionID != "dec_01DEC" || len(got.GrantIDs) != 0 {
		t.Errorf("result %+v", got)
	}
	if f.calls != 0 {
		t.Errorf("the facts were asked %d times; an incomplete basis has no snapshot and no policy to take them from", f.calls)
	}
	if len(w.got) != 1 || w.got[0].TurnID != recTurn || w.got[0].TurnPrincipalID != r.PrincipalID || len(w.got[0].Steps) != 0 || w.got[0].PolicyID != "" {
		t.Fatalf("writer got %+v", w.got)
	}
	if tx.commits != 1 || tx.rollbacks != 0 {
		t.Errorf("commits %d rollbacks %d", tx.commits, tx.rollbacks)
	}
}

func TestA663AnEarlyEscalateWithNoWellFormedTurnIsRefusedBeforeATransaction(t *testing.T) {
	for name, turn := range map[string]string{"none": "", "upper case": "0B8F3C1E-5D2A-4E7B-9C41-6A1D2F3E4B50", "a newline": recTurn + "\n", "short": recTurn[:20]} {
		w, b := &recWriter{res: recGrants(0)}, &recBegin{tx: &recTx{}}
		_, err := recAdapter(t, b, newRecFacts(), w).Record(recCtx(t), recEarlyEscalate(t, turn))
		if !errors.Is(err, ErrRecordRequest) || len(w.got) != 0 || len(b.tenants) != 0 {
			t.Errorf("%s: err %v, writer calls %d, begin calls %d; want ErrRecordRequest and nothing opened", name, err, len(w.got), len(b.tenants))
		}
	}
}

// No other decision names a turn: the turn is the key of an early decision only (A581).
func TestA663ADecisionThatIsNotAnEarlyEscalateNamesNoTurn(t *testing.T) {
	r := rnaRequest(t, "REJECT", false)
	r.TurnID = recTurn
	w, b := &recWriter{res: recGrants(0)}, &recBegin{tx: &recTx{}}
	if _, err := recAdapter(t, b, newRecFacts(), w).Record(recCtx(t), r); !errors.Is(err, ErrRecordRequest) || len(w.got) != 0 || len(b.tenants) != 0 {
		t.Errorf("a REJECT that names a turn: err %v, writer calls %d, begin calls %d; want ErrRecordRequest and nothing opened", err, len(w.got), len(b.tenants))
	}
	e := rnaRequest(t, "REJECT", false)
	e.Decision, e.TurnID = recAllowEscalate(t), recTurn
	if _, err := recAdapter(t, &recBegin{tx: &recTx{}}, newRecFacts(), &recWriter{res: recGrants(0)}).Record(recCtx(t), e); !errors.Is(err, ErrRecordRequest) {
		t.Errorf("an ESCALATE with a complete basis that names a turn: err %v, want ErrRecordRequest", err)
	}
}

// recAllowEscalate is an ESCALATE whose basis is complete.
func recAllowEscalate(t *testing.T) decide.Decision {
	t.Helper()
	rc, err := decide.Core("NO_RUNNABLE_CAPABILITY")
	if err != nil {
		t.Fatal(err)
	}
	u, ok := decide.Admit(decide.IntentView{Version: 2, MaterialFlags: []string{"X"}}).Unresolved()
	if !ok {
		t.Fatal("resolved")
	}
	d, err := u.Escalate(rc)
	if err != nil {
		t.Fatal(err)
	}
	if d, err = recBuilder(t).Attach(d); err != nil {
		t.Fatal(err)
	}
	return d
}

// recEarlyReject is the record request the intake builds for a refusal at a gate (T4: a prohibited capability) over a builder that has read nothing.
func recEarlyReject(t *testing.T, turn string) intake.RecordRequest {
	t.Helper()
	r := recEarlyEscalate(t, turn)
	b, err := decide.NewBasisBuilder("fin.pack@1.0.0", recEpoch.Unix())
	if err != nil {
		t.Fatal(err)
	}
	d, err := decide.FailAtGate(decide.T4Capability, 2)
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
	r.Decision, r.Evaluated, r.BasisDigest = d, b.Sealed(), digest
	return r
}

// A680 (interim, relayed): an early REJECT is served like an early ESCALATE: the facts are not asked and the Writer gets the turn and the principal.
func TestA680AnEarlyRejectReachesTheWriterWithTheTurnAndThePrincipalAndNoFacts(t *testing.T) {
	r := recEarlyReject(t, recTurn)
	w, tx, f := &recWriter{res: recGrants(0)}, &recTx{}, newRecFacts()
	got, err := recAdapter(t, &recBegin{tx: tx}, f, w).Record(recCtx(t), r)
	if err != nil {
		t.Fatal(err)
	}
	if got.DecisionID != "dec_01DEC" || len(got.GrantIDs) != 0 || f.calls != 0 {
		t.Errorf("result %+v, facts asked %d times", got, f.calls)
	}
	if len(w.got) != 1 || w.got[0].TurnID != recTurn || w.got[0].TurnPrincipalID != r.PrincipalID || len(w.got[0].Steps) != 0 {
		t.Fatalf("writer got %+v", w.got)
	}
}

func TestA680AnEarlyRejectWithNoWellFormedTurnIsRefusedBeforeATransaction(t *testing.T) {
	w, b := &recWriter{res: recGrants(0)}, &recBegin{tx: &recTx{}}
	_, err := recAdapter(t, b, newRecFacts(), w).Record(recCtx(t), recEarlyReject(t, ""))
	if !errors.Is(err, ErrRecordRequest) || len(w.got) != 0 || len(b.tenants) != 0 {
		t.Errorf("err %v, writer calls %d, begin calls %d; want ErrRecordRequest and nothing opened", err, len(w.got), len(b.tenants))
	}
}
