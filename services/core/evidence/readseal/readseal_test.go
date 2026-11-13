package readseal_test

// ENV-04 wiring slice W2 (REQ-CTX-001, REQ-CTX-004, REQ-AUD-001; INV-11, INV-12, INV-17, INV-19): the adapter that implements orchestrator.Evidence. These tests
// use fakes for the adapter's collaborators (the intent reader, the read call site, the reconciler, the sealer and the transaction source) and need no
// database; the sealer and the Record are tested where they live (evidence/snapshot, evidence/layer). All data is SYNTHETIC.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Noemai-org/noema/services/core/adapters"
	"github.com/Noemai-org/noema/services/core/evidence/layer"
	"github.com/Noemai-org/noema/services/core/evidence/readseal"
	"github.com/Noemai-org/noema/services/core/evidence/snapshot"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/orchestrator"
	"github.com/Noemai-org/noema/services/core/understanding/intentstore"
)

const (
	tenant   = "ten_aa"
	other    = "ten_bb"
	user     = "usr_ab12"
	corr     = "corr_AA11"
	intentID = "int_0001"
	ref      = intentID + "@1"
)

var session = orchestrator.Session{TenantID: tenant, PrincipalID: user}

func deadline(t *testing.T) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return c
}

// admitted builds the stored intent: the slots object is the part of the document the adapter reads.
func admitted(tenantID, slots string) intentstore.Admitted {
	doc := `{"intent_id":"` + intentID + `","version":1,"tenant_id":"` + tenantID + `","slots":` + slots + `}`
	return intentstore.Admitted{TenantID: tenantID, IntentID: intentID, Version: 1, IntentNode: "fin.payments.exception.investigate", CorrelationID: corr, JSON: []byte(doc)}
}

const resolvedTx = `{"subject_resource":{"type":"Resource","status":"RESOLVED","resolved_ref":"fin.transaction:TX88213"}}`

type intents struct {
	a     intentstore.Admitted
	err   error
	calls []string
}

func (i *intents) Get(_ context.Context, id string, version int) (intentstore.Admitted, error) {
	i.calls = append(i.calls, id)
	_ = version
	return i.a, i.err
}

type reads struct {
	items []layer.Item
	err   error
	calls []struct {
		scope adapters.Scope
		corr  string
		tx    string
	}
}

func (r *reads) ReadTransaction(_ context.Context, s adapters.Scope, c, tx string) ([]layer.Item, error) {
	r.calls = append(r.calls, struct {
		scope adapters.Scope
		corr  string
		tx    string
	}{s, c, tx})
	return r.items, r.err
}

type classifier struct {
	out   readseal.Classified
	err   error
	calls int
	got   []layer.Item
}

func (c *classifier) Classify(_ context.Context, _ string, items []layer.Item) (readseal.Classified, error) {
	c.calls++
	c.got = items
	return c.out, c.err
}

type sealer struct {
	err   error
	calls []snapshot.Input
}

func (s *sealer) Seal(_ context.Context, _ pgx.Tx, in snapshot.Input) (snapshot.Sealed, error) {
	s.calls = append(s.calls, in)
	if s.err != nil {
		return snapshot.Sealed{}, s.err
	}
	return snapshot.Sealed{SnapshotID: "evs_01HZ", TenantID: in.TenantID}, nil
}

// tx records how the transaction ended; the other methods are never called by the adapter (a call would panic on the nil interface, which fails the test).
type tx struct {
	pgx.Tx
	commits, rollbacks int
	commitErr          error
}

func (t *tx) Commit(context.Context) error   { t.commits++; return t.commitErr }
func (t *tx) Rollback(context.Context) error { t.rollbacks++; return nil }

type source struct {
	tx    *tx
	err   error
	begun int
}

func (s *source) Begin(context.Context) (pgx.Tx, error) {
	s.begun++
	if s.err != nil {
		return nil, s.err
	}
	return s.tx, nil
}

func item(n string, status contracts.EnumsV1FactStatus) layer.Item {
	return layer.Item{EvidenceID: "evd_" + n, PayloadRef: "pay_" + n, PayloadHash: "sha256:" + strings.Repeat("ab", 32), Status: status}
}

func known() []layer.Item {
	k := contracts.EnumsV1FactStatusKNOWN
	return []layer.Item{item("C", k), item("A", k), item("B", k)}
}

type rig struct {
	a  *readseal.Adapter
	in *intents
	rd *reads
	cl *classifier
	se *sealer
	sr *source
}

