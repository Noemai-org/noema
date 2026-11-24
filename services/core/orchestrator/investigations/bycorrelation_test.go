package investigations_test

// TRU-12, ruling A625 (interim, relayed, not yet committed; REQ-TRU-010, REQ-SEC-004; INV-12, BD-05): Store.SessionByCorrelation is the orchestrator's by-correlation read of the
// session facts a route needs. It is read-only, tenant-scoped by the login's forced row-level security and by its own clause, and it refuses an ambiguous correlation. These tests
// need PostgreSQL and the signing extension (CI only). All data is SYNTHETIC.

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/Noemai-org/noema/services/core/orchestrator/investigations"
)

const bcAuthn = "ses_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

// bcSeed writes one investigation of a correlation as the administrator, with the session columns a Start writes.
func (w *storeWorld) bcSeed(t *testing.T, tenant, inv, ref, corr, principal, role string) {
	t.Helper()
	if _, err := w.admin.Exec(ctx(t), `INSERT INTO investigation (tenant_id, investigation_id, input_received_ref, correlation_id, state, state_version, principal_ref, principal_role, authn_ref)
		VALUES ($1, $2, $3, $4, 'RECEIVED', 0, $5, $6, $7)`, tenant, inv, ref, corr, principal, role, bcAuthn); err != nil {
		t.Fatal(err)
	}
}

func TestSessionByCorrelationReturnsTheRecordedSessionOfTheTenantsInvestigation(t *testing.T) { // A625, INV-12
	w := newStoreWorld(t)
	w.bcSeed(t, tenantA, "inv_AA11", "recev_AA11", "corr_AA11", "usr_01", "ops")
	got, err := w.storeA.SessionByCorrelation(ctx(t), tenantA, "corr_AA11")
	if want := (investigations.RecordedSession{PrincipalID: "usr_01", PrincipalRole: "ops", AuthnRef: bcAuthn}); err != nil || got != want {
		t.Fatalf("got %+v, %v; want %+v", got, err, want)
	}
}

func TestSessionByCorrelationFindsNoInvestigationForAnAbsentCorrelation(t *testing.T) { // A625
	w := newStoreWorld(t)
	w.bcSeed(t, tenantA, "inv_AA11", "recev_AA11", "corr_AA11", "usr_01", "ops")
	if got, err := w.storeA.SessionByCorrelation(ctx(t), tenantA, "corr_ZZ99"); !errors.Is(err, investigations.ErrNoInvestigation) || got != (investigations.RecordedSession{}) {
		t.Fatalf("got %+v, %v; want ErrNoInvestigation and nothing", got, err)
	}
}

// Another tenant's correlation is not found, and not told apart from an absent one. The administrator's pool is not bound by row-level security, so only the query's own clause can
// keep tenant B's row from tenant A here; the tenant login's pool is the database's own bound.
func TestSessionByCorrelationNeverReturnsAnotherTenantsInvestigation(t *testing.T) { // A625, REQ-SEC-004
	w := newStoreWorld(t)
	w.bcSeed(t, tenantB, "inv_BB22", "recev_BB22", "corr_BB22", "usr_09", "ops")
	unbound := &investigations.Store{Pool: w.admin, Appender: w.storeA.Appender, Inputs: w.inputs}
	for name, s := range map[string]*investigations.Store{"a login bound by row-level security": w.storeA, "a pool the database does not bind": unbound} {
		if got, err := s.SessionByCorrelation(ctx(t), tenantA, "corr_BB22"); !errors.Is(err, investigations.ErrNoInvestigation) || got != (investigations.RecordedSession{}) {
			t.Errorf("%s: %+v, %v; want ErrNoInvestigation and nothing", name, got, err)
		}
	}
}

// Two investigations may carry one correlation (the table's unique keys are the investigation and the recorded input): the read refuses to pick one, whatever either holds.
func TestSessionByCorrelationRefusesACorrelationOfSeveralInvestigations(t *testing.T) { // A625
	w := newStoreWorld(t)
	w.bcSeed(t, tenantA, "inv_AA11", "recev_AA11", "corr_AA11", "usr_01", "ops")
	w.bcSeed(t, tenantA, "inv_AA12", "recev_AA12", "corr_AA11", "usr_02", "customer")
	if got, err := w.storeA.SessionByCorrelation(ctx(t), tenantA, "corr_AA11"); !errors.Is(err, investigations.ErrAmbiguousInvestigation) || got != (investigations.RecordedSession{}) {
		t.Fatalf("got %+v, %v; want ErrAmbiguousInvestigation and nothing", got, err)
	}
}

