package recoverypg_test

// A696 (STM-05; REQ-STM-001, REQ-RCV-003, REQ-SEC-004; INV-03, INV-12; A476, A477, A490): the recovery pass's Postgres reader. Real PostgreSQL and real per-tenant orchestrator_rw
// logins under forced row-level security; the Trust side is a double that answers a recorded Decision. Idleness is the DATABASE's clock. All data is SYNTHETIC (AGENT-25).

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/migrations"
	"github.com/Noemai-org/noema/services/core/orchestrator"
	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/recoverypg"
	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
	"github.com/Noemai-org/noema/services/core/orchestrator/recovery"
	"github.com/Noemai-org/noema/services/core/platform/db"
)

const (
	tenantA = "ten_aa"
	tenantB = "ten_bb"
	timeout = 20 * time.Second
)

var inFlight = []orchestrator.State{"PLANNED", "EXECUTING", "AWAITING_VERIFICATION"}

func ctx(t testing.TB) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	return c
}

// fakeDecisions answers a recorded ALLOW for the correlations it holds and ErrNoRecord for the rest; err, when set, answers every question.
type fakeDecisions struct {
	mu    sync.Mutex
	by    map[string]orchestrator.Decision
	err   error
	asked []string
}

func (f *fakeDecisions) AllowOf(_ context.Context, tenantID, correlationID string) (orchestrator.Decision, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, tenantID+"|"+correlationID)
	if f.err != nil {
		return orchestrator.Decision{}, f.err
	}
	if d, ok := f.by[tenantID+"|"+correlationID]; ok {
		return d, nil
	}
	return orchestrator.Decision{}, trustport.ErrNoRecord
}

type world struct {
	admin        *pgxpool.Pool
	orchA, orchB *pgxpool.Pool
	dec          *fakeDecisions
	reader       *recoverypg.Reader
}

