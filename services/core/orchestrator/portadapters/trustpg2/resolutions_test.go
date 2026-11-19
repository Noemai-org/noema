package trustpg2_test

// TRU-12: the Postgres store of resolved confirmations (migration 0028; REQ-TRU-009, REQ-AUD-001; INV-11, INV-17, INV-25). These tests run against a real PostgreSQL with real
// tenant logins and forced row security: they need the CI database (a local one may lack the signing extension). All data is SYNTHETIC.

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/migrations"
	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trustpg2"
	"github.com/Noemai-org/noema/services/core/platform/db"
)

func TestMain(m *testing.M) { testpg.Main(m) }

const (
	tenantA = "ten_aa"
	tenantB = "ten_bb"
)

var base = time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)

type world struct {
	admin      *pgxpool.Pool
	trustA     trustpg2.Resolutions
	trustB     trustpg2.Resolutions
	trustAPool *pgxpool.Pool
}

func ctx(t testing.TB) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	return c
}

func newWorld(t *testing.T) *world {
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
	a := testpg.TenantLogin(t, admin, dsn, "trust_rw", tenantA)
	b := testpg.TenantLogin(t, admin, dsn, "trust_rw", tenantB)
	return &world{admin: admin, trustA: trustpg2.Resolutions{Pool: a}, trustB: trustpg2.Resolutions{Pool: b}, trustAPool: a}
}

func (w *world) rows(t *testing.T) int {
	t.Helper()
	var n int
	if err := w.admin.QueryRow(ctx(t), `SELECT count(*) FROM confirmation_resolution`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func hash(c byte) string {
	b := make([]byte, 64)
	for i := range b {
		b[i] = c
	}
	return "sha256:" + string(b)
}

func resolved(event, request string) trustport.ResolvedConfirmation {
	return trustport.ResolvedConfirmation{TenantID: tenantA, EventID: event, RequestID: request, BindingHash: hash('a'),
		AcceptedAt: base.Add(-time.Minute), ExpiresAt: base.Add(time.Hour)}
}

func TestRecordInsertsTheResolutionOnceAndReadsItBack(t *testing.T) {
	w := newWorld(t)
	c := resolved("cfe_01AA", "cfr_01AA")
	if err := w.trustA.Record(ctx(t), c); err != nil {
		t.Fatal(err)
	}
	got, found, err := w.trustA.Get(ctx(t), tenantA, "cfe_01AA")
	if err != nil || !found || got.RequestID != "cfr_01AA" || got.BindingHash != c.BindingHash || !got.ExpiresAt.Equal(c.ExpiresAt) || !got.AcceptedAt.Equal(c.AcceptedAt) {
		t.Fatalf("got %+v found %v err %v", got, found, err)
	}
	if n := w.rows(t); n != 1 {
		t.Fatalf("%d rows, want 1", n)
	}
}

func TestDoubleRecordOfTheSameResolutionIsIdempotentOneRow(t *testing.T) {
	w := newWorld(t)
	c := resolved("cfe_01AA", "cfr_01AA")
	for i := 0; i < 3; i++ {
		if err := w.trustA.Record(ctx(t), c); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if n := w.rows(t); n != 1 {
		t.Fatalf("%d rows, want 1", n)
	}
}

func TestConcurrentRecordsOfTheSameResolutionWriteOneRow(t *testing.T) {
	w := newWorld(t)
	c := resolved("cfe_01AA", "cfr_01AA")
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- w.trustA.Record(ctx(t), c)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if n := w.rows(t); n != 1 {
		t.Fatalf("%d rows, want 1", n)
	}
}

func TestADifferentResolutionForTheSameEventOrRequestIsAConflictAndWritesNothing(t *testing.T) {
	w := newWorld(t)
	if err := w.trustA.Record(ctx(t), resolved("cfe_01AA", "cfr_01AA")); err != nil {
		t.Fatal(err)
	}
	otherBinding := resolved("cfe_01AA", "cfr_01AA")
	otherBinding.BindingHash = hash('b')
	secondEvent := resolved("cfe_01BB", "cfr_01AA") // a request is answered once
	for name, c := range map[string]trustport.ResolvedConfirmation{"same event, other binding": otherBinding, "same request, other event": secondEvent} {
		t.Run(name, func(t *testing.T) {
			if err := w.trustA.Record(ctx(t), c); !errors.Is(err, trustpg2.ErrConflict) {
				t.Fatalf("got %v, want ErrConflict", err)
			}
			if n := w.rows(t); n != 1 {
				t.Fatalf("%d rows, want 1", n)
			}
		})
	}
}

func TestRecordRefusesAFieldOutsideTheContractAndAContextWithNoDeadline(t *testing.T) {
	w := newWorld(t)
	bad := map[string]func(*trustport.ResolvedConfirmation){
		"event":       func(c *trustport.ResolvedConfirmation) { c.EventID = "evt_1" },
		"request":     func(c *trustport.ResolvedConfirmation) { c.RequestID = "x" },
		"hash":        func(c *trustport.ResolvedConfirmation) { c.BindingHash = "sha256:zz" },
		"tenant":      func(c *trustport.ResolvedConfirmation) { c.TenantID = "" },
		"no accepted": func(c *trustport.ResolvedConfirmation) { c.AcceptedAt = time.Time{} },
		"expiry":      func(c *trustport.ResolvedConfirmation) { c.ExpiresAt = c.AcceptedAt.Add(-time.Second) },
	}
	for name, mutate := range bad {
		t.Run(name, func(t *testing.T) {
			c := resolved("cfe_01AA", "cfr_01AA")
			mutate(&c)
			if err := w.trustA.Record(ctx(t), c); !errors.Is(err, trustpg2.ErrRequest) {
				t.Fatalf("got %v, want ErrRequest", err)
			}
		})
	}
	if err := w.trustA.Record(context.Background(), resolved("cfe_01AA", "cfr_01AA")); !errors.Is(err, trustpg2.ErrRequest) {
		t.Fatalf("no deadline: %v", err)
	}
	if n := w.rows(t); n != 0 {
		t.Fatalf("%d rows, want 0", n)
	}
}

func TestATenantLoginCannotRecordOrReadForAnotherTenant(t *testing.T) {
	w := newWorld(t)
	if err := w.trustA.Record(ctx(t), resolved("cfe_01AA", "cfr_01AA")); err != nil {
		t.Fatal(err)
	}
	if err := w.trustB.Record(ctx(t), resolved("cfe_01BB", "cfr_01BB")); err == nil {
		t.Fatal("tenant B's login wrote a row for tenant A")
	}
	if _, found, err := w.trustB.Get(ctx(t), tenantA, "cfe_01AA"); err != nil || found {
		t.Fatalf("tenant B read tenant A's resolution: found %v err %v", found, err)
	}
	if n := w.rows(t); n != 1 {
		t.Fatalf("%d rows, want 1", n)
	}
}

func TestAResolutionIsNeverChangedOrDeletedEvenByItsOwnLogin(t *testing.T) {
	w := newWorld(t)
	if err := w.trustA.Record(ctx(t), resolved("cfe_01AA", "cfr_01AA")); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`UPDATE confirmation_resolution SET binding_hash = '` + hash('b') + `'`, `DELETE FROM confirmation_resolution`} {
		if _, err := w.trustAPool.Exec(ctx(t), q); err == nil {
			t.Fatalf("%s succeeded", q)
		}
	}
	if got, _, _ := w.trustA.Get(ctx(t), tenantA, "cfe_01AA"); got.BindingHash != hash('a') {
		t.Fatalf("the row changed: %+v", got)
	}
}
