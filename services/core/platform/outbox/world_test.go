package outbox_test

// Shared fixtures of the PLT-08 tests. All data is SYNTHETIC (AGENT-25). The "state" table is a plain table the tests create in a throwaway database: the tests are about
// the outbox, not about one producer's domain tables, so the state change is a row of this table. The two producers (execution/ledger, outcome/store) have their own
// crash tests that run through their domain transactions and the Record (CI only: they need pgsodium).

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/migrations"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/db"
	"github.com/Noemai-org/noema/services/core/platform/outbox"
)

const (
	tenantA = "ten_aa"
	tenantB = "ten_bb"
	topicV  = "outcome.verify"
)

// secret is a marker that sits in payloads and in handler errors: no error text of the package may carry it (the payload is data and may hold identifiers).
const secret = "PAYLOAD-MARKER-7f3a"

func ctx(t testing.TB) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	return c
}

// oid returns the n-th outbox ID: `obx_` and 26 Crockford characters, in the order of n.
func oid(n int) string { return fmt.Sprintf("obx_%026d", n) }

// payload is a JSON object that carries the marker.
func payload(n int) []byte {
	return []byte(fmt.Sprintf(`{"grant_id":"grt_%d","note":"%s"}`, n, secret))
}

type world struct {
	t     *testing.T
	admin *pgxpool.Pool // the migrating superuser
	dsn   string
	// one login per tenant and power, as DEBT-04 requires: row security keys on session_user
	execA, orchA, execB, orchB *pgxpool.Pool
	clk                        *clock.Manual
}

func newWorld(t *testing.T) *world {
	t.Helper()
	admin, dsn := testpg.NewDatabase(t)
	if _, err := db.Migrate(ctx(t), admin, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	for _, q := range []string{
		`INSERT INTO tenant (tenant_id, name) VALUES ('` + tenantA + `', 'A'), ('` + tenantB + `', 'B')`,
		// SYNTHETIC state table: the "state change" that an event must commit with.
		`CREATE TABLE plt08_state (tenant_id text NOT NULL REFERENCES tenant (tenant_id), id text PRIMARY KEY, v text NOT NULL CHECK (v <> 'refuse'))`,
		`GRANT SELECT, INSERT ON plt08_state TO execution_rw`,
	} {
		if _, err := admin.Exec(ctx(t), q); err != nil {
			t.Fatalf("setup %q: %v", q, err)
		}
	}
	return &world{
		t: t, admin: admin, dsn: dsn, clk: clock.NewManual(time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)),
		execA: testpg.TenantLogin(t, admin, dsn, "execution_rw", tenantA),
		orchA: testpg.TenantLogin(t, admin, dsn, "orchestrator_rw", tenantA),
		execB: testpg.TenantLogin(t, admin, dsn, "execution_rw", tenantB),
		orchB: testpg.TenantLogin(t, admin, dsn, "orchestrator_rw", tenantB),
	}
}

func (w *world) count(q string, args ...any) int {
	w.t.Helper()
	var n int
	if err := w.admin.QueryRow(ctx(w.t), q, args...).Scan(&n); err != nil {
		w.t.Fatalf("%s: %v", q, err)
	}
	return n
}

// enqueue writes one pending event for the tenant of the pool's login, in a transaction of its own that commits.
func (w *world) enqueue(pool *pgxpool.Pool, tenant string, n int, topic string) {
	w.t.Helper()
	tx, err := pool.Begin(ctx(w.t))
	if err != nil {
		w.t.Fatal(err)
	}
	e := outbox.Event{OutboxID: oid(n), TenantID: tenant, Topic: topic, Payload: payload(n), CreatedAt: w.clk.Now()}
	w.clk.Advance(time.Millisecond)
	if err := outbox.Enqueue(ctx(w.t), tx, e); err != nil {
		_ = tx.Rollback(ctx(w.t))
		w.t.Fatalf("enqueue %d: %v", n, err)
	}
	if err := tx.Commit(ctx(w.t)); err != nil {
		w.t.Fatal(err)
	}
}

// pending lists the pending outbox IDs of a tenant, oldest first, read as the migrating superuser.
func (w *world) pending(tenant string) []string {
	w.t.Helper()
	rows, err := w.admin.Query(ctx(w.t), `SELECT outbox_id FROM outbox_event WHERE tenant_id = $1 AND dispatched_at IS NULL ORDER BY created_at, outbox_id`, tenant)
	if err != nil {
		w.t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			w.t.Fatal(err)
		}
		out = append(out, id)
	}
	return out
}

func (w *world) dispatcher(pool *pgxpool.Pool, tenant string, h outbox.Handler, max int) *outbox.Dispatcher {
	w.t.Helper()
	d, err := outbox.New(pool, w.clk, outbox.Config{TenantID: tenant, Handlers: map[string]outbox.Handler{topicV: h}, MaxEvents: max, DeliveryTimeout: 20 * time.Second})
	if err != nil {
		w.t.Fatalf("New: %v", err)
	}
	return d
}

// recorder is a handler that remembers what it was given, in order.
type recorder struct {
	mu   sync.Mutex
	seen []outbox.Delivery
	err  error // returned for every delivery when set
}

func (r *recorder) Deliver(_ context.Context, d outbox.Delivery) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seen = append(r.seen, d)
	return r.err
}

func (r *recorder) ids() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.seen))
	for _, d := range r.seen {
		out = append(out, d.OutboxID)
	}
	return out
}

// nopBeginner is a database that is never reached: a unit test that must fail before any call proves it by this (a call would return an error that is not the one the test wants).
type nopBeginner struct{}

func (nopBeginner) Begin(context.Context) (pgx.Tx, error) {
	return nil, errors.New("test: the database must not be reached")
}
