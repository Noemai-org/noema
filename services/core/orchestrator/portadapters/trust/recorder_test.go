package trustport

// TRU-12 (REQ-TRU-009, REQ-AUD-001; INV-03, INV-16, INV-17, EC-E16): the Recorder adapter writes the Decision Record and the grants in one transaction it owns. All data is
// SYNTHETIC. The fake-writer tests run anywhere; the tests that use testpg run against a real PostgreSQL (CI has the pgsodium build, so they run there).

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/internal/recordsig"
	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/migrations"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/db"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/record/chain"
	"github.com/Noemai-org/noema/services/core/trust/decide"
	"github.com/Noemai-org/noema/services/core/trust/grants"
	"github.com/Noemai-org/noema/services/core/trust/intake"
	"github.com/Noemai-org/noema/services/core/trust/reconcile"
	"github.com/Noemai-org/noema/services/core/trust/recorder"
)

const (
	recTenant = "ten_aa"
	recCap    = "fin.cap.create_escalation_case"
	recTarget = "fin.transaction:TX88213"
)

var recEpoch = time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)

func recHash(c byte) string {
	s := "sha256:"
	for i := 0; i < 64; i++ {
		s += string("0123456789abcdef"[int(c)%16])
	}
	return s
}

func recBuilder(t *testing.T) *decide.BasisBuilder {
	t.Helper()
	b, err := decide.NewBasisBuilder("fin.pack@1.0.0", recEpoch.Unix())
	if err != nil {
		t.Fatal(err)
	}
	if err := b.RecordEvidence("evs_01ABC", []decide.Freshness{{EvidenceID: "ev_1", EffectiveAt: recEpoch.Unix() - 30, Source: "fin.src.ledger", AgeSeconds: 30, EffectiveLimitSeconds: 300, LimitSetBy: decide.LimitByTrustProfile}}); err != nil {
		t.Fatal(err)
	}
	if err := b.RecordPolicy([]decide.PolicyRuleVersion{{RuleID: "fin.pol.default.permit_case_writes", PolicyID: "fin.pol", Version: "3.1"}}); err != nil {
		t.Fatal(err)
	}
	return b
}

// recRequest is an ALLOW request with n write steps (0 is a read), built the way intake.t15 builds it.
func recRequest(t *testing.T, n int) intake.RecordRequest {
	t.Helper()
	b := recBuilder(t)
	res, ok := decide.Admit(decide.IntentView{Version: 2}).Resolved()
	if !ok {
		t.Fatal("not resolved")
	}
	d, err := res.Allow(decide.Authority{Technical: decide.Pass, Policy: decide.Pass, Business: decide.Pass})
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
	var steps []intake.RecordStep
	for i := 0; i < n; i++ {
		steps = append(steps, intake.RecordStep{PlanStep: int64(i), CapabilityID: recCap, TargetRef: recTarget, ParamsHash: recHash(byte(2 + i))})
	}
	// a decision with steps names its ESCALATE disposition, and one with none names none (A503). The fake-port tests name a decision ID the fake Writer does not look at; the
	// tests against the real Writer record the disposition first (escalateDisposition) and name what it returns.
	disposition := ""
	if n > 0 {
		disposition = "dec_01DISP"
	}
	return intake.RecordRequest{TenantID: recTenant, CorrelationID: "corr_ABC123", PrincipalID: "usr_88213", IntentID: "int_01J9X3", IntentHash: recHash(1), IntentVersion: 2,
		Decision: d, Evaluated: b.Sealed(), BasisDigest: digest, PlanHash: recHash(4), Steps: steps, DispositionDecisionID: disposition}
}

type recFacts struct {
	f     RecordFacts
	err   error
	mu    sync.Mutex // Facts is called from many goroutines by the concurrent Record test
	calls int
	ctxs  []context.Context
}

func (s *recFacts) Facts(ctx context.Context, r intake.RecordRequest) (RecordFacts, error) {
	s.mu.Lock()
	s.calls++
	s.ctxs = append(s.ctxs, ctx)
	s.mu.Unlock()
	if s.err != nil {
		return RecordFacts{}, s.err
	}
	f := s.f
	if f.Steps == nil {
		for range r.Steps {
			p, err := reconcile.NewFreezeGate(nil).PermitTarget(false, recCap, recTarget)
			if err != nil {
				panic(err)
			}
			f.Steps = append(f.Steps, RecordStepFacts{TargetVersions: []contracts.DecisionGrantV1TargetVersionsItem{{ResourceRef: recTarget, Source: "fin.src.ledger", VersionToken: "v" + string(rune('7'+len(f.Steps)))}}, Permission: p})
		}
	}
	return f, nil
}

func newRecFacts() *recFacts {
	return &recFacts{f: RecordFacts{PolicyID: "fin.pol", EvidenceSetHash: recHash(3), ConfirmationBindingEvidenceHash: recHash(5)}}
}

// recTx records what is done to a transaction; it embeds a nil pgx.Tx, so any other call panics, which the tests treat as a failure.
type recTx struct {
	pgx.Tx
	commits, rollbacks int
	commitErr          error
	rollbackCtxErr     error
	onCommit           func()
	commitCtx          context.Context
	rollbackCtx        context.Context
}

func (x *recTx) Commit(ctx context.Context) error {
	x.commits++
	x.commitCtx = ctx
	if x.onCommit != nil {
		x.onCommit()
	}
	return x.commitErr
}

func (x *recTx) Rollback(ctx context.Context) error {
	x.rollbacks++
	x.rollbackCtx = ctx
	x.rollbackCtxErr = ctx.Err()
	return nil
}

type recBegin struct {
	tx      *recTx
	tenants []string
	err     error
	ctxs    []context.Context
}