func newRig() *rig {
	r := &rig{in: &intents{a: admitted(tenant, resolvedTx)}, rd: &reads{items: known()},
		cl: &classifier{out: readseal.Classified{ReconciliationID: "rec_01HZ", Classification: "SUCCESS_CONFIRMED"}}, se: &sealer{}, sr: &source{tx: &tx{}}}
	r.a = &readseal.Adapter{TenantID: tenant, Intents: r.in, Transactions: r.rd, Reconciler: r.cl, Sealer: r.se, Tx: r.sr, Timeout: 5 * time.Second}
	return r
}

func (r *rig) nothingRead(t *testing.T) {
	t.Helper()
	if len(r.rd.calls) != 0 || r.cl.calls != 0 || len(r.se.calls) != 0 || r.sr.begun != 0 {
		t.Errorf("nothing may be read, classified, begun or sealed: reads=%d classified=%d sealed=%d begun=%d", len(r.rd.calls), r.cl.calls, len(r.se.calls), r.sr.begun)
	}
}

func TestTheTransactionIsReadAsTheSessionAndSealedWithItsClassification(t *testing.T) {
	// REQ-CTX-001, REQ-CTX-004, INV-12: the scope is the session's, the transaction is the intent's resolved subject_resource, the correlation is the intent's, and the
	// snapshot is sealed over exactly the items read with the classification the Reconciler gave.
	r := newRig()
	got, err := r.a.Read(deadline(t), session, ref)
	if err != nil {
		t.Fatal(err)
	}
	if got.SnapshotID != "evs_01HZ" || got.Unavailable {
		t.Errorf("got %+v, want the sealed snapshot's ID and Unavailable false", got)
	}
	if len(r.rd.calls) != 1 {
		t.Fatalf("want one read of the transaction, got %d", len(r.rd.calls))
	}
	c := r.rd.calls[0]
	if c.scope != (adapters.Scope{TenantID: tenant, PrincipalID: user}) || c.corr != corr || c.tx != "TX88213" {
		t.Errorf("read with %+v, want the session's scope, the intent's correlation and the transaction ID after the colon", c)
	}
	if len(r.se.calls) != 1 {
		t.Fatalf("want one seal, got %d", len(r.se.calls))
	}
	in := r.se.calls[0]
	if in.TenantID != tenant || in.CorrelationID != corr || in.ReconciliationID != "rec_01HZ" || in.Classification != "SUCCESS_CONFIRMED" {
		t.Errorf("sealed with %+v, want the tenant, the correlation and the Reconciler's answer", in)
	}
	if strings.Join(in.EvidenceIDs, ",") != "evd_C,evd_A,evd_B" && strings.Join(in.EvidenceIDs, ",") != "evd_A,evd_B,evd_C" {
		t.Errorf("sealed over %v, want exactly the three items read", in.EvidenceIDs)
	}
	if len(r.cl.got) != 3 {
		t.Errorf("the Reconciler saw %d items, want the three read", len(r.cl.got))
	}
	if r.sr.tx.commits != 1 || r.sr.tx.rollbacks > 1 {
		t.Errorf("commits=%d rollbacks=%d, want one commit", r.sr.tx.commits, r.sr.tx.rollbacks)
	}
}

func TestASourceThatCouldNotAnswerIsReportedUnavailableAndTheSnapshotIsStillSealed(t *testing.T) {
	// INV-19: an unavailable, unknown or unauthorized source is not a fact and is not hidden. The adapter says so and Trust decides what it means.
	for _, st := range []contracts.EnumsV1FactStatus{contracts.EnumsV1FactStatusUNAVAILABLE, contracts.EnumsV1FactStatusUNKNOWN, contracts.EnumsV1FactStatusUNAUTHORIZED} {
		r := newRig()
		r.rd.items = []layer.Item{item("A", contracts.EnumsV1FactStatusKNOWN), item("B", st), item("C", contracts.EnumsV1FactStatusKNOWN)}
		got, err := r.a.Read(deadline(t), session, ref)
		if err != nil {
			t.Fatalf("%s: %v", st, err)
		}
		if !got.Unavailable || got.SnapshotID == "" || len(r.se.calls) != 1 {
			t.Errorf("%s: got %+v, sealed %d times; want Unavailable true with a sealed snapshot", st, got, len(r.se.calls))
		}
	}
}

