package inputgateway_test

// UND-02, core half (ruling A161; REQ-INP-002, REQ-INP-003, REQ-AUD-001; INV-12, INV-11, EC-E16): the test world. A real PostgreSQL with every migration, the
// core-edge emitter login that appends and the tenant's record_read login that looks a request up. All data is SYNTHETIC.

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/inputgateway"
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
	reqID   = "0b8f3c1e-52a4-4d7e-9a61-3c0f5d2e7b19" // SYNTHETIC
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

// spy is the orchestrator seam: it records every turn it is handed, and what the Record held at that moment.
type spy struct {
	mu       sync.Mutex
	w        *world
	turns    []inputgateway.Turn
	received []int // INPUT_RECEIVED events in the Record when the turn arrived
	err      error
}

func (s *spy) Start(c context.Context, turn inputgateway.Turn) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.turns = append(s.turns, turn)
	s.received = append(s.received, len(s.w.events("INPUT_RECEIVED")))
	return s.err
}

type world struct {
	t     *testing.T
	admin *pgxpool.Pool
	dsn   string
	clk   *clock.Manual
	gen   *ids.Generator
	fail  *failingIDs
	edge  map[string]*pgxpool.Pool
	read  map[string]*pgxpool.Pool
}

func newWorld(t *testing.T) *world {
	t.Helper()
	admin, dsn := testpg.NewDatabase(t)
	if _, err := db.Migrate(ctx(t), admin, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	w := &world{t: t, admin: admin, dsn: dsn, clk: clock.NewManual(time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)), edge: map[string]*pgxpool.Pool{}, read: map[string]*pgxpool.Pool{}}
	w.gen = ids.New(w.clk, nil)
	w.fail = &failingIDs{inner: w.gen}
	for _, id := range []string{tenantA, tenantB} {
		if _, err := admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ($1, 'Synthetic')`, id); err != nil {
			t.Fatal(err)
		}
		w.edge[id] = testpg.EmitterLogin(t, admin, dsn, id, "core-edge")
		w.read[id] = testpg.TenantLogin(t, admin, dsn, "record_read", id)
	}
	return w
}

// gateway builds the gateway for one tenant over the real Record, with the spy as the orchestrator seam.
func (w *world) gateway(tenant string, sp *spy) *inputgateway.Gateway {
	w.t.Helper()
	sp.w = w
	g, err := inputgateway.New(inputgateway.Config{
		Recorder: &inputgateway.RecordRecorder{Pool: w.edge[tenant], Appender: &chain.Appender{Clock: w.clk, IDs: w.fail}, Timeout: 5 * time.Second},
		Index:    &inputgateway.RecordIndex{Pool: w.read[tenant], Clock: w.clk, Window: 24 * time.Hour, Timeout: 5 * time.Second},
		Start:    sp.Start,
		IDs:      w.gen,
		MaxBody:  64 << 10,
	})
	if err != nil {
		w.t.Fatal(err)
	}
	return g
}

func identity(tenant, principal string) inputgateway.Identity {
	return inputgateway.Identity{TenantID: tenant, PrincipalID: principal, PrincipalKind: "customer", SessionID: "ses_7a1b"}
}

// stored is one Record event as the tests read it (as the administrator, who can read the table).
type stored struct {
	Seq       int64
	EventID   string
	WrittenBy string
	Actor     string
	EventType string
	ObjectRef string
	Corr      string
	Payload   map[string]any
	Body      map[string]any
}

func (w *world) events(types ...string) []stored {
	w.t.Helper()
	rows, err := w.admin.Query(ctx(w.t), `SELECT seq, event_id, written_by, actor, event_type, object_ref, correlation_id, payload::text, body::text FROM record_event ORDER BY tenant_id, seq`)
	if err != nil {
		w.t.Fatal(err)
	}
	defer rows.Close()
	var out []stored
	for rows.Next() {
		var s stored
		var p, b string
		if err := rows.Scan(&s.Seq, &s.EventID, &s.WrittenBy, &s.Actor, &s.EventType, &s.ObjectRef, &s.Corr, &p, &b); err != nil {
			w.t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(p), &s.Payload); err != nil {
			w.t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(b), &s.Body); err != nil {
			w.t.Fatal(err)
		}
		if len(types) == 0 {
			out = append(out, s)
			continue
		}
		for _, ty := range types {
			if s.EventType == ty {
				out = append(out, s)
			}
		}
	}
	return out
}