func (b *recBegin) Begin(ctx context.Context, tenant string) (pgx.Tx, error) {
	b.ctxs = append(b.ctxs, ctx)
	b.tenants = append(b.tenants, tenant)
	if b.err != nil {
		return nil, b.err
	}
	return b.tx, nil
}

type recWriter struct {
	got  []recorder.Request
	res  recorder.Result
	err  error
	do   func()
	ctxs []context.Context
}

func (w *recWriter) Write(ctx context.Context, _ pgx.Tx, r recorder.Request) (recorder.Result, error) {
	w.ctxs = append(w.ctxs, ctx)
	w.got = append(w.got, r)
	if w.do != nil {
		w.do()
	}
	return w.res, w.err
}

func recAdapter(t *testing.T, b *recBegin, f *recFacts, w RecordWriter) *RecorderAdapter {
	t.Helper()
	return &RecorderAdapter{begin: b, facts: f, writer: w, txTimeout: recTxTimeout}
}

// recTxTimeout is the adapter's own bound in the fake-port tests (SYNTHETIC value); it is shorter than recCtx's 30 s so a test can tell the two apart.
const recTxTimeout = 10 * time.Second

func recCtx(t *testing.T) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return c
}

func recGrants(n int) recorder.Result {
	res := recorder.Result{Record: contracts.DecisionRecordV2{DecisionId: "dec_01DEC"}}
	for i := 0; i < n; i++ {
		res.Grants = append(res.Grants, contracts.DecisionGrantV1{GrantId: contracts.CommonV1GrantId("grt_0" + string(rune('A'+i)) + "XX")})
	}
	return res
}

// The request the writer gets is built from the intake's request, the facts and nothing else: one grant step per write step in step order, the Evaluated builder passed on as it came.
func TestRecordHandsTheWriterTheRequestOfTheIntakeAndTheFacts(t *testing.T) {
	r := recRequest(t, 2)
	r.ConfirmationEventID, r.ConfirmationPlanHash = "cfe_01AB", r.PlanHash
	w, tx, f := &recWriter{res: recGrants(2)}, &recTx{}, newRecFacts()
	got, err := recAdapter(t, &recBegin{tx: tx}, f, w).Record(recCtx(t), r)
	if err != nil {
		t.Fatal(err)
	}
	if got.DecisionID != "dec_01DEC" || len(got.GrantIDs) != 2 || got.GrantIDs[0] == got.GrantIDs[1] {
		t.Fatalf("result %+v", got)
	}
	if len(w.got) != 1 {
		t.Fatalf("%d writes", len(w.got))
	}
	q := w.got[0]
	if q.TenantID != recTenant || q.CorrelationID != "corr_ABC123" || q.IntentID != "int_01J9X3" || string(q.IntentHash) != r.IntentHash || q.PolicyID != "fin.pol" {
		t.Fatalf("identity fields: %+v", q)
	}
	if q.Evaluated != r.Evaluated {
		t.Error("the Evaluated builder was not passed on unchanged (TRU-13)")
	}
	if string(q.ConfirmationPlanHash) != r.ConfirmationPlanHash || q.ConfirmationEventID != "cfe_01AB" {
		t.Errorf("confirmation fields: %q %q", q.ConfirmationPlanHash, q.ConfirmationEventID)
	}
	if len(q.Steps) != 2 {
		t.Fatalf("%d steps", len(q.Steps))
	}
	for i, s := range q.Steps {
		want := r.Steps[i]
		if s.PlanStep != want.PlanStep || s.CapabilityID != want.CapabilityID || s.TargetRef != want.TargetRef || string(s.ParamsHash) != want.ParamsHash {
			t.Errorf("step %d: %+v", i, s)
		}
		if s.PrincipalID != "usr_88213" || string(s.PlanHash) != r.PlanHash || string(s.EvidenceSetHash) != recHash(3) || len(s.TargetVersions) != 1 || s.TargetVersions[0].VersionToken != "v"+string(rune('7'+i)) {
			t.Errorf("step %d: %+v", i, s)
		}
	}
	if tx.commits != 1 || tx.rollbacks != 0 {
		t.Errorf("commits %d rollbacks %d", tx.commits, tx.rollbacks)
	}
}

// A writer error rolls back the transaction, commits nothing, and returns the writer's error: nothing was written (EC-E16).
func TestAWriterErrorRollsBackAndCommitsNothing(t *testing.T) {
	for _, werr := range []error{recorder.ErrWritesDisabled, recorder.ErrConfirmationSpent, errors.New("the append failed")} {
		w, tx := &recWriter{res: recGrants(1), err: werr}, &recTx{}
		got, err := recAdapter(t, &recBegin{tx: tx}, newRecFacts(), w).Record(recCtx(t), recRequest(t, 1))
		if !errors.Is(err, werr) {
			t.Fatalf("err %v, want %v", err, werr)
		}
		if tx.commits != 0 || tx.rollbacks != 1 {
			t.Errorf("commits %d rollbacks %d", tx.commits, tx.rollbacks)
		}
		if got.DecisionID != "" || got.GrantIDs != nil {
			t.Errorf("a result came with an error: %+v", got)
		}
	}
}

// A read has no write step: the decision is recorded, no grant is issued, and the result names none.
func TestARecordWithNoStepsIssuesNoGrant(t *testing.T) {
	w, tx := &recWriter{res: recGrants(0)}, &recTx{}
	got, err := recAdapter(t, &recBegin{tx: tx}, newRecFacts(), w).Record(recCtx(t), recRequest(t, 0))
	if err != nil {
		t.Fatal(err)
	}
	if got.DecisionID != "dec_01DEC" || len(got.GrantIDs) != 0 || len(w.got[0].Steps) != 0 || tx.commits != 1 {
		t.Fatalf("%+v %+v commits %d", got, w.got[0].Steps, tx.commits)
	}
}

