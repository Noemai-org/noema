package investigations_test

// STM-07 PR A (A584 and A592, interim, relayed, not yet committed; REQ-STM-001, REQ-SEC-004; INV-12, INV-30). Option (b): the investigation row stores the VERIFIED issuer of the session
// that started it, in the nullable column asserted_by (migration 0039), so a session rebuilt from the row says who verified it. The value comes from the verified session only
// (sess.AssertedBy, which the edge's session verification fills), never from a request or a model. A row from before 0039 holds NULL and nothing is invented for it. The
// column has the form of the contract's asserted_by pattern. PR A does not make recovery refuse a row without it (PR B, after A merges). The database tests need Postgres and the
// signing extension, so they run in CI only. All data is SYNTHETIC.

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
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

// issuerA is the verified issuer of the synthetic session (ruling A1: the POC's own provider). SYNTHETIC.
const issuerA = "noema-poc-idp"

const insertIssuer = `INSERT INTO investigation (tenant_id, investigation_id, input_received_ref, correlation_id, state, state_version, principal_ref, principal_role, authn_ref, asserted_by)
	VALUES ($1, $2, $3, 'corr_AA11', 'RECEIVED', 0, 'usr_01', 'customer', 'ses_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', $4)`

func withIssuer(s orchestrator.Session, issuer string) orchestrator.Session {
	s.AssertedBy = issuer
	return s
}

// Start writes the issuer of the VERIFIED session it is given, SessionOf returns it, and a later Start of the same input does not change it (INV-12).
func TestStartWritesTheVerifiedIssuerOfTheSessionAndSessionOfReturnsIt(t *testing.T) {
	w := newStoreWorld(t)
	sess := withIssuer(withRef(orchestrator.Session{TenantID: tenantA, PrincipalID: "usr_01", Role: "approver"}, authnA), "idp_two-9") // this test's own issuer, so a fixed value in the store cannot pass
	if _, created, err := w.storeA.Start(ctx(t), sess, start("inv_AA11", "recev_AA11")); err != nil || !created {
		t.Fatalf("Start: created %v, %v", created, err)
	}
	var issuer *string
	if err := w.admin.QueryRow(ctx(t), `SELECT asserted_by FROM investigation WHERE tenant_id = $1 AND investigation_id = 'inv_AA11'`, tenantA).Scan(&issuer); err != nil || issuer == nil || *issuer != "idp_two-9" {
		t.Fatalf("row holds %v (%v); want idp_two-9", issuer, err)
	}
	got, err := w.storeA.SessionOf(ctx(t), sessA, "inv_AA11")
	if err != nil || got != (investigations.RecordedSession{PrincipalID: "usr_01", PrincipalRole: "approver", AuthnRef: authnA, AssertedBy: "idp_two-9"}) {
		t.Fatalf("SessionOf %+v, %v; want the issuer of the Start (not the caller's)", got, err)
	}
	later := withIssuer(sess, "another-idp")
	if _, created, err := w.storeA.Start(ctx(t), later, start("inv_ZZ99", "recev_AA11")); err != nil || created {
		t.Fatalf("a second Start of the input: created %v, %v", created, err)
	}
	if got, err := w.storeA.SessionOf(ctx(t), sessA, "inv_AA11"); err != nil || got.AssertedBy != "idp_two-9" {
		t.Errorf("a second Start changed the issuer to %q (%v)", got.AssertedBy, err)
	}
}

// A session that carries no issuer (the gateway writer of EL is not in yet, or a caller that never verified one) stores NULL, never an invented value, and SessionOf reports it absent
// without refusing the row: refusing it is PR B (A592), after this one merges.
func TestAStartWithoutAnIssuerStoresNullAndSessionOfReportsItAbsent(t *testing.T) {
	w := newStoreWorld(t)
	if _, _, err := w.storeA.Start(ctx(t), withRef(sessA, authnA), start("inv_AA11", "recev_AA11")); err != nil {
		t.Fatal(err)
	}
	var issuer *string
	if err := w.admin.QueryRow(ctx(t), `SELECT asserted_by FROM investigation WHERE investigation_id = 'inv_AA11'`).Scan(&issuer); err != nil || issuer != nil {
		t.Fatalf("a Start without an issuer stored %v (%v); want NULL", issuer, err)
	}
	got, err := w.storeA.SessionOf(ctx(t), sessA, "inv_AA11")
	if err != nil || got != (investigations.RecordedSession{PrincipalID: "usr_01", PrincipalRole: "customer", AuthnRef: authnA}) {
		t.Fatalf("SessionOf %+v, %v; want the row with an empty issuer and no refusal", got, err)
	}
}

// An issuer outside the contract's asserted_by pattern ^[a-z][a-z0-9_-]*$ is refused by the store before anything is written, and by the database's CHECK when written directly.
func TestAnIssuerOutsideItsFormIsRefusedByTheStoreAndByTheDatabase(t *testing.T) {
	w := newStoreWorld(t)
	bad := map[string]string{
		"capitals":             "Noema-poc-idp",
		"a leading digit":      "1idp",
		"a leading hyphen":     "-idp",
		"a space":              "noema poc",
		"a dot":                "noema.poc",
		"a slash":              "noema/poc",
		"a newline at the end": "noema-poc-idp\n",
		"a url":                "https://idp.example/",
	}
	for name, issuer := range bad {
		if _, created, err := w.storeA.Start(ctx(t), withIssuer(withRef(sessA, authnA), issuer), start("inv_AA11", "recev_AA11")); !errors.Is(err, investigations.ErrRequest) || created {
			t.Errorf("Start, %s: created %v, %v; want ErrRequest", name, created, err)
		}
		if _, err := w.admin.Exec(ctx(t), insertIssuer, tenantA, "inv_DB11", "recev_DB11", issuer); err == nil {
			t.Errorf("the database accepted %s", name)
		}
	}
	if _, err := w.admin.Exec(ctx(t), insertIssuer, tenantA, "inv_DB22", "recev_DB22", issuerA); err != nil {
		t.Fatalf("the control insert with a good issuer: %v", err)
	}
	if _, err := w.admin.Exec(ctx(t), `DELETE FROM investigation WHERE investigation_id = 'inv_DB22'`); err != nil {
		t.Logf("cleanup: %v", err)
	}
	var n int
	if err := w.admin.QueryRow(ctx(t), `SELECT count(*) FROM investigation WHERE asserted_by IS NOT NULL`).Scan(&n); err != nil || n != 0 {
		t.Errorf("%d rows hold an issuer after only refused writes (%v)", n, err)
	}
}

