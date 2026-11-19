package trustport

// TRU-12 (REQ-TRU-009, REQ-AUD-001; INV-03, INV-16, INV-17, EC-E16): the Recorder adapter writes the Decision Record and the grants in one transaction it owns. All data is
// SYNTHETIC. The fake-writer tests run anywhere; the tests that use testpg run against a real PostgreSQL (CI has the pgsodium build, so they run there).

import (
	"context"
	"errors"
	"math/rand"
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
		steps = append(steps, intake.RecordStep{PlanStep: int64(i + 1), CapabilityID: recCap, TargetRef: recTarget, ParamsHash: recHash(byte(2 + i))})
	}
	return intake.RecordRequest{TenantID: recTenant, CorrelationID: "corr_ABC123", PrincipalID: "usr_88213", IntentID: "int_01J9X3", IntentHash: recHash(1), IntentVersion: 2,
		Decision: d, Evaluated: b.Sealed(), BasisDigest: digest, PlanHash: recHash(4), Steps: steps}
}

type recFacts struct {
	f     RecordFacts
	err   error
	calls int
}

func (s *recFacts) Facts(_ context.Context, r intake.RecordRequest) (RecordFacts, error) {
	s.calls++
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
			f.Steps = append(f.Steps, RecordStepFacts{TargetVersions: []contracts.DecisionGrantV1TargetVersionsItem{{ResourceRef: recTarget, Source: "fin.src.ledger", VersionToken: "v7"}}, Permission: p})
		}
	}
	return f, nil
}

func newRecFacts() *recFacts {
	return &recFacts{f: RecordFacts{PolicyID: "fin.pol", EvidenceSetHash: recHash(3)}}
}

// recTx records what is done to a transaction; it embeds a nil pgx.Tx, so any other call panics, which the tests treat as a failure.
type recTx struct {
	pgx.Tx
	commits, rollbacks int
	commitErr          error
	rollbackCtxErr     error
	onCommit           func()
}

func (x *recTx) Commit(context.Context) error {
	x.commits++
	if x.onCommit != nil {
		x.onCommit()
	}
	return x.commitErr
}

func (x *recTx) Rollback(ctx context.Context) error {
	x.rollbacks++
	x.rollbackCtxErr = ctx.Err()
	return nil
}

type recBegin struct {
	tx      *recTx
	tenants []string
	err     error
}

func (b *recBegin) Begin(_ context.Context, tenant string) (pgx.Tx, error) {
	b.tenants = append(b.tenants, tenant)
	if b.err != nil {
		return nil, b.err
	}
	return b.tx, nil
}

type recWriter struct {
	got []recorder.Request
	res recorder.Result
	err error
	do  func()
}

func (w *recWriter) Write(_ context.Context, _ pgx.Tx, r recorder.Request) (recorder.Result, error) {
	w.got = append(w.got, r)
	if w.do != nil {
		w.do()
	}
	return w.res, w.err
}

func recAdapter(t *testing.T, b *recBegin, f *recFacts, w RecordWriter) *RecorderAdapter {
	t.Helper()
	return &RecorderAdapter{begin: b, facts: f, writer: w}
}

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
		if s.PrincipalID != "usr_88213" || string(s.PlanHash) != r.PlanHash || string(s.EvidenceSetHash) != recHash(3) || len(s.TargetVersions) != 1 {
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
	if _, err := recAdapter(t, b, newRecFacts(), w).Record(recCtx(t), recRequest(t, 1)); err != nil {
		t.Fatal(err)
	}
	if len(b.tenants) != 1 || b.tenants[0] != recTenant {
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

// A commit that fails is an error carrying ErrCommit: the outcome is then the stored decision's, which a retry returns (A186).
func TestAFailedCommitIsAnErrorAndNoResult(t *testing.T) {
	boom := errors.New("connection reset")
	tx := &recTx{commitErr: boom}
	got, err := recAdapter(t, &recBegin{tx: tx}, newRecFacts(), &recWriter{res: recGrants(1)}).Record(recCtx(t), recRequest(t, 1))
	if !errors.Is(err, ErrCommit) || !errors.Is(err, boom) || got.DecisionID != "" || got.GrantIDs != nil {
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

// ---- real PostgreSQL ----

type recWorld struct {
	admin  *pgxpool.Pool
	trust  *pgxpool.Pool
	reader *pgxpool.Pool
	a      *RecorderAdapter
}

type recPool struct{ p *pgxpool.Pool }

func (b recPool) Begin(ctx context.Context, tenant string) (pgx.Tx, error) {
	if tenant != recTenant {
		return nil, errors.New("not the test tenant")
	}
	return b.p.Begin(ctx)
}

func newRecWorld(t *testing.T) *recWorld {
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
	a, err := NewRecorderAdapter(RecorderConfig{Begin: recPool{trust}, Facts: newRecFacts(), Keys: keys,
		Appender: &chain.Appender{Clock: clk, IDs: idg, Signers: recordsig.For(t, admin, "recorder", nil, "core-trust")},
		Clock:    clk, IDs: idg, Entropy: rand.New(rand.NewSource(2)), GrantLifetime: 10 * time.Minute, RecordLifetime: 15 * time.Minute})
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
	return w.count(t, `SELECT count(*) FROM decision_record`), w.count(t, `SELECT count(*) FROM decision_grant`), w.count(t, `SELECT count(*) FROM record_event`)
}

// An ALLOW with two write steps writes the record, two grants and three events together, and the result names them, one grant per step in step order.
func TestRecordWritesTheDecisionTheGrantsAndTheirEventsTogether(t *testing.T) {
	w := newRecWorld(t)
	got, err := w.a.Record(recCtx(t), recRequest(t, 2))
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
	if len(steps) != 2 || steps[0] != 1 || steps[1] != 2 {
		t.Errorf("plan steps %v", steps)
	}
	if n, err := chain.Verify(recCtx(t), w.admin, recTenant); err != nil || n != 3 {
		t.Fatalf("chain verify: %d %v", n, err)
	}
}

// The same decision recorded twice is stored once: the second Record returns the stored decision and grants and writes nothing (A186).
func TestASecondRecordOfTheSameDecisionReturnsTheStoredOne(t *testing.T) {
	w := newRecWorld(t)
	first, err := w.a.Record(recCtx(t), recRequest(t, 2))
	if err != nil {
		t.Fatal(err)
	}
	second, err := w.a.Record(recCtx(t), recRequest(t, 2))
	if err != nil {
		t.Fatal(err)
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
	got, err := w.a.Record(recCtx(t), recRequest(t, 0))
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
	if _, err := w.admin.Exec(recCtx(t), `UPDATE tenant SET kill_switch = true WHERE tenant_id = $1`, recTenant); err != nil {
		t.Fatal(err)
	}
	if _, err := w.a.Record(recCtx(t), recRequest(t, 1)); !errors.Is(err, recorder.ErrWritesDisabled) {
		t.Fatalf("err %v", err)
	}
	if rec, gr, ev := w.rows(t); rec+gr+ev != 0 {
		t.Fatalf("rows after a refused write: %d %d %d", rec, gr, ev)
	}
	if _, err := w.admin.Exec(recCtx(t), `UPDATE tenant SET kill_switch = false WHERE tenant_id = $1`, recTenant); err != nil {
		t.Fatal(err)
	}
	bad := recRequest(t, 2)
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