// A digest that is not the builder's is refused before any transaction is opened (TRU-13): the adapter compares, it never takes the caller's word.
func TestABasisDigestThatIsNotTheBuildersIsRefusedBeforeAnyTransaction(t *testing.T) {
	r := recRequest(t, 1)
	r.BasisDigest = recHash(9)
	b, w := &recBegin{tx: &recTx{}}, &recWriter{}
	if _, err := recAdapter(t, b, newRecFacts(), w).Record(recCtx(t), r); !errors.Is(err, ErrBasisDigest) {
		t.Fatalf("err %v", err)
	}
	if len(b.tenants) != 0 || len(w.got) != 0 {
		t.Errorf("a refused request opened %d transactions and made %d writes", len(b.tenants), len(w.got))
	}
}

// A confirmation given for another plan is refused before any transaction (A230); so is a plan hash named with no confirmation.
func TestAConfirmationForAnotherPlanIsRefusedBeforeAnyTransaction(t *testing.T) {
	for name, mutate := range map[string]func(*intake.RecordRequest){
		"another plan":              func(r *intake.RecordRequest) { r.ConfirmationEventID, r.ConfirmationPlanHash = "cfe_01AB", recHash(8) },
		"an event and no plan hash": func(r *intake.RecordRequest) { r.ConfirmationEventID, r.ConfirmationPlanHash = "cfe_01AB", "" },
		"a plan hash and no event":  func(r *intake.RecordRequest) { r.ConfirmationEventID, r.ConfirmationPlanHash = "", r.PlanHash },
	} {
		r := recRequest(t, 1)
		mutate(&r)
		b, w := &recBegin{tx: &recTx{}}, &recWriter{}
		if _, err := recAdapter(t, b, newRecFacts(), w).Record(recCtx(t), r); !errors.Is(err, ErrPlanHash) {
			t.Errorf("%s: err %v", name, err)
		}
		if len(b.tenants) != 0 || len(w.got) != 0 {
			t.Errorf("%s: a refused request reached the database", name)
		}
	}
}

// A request the adapter cannot map is refused before any transaction: a missing tenant, principal or builder, a fact source that fails or gives the wrong number of steps or no policy.
func TestARequestThatCannotBeMappedIsRefusedBeforeAnyTransaction(t *testing.T) {
	boom := errors.New("the fact source failed")
	cases := map[string]struct {
		mutate func(*intake.RecordRequest)
		facts  *recFacts
		want   error
	}{
		"no tenant":        {func(r *intake.RecordRequest) { r.TenantID = "" }, newRecFacts(), ErrRecordRequest},
		"no principal":     {func(r *intake.RecordRequest) { r.PrincipalID = "" }, newRecFacts(), ErrRecordRequest},
		"no builder":       {func(r *intake.RecordRequest) { r.Evaluated = nil }, newRecFacts(), ErrRecordRequest},
		"no plan hash":     {func(r *intake.RecordRequest) { r.PlanHash = "" }, newRecFacts(), ErrRecordRequest},
		"facts fail":       {func(*intake.RecordRequest) {}, &recFacts{err: boom}, boom},
		"too few steps":    {func(*intake.RecordRequest) {}, &recFacts{f: RecordFacts{PolicyID: "fin.pol", EvidenceSetHash: recHash(3), Steps: []RecordStepFacts{}}}, ErrRecordRequest},
		"too many steps":   {func(*intake.RecordRequest) {}, &recFacts{f: RecordFacts{PolicyID: "fin.pol", EvidenceSetHash: recHash(3), Steps: make([]RecordStepFacts, 2)}}, ErrRecordRequest},
		"no policy":        {func(*intake.RecordRequest) {}, &recFacts{f: RecordFacts{EvidenceSetHash: recHash(3)}}, ErrRecordRequest},
		"no evidence hash": {func(*intake.RecordRequest) {}, &recFacts{f: RecordFacts{PolicyID: "fin.pol"}}, ErrRecordRequest},
	}
	for name, c := range cases {
		r := recRequest(t, 1)
		c.mutate(&r)
		b, w := &recBegin{tx: &recTx{}}, &recWriter{}
		if _, err := recAdapter(t, b, c.facts, w).Record(recCtx(t), r); !errors.Is(err, c.want) {
			t.Errorf("%s: err %v, want %v", name, err, c.want)
		}
		if len(b.tenants) != 0 || len(w.got) != 0 {
			t.Errorf("%s: a refused request reached the database", name)
		}
	}
}

// The transaction is opened as the request's tenant, and a transaction that cannot be opened is an error with nothing written.
func TestTheTransactionIsOpenedAsTheRequestsTenant(t *testing.T) {
	b, w := &recBegin{tx: &recTx{}}, &recWriter{res: recGrants(1)}
	r := recRequest(t, 1)
	r.TenantID = "ten_bb"
	if _, err := recAdapter(t, b, newRecFacts(), w).Record(recCtx(t), r); err != nil {
		t.Fatal(err)
	}
	if len(b.tenants) != 1 || b.tenants[0] != "ten_bb" {
		t.Errorf("tenants %v", b.tenants)
	}
	boom := errors.New("no connection")
	b, w = &recBegin{err: boom}, &recWriter{}
	if _, err := recAdapter(t, b, newRecFacts(), w).Record(recCtx(t), recRequest(t, 1)); !errors.Is(err, boom) || len(w.got) != 0 {
		t.Errorf("err %v, %d writes", err, len(w.got))
	}
}