func TestAnythingElseThanTheAdaptersTenantIsRefusedBeforeAnyRead(t *testing.T) {
	// INV-11, INV-12: a session of another tenant, and an intent of another tenant, are refused; nothing is read or sealed.
	r := newRig()
	if _, err := r.a.Read(deadline(t), orchestrator.Session{TenantID: other, PrincipalID: user}, ref); !errors.Is(err, readseal.ErrTenant) {
		t.Errorf("a session of another tenant: want ErrTenant, got %v", err)
	}
	r.nothingRead(t)
	r = newRig()
	r.in.a = admitted(other, resolvedTx)
	if _, err := r.a.Read(deadline(t), session, ref); !errors.Is(err, readseal.ErrTenant) {
		t.Errorf("an intent of another tenant: want ErrTenant, got %v", err)
	}
	r.nothingRead(t)
}

func TestAnIntentReferenceThatIsNotIntentIdAtVersionIsRefused(t *testing.T) {
	for name, bad := range map[string]string{"empty": "", "no version": intentID, "version zero": intentID + "@0", "version not a number": intentID + "@x", "version with a sign": intentID + "@-1",
		"two separators": intentID + "@1@2", "id with a slash": "int/0001@1", "no id": "@1", "huge version": intentID + "@99999999999999999999"} {
		r := newRig()
		if _, err := r.a.Read(deadline(t), session, bad); !errors.Is(err, readseal.ErrIntentRef) {
			t.Errorf("%s: want ErrIntentRef, got %v", name, err)
		}
		if len(r.in.calls) != 0 {
			t.Errorf("%s: the intent reader must not be called for a malformed reference", name)
		}
		r.nothingRead(t)
	}
}

func TestAnIntentThatIsNotStoredIsAnErrorAndNothingIsRead(t *testing.T) {
	r := newRig()
	r.in.err = intentstore.ErrNotFound
	if _, err := r.a.Read(deadline(t), session, ref); !errors.Is(err, intentstore.ErrNotFound) {
		t.Errorf("want intentstore.ErrNotFound, got %v", err)
	}
	r.nothingRead(t)
}

func TestAnIntentWithNoResolvedTransactionIsRefusedAndNothingIsRead(t *testing.T) {
	// INV-06 and INV-19: a transaction that was never resolved is not guessed from a surface form or a candidate.
	for name, slots := range map[string]string{
		"no slots":                   `{}`,
		"no subject_resource":        `{"amount":{"type":"Quantity","status":"RESOLVED"}}`,
		"not resolved":               `{"subject_resource":{"type":"Resource","status":"AMBIGUOUS","candidates_ref":"cset_01"}}`,
		"resolved with no ref":       `{"subject_resource":{"type":"Resource","status":"RESOLVED"}}`,
		"another kind":               `{"subject_resource":{"type":"Resource","status":"RESOLVED","resolved_ref":"fin.account:ACC1"}}`,
		"no id after kind":           `{"subject_resource":{"type":"Resource","status":"RESOLVED","resolved_ref":"fin.transaction:"}}`,
		"an id with a path":          `{"subject_resource":{"type":"Resource","status":"RESOLVED","resolved_ref":"fin.transaction:../x"}}`,
		"a ref that is not a string": `{"subject_resource":{"type":"Resource","status":"RESOLVED","resolved_ref":7}}`,
	} {
		r := newRig()
		r.in.a = admitted(tenant, slots)
		if _, err := r.a.Read(deadline(t), session, ref); !errors.Is(err, readseal.ErrNoTransaction) {
			t.Errorf("%s: want ErrNoTransaction, got %v", name, err)
		}
		r.nothingRead(t)
	}
	r := newRig()
	r.in.a.JSON = []byte(`not json`)
	if _, err := r.a.Read(deadline(t), session, ref); !errors.Is(err, readseal.ErrNoTransaction) {
		t.Errorf("a document that is not JSON: want ErrNoTransaction, got %v", err)
	}
	r.nothingRead(t)
}

func TestAFailedReadIsAnErrorAndNothingIsSealed(t *testing.T) {
	// The Record comes first (EC-E16): if recording an item failed, nothing is classified, begun or sealed.
	boom := errors.New("synthetic: the Record refused the event")
	r := newRig()
	r.rd.err = boom
	if _, err := r.a.Read(deadline(t), session, ref); !errors.Is(err, boom) {
		t.Errorf("want the read's error, got %v", err)
	}
	if r.cl.calls != 0 || len(r.se.calls) != 0 || r.sr.begun != 0 {
		t.Errorf("a failed read must stop the run: classified=%d sealed=%d begun=%d", r.cl.calls, len(r.se.calls), r.sr.begun)
	}
}

