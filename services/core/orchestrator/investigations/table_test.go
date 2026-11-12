package investigations_test

// STM-01 slice 2, the investigation table (REQ-STM-001, REQ-SEC-004, INV-12; SR's note 2 on noema#264: the expected version is part of the compare-and-swap).
// Migration 0025 stores one row per investigation, keyed by (tenant, investigation) and unique on (tenant, input_received_ref), so a second Start for one recorded
// input finds the first. The orchestrator's own login (power "orchestrator") writes it; the tenant of a row is the login's (row-level security with FORCE); a state is
// one of the machine's; a new row is RECEIVED at version 0; a row moves by exactly one version at a time and nothing else about it changes; no row is deleted. These
// tests run in SQL only (no Record event), so they run without the signing extension. All data is SYNTHETIC.

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/migrations"
	"github.com/Noemai-org/noema/services/core/orchestrator"
	"github.com/Noemai-org/noema/services/core/platform/db"
)

func TestMain(m *testing.M) { testpg.Main(m) }

const tenantA, tenantB = "ten_aa", "ten_bb"

func ctx(t testing.TB) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	return c
}

type sqlWorld struct {
	admin        *pgxpool.Pool
	orchA, orchB *pgxpool.Pool
	trustA       *pgxpool.Pool
}

func newSQLWorld(t *testing.T) *sqlWorld {
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
	return &sqlWorld{admin: admin, orchA: testpg.TenantLogin(t, admin, dsn, "orchestrator_rw", tenantA), orchB: testpg.TenantLogin(t, admin, dsn, "orchestrator_rw", tenantB),
		trustA: testpg.TenantLogin(t, admin, dsn, "trust_rw", tenantA)}
}

const insertRow = `INSERT INTO investigation (tenant_id, investigation_id, input_received_ref, correlation_id, state, state_version) VALUES ($1, $2, $3, $4, $5, $6)`

func (w *sqlWorld) insert(t *testing.T, p *pgxpool.Pool, tenant, inv, ref, state string, version int64) error {
	t.Helper()
	_, err := p.Exec(ctx(t), insertRow, tenant, inv, ref, "corr_AA11", state, version)
	return err
}

// The CHECK on state lists exactly the machine's states: a state added to transitions.yaml without a migration, or the other way round, fails here.
func TestTheStatesTheTableAdmitsAreExactlyTheMachinesStates(t *testing.T) {
	w := newSQLWorld(t)
	var def string
	if err := w.admin.QueryRow(ctx(t), `SELECT pg_get_constraintdef(c.oid) FROM pg_constraint c WHERE c.conrelid = 'public.investigation'::regclass AND c.contype = 'c'
		AND pg_get_constraintdef(c.oid) LIKE '%state = ANY%' OR pg_get_constraintdef(c.oid) LIKE '%state%IN%'`).Scan(&def); err != nil {
		t.Fatalf("no CHECK on state: %v", err)
	}
	got := regexp.MustCompile(`'([A-Z_]+)'`).FindAllStringSubmatch(def, -1)
	var in []string
	for _, m := range got {
		in = append(in, m[1])
	}
	tab, err := orchestrator.LoadTable()
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	for _, s := range tab.States() {
		want = append(want, string(s))
	}
	sort.Strings(in)
	sort.Strings(want)
	if strings.Join(in, ",") != strings.Join(want, ",") {
		t.Fatalf("table admits %v, machine has %v", in, want)
	}
}