// A9/A186 choice: a context that ends while Record runs rolls the transaction back and returns the context's error; nothing is committed. The rollback itself runs on a context
// that is not cancelled, so that it can reach the database.
func TestAContextThatEndsWhileRecordRunsRollsBack(t *testing.T) {
	ctx, cancel := context.WithCancel(recCtx(t))
	w, tx := &recWriter{res: recGrants(1), do: cancel}, &recTx{}
	got, err := recAdapter(t, &recBegin{tx: tx}, newRecFacts(), w).Record(ctx, recRequest(t, 1))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err %v", err)
	}
	if tx.commits != 0 || tx.rollbacks != 1 || tx.rollbackCtxErr != nil {
		t.Errorf("commits %d rollbacks %d rollback ctx error %v", tx.commits, tx.rollbacks, tx.rollbackCtxErr)
	}
	if got.DecisionID != "" {
		t.Errorf("result %+v", got)
	}
}

// AGENTS section 7: a context deadline on every call. A context with none is refused with the typed ErrNoDeadline before the facts, any transaction or the writer are touched.
func TestARecordWithoutAContextDeadlineIsRefusedBeforeAnyTransaction(t *testing.T) {
	cancelable, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	for name, ctx := range map[string]context.Context{"background": context.Background(), "cancel only": cancelable} {
		b, f, w := &recBegin{tx: &recTx{}}, newRecFacts(), &recWriter{res: recGrants(1)}
		got, err := recAdapter(t, b, f, w).Record(ctx, recRequest(t, 1))
		if !errors.Is(err, ErrNoDeadline) || got.DecisionID != "" || len(b.tenants) != 0 || f.calls != 0 || len(w.got) != 0 {
			t.Errorf("%s: err %v result %+v begins %d facts %d writes %d, want ErrNoDeadline and nothing touched", name, err, got, len(b.tenants), f.calls, len(w.got))
		}
	}
}

// A commit that fails leaves the outcome unknown (ErrCommit, with the cause wrapped so errors.Is finds it); a retry then returns the stored ALLOW, the same decision and grants (A186).
func TestAFailedCommitThenARetryReturnsTheStoredDecision(t *testing.T) {
	boom := errors.New("connection reset")
	w := &recWriter{res: recGrants(1)}
	a := recAdapter(t, &recBegin{tx: &recTx{commitErr: boom}}, newRecFacts(), w)
	if _, err := a.Record(recCtx(t), recRequest(t, 1)); !errors.Is(err, ErrCommit) || !errors.Is(err, boom) {
		t.Fatalf("first call: %v, want ErrCommit wrapping the cause", err)
	}
	a = recAdapter(t, &recBegin{tx: &recTx{}}, newRecFacts(), w)
	got, err := a.Record(recCtx(t), recRequest(t, 1))
	if err != nil || got.DecisionID != "dec_01DEC" || len(got.GrantIDs) != 1 {
		t.Fatalf("retry: %v %+v, want the stored decision", err, got)
	}
}

// A commit that fails is an error carrying ErrCommit: the outcome is then the stored decision's, which a retry returns (A186).
func TestAFailedCommitIsAnErrorAndNoResult(t *testing.T) {
	boom := errors.New("connection reset")
	tx := &recTx{commitErr: boom}
	got, err := recAdapter(t, &recBegin{tx: tx}, newRecFacts(), &recWriter{res: recGrants(1)}).Record(recCtx(t), recRequest(t, 1))
	if !errors.Is(err, ErrCommit) || !errors.Is(err, boom) || got.DecisionID != "" || got.GrantIDs != nil || tx.rollbacks != 1 {
		t.Fatalf("err %v result %+v", err, got)
	}
}

// A writer that answers with another number of grants than steps is an error, never a result (one grant per write step, none for a read).
func TestAWriterAnswerOfTheWrongShapeIsRefusedAndRolledBack(t *testing.T) {
	for name, res := range map[string]recorder.Result{"too few grants": recGrants(1), "too many for a read": recGrants(1), "no decision id": {Grants: recGrants(2).Grants}} {
		n := 2
		if name == "too many for a read" {
			n = 0
		}
		tx := &recTx{}
		if _, err := recAdapter(t, &recBegin{tx: tx}, newRecFacts(), &recWriter{res: res}).Record(recCtx(t), recRequest(t, n)); !errors.Is(err, ErrBadAnswer) {
			t.Errorf("%s: err %v", name, err)
		}
		if tx.commits != 0 || tx.rollbacks != 1 {
			t.Errorf("%s: commits %d rollbacks %d", name, tx.commits, tx.rollbacks)
		}
	}
}

// An adapter built without a port, a key, a clock or a lifetime is an error (ErrConfig).
func TestAnAdapterWithoutWhatItNeedsIsNotBuilt(t *testing.T) {
	if _, err := NewRecorderAdapter(RecorderConfig{}); !errors.Is(err, ErrConfig) {
		t.Fatalf("err %v", err)
	}
}

// Codex P2 and IL P2 on noema#752 (AGENTS section 7, AGENT-16): the deadline reaches the fact source, Begin and the writer, none of them later than the caller's, and the adapter
// bounds the transaction by a time of its own (TxTimeout, no default) so a caller with a distant deadline cannot hold a transaction open.
func TestTheDeadlineReachesTheFactsTheBeginAndTheWriterAndIsTheEarlierOfTheTwo(t *testing.T) {
	caller := recCtx(t) // 30 s
	cd, _ := caller.Deadline()
	f, b, w, tx := newRecFacts(), &recBegin{tx: &recTx{}}, &recWriter{res: recGrants(1)}, (*recTx)(nil)
	tx = b.tx
	if _, err := recAdapter(t, b, f, w).Record(caller, recRequest(t, 1)); err != nil {
		t.Fatal(err)
	}
	all := append(append(append([]context.Context{}, f.ctxs...), b.ctxs...), w.ctxs...)
	if len(f.ctxs) != 1 || len(b.ctxs) != 1 || len(w.ctxs) != 1 {
		t.Fatalf("facts %d begin %d write %d calls, want one each", len(f.ctxs), len(b.ctxs), len(w.ctxs))
	}
	for i, c := range all {
		d, ok := c.Deadline()
		if !ok || !d.Before(cd) {
			t.Errorf("call %d: deadline %v present %v, want one earlier than the caller's %v (the adapter's TxTimeout %v is shorter)", i, d, ok, cd, recTxTimeout)
		}
	}
	if tx.commits != 1 {
		t.Errorf("commits %d", tx.commits)
	}
}