func TestSessionByCorrelationRefusesARowThatRecordsNoSession(t *testing.T) { // A625, A441
	w := newStoreWorld(t)
	w.bcSeed(t, tenantA, "inv_OLD1", "recev_OLD1", "corr_AA11", "usr_01", "ops")
	for _, stmt := range []string{`ALTER TABLE investigation DISABLE TRIGGER USER`, `UPDATE investigation SET principal_role = NULL, authn_ref = NULL WHERE investigation_id = 'inv_OLD1'`} { // a row of before 0036
		if _, err := w.admin.Exec(ctx(t), stmt); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := w.storeA.SessionByCorrelation(ctx(t), tenantA, "corr_AA11"); !errors.Is(err, investigations.ErrNoRecordedSession) {
		t.Fatalf("err %v; want ErrNoRecordedSession", err)
	}
}

func TestSessionByCorrelationRefusesWhatIsNotAValidRequest(t *testing.T) { // A625
	w := newStoreWorld(t)
	w.bcSeed(t, tenantA, "inv_AA11", "recev_AA11", "corr_AA11", "usr_01", "ops")
	for name, c := range map[string]struct{ tenant, corr string }{
		"a malformed correlation": {tenantA, "corr_!!"},
		"a malformed tenant":      {"tenant", "corr_AA11"},
		"no tenant":               {"", "corr_AA11"},
		"no correlation":          {tenantA, ""},
	} {
		if _, err := w.storeA.SessionByCorrelation(ctx(t), c.tenant, c.corr); !errors.Is(err, investigations.ErrRequest) {
			t.Errorf("%s: err %v; want ErrRequest", name, err)
		}
	}
	if _, err := w.storeA.SessionByCorrelation(context.Background(), tenantA, "corr_AA11"); err == nil {
		t.Error("a context with no deadline was accepted")
	}
	if _, err := (*investigations.Store)(nil).SessionByCorrelation(ctx(t), tenantA, "corr_AA11"); !errors.Is(err, investigations.ErrRequest) {
		t.Errorf("a nil store: err %v; want ErrRequest", err)
	}
	if _, err := (&investigations.Store{}).SessionByCorrelation(ctx(t), tenantA, "corr_AA11"); !errors.Is(err, investigations.ErrRequest) {
		t.Errorf("an unconfigured store: err %v; want ErrRequest", err)
	}
}

// The read changes nothing: no row moves and no Record event is appended.
func TestSessionByCorrelationWritesNothing(t *testing.T) { // A625
	w := newStoreWorld(t)
	w.bcSeed(t, tenantA, "inv_AA11", "recev_AA11", "corr_AA11", "usr_01", "ops")
	before := len(w.events(t, tenantA))
	if _, err := w.storeA.SessionByCorrelation(ctx(t), tenantA, "corr_AA11"); err != nil {
		t.Fatal(err)
	}
	if st, v := w.row(t, tenantA, "inv_AA11"); st != "RECEIVED" || v != 0 || len(w.events(t, tenantA)) != before {
		t.Fatalf("row %s/%d, events %d -> %d: the read changed something", st, v, before, len(w.events(t, tenantA)))
	}
}

// optsPool delegates to the real pool and remembers how each transaction was begun.
type optsPool struct {
	inner investigations.Pool
	modes []pgx.TxAccessMode
}

func (p *optsPool) BeginTx(c context.Context, o pgx.TxOptions) (pgx.Tx, error) {
	p.modes = append(p.modes, o.AccessMode)
	return p.inner.BeginTx(c, o)
}

// The read begins its transaction read-only, so the database itself refuses a write in it (A625: read-only).
func TestSessionByCorrelationBeginsAReadOnlyTransaction(t *testing.T) { // A625
	w := newStoreWorld(t)
	w.bcSeed(t, tenantA, "inv_AA11", "recev_AA11", "corr_AA11", "usr_01", "ops")
	p := &optsPool{inner: w.orchA}
	if _, err := (&investigations.Store{Pool: p}).SessionByCorrelation(ctx(t), tenantA, "corr_AA11"); err != nil {
		t.Fatal(err)
	}
	if len(p.modes) != 1 || p.modes[0] != pgx.ReadOnly {
		t.Fatalf("transactions begun with access modes %v; want exactly one, read-only", p.modes)
	}
}