func newWorld(t *testing.T) *world {
	t.Helper()
	admin, dsn := testpg.NewDatabase(t)
	if _, err := db.Migrate(ctx(t), admin, migrations.FS); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{tenantA, tenantB} {
		if _, err := admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ($1, $1)`, id); err != nil {
			t.Fatal(err)
		}
	}
	w := &world{admin: admin, dec: &fakeDecisions{by: map[string]orchestrator.Decision{}},
		orchA: testpg.TenantLogin(t, admin, dsn, "orchestrator_rw", tenantA), orchB: testpg.TenantLogin(t, admin, dsn, "orchestrator_rw", tenantB)}
	var err error
	w.reader, err = recoverypg.New(map[string]*pgxpool.Pool{tenantA: w.orchA, tenantB: w.orchB}, w.dec, inFlight, timeout)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// seed inserts one investigation as the administrator at the given state and age (seconds before the database's now()).
func (w *world) seed(t *testing.T, tenant, inv, state string, ageSeconds int) {
	t.Helper()
	suffix := inv[len(inv)-3:]
	if _, err := w.admin.Exec(ctx(t), `INSERT INTO investigation (tenant_id, investigation_id, input_received_ref, correlation_id, state, state_version, principal_ref, principal_role, authn_ref)
		VALUES ($1, $2, $3, $4, 'RECEIVED', 0, 'usr_01', 'customer', 'ses_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa')`, tenant, inv, "recev_"+suffix, "corr_"+suffix); err != nil {
		t.Fatal(err)
	}
	if _, err := w.admin.Exec(ctx(t), `UPDATE investigation SET state = $3, state_version = 1, updated_at = now() - make_interval(secs => $4) WHERE tenant_id = $1 AND investigation_id = $2`,
		tenant, inv, state, ageSeconds); err != nil {
		t.Fatal(err)
	}
}

func ids(cs []recovery.Candidate) []string {
	var out []string
	for _, c := range cs {
		out = append(out, c.Investigation.ID)
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// REQ-STM-001, REQ-SEC-004: only the tenant's own idle rows in a configured state are listed, the oldest first; a row that is young, terminal, in another state or another tenant's is not.
func TestIdleListsOnlyTheTenantsIdleRowsInTheConfiguredStatesOldestFirst(t *testing.T) {
	w := newWorld(t)
	w.seed(t, tenantA, "inv_A01", "PLANNED", 600)
	w.seed(t, tenantA, "inv_A02", "PLANNED", 1)
	w.seed(t, tenantA, "inv_A03", "COMPLETED", 900)
	w.seed(t, tenantA, "inv_A04", "AWAITING_CLARIFICATION", 900)
	w.seed(t, tenantA, "inv_A05", "EXECUTING", 1200)
	w.seed(t, tenantB, "inv_B01", "PLANNED", 5000)
	got, err := w.reader.Idle(ctx(t), tenantA, 300000, 10)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"inv_A05", "inv_A01"}; !equal(ids(got), want) {
		t.Fatalf("got %v; want %v", ids(got), want)
	}
	c := got[1]
	if c.TenantID != tenantA || c.CorrelationID != "corr_A01" || c.InputReceivedRef != "recev_A01" || c.PrincipalRef != "usr_01" ||
		c.Investigation.State != "PLANNED" || c.Investigation.Version != 1 {
		t.Fatalf("candidate %+v: not the stored row", c)
	}
	gotB, err := w.reader.Idle(ctx(t), tenantB, 300000, 10)
	if err != nil || !equal(ids(gotB), []string{"inv_B01"}) {
		t.Fatalf("tenant B: got %v, %v; want only its own row", ids(gotB), err)
	}
}

// AGENT-16, A477: the batch is a bound, the oldest rows win; the idle age is the argument, measured by the database's clock.
func TestTheBatchIsABoundAndTheIdleAgeIsTheArgumentOnTheDatabaseClock(t *testing.T) {
	w := newWorld(t)
	w.seed(t, tenantA, "inv_A01", "PLANNED", 100)
	w.seed(t, tenantA, "inv_A02", "PLANNED", 200)
	w.seed(t, tenantA, "inv_A03", "PLANNED", 300)
	got, err := w.reader.Idle(ctx(t), tenantA, 50000, 2)
	if err != nil || !equal(ids(got), []string{"inv_A03", "inv_A02"}) {
		t.Fatalf("limit 2: got %v, %v; want the two oldest", ids(got), err)
	}
	got, err = w.reader.Idle(ctx(t), tenantA, 250000, 10)
	if err != nil || !equal(ids(got), []string{"inv_A03"}) {
		t.Fatalf("idle 250 s: got %v, %v; want only the 300 s row", ids(got), err)
	}
	got, err = w.reader.Idle(ctx(t), tenantA, 1000000, 10)
	if err != nil || len(got) != 0 {
		t.Fatalf("idle 1000 s: got %v, %v; want nothing", ids(got), err)
	}
}

// INV-03, A490: each candidate carries the decision Trust recorded for its correlation (grants and intent reference as Trust gave them); a correlation with no ALLOW is a candidate
// with no decision, which recovery sends to a person.
func TestACandidateCarriesTheRecordedDecisionOrNone(t *testing.T) {
	w := newWorld(t)
	w.seed(t, tenantA, "inv_A01", "PLANNED", 600)
	w.seed(t, tenantA, "inv_A02", "PLANNED", 700)
	want := orchestrator.Decision{ID: "dec_01ABC", Outcome: contracts.EnumsV1DecisionOutcomeALLOW, GrantIDs: []string{"grt_01ABC"}, IntentRef: "int_01J9X3@2"}
	w.dec.by[tenantA+"|corr_A01"] = want
	got, err := w.reader.Idle(ctx(t), tenantA, 300000, 10)
	if err != nil || len(got) != 2 {
		t.Fatalf("got %v, %v", ids(got), err)
	}
	byID := map[string]recovery.Candidate{got[0].Investigation.ID: got[0], got[1].Investigation.ID: got[1]}
	if d := byID["inv_A01"].Decision; d.ID != want.ID || len(d.GrantIDs) != 1 || d.GrantIDs[0] != "grt_01ABC" || d.IntentRef != want.IntentRef || d.Outcome != want.Outcome {
		t.Fatalf("decision %+v; want %+v", d, want)
	}
	if d := byID["inv_A02"].Decision; d.ID != "" || len(d.GrantIDs) != 0 {
		t.Fatalf("no ALLOW on the Record, but the candidate carries %+v", d)
	}
	for _, q := range w.dec.asked {
		if !strings.HasPrefix(q, tenantA+"|") {
			t.Fatalf("the Trust side was asked as %q: only the pass's tenant", q)
		}
	}
}

// A read that cannot be answered is an error and no candidates: never "none" (a recovery pass that sees none would leave a stuck run alone).
func TestAReadThatFailsIsAnErrorAndNeverNone(t *testing.T) {
	w := newWorld(t)
	w.seed(t, tenantA, "inv_A01", "PLANNED", 600)
	w.dec.err = errors.New("synthetic: the Trust side cannot answer")
	if got, err := w.reader.Idle(ctx(t), tenantA, 300000, 10); err == nil || len(got) != 0 {
		t.Fatalf("a failing Trust read: got %v, %v; want an error and no candidates", ids(got), err)
	}
	w.dec.err = nil
	w.orchA.Close()
	got, err := w.reader.Idle(ctx(t), tenantA, 300000, 10)
	if err == nil || len(got) != 0 {
		t.Fatalf("a closed pool: got %v, %v; want an error and no candidates", ids(got), err)
	}
	if !strings.Contains(err.Error(), "recoverypg:") || strings.Contains(strings.ToLower(err.Error()), "postgres") || strings.Contains(err.Error(), "127.0.0.1") {
		t.Fatalf("the error text must be fixed and name no server: %q", err.Error())
	}
}

// A tenant with no login is ErrNoPool and never falls back to another tenant's login (INV-12).
func TestATenantWithNoLoginIsRefusedAndNeverFallsBack(t *testing.T) {
	w := newWorld(t)
	w.seed(t, tenantA, "inv_A01", "PLANNED", 600)
	got, err := w.reader.Idle(ctx(t), "ten_zz", 300000, 10)
	if !errors.Is(err, recoverypg.ErrNoPool) || len(got) != 0 {
		t.Fatalf("got %v, %v; want ErrNoPool", ids(got), err)
	}
}

// The arguments are checked before any query: no deadline, a tenant outside its pattern, an idle age or a limit below one.
func TestAnIncompleteQuestionIsRefusedBeforeAnyQuery(t *testing.T) {
	w := newWorld(t)
	w.seed(t, tenantA, "inv_A01", "PLANNED", 600)
	for name, f := range map[string]func() ([]recovery.Candidate, error){
		"no deadline":  func() ([]recovery.Candidate, error) { return w.reader.Idle(context.Background(), tenantA, 300000, 10) },
		"bad tenant":   func() ([]recovery.Candidate, error) { return w.reader.Idle(ctx(t), "tenant A", 300000, 10) },
		"zero idle":    func() ([]recovery.Candidate, error) { return w.reader.Idle(ctx(t), tenantA, 0, 10) },
		"negative age": func() ([]recovery.Candidate, error) { return w.reader.Idle(ctx(t), tenantA, -1, 10) },
		"zero limit":   func() ([]recovery.Candidate, error) { return w.reader.Idle(ctx(t), tenantA, 300000, 0) },
	} {
		if got, err := f(); err == nil || len(got) != 0 {
			t.Errorf("%s: got %v, %v; want an error", name, ids(got), err)
		}
	}
	if len(w.dec.asked) != 0 {
		t.Errorf("the Trust side was asked %v for a refused question", w.dec.asked)
	}
}

// The reader is built whole or not at all: no login, no Trust side, no states, an unknown state, a terminal state, a duplicate state or no timeout is ErrConfig.
func TestAReaderMissingAPartIsNotBuilt(t *testing.T) {
	w := newWorld(t)
	pools := map[string]*pgxpool.Pool{tenantA: w.orchA}
	for name, f := range map[string]func() (*recoverypg.Reader, error){
		"no pools": func() (*recoverypg.Reader, error) { return recoverypg.New(nil, w.dec, inFlight, timeout) },
		"nil pool": func() (*recoverypg.Reader, error) {
			return recoverypg.New(map[string]*pgxpool.Pool{tenantA: nil}, w.dec, inFlight, timeout)
		},
		"no trust side": func() (*recoverypg.Reader, error) { return recoverypg.New(pools, nil, inFlight, timeout) },
		"no states":     func() (*recoverypg.Reader, error) { return recoverypg.New(pools, w.dec, nil, timeout) },
		"unknown state": func() (*recoverypg.Reader, error) {
			return recoverypg.New(pools, w.dec, []orchestrator.State{"NOT_A_STATE"}, timeout)
		},
		"terminal": func() (*recoverypg.Reader, error) {
			return recoverypg.New(pools, w.dec, []orchestrator.State{"COMPLETED"}, timeout)
		},
		"duplicate": func() (*recoverypg.Reader, error) {
			return recoverypg.New(pools, w.dec, []orchestrator.State{"PLANNED", "PLANNED"}, timeout)
		},
		"no timeout": func() (*recoverypg.Reader, error) { return recoverypg.New(pools, w.dec, inFlight, 0) },
	} {
		if r, err := f(); !errors.Is(err, recoverypg.ErrConfig) || r != nil {
			t.Errorf("%s: got %v, %v; want ErrConfig and no reader", name, r, err)
		}
	}
}

// The reader writes nothing and decides nothing: its source has no statement that changes a row, and it imports neither the Trust nor the Execution internals.
func TestTheReaderSourceWritesNothingAndImportsNoPower(t *testing.T) {
	src, err := os.ReadFile("reader.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"INSERT ", "UPDATE ", "DELETE ", "TRUNCATE", ".Exec(", "/trust/", "/execution/", "/outcome/"} {
		if strings.Contains(string(src), bad) {
			t.Errorf("reader.go contains %q", bad)
		}
	}
	if !strings.Contains(string(src), "pgx.ReadOnly") {
		t.Error("reader.go must read in a read-only transaction")
	}
}