// A new investigation is RECEIVED at version 0: any other state or version is refused, so a row cannot be created in the middle of its life.
func TestANewRowIsReceivedAtVersionZero(t *testing.T) {
	w := newSQLWorld(t)
	if err := w.insert(t, w.orchA, tenantA, "inv_AA11", "recev_AA11", "RECEIVED", 0); err != nil {
		t.Fatalf("a new row: %v", err)
	}
	for name, c := range map[string]struct {
		state   string
		version int64
	}{"another state": {"COMPLETED", 0}, "another version": {"RECEIVED", 1}, "a state the machine does not have": {"FAILED", 0}, "a negative version": {"RECEIVED", -1}} {
		if err := w.insert(t, w.orchA, tenantA, "inv_AA22", "recev_AA22", c.state, c.version); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// Idempotent start: one recorded input is one investigation in a tenant; the same reference in another tenant is another input.
func TestOneRecordedInputIsOneInvestigationInATenant(t *testing.T) {
	w := newSQLWorld(t)
	if err := w.insert(t, w.orchA, tenantA, "inv_AA11", "recev_AA11", "RECEIVED", 0); err != nil {
		t.Fatal(err)
	}
	if err := w.insert(t, w.orchA, tenantA, "inv_AA99", "recev_AA11", "RECEIVED", 0); err == nil {
		t.Error("a second investigation for the same recorded input was accepted")
	}
	if err := w.insert(t, w.orchA, tenantA, "inv_AA11", "recev_AA33", "RECEIVED", 0); err == nil {
		t.Error("a second row with the same investigation ID was accepted")
	}
	if err := w.insert(t, w.orchB, tenantB, "inv_AA11", "recev_AA11", "RECEIVED", 0); err != nil {
		t.Errorf("the same IDs in another tenant: %v", err)
	}
}

// A row moves by exactly one version, to a state of the machine, and only the state, the version and the time can change.
func TestARowMovesByExactlyOneVersionAndNothingElseChanges(t *testing.T) {
	w := newSQLWorld(t)
	if err := w.insert(t, w.orchA, tenantA, "inv_AA11", "recev_AA11", "RECEIVED", 0); err != nil {
		t.Fatal(err)
	}
	move := func(to string, from, version int64) (int64, error) {
		tag, err := w.orchA.Exec(ctx(t), `UPDATE investigation SET state = $1, state_version = $2, updated_at = now() WHERE tenant_id = $3 AND investigation_id = $4 AND state_version = $5`,
			to, version, tenantA, "inv_AA11", from)
		return tag.RowsAffected(), err
	}
	if n, err := move("INTERPRETING", 0, 1); err != nil || n != 1 {
		t.Fatalf("the next version: %d rows, %v", n, err)
	}
	for name, c := range map[string]struct {
		to            string
		from, version int64
	}{"a skipped version": {"EVALUATING", 1, 3}, "the same version": {"EVALUATING", 1, 1}, "a lower version": {"EVALUATING", 1, 0}, "a state the machine does not have": {"FAILED", 1, 2}} {
		if _, err := move(c.to, c.from, c.version); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// the compare-and-swap: a stale expected version updates no row
	if n, err := move("EVALUATING", 0, 1); err != nil || n != 0 {
		t.Errorf("a stale expected version: %d rows, %v; want none", n, err)
	}
	for name, stmt := range map[string]string{
		"the recorded input": `UPDATE investigation SET input_received_ref = 'recev_ZZ99' WHERE investigation_id = 'inv_AA11'`,
		"the correlation":    `UPDATE investigation SET correlation_id = 'corr_ZZ99' WHERE investigation_id = 'inv_AA11'`,
		"the investigation":  `UPDATE investigation SET investigation_id = 'inv_ZZ99' WHERE investigation_id = 'inv_AA11'`,
		"the tenant":         `UPDATE investigation SET tenant_id = 'ten_bb' WHERE investigation_id = 'inv_AA11'`,
	} {
		if _, err := w.orchA.Exec(ctx(t), stmt); err == nil {
			t.Errorf("a login changed %s", name)
		}
	}
	for name, stmt := range map[string]string{"delete": `DELETE FROM investigation`, "truncate": `TRUNCATE investigation`} {
		if _, err := w.orchA.Exec(ctx(t), stmt); err == nil {
			t.Errorf("%s was accepted", name)
		}
		if _, err := w.admin.Exec(ctx(t), stmt); err == nil {
			t.Errorf("%s by the administrator was accepted", name)
		}
	}
}

// Row-level security with FORCE: a login sees and writes only its own tenant's rows; no other power's login touches the table.
func TestATenantSeesOnlyItsOwnRowsAndOnlyTheOrchestratorTouchesThem(t *testing.T) {
	w := newSQLWorld(t)
	if err := w.insert(t, w.orchA, tenantA, "inv_AA11", "recev_AA11", "RECEIVED", 0); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := w.orchB.QueryRow(ctx(t), `SELECT count(*) FROM investigation`).Scan(&n); err != nil || n != 0 {
		t.Errorf("tenant B sees %d rows of tenant A (%v)", n, err)
	}
	if err := w.insert(t, w.orchB, tenantA, "inv_BB11", "recev_BB11", "RECEIVED", 0); err == nil {
		t.Error("tenant B's login wrote a row for tenant A")
	}
	if tag, err := w.orchB.Exec(ctx(t), `UPDATE investigation SET state = 'INTERPRETING', state_version = 1 WHERE investigation_id = 'inv_AA11'`); err != nil || tag.RowsAffected() != 0 {
		t.Errorf("tenant B's login moved tenant A's row: %d rows, %v", tag.RowsAffected(), err)
	}
	if err := w.trustA.QueryRow(ctx(t), `SELECT count(*) FROM investigation`).Scan(&n); err == nil {
		t.Error("the Trust login can read the investigation table")
	}
	if err := w.insert(t, w.trustA, tenantA, "inv_TT11", "recev_TT11", "RECEIVED", 0); err == nil {
		t.Error("the Trust login can write the investigation table")
	}
	var forced bool
	if err := w.admin.QueryRow(ctx(t), `SELECT relrowsecurity AND relforcerowsecurity FROM pg_class WHERE oid = 'public.investigation'::regclass`).Scan(&forced); err != nil || !forced {
		t.Errorf("row-level security is not enabled and forced (%v)", err)
	}
}
