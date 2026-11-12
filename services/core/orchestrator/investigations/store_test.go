package investigations_test

// STM-01 slice 2, the store (REQ-STM-001, REQ-AUD-001, EC-E16, INV-12; SR's note 2 on noema#264; noema#477): Start finds or makes the one investigation of a recorded
// input; AppendChange appends INVESTIGATION_STATE_CHANGED as core-workflows and moves the row in ONE transaction, the event first (the Record comes first: if the append
// fails the row does not move), and the expected version is part of the compare-and-swap, taken from what the caller read. These tests append signed Record events, so
// they need the signing extension (the CI database has it; a local one may not). All data is SYNTHETIC.

import (
	"context"
	"errors"
	"math/rand"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/internal/recordsig"
	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/migrations"
	"github.com/Noemai-org/noema/services/core/orchestrator"
	"github.com/Noemai-org/noema/services/core/orchestrator/investigations"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/db"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/record/chain"
)

// fakeInputs stands for the Record's confirmation of an INPUT_RECEIVED event: it confirms every reference except those in refuse, and remembers what it was asked.
type fakeInputs struct {
	refuse map[string]bool
	asked  []string
}

func (f *fakeInputs) Confirm(_ context.Context, sess orchestrator.Session, ref, correlation string) error {
	f.asked = append(f.asked, sess.TenantID+"|"+sess.PrincipalID+"|"+ref+"|"+correlation)
	if f.refuse[ref] {
		return errors.New("synthetic: the Record does not confirm this input")
	}
	return nil
}

type storeWorld struct {
	inputs       *fakeInputs
	admin        *pgxpool.Pool
	orchA, orchB *pgxpool.Pool
	storeA       *investigations.Store
	failing      *atomic.Bool // when set, the next Append fails
}

type flakyIDs struct {
	inner chain.IDSource
	fail  *atomic.Bool
}

func (f flakyIDs) ID(prefix string) (string, error) {
	if f.fail.Load() {
		return "", errors.New("synthetic id failure")
	}
	return f.inner.ID(prefix)
}