// A caller whose deadline is nearer than TxTimeout keeps it: the adapter never extends a deadline.
func TestACallersNearerDeadlineIsNeverExtended(t *testing.T) {
	caller, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	cd, _ := caller.Deadline()
	f, b, w := newRecFacts(), &recBegin{tx: &recTx{}}, &recWriter{res: recGrants(1)}
	if _, err := recAdapter(t, b, f, w).Record(caller, recRequest(t, 1)); err != nil {
		t.Fatal(err)
	}
	for _, c := range []context.Context{f.ctxs[0], b.ctxs[0], w.ctxs[0]} {
		if d, ok := c.Deadline(); !ok || d.After(cd) {
			t.Errorf("deadline %v present %v, want one not after the caller's %v", d, ok, cd)
		}
	}
}

// The commit and the rollback end the transaction, so they run on a context that is not the request's: bounded, never later than their own bound, and not cancelled by the
// request. The commit's bound is its own, not the write's (a commit that has begun is finished, not abandoned halfway).
func TestTheCommitRunsOnABoundedContextThatTheRequestDoesNotCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(recCtx(t))
	defer cancel()
	b, w := &recBegin{tx: &recTx{}}, &recWriter{res: recGrants(1)}
	var errAtCommit error
	b.tx.onCommit = func() { // the request is cancelled while the commit runs; the commit's own context must not notice
		cancel()
		errAtCommit = b.tx.commitCtx.Err()
	}
	if _, err := recAdapter(t, b, newRecFacts(), w).Record(ctx, recRequest(t, 1)); err != nil {
		t.Fatal(err)
	}
	d, ok := b.tx.commitCtx.Deadline()
	wd, _ := w.ctxs[0].Deadline()
	if !ok || d.Equal(wd) {
		t.Errorf("commit deadline %v present %v, want one of its own, not the write's %v", d, ok, wd)
	}
	if errAtCommit != nil {
		t.Errorf("the commit context was cancelled with the request: %v", errAtCommit)
	}
}

// The adapter's TxTimeout has no default: with everything else valid, zero or negative is ErrConfig and a positive one builds.
func TestAnAdapterWithoutATxTimeoutIsNotBuilt(t *testing.T) {
	keys, err := grants.NewKeys([]grants.Key{{ID: "k2", Secret: bytes.Repeat([]byte{7}, 32)}})
	if err != nil {
		t.Fatal(err)
	}
	clk := clock.NewManual(recEpoch)
	cfg := RecorderConfig{Begin: &recBegin{}, Facts: newRecFacts(), Keys: keys, Appender: &chain.Appender{}, Clock: clk, IDs: ids.New(clk, rand.New(rand.NewSource(1))),
		Entropy: rand.New(rand.NewSource(2)), GrantLifetime: time.Minute, RecordLifetime: time.Minute}
	for _, d := range []time.Duration{0, -time.Second} {
		cfg.TxTimeout = d
		if _, err := NewRecorderAdapter(cfg); !errors.Is(err, ErrConfig) {
			t.Errorf("TxTimeout %v: err %v", d, err)
		}
	}
	cfg.TxTimeout = time.Second
	if _, err := NewRecorderAdapter(cfg); err != nil {
		t.Errorf("a positive TxTimeout: %v", err)
	}
}

// ---- real PostgreSQL ----

// recLockedReader makes the entropy source safe for the concurrent Records of one test (rand.Rand is not).
type recLockedReader struct {
	mu sync.Mutex
	r  io.Reader
}

func (l *recLockedReader) Read(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.r.Read(p)
}

type recWorld struct {
	admin  *pgxpool.Pool
	trust  *pgxpool.Pool
	reader *pgxpool.Pool
	a      *RecorderAdapter
	// dispositions is the number of ESCALATE disposition decisions the world recorded through the real Writer for its action requests (action); rows leaves them out.
	dispositions int
}

// action is recRequest with a disposition the real Writer has recorded: a decision with steps names its ESCALATE disposition, recorded first as its own Trust decision with no
// step and no grant (A503). A decision with no step names none. The same disposition is recorded once however often action is called, so the world holds one.
func (w *recWorld) action(t *testing.T, n int) intake.RecordRequest {
	t.Helper()
	r := recRequest(t, n)
	if n == 0 {
		return r
	}
	disp, err := w.a.Record(recCtx(t), escalateDisposition(t, 30))
	if err != nil || disp.DecisionID == "" || len(disp.GrantIDs) != 0 {
		t.Fatalf("the disposition: %+v, %v; want a decision ID and no grant", disp, err)
	}
	w.dispositions = 1
	r.DispositionDecisionID = disp.DecisionID
	return r
}

type recPool struct{ p *pgxpool.Pool }

func (b recPool) Begin(ctx context.Context, tenant string) (pgx.Tx, error) {
	if tenant != recTenant {
		return nil, errors.New("not the test tenant")
	}
	return b.p.Begin(ctx)
}

