package investigations_test

// STM-01 / noema#601, ruling A243 (interim; migration 0029; REQ-STM-001, REQ-SEC-004, REQ-API-001; INV-12): Store.Read is the read-only method the investigation route reads
// through. It returns the state and the principal of one investigation of the login's tenant, and nothing else. The tenant bound is tested with a login that row-level security
// does not bind (the administrator), so a read that lost its own tenant clause cannot hide behind the database. All data is SYNTHETIC.

import (
	"errors"
	"testing"

	"github.com/Noemai-org/noema/services/core/orchestrator"
	"github.com/Noemai-org/noema/services/core/orchestrator/investigations"
)

// seedRow writes one investigation as the administrator, the way a Start and one move would have left it (no Record event is needed to read it). The database admits only
// RECEIVED at version 0 as a new row, so any other state is a move of one version.
func (w *storeWorld) seedRow(t *testing.T, tenant, inv, ref, state, principal string) {
	t.Helper()
	if _, err := w.admin.Exec(ctx(t), `INSERT INTO investigation (tenant_id, investigation_id, input_received_ref, correlation_id, state, state_version, principal_ref, principal_role, authn_ref)
		VALUES ($1, $2, $3, 'corr_AA11', 'RECEIVED', 0, $4, 'customer', 'ses_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa')`, tenant, inv, ref, principal); err != nil {
		t.Fatal(err)
	}
	if state == "RECEIVED" {
		return
	}
	if _, err := w.admin.Exec(ctx(t), `UPDATE investigation SET state = $1, state_version = 1 WHERE tenant_id = $2 AND investigation_id = $3`, state, tenant, inv); err != nil {
		t.Fatal(err)
	}
}

func TestReadReturnsTheStateAndThePrincipalOfTheTenantsOwnInvestigation(t *testing.T) {
	w := newStoreWorld(t)
	w.seedRow(t, tenantA, "inv_AA11", "recev_AA11", "AWAITING_CONFIRMATION", "usr_01")
	got, err := w.storeA.Read(ctx(t), sessA, "inv_AA11")
	if err != nil || got.State != "AWAITING_CONFIRMATION" || got.Principal != "usr_01" {
		t.Fatalf("got %+v, %v; want AWAITING_CONFIRMATION and usr_01", got, err)
	}
}

// The read says what the row says; it does not decide who may see it. A row from before migration 0029 has an empty principal and Read reports it as it is.
func TestReadReportsALegacyRowsEmptyPrincipalAsItIs(t *testing.T) {
	w := newStoreWorld(t)
	if _, err := w.admin.Exec(ctx(t), `ALTER TABLE investigation DROP CONSTRAINT investigation_principal_named`); err != nil { // a row that predates the CHECK
		t.Fatal(err)
	}
	w.seedRow(t, tenantA, "inv_AA11", "recev_AA11", "RECEIVED", "")
	got, err := w.storeA.Read(ctx(t), sessA, "inv_AA11")
	if err != nil || got.State != "RECEIVED" || got.Principal != "" {
		t.Fatalf("got %+v, %v; want RECEIVED and an empty principal", got, err)
	}
}

// Another tenant's investigation and an absent one are the same error. The administrator's pool is not bound by row-level security, so only the clause of the query can keep
// tenant B's row from tenant A's session here; the tenant login's pool is the database's own bound.
func TestReadNeverReturnsAnotherTenantsInvestigationAndDoesNotTellItFromAbsent(t *testing.T) {
	w := newStoreWorld(t)
	w.seedRow(t, tenantB, "inv_BB22", "recev_BB22", "EXECUTING", "usr_01")
	unbound := &investigations.Store{Pool: w.admin, Appender: w.storeA.Appender, Inputs: w.inputs}
	for name, s := range map[string]*investigations.Store{"a login bound by row-level security": w.storeA, "a pool the database does not bind": unbound} {
		if got, err := s.Read(ctx(t), sessA, "inv_BB22"); !errors.Is(err, investigations.ErrNoInvestigation) || got != (investigations.Snapshot{}) {
			t.Errorf("%s, another tenant's investigation: %+v, %v; want ErrNoInvestigation and nothing", name, got, err)
		}
		if _, err := s.Read(ctx(t), sessA, "inv_ZZ99"); !errors.Is(err, investigations.ErrNoInvestigation) {
			t.Errorf("%s, an absent investigation: %v; want ErrNoInvestigation", name, err)
		}
	}
}

func TestReadRefusesWhatIsNotAValidRequestAndAnUnconfiguredStore(t *testing.T) {
	w := newStoreWorld(t)
	w.seedRow(t, tenantA, "inv_AA11", "recev_AA11", "RECEIVED", "usr_01")
	for name, c := range map[string]struct {
		sess orchestrator.Session
		id   string
	}{
		"a malformed investigation":   {sessA, "inv_!!"},
		"no principal in the session": {orchestrator.Session{TenantID: tenantA}, "inv_AA11"},
		"a malformed tenant":          {orchestrator.Session{TenantID: "tenant", PrincipalID: "usr_01"}, "inv_AA11"},
	} {
		if _, err := w.storeA.Read(ctx(t), c.sess, c.id); !errors.Is(err, investigations.ErrRequest) {
			t.Errorf("%s: %v; want ErrRequest", name, err)
		}
	}
	if _, err := (&investigations.Store{}).Read(ctx(t), sessA, "inv_AA11"); !errors.Is(err, investigations.ErrRequest) {
		t.Errorf("an unconfigured store: %v; want ErrRequest", err)
	}
	if _, err := w.storeA.Read(t.Context(), sessA, "inv_AA11"); err == nil {
		t.Error("a read without a deadline was served")
	}
}

// A store that cannot be reached is an error, never "not found": the caller must be able to tell a failure from an absence.
func TestReadOnAPoolThatFailsIsAnErrorAndNotAnAbsence(t *testing.T) {
	w := newStoreWorld(t)
	w.seedRow(t, tenantA, "inv_AA11", "recev_AA11", "RECEIVED", "usr_01")
	w.orchA.Close()
	_, err := w.storeA.Read(ctx(t), sessA, "inv_AA11")
	if err == nil || errors.Is(err, investigations.ErrNoInvestigation) {
		t.Fatalf("a closed pool: %v; want an error that is not ErrNoInvestigation", err)
	}
}

// The read changes nothing: the row (state, version, time) is the same afterwards, and the transaction it opens is read-only.
func TestReadChangesNothing(t *testing.T) {
	w := newStoreWorld(t)
	w.seedRow(t, tenantA, "inv_AA11", "recev_AA11", "PLANNED", "usr_01")
	var before, after string
	q := `SELECT state || '|' || state_version || '|' || updated_at::text FROM investigation WHERE investigation_id = 'inv_AA11'`
	if err := w.admin.QueryRow(ctx(t), q).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := w.storeA.Read(ctx(t), sessA, "inv_AA11"); err != nil {
		t.Fatal(err)
	}
	if err := w.admin.QueryRow(ctx(t), q).Scan(&after); err != nil || before != after {
		t.Errorf("row before %q, after %q (%v)", before, after, err)
	}
}
