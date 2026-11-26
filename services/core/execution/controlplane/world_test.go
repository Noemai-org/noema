package controlplane_test

// EXE-06, the control plane (REQ-EXE-008, REQ-OPS-002; INV-03, INV-11, INV-12; ruling A71, interim, relayed, not yet committed): the test world. A real PostgreSQL with
// every migration, one admin emitter login per tenant (migration 0011) and the per-power logins that read the switch. All data is SYNTHETIC.

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/execution/controlplane"
	"github.com/Noemai-org/noema/services/core/internal/recordsig"
	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/migrations"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/db"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/record/chain"
)

func TestMain(m *testing.M) { testpg.Main(m) }

const (
	tenantA = "ten_aa"
	tenantB = "ten_bb"
	corr    = "corr_ctl0001" // SYNTHETIC
	person  = "usr_admin1"   // SYNTHETIC
)

func ctx(t testing.TB) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return c
}

// failingIDs fails the Record event IDs while fail is set, so that the append of the event fails (EC-E16).
type failingIDs struct {
	mu    sync.Mutex
	inner chain.IDSource
	fail  bool
}

func (f *failingIDs) ID(prefix string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail && prefix == "recev" {
		return "", errors.New("synthetic: the Record refused the event")
	}
	return f.inner.ID(prefix)
}

func (f *failingIDs) set(v bool) { f.mu.Lock(); f.fail = v; f.mu.Unlock() }

// logins maps a tenant to its admin emitter login pool; a tenant with none is an error, as the composition's would be.
type logins map[string]*pgxpool.Pool

func (l logins) For(tenant string) (controlplane.Beginner, error) {
	p, ok := l[tenant]
	if !ok {
		return nil, errors.New("synthetic: no admin login for this tenant")
	}
	return p, nil
}

type world struct {
	t      *testing.T
	admin  *pgxpool.Pool
	dsn    string
	clk    *clock.Manual
	fail   *failingIDs
	admins logins
	trust  map[string]*pgxpool.Pool
	exec   map[string]*pgxpool.Pool
	sw     *controlplane.Switch
}

func newWorld(t *testing.T) *world {
	t.Helper()
	admin, dsn := testpg.NewDatabase(t)
	if _, err := db.Migrate(ctx(t), admin, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	clk := clock.NewManual(time.Date(2026, 11, 25, 9, 0, 0, 0, time.UTC))
	gen := ids.New(clk, nil)
	w := &world{t: t, admin: admin, dsn: dsn, clk: clk, fail: &failingIDs{inner: gen}, admins: logins{}, trust: map[string]*pgxpool.Pool{}, exec: map[string]*pgxpool.Pool{}}
	sign := recordsig.All(t, admin, "controlplane", nil)
	for _, id := range []string{tenantA, tenantB} {
		if _, err := admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ($1, 'Synthetic')`, id); err != nil {
			t.Fatal(err)
		}
		w.admins[id] = testpg.EmitterLogin(t, admin, dsn, id, "admin")
		w.trust[id] = testpg.TenantLogin(t, admin, dsn, "trust_rw", id)
		w.exec[id] = testpg.TenantLogin(t, admin, dsn, "execution_rw", id)
	}
	w.sw = &controlplane.Switch{Logins: w.admins, Appender: &chain.Appender{Clock: clk, IDs: w.fail, Signers: sign}, Timeout: 5 * time.Second}
	return w
}

// on reads a tenant's switch as the administrator.
func (w *world) on(tenant string) bool {
	w.t.Helper()
	var on bool
	if err := w.admin.QueryRow(ctx(w.t), `SELECT kill_switch FROM tenant WHERE tenant_id = $1`, tenant).Scan(&on); err != nil {
		w.t.Fatal(err)
	}
	return on
}

// setAs sets the switch directly as the administrator (the break-glass path), to prepare a state.
func (w *world) setAs(tenant string, on bool) {
	w.t.Helper()
	if _, err := w.admin.Exec(ctx(w.t), `UPDATE tenant SET kill_switch = $2 WHERE tenant_id = $1`, tenant, on); err != nil {
		w.t.Fatal(err)
	}
}

type stored struct {
	Seq       int64
	Tenant    string
	WrittenBy string
	Actor     string
	EventType string
	Kind      string
	Payload   map[string]any
}

// events are the tenant's Record events in chain order, read as the administrator.
func (w *world) events(tenant string) []stored {
	w.t.Helper()
	rows, err := w.admin.Query(ctx(w.t), `SELECT seq, tenant_id, written_by, actor, event_type, COALESCE(body->>'kind', ''), payload::text FROM record_event WHERE tenant_id = $1 ORDER BY seq`, tenant)
	if err != nil {
		w.t.Fatal(err)
	}
	defer rows.Close()
	var out []stored
	for rows.Next() {
		var s stored
		var p string
		if err := rows.Scan(&s.Seq, &s.Tenant, &s.WrittenBy, &s.Actor, &s.EventType, &s.Kind, &p); err != nil {
			w.t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(p), &s.Payload); err != nil {
			w.t.Fatal(err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		w.t.Fatal(err)
	}
	return out
}