// newRecWorld holds the escalation plan of the test capability, so the disposition of an action decision can be recorded through the real Writer (A503).
func newRecWorld(t *testing.T) *recWorld {
	return newRecWorldWith(t, recorder.NewEscalationSet(recCap))
}

// newRecWorldWith is newRecWorld with the escalation plan the composition root would inject (A335): a decision that names a disposition carries only its capabilities.
func newRecWorldWith(t *testing.T, escalation recorder.EscalationSet) *recWorld {
	t.Helper()
	admin, dsn := testpg.NewDatabase(t)
	if _, err := db.Migrate(recCtx(t), admin, migrations.FS); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(recCtx(t), `INSERT INTO tenant (tenant_id, name) VALUES ($1, 'A')`, recTenant); err != nil {
		t.Fatal(err)
	}
	secret := make([]byte, 32)
	for i := range secret {
		secret[i] = byte(i + 1)
	}
	keys, err := grants.NewKeys([]grants.Key{{ID: "k2", Secret: secret}})
	if err != nil {
		t.Fatal(err)
	}
	clk := clock.NewManual(recEpoch)
	idg := ids.New(clk, rand.New(rand.NewSource(1)))
	trust := testpg.TenantLogin(t, admin, dsn, "trust_rw", recTenant)
	a, err := NewRecorderAdapter(RecorderConfig{Begin: recPool{trust}, Facts: newRecFacts(), Keys: keys, Escalation: escalation,
		Appender: &chain.Appender{Clock: clk, IDs: idg, Signers: recordsig.For(t, admin, "recorder", nil, "core-trust")},
		Clock:    clk, IDs: idg, Entropy: &recLockedReader{r: rand.New(rand.NewSource(2))}, GrantLifetime: 10 * time.Minute, RecordLifetime: 15 * time.Minute, TxTimeout: recTxTimeout})
	if err != nil {
		t.Fatal(err)
	}
	return &recWorld{admin: admin, trust: trust, reader: testpg.TenantLogin(t, admin, dsn, "record_read", recTenant), a: a}
}