func newStoreWorld(t *testing.T) *storeWorld {
	t.Helper()
	admin, dsn := testpg.NewDatabase(t)
	if _, err := db.Migrate(ctx(t), admin, migrations.FS); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{tenantA, tenantB} {
		if _, err := admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ($1, 'T')`, id); err != nil {
			t.Fatal(err)
		}
	}
	clk := clock.NewManual(testEpoch)
	fail := new(atomic.Bool)
	app := &chain.Appender{Clock: clk, IDs: flakyIDs{inner: ids.New(clk, rand.New(rand.NewSource(1))), fail: fail},
		Signers: recordsig.For(t, admin, "investigations", nil, "core-workflows")}
	w := &storeWorld{admin: admin, failing: fail,
		orchA: testpg.TenantLogin(t, admin, dsn, "orchestrator_rw", tenantA), orchB: testpg.TenantLogin(t, admin, dsn, "orchestrator_rw", tenantB)}
	w.inputs = &fakeInputs{refuse: map[string]bool{}}
	w.storeA = &investigations.Store{Pool: w.orchA, Appender: app, Inputs: w.inputs}
	return w
}

var sessA = orchestrator.Session{TenantID: tenantA, PrincipalID: "usr_01"}

func start(inv, ref string) investigations.StartRequest {
	return investigations.StartRequest{InvestigationID: inv, CorrelationID: "corr_AA11", InputReceivedRef: ref}
}

func (w *storeWorld) events(t *testing.T, tenant string) []string {
	t.Helper()
	rows, err := w.admin.Query(ctx(t), `SELECT event_type || '|' || written_by || '|' || correlation_id || '|' || payload::text FROM record_event WHERE tenant_id = $1 ORDER BY seq`, tenant)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

func (w *storeWorld) row(t *testing.T, tenant, inv string) (string, int64) {
	t.Helper()
	var st string
	var v int64
	if err := w.admin.QueryRow(ctx(t), `SELECT state, state_version FROM investigation WHERE tenant_id = $1 AND investigation_id = $2`, tenant, inv).Scan(&st, &v); err != nil {
		t.Fatal(err)
	}
	return st, v
}

// Idempotent start: the same recorded input gives the same investigation, whatever investigation ID the second caller proposes, and says it already existed.
func TestStartFindsTheInvestigationOfARecordedInputAndNeverMakesASecond(t *testing.T) {
	w := newStoreWorld(t)
	first, created, err := w.storeA.Start(ctx(t), sessA, start("inv_AA11", "recev_AA11"))
	if err != nil || !created || first.ID != "inv_AA11" || first.State != "RECEIVED" || first.Version != 0 {
		t.Fatalf("first start: %+v created %v err %v", first, created, err)
	}
	again, created, err := w.storeA.Start(ctx(t), sessA, start("inv_BB22", "recev_AA11"))
	if err != nil || created || again.ID != "inv_AA11" {
		t.Fatalf("second start: %+v created %v err %v; want the first investigation and created false", again, created, err)
	}
	var n int
	if err := w.admin.QueryRow(ctx(t), `SELECT count(*) FROM investigation`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("%d rows (%v), want 1", n, err)
	}
}

// Eight concurrent starts of one recorded input make exactly one investigation, and exactly one caller is told it created it.
func TestConcurrentStartsOfOneInputMakeOneInvestigation(t *testing.T) {
	w := newStoreWorld(t)
	var created atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			inv, c, err := w.storeA.Start(ctx(t), sessA, start("inv_C"+strings.Repeat("A", i+1), "recev_CC11"))
			if err != nil || inv.ID == "" {
				t.Errorf("start %d: %+v %v", i, inv, err)
			}
			if c {
				created.Add(1)
			}
		}(i)
	}
	wg.Wait()
	var n int
	if err := w.admin.QueryRow(ctx(t), `SELECT count(*) FROM investigation WHERE input_received_ref = 'recev_CC11'`).Scan(&n); err != nil || n != 1 || created.Load() != 1 {
		t.Fatalf("%d rows (%v), %d callers told they created it; want 1 and 1", n, err, created.Load())
	}
}

// A request outside its patterns, or a session for a tenant other than the login's, makes nothing.
func TestStartRefusesWhatItCannotHonour(t *testing.T) {
	w := newStoreWorld(t)
	for name, r := range map[string]investigations.StartRequest{
		"no investigation": {CorrelationID: "corr_AA11", InputReceivedRef: "recev_AA11"}, "no correlation": {InvestigationID: "inv_AA11", InputReceivedRef: "recev_AA11"},
		"no recorded input": {InvestigationID: "inv_AA11", CorrelationID: "corr_AA11"}, "a bad input ref": start("inv_AA11", "evt_AA11"),
		"a newline in the id": start("inv_AA\n11", "recev_AA11"),
	} {
		if _, _, err := w.storeA.Start(ctx(t), sessA, r); !errors.Is(err, investigations.ErrRequest) {
			t.Errorf("%s: %v; want ErrRequest", name, err)
		}
	}
	if _, _, err := w.storeA.Start(ctx(t), orchestrator.Session{TenantID: tenantB, PrincipalID: "usr_01"}, start("inv_AA11", "recev_AA11")); err == nil {
		t.Error("a session for another tenant than the login's started an investigation")
	}
	if _, _, err := w.storeA.Start(ctx(t), orchestrator.Session{}, start("inv_AA11", "recev_AA11")); !errors.Is(err, investigations.ErrRequest) {
		t.Errorf("an empty session: %v; want ErrRequest", err)
	}
	var n int
	if err := w.admin.QueryRow(ctx(t), `SELECT count(*) FROM investigation`).Scan(&n); err != nil || n != 0 {
		t.Errorf("%d rows after refusals (%v)", n, err)
	}
}

// Record first and one transaction: a change appends INVESTIGATION_STATE_CHANGED as core-workflows with the investigation's correlation, and the row moves with it.
func TestAChangeAppendsTheEventAndMovesTheRowTogether(t *testing.T) {
	w := newStoreWorld(t)
	if _, _, err := w.storeA.Start(ctx(t), sessA, start("inv_AA11", "recev_AA11")); err != nil {
		t.Fatal(err)
	}
	if err := w.storeA.AppendChange(ctx(t), sessA, orchestrator.Change{ID: "inv_AA11", From: "RECEIVED", To: "INTERPRETING", FromVersion: 0, ToVersion: 1}); err != nil {
		t.Fatal(err)
	}
	if st, v := w.row(t, tenantA, "inv_AA11"); st != "INTERPRETING" || v != 1 {
		t.Fatalf("row %s v%d", st, v)
	}
	ev := w.events(t, tenantA)
	if len(ev) != 1 || !strings.HasPrefix(ev[0], "INVESTIGATION_STATE_CHANGED|core-workflows|corr_AA11|") {
		t.Fatalf("events %v", ev)
	}
	for _, want := range []string{`"from": "RECEIVED"`, `"to": "INTERPRETING"`, `"from_version": 0`, `"to_version": 1`, `"investigation_id": "inv_AA11"`} {
		if !strings.Contains(ev[0], want) {
			t.Errorf("payload %s lacks %s", ev[0], want)
		}
	}
}

// The expected version is part of the compare-and-swap: a change made against a version the row has left, or from a state it is not in, is ErrStaleVersion, and it
// appends no event and moves no row.
func TestAStaleChangeIsRefusedAndWritesNothing(t *testing.T) {
	w := newStoreWorld(t)
	if _, _, err := w.storeA.Start(ctx(t), sessA, start("inv_AA11", "recev_AA11")); err != nil {
		t.Fatal(err)
	}
	if err := w.storeA.AppendChange(ctx(t), sessA, orchestrator.Change{ID: "inv_AA11", From: "RECEIVED", To: "INTERPRETING", FromVersion: 0, ToVersion: 1}); err != nil {
		t.Fatal(err)
	}
	before := len(w.events(t, tenantA))
	for name, ch := range map[string]orchestrator.Change{
		"the version it left":      {ID: "inv_AA11", From: "RECEIVED", To: "INTERPRETING", FromVersion: 0, ToVersion: 1},
		"a version ahead":          {ID: "inv_AA11", From: "INTERPRETING", To: "EVALUATING", FromVersion: 5, ToVersion: 6},
		"a state it is not in":     {ID: "inv_AA11", From: "EVALUATING", To: "EXPLAINING", FromVersion: 1, ToVersion: 2},
		"a skipped version":        {ID: "inv_AA11", From: "INTERPRETING", To: "EVALUATING", FromVersion: 1, ToVersion: 3},
		"an investigation unknown": {ID: "inv_ZZ99", From: "RECEIVED", To: "INTERPRETING", FromVersion: 0, ToVersion: 1},
	} {
		if err := w.storeA.AppendChange(ctx(t), sessA, ch); !errors.Is(err, orchestrator.ErrStaleVersion) && !errors.Is(err, investigations.ErrRequest) {
			t.Errorf("%s: %v; want a stale-version or request refusal", name, err)
		}
	}
	if got := len(w.events(t, tenantA)); got != before {
		t.Errorf("a refused change appended %d event(s)", got-before)
	}
	if st, v := w.row(t, tenantA, "inv_AA11"); st != "INTERPRETING" || v != 1 {
		t.Errorf("row %s v%d after refusals", st, v)
	}
}

// EC-E16, the Record comes first: when the append fails the row does not move, and nothing is left behind.
func TestWhenTheAppendFailsTheRowDoesNotMove(t *testing.T) {
	w := newStoreWorld(t)
	if _, _, err := w.storeA.Start(ctx(t), sessA, start("inv_AA11", "recev_AA11")); err != nil {
		t.Fatal(err)
	}
	w.failing.Store(true)
	if err := w.storeA.AppendChange(ctx(t), sessA, orchestrator.Change{ID: "inv_AA11", From: "RECEIVED", To: "INTERPRETING", FromVersion: 0, ToVersion: 1}); err == nil {
		t.Fatal("a change was accepted although the Record append failed")
	}
	w.failing.Store(false)
	if st, v := w.row(t, tenantA, "inv_AA11"); st != "RECEIVED" || v != 0 {
		t.Fatalf("row %s v%d after a failed append, want RECEIVED v0", st, v)
	}
	if got := len(w.events(t, tenantA)); got != 0 {
		t.Fatalf("%d events after a failed append", got)
	}
	if err := w.storeA.AppendChange(ctx(t), sessA, orchestrator.Change{ID: "inv_AA11", From: "RECEIVED", To: "INTERPRETING", FromVersion: 0, ToVersion: 1}); err != nil {
		t.Fatalf("the same change after the fault cleared: %v", err)
	}
}

// Two writers racing on one version: exactly one wins, the other gets ErrStaleVersion and appends nothing.
func TestTwoChangesFromOneVersionHaveOneWinner(t *testing.T) {
	w := newStoreWorld(t)
	if _, _, err := w.storeA.Start(ctx(t), sessA, start("inv_AA11", "recev_AA11")); err != nil {
		t.Fatal(err)
	}
	var won, stale atomic.Int32
	var wg sync.WaitGroup
	for _, to := range []orchestrator.State{"INTERPRETING", "INTERPRETING", "INTERPRETING", "INTERPRETING"} {
		wg.Add(1)
		go func(to orchestrator.State) {
			defer wg.Done()
			err := w.storeA.AppendChange(ctx(t), sessA, orchestrator.Change{ID: "inv_AA11", From: "RECEIVED", To: to, FromVersion: 0, ToVersion: 1})
			switch {
			case err == nil:
				won.Add(1)
			case errors.Is(err, orchestrator.ErrStaleVersion):
				stale.Add(1)
			default:
				t.Errorf("unexpected: %v", err)
			}
		}(to)
	}
	wg.Wait()
	if won.Load() != 1 || stale.Load() != 3 || len(w.events(t, tenantA)) != 1 {
		t.Fatalf("won %d, stale %d, events %d; want 1, 3, 1", won.Load(), stale.Load(), len(w.events(t, tenantA)))
	}
}

// Tenant isolation: a login for tenant B neither finds tenant A's investigation nor moves it.
func TestAnotherTenantsLoginNeitherFindsNorMovesAnInvestigation(t *testing.T) {
	w := newStoreWorld(t)
	if _, _, err := w.storeA.Start(ctx(t), sessA, start("inv_AA11", "recev_AA11")); err != nil {
		t.Fatal(err)
	}
	storeB := &investigations.Store{Pool: w.orchB, Appender: w.storeA.Appender, Inputs: w.inputs}
	err := storeB.AppendChange(ctx(t), orchestrator.Session{TenantID: tenantB, PrincipalID: "usr_01"}, orchestrator.Change{ID: "inv_AA11", From: "RECEIVED", To: "INTERPRETING", FromVersion: 0, ToVersion: 1})
	if err == nil {
		t.Fatal("tenant B moved tenant A's investigation")
	}
	if st, v := w.row(t, tenantA, "inv_AA11"); st != "RECEIVED" || v != 0 {
		t.Fatalf("row %s v%d", st, v)
	}
}

// The store is a Recorder for the runner.
var _ orchestrator.Recorder = (*investigations.Store)(nil)

// Reviewer P1 on noema#490: an investigation is made only for an input the Record confirms (an INPUT_RECEIVED of this tenant, principal and correlation, A161). The
// confirmation is asked with the session and the request's reference and correlation, before anything is inserted; a reference it does not confirm makes no row.
func TestStartMakesNoInvestigationForAnInputTheRecordDoesNotConfirm(t *testing.T) {
	w := newStoreWorld(t)
	w.inputs.refuse["recev_FA11"] = true
	if _, _, err := w.storeA.Start(ctx(t), sessA, start("inv_AA11", "recev_FA11")); !errors.Is(err, investigations.ErrNoRecordedInput) {
		t.Fatalf("a fabricated reference: %v; want ErrNoRecordedInput", err)
	}
	var n int
	if err := w.admin.QueryRow(ctx(t), `SELECT count(*) FROM investigation`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("%d rows after a refused start (%v)", n, err)
	}
	if _, _, err := w.storeA.Start(ctx(t), sessA, start("inv_AA11", "recev_AA11")); err != nil {
		t.Fatal(err)
	}
	if got := w.inputs.asked[len(w.inputs.asked)-1]; got != "ten_aa|usr_01|recev_AA11|corr_AA11" {
		t.Errorf("the confirmation was asked %q; want the session's tenant and principal and the request's reference and correlation", got)
	}
}

// Reviewer P2 on noema#490: reuse of a recorded input with another correlation is refused, not silently joined to the old investigation.
func TestStartRefusesAReuseOfARecordedInputWithAnotherCorrelation(t *testing.T) {
	w := newStoreWorld(t)
	if _, _, err := w.storeA.Start(ctx(t), sessA, start("inv_AA11", "recev_AA11")); err != nil {
		t.Fatal(err)
	}
	other := start("inv_AA22", "recev_AA11")
	other.CorrelationID = "corr_ZZ99"
	if _, _, err := w.storeA.Start(ctx(t), sessA, other); !errors.Is(err, investigations.ErrRequest) {
		t.Fatalf("another correlation: %v; want ErrRequest", err)
	}
	if again, created, err := w.storeA.Start(ctx(t), sessA, start("inv_AA33", "recev_AA11")); err != nil || created || again.ID != "inv_AA11" {
		t.Fatalf("the same correlation still finds the first: %+v created %v err %v", again, created, err)
	}
}

// A store with no way to confirm an input is not configured: it makes nothing.
func TestAStoreWithNoInputsPortMakesNothing(t *testing.T) {
	w := newStoreWorld(t)
	s := &investigations.Store{Pool: w.orchA, Appender: w.storeA.Appender}
	if _, _, err := s.Start(ctx(t), sessA, start("inv_AA11", "recev_AA11")); !errors.Is(err, investigations.ErrRequest) {
		t.Fatalf("%v; want ErrRequest", err)
	}
}

type fakeLookup struct {
	r   investigations.Recorded
	err error
}

func (f fakeLookup) Lookup(context.Context, string, string) (investigations.Recorded, error) {
	return f.r, f.err
}

// ConfirmWith turns a Record lookup into a confirmation: the event must be an INPUT_RECEIVED of the session's tenant and principal and the request's correlation.
func TestConfirmWithComparesTheRecordedEventWithTheSessionAndTheRequest(t *testing.T) {
	good := investigations.Recorded{EventType: "INPUT_RECEIVED", TenantID: tenantA, PrincipalID: "usr_01", CorrelationID: "corr_AA11"}
	ask := func(l fakeLookup) error {
		return investigations.ConfirmWith(l).Confirm(ctx(t), sessA, "recev_AA11", "corr_AA11")
	}
	if err := ask(fakeLookup{r: good}); err != nil {
		t.Fatalf("a matching event: %v", err)
	}
	bad := map[string]fakeLookup{"another type": {r: good}, "another tenant": {r: good}, "another principal": {r: good}, "another correlation": {r: good}, "no principal": {r: good}, "a lookup failure": {r: good, err: errors.New("down")}}
	b := bad["another type"]
	b.r.EventType = "INPUT_DEDUPLICATED"
	bad["another type"] = b
	b = bad["another tenant"]
	b.r.TenantID = tenantB
	bad["another tenant"] = b
	b = bad["another principal"]
	b.r.PrincipalID = "usr_02"
	bad["another principal"] = b
	b = bad["another correlation"]
	b.r.CorrelationID = "corr_BB22"
	bad["another correlation"] = b
	b = bad["no principal"]
	b.r.PrincipalID = ""
	bad["no principal"] = b
	for name, l := range bad {
		if err := ask(l); !errors.Is(err, investigations.ErrNoRecordedInput) {
			t.Errorf("%s: %v; want ErrNoRecordedInput", name, err)
		}
	}
}
