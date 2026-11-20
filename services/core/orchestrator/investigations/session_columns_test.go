package investigations_test

// STM-05 (A441, interim, relayed, not yet committed; REQ-STM-001, REQ-SEC-004; INV-12, INV-30). The investigation row carries principal_role and authn_ref, written at
// creation from the VERIFIED session and never from a request or a model, so recovery (noema#789) can rebuild the session from the stored row. Migration 0036 adds the
// columns: a row from before it holds NULL in both (nothing is invented for it), and every new row must carry both, by a BEFORE INSERT trigger and by CHECKs on the form.
// The database tests need Postgres and the signing extension, so they run in CI only. All data is SYNTHETIC.

import (
	"errors"
	"io/fs"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/migrations"
	"github.com/Noemai-org/noema/services/core/orchestrator"
	"github.com/Noemai-org/noema/services/core/orchestrator/investigations"
	"github.com/Noemai-org/noema/services/core/platform/db"
)

const insertSession = `INSERT INTO investigation (tenant_id, investigation_id, input_received_ref, correlation_id, state, state_version, principal_ref, principal_role, authn_ref)
	VALUES ($1, $2, $3, 'corr_AA11', 'RECEIVED', 0, 'usr_01', $4, $5)`

// The database refuses a new row without a role and an authn reference, with one without the other, and with either outside its form (REQ-STM-001; INV-12).
func TestANewRowNeedsAPrincipalRoleAndAnAuthnRefOfTheirForm(t *testing.T) {
	w := newSQLWorld(t)
	for i, role := range []string{"customer", "ops", "approver", "audit"} {
		if _, err := w.orchA.Exec(ctx(t), insertSession, tenantA, "inv_OK"+string(rune('A'+i)), "recev_OK"+string(rune('A'+i)), role, authnA); err != nil {
			t.Errorf("role %s: %v", role, err)
		}
	}
	for name, c := range map[string]struct{ role, authn any }{
		"no role and no authn_ref":     {nil, nil},
		"a role without an authn_ref":  {"customer", nil},
		"an authn_ref without a role":  {nil, authnA},
		"an empty role":                {"", authnA},
		"a principal type, not a role": {"operator", authnA},
		"an unknown role":              {"admin", authnA},
		"a role in capitals":           {"Customer", authnA},
		"an empty authn_ref":           {"customer", ""},
		"a token, not a reference":     {"customer", "a.bearer.credential"},
		"another prefix":               {"customer", "tok_0123456789abcdef0123456789abcdef"},
		"too short":                    {"customer", "ses_a"},
		"too long":                     {"customer", "ses_" + strings.Repeat("a", 33)},
		"a space in it":                {"customer", "ses_0123456789 bcdef"},
	} {
		if _, err := w.orchA.Exec(ctx(t), insertSession, tenantA, "inv_BAD1", "recev_BAD1", c.role, c.authn); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	var n int
	if err := w.admin.QueryRow(ctx(t), `SELECT count(*) FROM investigation WHERE investigation_id = 'inv_BAD1'`).Scan(&n); err != nil || n != 0 {
		t.Errorf("%d refused rows remain (%v)", n, err)
	}
	if _, err := w.admin.Exec(ctx(t), `INSERT INTO investigation (tenant_id, investigation_id, input_received_ref, correlation_id, state, state_version, principal_ref)
		VALUES ($1, 'inv_BAD2', 'recev_BAD2', 'corr_AA11', 'RECEIVED', 0, 'usr_01')`, tenantA); err == nil {
		t.Error("the administrator wrote a new row without the two columns")
	}
}

// Neither column changes after the insert, for the orchestrator's login: they are not in its column-limited UPDATE grant (0025).
func TestThePrincipalRoleAndTheAuthnRefNeverChangeAfterTheInsert(t *testing.T) {
	w := newSQLWorld(t)
	if _, err := w.orchA.Exec(ctx(t), insertSession, tenantA, "inv_AA11", "recev_AA11", "customer", authnA); err != nil {
		t.Fatal(err)
	}
	for name, stmt := range map[string]string{
		"the role":      `UPDATE investigation SET principal_role = 'ops' WHERE investigation_id = 'inv_AA11'`,
		"the authn_ref": `UPDATE investigation SET authn_ref = 'ses_ffffffffffffffffffffffffffffffff' WHERE investigation_id = 'inv_AA11'`,
	} {
		if _, err := w.orchA.Exec(ctx(t), stmt); err == nil {
			t.Errorf("the orchestrator's login changed %s", name)
		}
	}
	var role, ref string
	if err := w.orchA.QueryRow(ctx(t), `SELECT principal_role, authn_ref FROM investigation WHERE investigation_id = 'inv_AA11'`).Scan(&role, &ref); err != nil || role != "customer" || ref != authnA {
		t.Errorf("row holds %q %q (%v)", role, ref, err)
	}
	if _, err := w.orchA.Exec(ctx(t), `UPDATE investigation SET state = 'INTERPRETING', state_version = 1, updated_at = now() WHERE investigation_id = 'inv_AA11'`); err != nil {
		t.Errorf("a move of the row after the columns exist: %v", err)
	}
}

// A row from before 0036 holds NULL in both columns and is not rewritten or invented; it still moves; a row made after it must carry both (the safer reading of A441).
func TestARowFromBefore0036KeepsNullAndStillMovesWhileNewRowsNeedBoth(t *testing.T) {
	admin := newLegacyAdmin(t)
	if _, err := admin.Exec(ctx(t), `INSERT INTO investigation (tenant_id, investigation_id, input_received_ref, correlation_id, state, state_version, principal_ref)
		VALUES ($1, 'inv_OLD1', 'recev_OLD1', 'corr_AA11', 'RECEIVED', 0, 'usr_01')`, tenantA); err != nil {
		t.Fatalf("a row before 0036: %v", err)
	}
	if _, err := db.Migrate(ctx(t), admin, migrations.FS); err != nil {
		t.Fatal(err)
	}
	var role, ref *string
	if err := admin.QueryRow(ctx(t), `SELECT principal_role, authn_ref FROM investigation WHERE investigation_id = 'inv_OLD1'`).Scan(&role, &ref); err != nil || role != nil || ref != nil {
		t.Fatalf("a legacy row holds %v %v (%v); want NULL and NULL", role, ref, err)
	}
	if _, err := admin.Exec(ctx(t), `UPDATE investigation SET state = 'INTERPRETING', state_version = 1 WHERE investigation_id = 'inv_OLD1'`); err != nil {
		t.Errorf("a legacy row cannot move: %v", err)
	}
	if _, err := admin.Exec(ctx(t), `UPDATE investigation SET principal_role = 'ops', state_version = state_version + 1 WHERE investigation_id = 'inv_OLD1'`); err == nil {
		t.Error("a role without an authn reference was set on a legacy row")
	}
	if _, err := admin.Exec(ctx(t), `INSERT INTO investigation (tenant_id, investigation_id, input_received_ref, correlation_id, state, state_version, principal_ref)
		VALUES ($1, 'inv_NEW1', 'recev_NEW1', 'corr_AA11', 'RECEIVED', 0, 'usr_01')`, tenantA); err == nil {
		t.Error("a new row without the two columns was accepted after 0036")
	}
}

// newLegacyAdmin returns a database migrated with every file except 0036, with the two tenants.
func newLegacyAdmin(t *testing.T) *pgxpool.Pool {
	t.Helper()
	admin, _ := testpg.NewDatabase(t)
	old := fstest.MapFS{}
	names, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		if strings.HasPrefix(n, "0036_") {
			continue
		}
		b, err := fs.ReadFile(migrations.FS, n)
		if err != nil {
			t.Fatal(err)
		}
		old[n] = &fstest.MapFile{Data: b}
	}
	if _, err := db.Migrate(ctx(t), admin, old); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ($1, 'T')`, tenantA); err != nil {
		t.Fatal(err)
	}
	return admin
}

// Start writes the role and the authn reference of the VERIFIED session it is given, and a later Start of the same input does not change them (INV-12).
func TestStartWritesTheRoleAndTheAuthnRefOfTheVerifiedSession(t *testing.T) {
	w := newStoreWorld(t)
	sess := orchestrator.Session{TenantID: tenantA, PrincipalID: "usr_01", Role: "approver"}
	const authnA = "ses_a1b2c3d4e5f60718293a4b5c6d7e8f90" // this test's own reference, so a fixed value in the store cannot pass
	if _, created, err := w.storeA.Start(ctx(t), sess, authnA, start("inv_AA11", "recev_AA11")); err != nil || !created {
		t.Fatalf("Start: created %v, %v", created, err)
	}
	var role, ref string
	if err := w.admin.QueryRow(ctx(t), `SELECT principal_role, authn_ref FROM investigation WHERE tenant_id = $1 AND investigation_id = 'inv_AA11'`, tenantA).Scan(&role, &ref); err != nil || role != "approver" || ref != authnA {
		t.Fatalf("row holds %q %q (%v); want approver and %s", role, ref, err, authnA)
	}
	later := "ses_fedcba9876543210fedcba9876543210"
	if _, created, err := w.storeA.Start(ctx(t), sess, later, start("inv_ZZ99", "recev_AA11")); err != nil || created {
		t.Fatalf("a second Start of the input: created %v, %v", created, err)
	}
	if err := w.admin.QueryRow(ctx(t), `SELECT principal_role, authn_ref FROM investigation WHERE tenant_id = $1 AND investigation_id = 'inv_AA11'`, tenantA).Scan(&role, &ref); err != nil || role != "approver" || ref != authnA {
		t.Errorf("a second Start changed the row to %q %q (%v)", role, ref, err)
	}
	if ev := w.events(t, tenantA); len(ev) != 0 {
		t.Errorf("Start appended Record events %v; the Record is unchanged by this task", ev)
	}
}

// The request names the investigation, the correlation and the recorded input and nothing else: no value of a request, and so none of a model, can reach the two columns.
// Hostile values in the request fields are refused by their patterns and never stored as a role or an authn reference.
func TestNoValueOfTheRequestReachesTheRoleOrTheAuthnRef(t *testing.T) {
	var names []string
	rt := reflect.TypeOf(investigations.StartRequest{})
	for i := 0; i < rt.NumField(); i++ {
		names = append(names, rt.Field(i).Name)
	}
	if strings.Join(names, ",") != "InvestigationID,CorrelationID,InputReceivedRef" {
		t.Fatalf("StartRequest fields %v; the request must carry no role or authn reference", names)
	}
	w := newStoreWorld(t)
	hostile := "ses_ffffffffffffffffffffffffffffffff"
	for name, r := range map[string]investigations.StartRequest{
		"an authn reference as the investigation": {InvestigationID: hostile, CorrelationID: "corr_AA11", InputReceivedRef: "recev_AA11"},
		"a role as the correlation":               {InvestigationID: "inv_AA11", CorrelationID: "ops", InputReceivedRef: "recev_AA11"},
		"an authn reference as the input":         {InvestigationID: "inv_AA11", CorrelationID: "corr_AA11", InputReceivedRef: hostile},
	} {
		if _, _, err := w.storeA.Start(ctx(t), sessA, authnA, r); !errors.Is(err, investigations.ErrRequest) {
			t.Errorf("%s: %v; want ErrRequest", name, err)
		}
	}
	var n int
	if err := w.admin.QueryRow(ctx(t), `SELECT count(*) FROM investigation WHERE authn_ref = $1 OR principal_role = 'ops'`, hostile).Scan(&n); err != nil || n != 0 {
		t.Errorf("%d rows hold a request-derived value (%v)", n, err)
	}
}

// Start refuses a session whose role is outside the four of A1, and an authn reference outside its form, before it writes anything (INV-12).
func TestStartRefusesARoleOutsideTheFourAndAnAuthnRefOutsideItsForm(t *testing.T) {
	w := newStoreWorld(t)
	for name, c := range map[string]struct {
		role, authn string
	}{
		"no role":              {"", authnA},
		"an unknown role":      {"admin", authnA},
		"a principal type":     {"operator", authnA},
		"no authn_ref":         {"customer", ""},
		"a token":              {"customer", "a.bearer.credential"},
		"another prefix":       {"customer", "tok_0123456789abcdef0123456789abcdef"},
		"a reference too long": {"customer", "ses_" + strings.Repeat("a", 33)},
	} {
		sess := orchestrator.Session{TenantID: tenantA, PrincipalID: "usr_01", Role: c.role}
		if _, created, err := w.storeA.Start(ctx(t), sess, c.authn, start("inv_AA11", "recev_AA11")); !errors.Is(err, investigations.ErrRequest) || created {
			t.Errorf("%s: created %v, %v; want ErrRequest", name, created, err)
		}
	}
	var n int
	if err := w.admin.QueryRow(ctx(t), `SELECT count(*) FROM investigation`).Scan(&n); err != nil || n != 0 {
		t.Errorf("%d rows written by refused Starts (%v)", n, err)
	}
}

// SessionOf is the read path for recovery: it returns the role and the authn reference the row holds, for the session's own tenant only, and fails closed for a legacy row.
func TestSessionOfReturnsTheStoredRoleAndAuthnRefAndFailsClosedForALegacyRow(t *testing.T) {
	w := newStoreWorld(t)
	if _, _, err := w.storeA.Start(ctx(t), orchestrator.Session{TenantID: tenantA, PrincipalID: "usr_01", Role: "ops"}, authnA, start("inv_AA11", "recev_AA11")); err != nil {
		t.Fatal(err)
	}
	got, err := w.storeA.SessionOf(ctx(t), sessA, "inv_AA11")
	if err != nil || got != (investigations.RecordedSession{PrincipalID: "usr_01", PrincipalRole: "ops", AuthnRef: authnA}) {
		t.Fatalf("got %+v, %v; want usr_01, ops and the authn reference of the Start (not the caller's role)", got, err)
	}
	w.seedRow(t, tenantA, "inv_OLD1", "recev_OLD1", "RECEIVED", "usr_01")
	if _, err := w.admin.Exec(ctx(t), `ALTER TABLE investigation DISABLE TRIGGER USER`); err != nil {
		t.Fatal(err)
	}
	if _, err := w.admin.Exec(ctx(t), `UPDATE investigation SET principal_role = NULL, authn_ref = NULL WHERE investigation_id = 'inv_OLD1'`); err != nil {
		t.Fatal(err)
	}
	if got, err := w.storeA.SessionOf(ctx(t), sessA, "inv_OLD1"); !errors.Is(err, investigations.ErrNoRecordedSession) || got != (investigations.RecordedSession{}) {
		t.Errorf("a legacy row: %+v, %v; want ErrNoRecordedSession and nothing", got, err)
	}
}

// Tenant isolation: another tenant's row and an absent one are the same error, by the database's bound and by the query's own clause.
func TestSessionOfNeverReturnsAnotherTenantsRowAndDoesNotTellItFromAbsent(t *testing.T) {
	w := newStoreWorld(t)
	w.seedRow(t, tenantB, "inv_BB22", "recev_BB22", "EXECUTING", "usr_01")
	unbound := &investigations.Store{Pool: w.admin, Appender: w.storeA.Appender, Inputs: w.inputs}
	for name, s := range map[string]*investigations.Store{"a login bound by row-level security": w.storeA, "a pool the database does not bind": unbound} {
		if got, err := s.SessionOf(ctx(t), sessA, "inv_BB22"); !errors.Is(err, investigations.ErrNoInvestigation) || got != (investigations.RecordedSession{}) {
			t.Errorf("%s, another tenant's row: %+v, %v; want ErrNoInvestigation", name, got, err)
		}
		if _, err := s.SessionOf(ctx(t), sessA, "inv_ZZ99"); !errors.Is(err, investigations.ErrNoInvestigation) {
			t.Errorf("%s, an absent row: %v; want ErrNoInvestigation", name, err)
		}
	}
	if _, err := w.storeA.SessionOf(ctx(t), sessA, "inv_!!"); !errors.Is(err, investigations.ErrRequest) {
		t.Errorf("a malformed investigation: %v; want ErrRequest", err)
	}
}