func (w *recWorld) count(t *testing.T, q string) int {
	t.Helper()
	var n int
	if err := w.admin.QueryRow(recCtx(t), q).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (w *recWorld) rows(t *testing.T) (records, grantRows, events int) {
	t.Helper()
	return w.count(t, `SELECT count(*) FROM decision_record`) - w.dispositions, w.count(t, `SELECT count(*) FROM decision_grant`), w.count(t, `SELECT count(*) FROM record_event`) - w.dispositions
}

// An ALLOW with two write steps writes the record, two grants and three events together, and the result names them, one grant per step in step order.
func TestRecordWritesTheDecisionTheGrantsAndTheirEventsTogether(t *testing.T) {
	w := newRecWorld(t)
	got, err := w.a.Record(recCtx(t), w.action(t, 2))
	if err != nil {
		t.Fatal(err)
	}
	if rec, gr, ev := w.rows(t); rec != 1 || gr != 2 || ev != 3 {
		t.Fatalf("rows: %d records, %d grants, %d events", rec, gr, ev)
	}
	if len(got.GrantIDs) != 2 {
		t.Fatalf("%+v", got)
	}
	var steps []int64
	rows, err := w.admin.Query(recCtx(t), `SELECT grant_id, (json->>'plan_step')::bigint FROM decision_grant ORDER BY (json->>'plan_step')::bigint`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for i := 0; rows.Next(); i++ {
		var id string
		var step int64
		if err := rows.Scan(&id, &step); err != nil {
			t.Fatal(err)
		}
		if id != got.GrantIDs[i] {
			t.Errorf("grant %d is %s, the result says %s", i, id, got.GrantIDs[i])
		}
		steps = append(steps, step)
	}
	if len(steps) != 2 || steps[0] != 0 || steps[1] != 1 {
		t.Errorf("plan steps %v, want the zero-based 0 and 1", steps)
	}
	if n, err := chain.Verify(recCtx(t), w.admin, recTenant); err != nil || n != int64(3+w.dispositions) {
		t.Fatalf("chain verify: %d %v", n, err)
	}
}

// The same decision recorded twice is stored once: the second Record returns the stored decision and grants and writes nothing (A186).
func TestASecondRecordOfTheSameDecisionReturnsTheStoredOne(t *testing.T) {
	w := newRecWorld(t)
	first, err := w.a.Record(recCtx(t), w.action(t, 2))
	if err != nil {
		t.Fatal(err)
	}
	r0, g0, e0 := w.rows(t)
	second, err := w.a.Record(recCtx(t), w.action(t, 2))
	if err != nil {
		t.Fatal(err)
	}
	if r1, g1, e1 := w.rows(t); r1 != r0 || g1 != g0 || e1 != e0 {
		t.Fatalf("a second Record changed the row counts: records %d to %d, grants %d to %d, events %d to %d", r0, r1, g0, g1, e0, e1)
	}
	if second.DecisionID != first.DecisionID || len(second.GrantIDs) != 2 || second.GrantIDs[0] != first.GrantIDs[0] || second.GrantIDs[1] != first.GrantIDs[1] {
		t.Fatalf("first %+v second %+v", first, second)
	}
	if rec, gr, ev := w.rows(t); rec != 1 || gr != 2 || ev != 3 {
		t.Fatalf("rows after a retry: %d records, %d grants, %d events", rec, gr, ev)
	}
}

// A read has no step: the record and its event are written, no grant.
func TestRecordOfAReadWritesNoGrant(t *testing.T) {
	w := newRecWorld(t)
	got, err := w.a.Record(recCtx(t), w.action(t, 0))
	if err != nil || got.DecisionID == "" || len(got.GrantIDs) != 0 {
		t.Fatalf("%+v %v", got, err)
	}
	if rec, gr, ev := w.rows(t); rec != 1 || gr != 0 || ev != 1 {
		t.Fatalf("rows: %d records, %d grants, %d events", rec, gr, ev)
	}
}

// A writer error leaves nothing: the kill switch refuses the write after the lock, and a step the issuer cannot grant fails after the record is inserted. Both roll back.
func TestAWriterErrorLeavesNothingInTheDatabase(t *testing.T) {
	w := newRecWorld(t)
	one, two := w.action(t, 1), w.action(t, 2) // the disposition is recorded before the kill switch goes on
	if _, err := w.admin.Exec(recCtx(t), `UPDATE tenant SET kill_switch = true WHERE tenant_id = $1`, recTenant); err != nil {
		t.Fatal(err)
	}
	if _, err := w.a.Record(recCtx(t), one); !errors.Is(err, recorder.ErrWritesDisabled) {
		t.Fatalf("err %v", err)
	}
	if rec, gr, ev := w.rows(t); rec+gr+ev != 0 {
		t.Fatalf("rows after a refused write: %d %d %d", rec, gr, ev)
	}
	if _, err := w.admin.Exec(recCtx(t), `UPDATE tenant SET kill_switch = false WHERE tenant_id = $1`, recTenant); err != nil {
		t.Fatal(err)
	}
	bad := two
	bad.Steps[1].TargetRef = "not a target" // the second grant fails after the record, the first grant and their events are inserted
	if _, err := w.a.Record(recCtx(t), bad); err == nil {
		t.Fatal("a write with an invalid second step succeeded")
	}
	if rec, gr, ev := w.rows(t); rec+gr+ev != 0 {
		t.Fatalf("rows after a failed write: %d %d %d", rec, gr, ev)
	}
}

// A refused request (digest, plan hash) writes nothing in the real database either.
func TestARefusedRequestWritesNothingInTheDatabase(t *testing.T) {
	w := newRecWorld(t)
	r := recRequest(t, 1)
	r.BasisDigest = recHash(9)
	if _, err := w.a.Record(recCtx(t), r); !errors.Is(err, ErrBasisDigest) {
		t.Fatalf("err %v", err)
	}
	r = recRequest(t, 1)
	r.ConfirmationEventID, r.ConfirmationPlanHash = "cfe_01AB", recHash(8)
	if _, err := w.a.Record(recCtx(t), r); !errors.Is(err, ErrPlanHash) {
		t.Fatalf("err %v", err)
	}
	if rec, gr, ev := w.rows(t); rec+gr+ev != 0 {
		t.Fatalf("rows: %d %d %d", rec, gr, ev)
	}
}

// Plan steps are zero-based and equal the index in the plan (Execution E5 compares the grant's plan_step with the stored plan step's number, controller.bound). Steps 0 and 1 pass
// through to the writer unchanged; PlanStep is not shifted, not renumbered and not defaulted.
func TestRecordPassesZeroBasedPlanStepsThrough(t *testing.T) {
	w := &recWriter{res: recGrants(2)}
	if _, err := recAdapter(t, &recBegin{tx: &recTx{}}, newRecFacts(), w).Record(recCtx(t), recRequest(t, 2)); err != nil {
		t.Fatal(err)
	}
	if got := w.got[0].Steps; len(got) != 2 || got[0].PlanStep != 0 || got[1].PlanStep != 1 {
		t.Fatalf("plan steps %+v, want 0 and 1", got)
	}
}

// A step number outside the plan, a negative one, one that is not the step's own index and a repeated one are refused before any transaction: no grant is issued for a step that
// E5 could not find in the stored plan.
func TestAPlanStepThatIsNotTheIndexIsRefusedBeforeAnyTransaction(t *testing.T) {
	cases := map[string]func(*intake.RecordRequest){
		"equal to the number of steps": func(r *intake.RecordRequest) { r.Steps[1].PlanStep = 2 },
		"far beyond the plan":          func(r *intake.RecordRequest) { r.Steps[1].PlanStep = 6 },
		"negative":                     func(r *intake.RecordRequest) { r.Steps[0].PlanStep = -1 },
		"one-based":                    func(r *intake.RecordRequest) { r.Steps[0].PlanStep, r.Steps[1].PlanStep = 1, 2 },
		"swapped":                      func(r *intake.RecordRequest) { r.Steps[0].PlanStep, r.Steps[1].PlanStep = 1, 0 },
		"repeated":                     func(r *intake.RecordRequest) { r.Steps[1].PlanStep = 0 },
	}
	for name, mutate := range cases {
		r := recRequest(t, 2)
		mutate(&r)
		b, w := &recBegin{tx: &recTx{}}, &recWriter{res: recGrants(2)}
		if _, err := recAdapter(t, b, newRecFacts(), w).Record(recCtx(t), r); !errors.Is(err, ErrRecordRequest) {
			t.Errorf("%s: err %v", name, err)
		}
		if len(b.tenants) != 0 || len(w.got) != 0 {
			t.Errorf("%s: a refused request reached the database", name)
		}
	}
}

// The writer's typed conflict (the same input tuple stored with other content, A186) is ErrRecordConflict, never a string match, and it rolls back.
func TestAStoredDecisionThatDisagreesIsATypedConflict(t *testing.T) {
	for _, werr := range []error{recorder.ErrDecisionConflict, fmt.Errorf("the writer wrapped it: %w", recorder.ErrDecisionConflict)} {
		tx := &recTx{}
		got, err := recAdapter(t, &recBegin{tx: tx}, newRecFacts(), &recWriter{err: werr}).Record(recCtx(t), recRequest(t, 1))
		if !errors.Is(err, ErrRecordConflict) || got.DecisionID != "" || tx.commits != 0 || tx.rollbacks != 1 {
			t.Errorf("err %v result %+v commits %d rollbacks %d", err, got, tx.commits, tx.rollbacks)
		}
	}
	for _, other := range []error{recorder.ErrConfirmationSpent, recorder.ErrWritesDisabled, errors.New("the append failed")} {
		_, err := recAdapter(t, &recBegin{tx: &recTx{}}, newRecFacts(), &recWriter{err: other}).Record(recCtx(t), recRequest(t, 1))
		if errors.Is(err, ErrRecordConflict) {
			t.Errorf("%v was reported as a conflict", other)
		}
	}
}

// The same idempotency key (intent, intent hash, evidence snapshot, policy) with different content is refused with the typed conflict and writes nothing more: the stored
// decision and its grants stay as they were (A186, INV-21).
func TestTheSameKeyWithDifferentContentIsATypedConflictInTheDatabase(t *testing.T) {
	w := newRecWorld(t)
	first, err := w.a.Record(recCtx(t), w.action(t, 2))
	if err != nil {
		t.Fatal(err)
	}
	r0, g0, e0 := w.rows(t)
	other := w.action(t, 2)
	other.Steps[1].ParamsHash = recHash(9) // the same key, other content
	got, err := w.a.Record(recCtx(t), other)
	if !errors.Is(err, ErrRecordConflict) {
		t.Fatalf("err %v, want ErrRecordConflict", err)
	}
	if got.DecisionID != "" || got.GrantIDs != nil {
		t.Errorf("a result came with the conflict: %+v", got)
	}
	if r1, g1, e1 := w.rows(t); r1 != r0 || g1 != g0 || e1 != e0 {
		t.Fatalf("a conflict changed the rows: records %d to %d, grants %d to %d, events %d to %d", r0, r1, g0, g1, e0, e1)
	}
	again, err := w.a.Record(recCtx(t), w.action(t, 2))
	if err != nil || again.DecisionID != first.DecisionID {
		t.Fatalf("the original no longer reads back: %+v %v", again, err)
	}
}

// Records of the same decision made at once write one set: one record, two grants, three events, and every caller gets the same decision and grant IDs (A186, advisory lock).
func TestConcurrentRecordsOfTheSameDecisionWriteOneSet(t *testing.T) {
	w := newRecWorld(t)
	const n = 6
	var wg sync.WaitGroup
	start := make(chan struct{})
	got := make([]intake.RecordResult, n)
	errs := make([]error, n)
	reqs := make([]intake.RecordRequest, n)
	for i := range reqs {
		reqs[i] = w.action(t, 2)
	}
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			got[i], errs[i] = w.a.Record(recCtx(t), reqs[i])
		}()
	}
	close(start)
	wg.Wait()
	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("record %d: %v", i, errs[i])
		}
		if got[i].DecisionID != got[0].DecisionID || len(got[i].GrantIDs) != 2 || got[i].GrantIDs[0] != got[0].GrantIDs[0] || got[i].GrantIDs[1] != got[0].GrantIDs[1] {
			t.Fatalf("record %d: %+v, record 0: %+v", i, got[i], got[0])
		}
	}
	if rec, gr, ev := w.rows(t); rec != 1 || gr != 2 || ev != 3 {
		t.Fatalf("rows after %d concurrent records: %d records, %d grants, %d events", n, rec, gr, ev)
	}
	if v, err := chain.Verify(recCtx(t), w.admin, recTenant); err != nil || v != int64(3+w.dispositions) {
		t.Fatalf("chain verify: %d %v", v, err)
	}
}