// The issuer never changes after the insert, for the orchestrator's login: the column is not in its column-limited UPDATE grant (0025). It is also not a request value: StartRequest is unchanged.
func TestTheIssuerNeverChangesAfterTheInsert(t *testing.T) {
	w := newStoreWorld(t)
	if _, _, err := w.storeA.Start(ctx(t), withIssuer(withRef(sessA, authnA), issuerA), start("inv_AA11", "recev_AA11")); err != nil {
		t.Fatal(err)
	}
	if _, err := w.orchA.Exec(ctx(t), `UPDATE investigation SET asserted_by = 'other-idp' WHERE investigation_id = 'inv_AA11'`); err == nil {
		t.Error("the orchestrator's login changed the issuer")
	}
	var issuer string
	if err := w.orchA.QueryRow(ctx(t), `SELECT asserted_by FROM investigation WHERE investigation_id = 'inv_AA11'`).Scan(&issuer); err != nil || issuer != issuerA {
		t.Errorf("row holds %q (%v); want %s", issuer, err, issuerA)
	}
	if _, err := w.orchA.Exec(ctx(t), `UPDATE investigation SET state = 'INTERPRETING', state_version = 1, updated_at = now() WHERE investigation_id = 'inv_AA11'`); err != nil {
		t.Errorf("a move of the row after the column exists: %v", err)
	}
}

// A row from before 0039 holds NULL, the migration invents nothing for it, it still moves, and SessionOf reads it with an empty issuer (PR A leaves recovery's behaviour alone).
func TestARowFromBefore0039KeepsNullAndTheMigrationInventsNoIssuer(t *testing.T) {
	admin := newLegacyAdminWithout(t, "0039_")
	if _, err := admin.Exec(ctx(t), `INSERT INTO investigation (tenant_id, investigation_id, input_received_ref, correlation_id, state, state_version, principal_ref, principal_role, authn_ref)
		VALUES ($1, 'inv_OLD1', 'recev_OLD1', 'corr_AA11', 'RECEIVED', 0, 'usr_01', 'customer', $2)`, tenantA, authnA); err != nil {
		t.Fatalf("a row before 0039: %v", err)
	}
	if _, err := db.Migrate(ctx(t), admin, migrations.FS); err != nil {
		t.Fatal(err)
	}
	var issuer *string
	if err := admin.QueryRow(ctx(t), `SELECT asserted_by FROM investigation WHERE investigation_id = 'inv_OLD1'`).Scan(&issuer); err != nil || issuer != nil {
		t.Fatalf("a legacy row holds %v (%v); want NULL", issuer, err)
	}
	if _, err := admin.Exec(ctx(t), `UPDATE investigation SET state = 'INTERPRETING', state_version = 1 WHERE investigation_id = 'inv_OLD1'`); err != nil {
		t.Errorf("a legacy row cannot move: %v", err)
	}
	if _, err := admin.Exec(ctx(t), `INSERT INTO investigation (tenant_id, investigation_id, input_received_ref, correlation_id, state, state_version, principal_ref, principal_role, authn_ref)
		VALUES ($1, 'inv_NEW1', 'recev_NEW1', 'corr_AA11', 'RECEIVED', 0, 'usr_01', 'customer', $2)`, tenantA, authnA); err != nil {
		t.Errorf("a new row without an issuer after 0039 (nullable by ruling A584): %v", err)
	}
}

// newLegacyAdminWithout returns a database migrated with every file except the one with the given prefix, with the tenant.
func newLegacyAdminWithout(t *testing.T, prefix string) *pgxpool.Pool {
	t.Helper()
	admin, _ := testpg.NewDatabase(t)
	old := fstest.MapFS{}
	names, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		if strings.HasPrefix(n, prefix) {
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

// The form of the stored issuer is the contract's asserted_by pattern: the migration's CHECK carries the pattern of intent-object.v2 and of understanding.Session.Valid. A DB-free guard,
// so a drift between the contract and the column is seen without Postgres.
func TestTheMigrationsIssuerCheckCarriesTheContractsAssertedByPattern(t *testing.T) {
	raw, err := os.ReadFile("../../../../contracts/schemas/intent-object.v2.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Properties struct {
			Principal struct {
				Properties struct {
					AssertedBy struct {
						Pattern string `json:"pattern"`
					} `json:"asserted_by"`
				} `json:"properties"`
			} `json:"principal"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	pattern := doc.Properties.Principal.Properties.AssertedBy.Pattern
	if pattern == "" {
		t.Fatal("the contract has no asserted_by pattern")
	}
	sql, err := fs.ReadFile(migrations.FS, "0039_investigation_verified_issuer.sql")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(sql), "asserted_by ~ '"+pattern+"'") {
		t.Errorf("migration 0039 does not carry the contract's pattern %s in its CHECK", pattern)
	}
}
