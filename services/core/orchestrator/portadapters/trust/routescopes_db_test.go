package trustport_test

// TRU-12, ruling A625 (interim, relayed, not yet committed; REQ-TRU-010, REQ-TRU-009, REQ-SEC-004; INV-12, INV-03, BD-05): the production RouteScopes. The scope of a route is the session
// the orchestrator handed over (read from the investigation by its own login), the correlation's latest admitted intent and its NEWEST sealed snapshot. Trust reads snapshots and
// intents under its own login, never the investigation. Real PostgreSQL, one login per tenant and power, CI only (the signing extension is not available locally). All data is SYNTHETIC.

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/evidence/snapshot"
	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/migrations"
	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
	"github.com/Noemai-org/noema/services/core/platform/db"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

const rscAuthn = "ses_cccccccccccccccccccccccccccccccc"

// rscWorld is the evdPG world plus each tenant's orchestrator login.
type rscWorld struct {
	*evdPG
	orch map[string]*pgxpool.Pool
}

func rscNewWorld(t *testing.T) *rscWorld {
	t.Helper()
	admin, dsn := testpg.NewDatabase(t)
	if _, err := db.Migrate(evdCtx(t), admin, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	w := &rscWorld{evdPG: &evdPG{admin: admin, evidence: map[string]*pgxpool.Pool{}, trust: map[string]*pgxpool.Pool{}}, orch: map[string]*pgxpool.Pool{}}
	for _, id := range []string{evdTenant, evdOther} {
		if _, err := admin.Exec(evdCtx(t), `INSERT INTO tenant (tenant_id, name) VALUES ($1, 'Synthetic')`, id); err != nil {
			t.Fatal(err)
		}
		w.evidence[id] = testpg.TenantLogin(t, admin, dsn, "evidence_rw", id)
		w.trust[id] = testpg.TenantLogin(t, admin, dsn, "trust_rw", id)
		w.orch[id] = testpg.TenantLogin(t, admin, dsn, "orchestrator_rw", id)
	}
	return w
}

// snap seals one snapshot of the correlation over one evidence item, at the given instant, as the tenant's Trust login (the database recomputes the hash). The item is written once
// per tenant and reused by every snapshot of it.
func (w *rscWorld) snap(t *testing.T, tenant, id, corr string, at time.Time) {
	t.Helper()
	var itemID string
	if err := w.admin.QueryRow(evdCtx(t), `SELECT evidence_id FROM evidence_item WHERE tenant_id = $1 LIMIT 1`, tenant).Scan(&itemID); err != nil {
		n := 1
		if tenant == evdOther {
			n = 91
		}
		it := w.item(t, tenant, evdItem(n, evdLedger), 600, evdPayload(evdLedger, 0))
		itemID = it.EvidenceID
	}
	var hash string
	if err := w.admin.QueryRow(evdCtx(t), `SELECT payload_hash FROM evidence_item WHERE tenant_id = $1 AND evidence_id = $2`, tenant, itemID).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	ids := []string{itemID}
	sort.Strings(ids)
	set, err := snapshot.SetHash([]snapshot.Pair{{EvidenceID: itemID, PayloadHash: hash}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.trust[tenant].Exec(evdCtx(t), `INSERT INTO context_snapshot (snapshot_id, tenant_id, correlation_id, evidence_ids, reconciliation_id, reconciliation_classification, evidence_set_hash, sealed_at)
		VALUES ($1, $2, $3, $4, 'rec_synthetic', 'CONSISTENT', $5, $6)`, id, tenant, corr, ids, set, at); err != nil {
		t.Fatalf("seal %s: %v", id, err)
	}
}

func (w *rscWorld) reader(t *testing.T, pools evdPools) trustport.RouteScopeReader {
	t.Helper()
	r, err := trustport.NewRouteScopeReader(pools, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

var rscSession = intake.Session{TenantID: evdTenant, PrincipalID: "usr_01", Role: "ops"}

func TestRouteScopeIsTheHandedSessionTheLatestIntentAndTheOnlySnapshot(t *testing.T) { // A625, REQ-TRU-010
	w := rscNewWorld(t)
	w.intent(t, evdTenant, 1, evdNow.Add(-6*time.Minute))
	w.intent(t, evdTenant, 2, evdNow.Add(-5*time.Minute))
	w.snap(t, evdTenant, "evs_AA01", evdCorr, evdNow.Add(-time.Minute))
	got, err := w.reader(t, evdPools(w.trust)).ScopeFor(evdCtx(t), evdTenant, evdCorr, rscSession)
	want := trustport.RouteScope{Session: rscSession, IntentRef: "int_0001@2", EvidenceSnapshotID: "evs_AA01"}
	if err != nil || got != want {
		t.Fatalf("got %+v, %v; want %+v", got, err, want)
	}
}

// The newest snapshot wins by its seal time, not by its id: the older one has the greater id here.
func TestRouteScopeTakesTheNewestSnapshotNotTheGreaterID(t *testing.T) { // A625
	w := rscNewWorld(t)
	w.intent(t, evdTenant, 1, evdNow.Add(-6*time.Minute))
	w.snap(t, evdTenant, "evs_ZZ09", evdCorr, evdNow.Add(-10*time.Minute))
	w.snap(t, evdTenant, "evs_AA02", evdCorr, evdNow.Add(-2*time.Minute))
	w.snap(t, evdTenant, "evs_MM05", evdCorr, evdNow.Add(-7*time.Minute))
	got, err := w.reader(t, evdPools(w.trust)).ScopeFor(evdCtx(t), evdTenant, evdCorr, rscSession)
	if err != nil || got.EvidenceSnapshotID != "evs_AA02" {
		t.Fatalf("got %+v, %v; want the newest snapshot evs_AA02", got, err)
	}
}

// Two snapshots sealed at the same instant: the greater snapshot id is the tie-break, whatever order they were written in.
func TestRouteScopeBreaksATieOfSealTimeByTheGreaterSnapshotID(t *testing.T) { // A625
	for name, order := range map[string][]string{"greater id written first": {"evs_BB02", "evs_AA02"}, "greater id written last": {"evs_AA02", "evs_BB02"}} {
		t.Run(name, func(t *testing.T) {
			w := rscNewWorld(t)
			w.intent(t, evdTenant, 1, evdNow.Add(-6*time.Minute))
			for _, id := range order {
				w.snap(t, evdTenant, id, evdCorr, evdNow.Add(-time.Minute))
			}
			got, err := w.reader(t, evdPools(w.trust)).ScopeFor(evdCtx(t), evdTenant, evdCorr, rscSession)
			if err != nil || got.EvidenceSnapshotID != "evs_BB02" {
				t.Fatalf("got %+v, %v; want evs_BB02", got, err)
			}
		})
	}
}

// No snapshot is no scope and no grant (INV-17): an intent alone is not enough.
func TestRouteScopeWithNoSnapshotIsNoScope(t *testing.T) { // A625, INV-03
	w := rscNewWorld(t)
	w.intent(t, evdTenant, 1, evdNow.Add(-6*time.Minute))
	got, err := w.reader(t, evdPools(w.trust)).ScopeFor(evdCtx(t), evdTenant, evdCorr, rscSession)
	if !errors.Is(err, trustport.ErrNoRouteScope) || got != (trustport.RouteScope{}) {
		t.Fatalf("got %+v, %v; want ErrNoRouteScope and the zero scope", got, err)
	}
}

func TestRouteScopeWithNoAdmittedIntentIsNoScope(t *testing.T) { // A625, REQ-TRU-009
	w := rscNewWorld(t)
	w.snap(t, evdTenant, "evs_AA01", evdCorr, evdNow.Add(-time.Minute))
	got, err := w.reader(t, evdPools(w.trust)).ScopeFor(evdCtx(t), evdTenant, evdCorr, rscSession)
	if !errors.Is(err, trustport.ErrIntentRef) || got != (trustport.RouteScope{}) {
		t.Fatalf("got %+v, %v; want ErrIntentRef and the zero scope", got, err)
	}
}

// Another tenant's snapshot and intent are not this tenant's scope. The administrator's pool is not bound by row-level security, so only the query's own tenant clause keeps tenant B's
// rows from tenant A there; the tenant login is the database's own bound.
func TestRouteScopeNeverReadsAnotherTenantsSnapshot(t *testing.T) { // A625, REQ-SEC-004
	w := rscNewWorld(t)
	w.intent(t, evdOther, 1, evdNow.Add(-6*time.Minute))
	w.snap(t, evdOther, "evs_BB01", evdCorr, evdNow.Add(-time.Minute))
	for name, pools := range map[string]evdPools{"the tenant login": evdPools(w.trust), "a pool the database does not bind": {evdTenant: w.admin}} {
		got, err := w.reader(t, pools).ScopeFor(evdCtx(t), evdTenant, evdCorr, rscSession)
		if !errors.Is(err, trustport.ErrNoRouteScope) || got != (trustport.RouteScope{}) {
			t.Errorf("%s: %+v, %v; want ErrNoRouteScope and the zero scope", name, got, err)
		}
	}
}

// The handed session must be the tenant's own and name a principal and a role: Trust never builds one (INV-12).
func TestRouteScopeRefusesASessionThatIsNotTheTenantsOrIsIncomplete(t *testing.T) { // A625, INV-12
	w := rscNewWorld(t)
	w.intent(t, evdTenant, 1, evdNow.Add(-6*time.Minute))
	w.snap(t, evdTenant, "evs_AA01", evdCorr, evdNow.Add(-time.Minute))
	for name, s := range map[string]intake.Session{
		"no session":      {},
		"another tenant":  {TenantID: evdOther, PrincipalID: "usr_01", Role: "ops"},
		"no principal":    {TenantID: evdTenant, Role: "ops"},
		"no role":         {TenantID: evdTenant, PrincipalID: "usr_01"},
		"no tenant named": {PrincipalID: "usr_01", Role: "ops"},
	} {
		if got, err := w.reader(t, evdPools(w.trust)).ScopeFor(evdCtx(t), evdTenant, evdCorr, s); !errors.Is(err, trustport.ErrSession) || got != (trustport.RouteScope{}) {
			t.Errorf("%s: %+v, %v; want ErrSession and the zero scope", name, got, err)
		}
	}
}

func TestRouteScopeRefusesAnUnconfiguredReaderAndACallWithNoDeadline(t *testing.T) { // A625, AGENT-16
	if _, err := trustport.NewRouteScopeReader(nil, time.Second); !errors.Is(err, trustport.ErrConfig) {
		t.Errorf("no logins: %v; want ErrConfig", err)
	}
	if _, err := trustport.NewRouteScopeReader(evdPools{}, 0); !errors.Is(err, trustport.ErrConfig) {
		t.Errorf("no timeout: %v; want ErrConfig", err)
	}
	w := rscNewWorld(t)
	if _, err := w.reader(t, evdPools(w.trust)).ScopeFor(context.Background(), evdTenant, evdCorr, rscSession); !errors.Is(err, trustport.ErrEvidenceNoDeadline) {
		t.Errorf("no deadline: %v; want ErrEvidenceNoDeadline", err)
	}
	if _, err := w.reader(t, evdPools{}).ScopeFor(evdCtx(t), evdTenant, evdCorr, rscSession); !errors.Is(err, trustport.ErrSnapshotStore) {
		t.Errorf("a tenant with no login: %v; want ErrSnapshotStore", err)
	}
}

// BD-05: the Trust login the reader runs under holds no privilege on the investigation, so the reader cannot read it even by mistake.
func TestTheTrustLoginCannotReadTheInvestigation(t *testing.T) { // A625, BD-05
	w := rscNewWorld(t)
	var n int
	if err := w.trust[evdTenant].QueryRow(evdCtx(t), `SELECT count(*) FROM investigation`).Scan(&n); err == nil {
		t.Fatalf("trust_rw read the investigation table (%d rows): Trust must never read the investigation", n)
	}
}