// A failure after the decision insert rolls back the grants (EC-E16): the second grant fails after the record, the first grant and their events are inserted, and nothing of
// them remains, so the same decision recorded correctly afterwards is written whole, once.
func TestAFailureAfterTheDecisionInsertRollsBackTheGrants(t *testing.T) {
	w := newRecWorld(t)
	bad := w.action(t, 2)
	bad.Steps[1].TargetRef = "not a target"
	if _, err := w.a.Record(recCtx(t), bad); err == nil || errors.Is(err, ErrRecordRequest) || errors.Is(err, ErrRecordConflict) {
		t.Fatalf("err %v, want a failure of the second grant after the inserts", err)
	}
	if rec, gr, ev := w.rows(t); rec+gr+ev != 0 {
		t.Fatalf("rows left after the failure: %d records, %d grants, %d events", rec, gr, ev)
	}
	if n := w.count(t, `SELECT count(*) FROM confirmation_spend`); n != 0 {
		t.Fatalf("%d spends left", n)
	}
	got, err := w.a.Record(recCtx(t), w.action(t, 2))
	if err != nil || len(got.GrantIDs) != 2 {
		t.Fatalf("%+v %v", got, err)
	}
	if rec, gr, ev := w.rows(t); rec != 1 || gr != 2 || ev != 3 {
		t.Fatalf("rows after the retry: %d records, %d grants, %d events", rec, gr, ev)
	}
}

// The DB tests of this package share one PostgreSQL server and need no TestMain: testpg.NewDatabase gives each test its own database, so a sibling test of the same package (the
// Confirmations adapter's) cannot see these rows. Two worlds in one test prove the isolation; every helper here carries the rec prefix, so siblings do not collide.
func TestTwoRecorderWorldsShareNoRows(t *testing.T) {
	a, b := newRecWorld(t), newRecWorld(t)
	if _, err := a.a.Record(recCtx(t), a.action(t, 1)); err != nil {
		t.Fatal(err)
	}
	if rec, gr, ev := b.rows(t); rec+gr+ev != 0 {
		t.Fatalf("the second world sees %d %d %d rows of the first", rec, gr, ev)
	}
}
