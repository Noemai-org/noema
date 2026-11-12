package intentstore_test

// A151 (the admitted-intent store; REQ-INT-001, REQ-SEC-004, REQ-AUD-001; INV-11, INV-12, INV-17): the test world. A real PostgreSQL with every migration and one
// login per tenant and power. All data is SYNTHETIC.

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/internal/recordsig"
	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/migrations"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/db"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/record/chain"
	"github.com/Noemai-org/noema/services/core/understanding/intentstore"
)

func TestMain(m *testing.M) { testpg.Main(m) }

const (
	tenantA = "ten_aa"
	tenantB = "ten_bb"
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

type world struct {
	t     *testing.T
	admin *pgxpool.Pool
	dsn   string
	clk   *clock.Manual
	fail  *failingIDs
	sign  map[string]chain.Signer  // one signer per emitter (REC-08): an emitter with no signer cannot append
	pools map[string]*pgxpool.Pool // "<group>/<tenant>"
}

func newWorld(t *testing.T) *world {
	t.Helper()
	admin, dsn := testpg.NewDatabase(t)
	if _, err := db.Migrate(ctx(t), admin, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	clk := clock.NewManual(time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC))
	w := &world{t: t, admin: admin, dsn: dsn, clk: clk, fail: &failingIDs{inner: ids.New(clk, nil)}, pools: map[string]*pgxpool.Pool{}}
	w.sign = recordsig.All(t, admin, "intentstore", nil)
	for _, id := range []string{tenantA, tenantB} {
		if _, err := admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ($1, 'Synthetic')`, id); err != nil {
			t.Fatal(err)
		}
	}
	return w
}

// as is the tenant's own login of one power (group role), made on first use.
func (w *world) as(group, tenant string) *pgxpool.Pool {
	w.t.Helper()
	k := group + "/" + tenant
	if w.pools[k] == nil {
		w.pools[k] = testpg.TenantLogin(w.t, w.admin, w.dsn, group, tenant)
	}
	return w.pools[k]
}

func (w *world) store(tenant string) *intentstore.Store {
	w.t.Helper()
	return &intentstore.Store{Pool: w.as("orchestrator_rw", tenant), Appender: &chain.Appender{Clock: w.clk, IDs: w.fail, Signers: w.sign}, Timeout: 5 * time.Second}
}

func (w *world) reader(group, tenant string) *intentstore.Reader {
	w.t.Helper()
	return &intentstore.Reader{Pool: w.as(group, tenant), Timeout: 5 * time.Second}
}

func (w *world) count(table string) int {
	w.t.Helper()
	var n int
	if err := w.admin.QueryRow(ctx(w.t), `SELECT count(*) FROM `+table).Scan(&n); err != nil {
		w.t.Fatal(err)
	}
	return n
}