func TestAnEmptySetIsNeverSealed(t *testing.T) {
	// INV-17: no snapshot over no evidence.
	r := newRig()
	r.rd.items = nil
	if _, err := r.a.Read(deadline(t), session, ref); !errors.Is(err, snapshot.ErrEmptySet) {
		t.Errorf("want snapshot.ErrEmptySet, got %v", err)
	}
	if r.cl.calls != 0 || len(r.se.calls) != 0 || r.sr.begun != 0 {
		t.Errorf("an empty set is refused before it is classified or sealed")
	}
}

func TestAClassifierThatFailsLeavesNothingSealed(t *testing.T) {
	boom := errors.New("synthetic: no rule set")
	r := newRig()
	r.cl.err = boom
	if _, err := r.a.Read(deadline(t), session, ref); !errors.Is(err, boom) {
		t.Errorf("want the Reconciler's error, got %v", err)
	}
	if len(r.se.calls) != 0 || r.sr.begun != 0 {
		t.Errorf("nothing may be sealed or begun: sealed=%d begun=%d", len(r.se.calls), r.sr.begun)
	}
}

func TestAFailedSealIsRolledBackAndNeverCommitted(t *testing.T) {
	// REQ-AUD-001, EC-E16: the snapshot row and its Record event stand or fall together; a failed seal commits nothing and returns no snapshot ID.
	boom := errors.New("synthetic: the Record refused the event")
	r := newRig()
	r.se.err = boom
	got, err := r.a.Read(deadline(t), session, ref)
	if !errors.Is(err, boom) || got.SnapshotID != "" {
		t.Errorf("got %+v, %v; want the seal's error and no snapshot ID", got, err)
	}
	if r.sr.tx.commits != 0 || r.sr.tx.rollbacks != 1 {
		t.Errorf("commits=%d rollbacks=%d, want no commit and one rollback", r.sr.tx.commits, r.sr.tx.rollbacks)
	}
}

func TestAFailedCommitReturnsNoSnapshotID(t *testing.T) {
	boom := errors.New("synthetic: the commit failed")
	r := newRig()
	r.sr.tx.commitErr = boom
	got, err := r.a.Read(deadline(t), session, ref)
	if !errors.Is(err, boom) || got.SnapshotID != "" {
		t.Errorf("got %+v, %v; want the commit's error and no snapshot ID", got, err)
	}
}

func TestATransactionThatCannotBeginIsAnError(t *testing.T) {
	boom := errors.New("synthetic: no connection")
	r := newRig()
	r.sr.err = boom
	if _, err := r.a.Read(deadline(t), session, ref); !errors.Is(err, boom) {
		t.Errorf("want the begin's error, got %v", err)
	}
	if len(r.se.calls) != 0 {
		t.Errorf("nothing may be sealed without a transaction")
	}
}

func TestAnAdapterThatIsNotFullyWiredDoesNothing(t *testing.T) {
	// AGENT-16: no default for the deadline, and no default for any collaborator.
	bad := map[string]func(*readseal.Adapter){
		"no tenant":       func(a *readseal.Adapter) { a.TenantID = "" },
		"no intents":      func(a *readseal.Adapter) { a.Intents = nil },
		"no transactions": func(a *readseal.Adapter) { a.Transactions = nil },
		"no reconciler":   func(a *readseal.Adapter) { a.Reconciler = nil },
		"no sealer":       func(a *readseal.Adapter) { a.Sealer = nil },
		"no tx source":    func(a *readseal.Adapter) { a.Tx = nil },
		"no timeout":      func(a *readseal.Adapter) { a.Timeout = 0 },
	}
	for name, mutate := range bad {
		r := newRig()
		mutate(r.a)
		if _, err := r.a.Read(deadline(t), session, ref); !errors.Is(err, readseal.ErrNotWired) {
			t.Errorf("%s: want ErrNotWired, got %v", name, err)
		}
		r.nothingRead(t)
	}
}

func TestAContextWithNoDeadlineIsRefused(t *testing.T) {
	r := newRig()
	if _, err := r.a.Read(context.Background(), session, ref); !errors.Is(err, readseal.ErrNoDeadline) {
		t.Errorf("want ErrNoDeadline, got %v", err)
	}
	r.nothingRead(t)
}

func TestASessionWithNoTenantOrNoPrincipalIsRefused(t *testing.T) {
	for name, s := range map[string]orchestrator.Session{"no tenant": {PrincipalID: user}, "no principal": {TenantID: tenant}} {
		r := newRig()
		if _, err := r.a.Read(deadline(t), s, ref); !errors.Is(err, orchestrator.ErrNoSession) {
			t.Errorf("%s: want orchestrator.ErrNoSession, got %v", name, err)
		}
		r.nothingRead(t)
	}
}
