package reconcilerport_test

// TRU-07 (A329): the Store, the Snapshots port over the tenant's own Trust login: REQ-TRU-003, REQ-CTX-004, REQ-SEC-004; INV-11, INV-12, INV-19; AGENT-13, AGENT-16.
// These tests run without a database: the transaction and its rows are a spy that records what was asked (the SQL's tenant binding, the isolation level) and answers with rows.
// The same questions are asked of a real database in store_db_test.go. SYNTHETIC.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Noemai-org/noema/services/core/evidence/layer"
	reconcilerport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust/reconciler"
)

// spyRows answers the query of the items with the rows it holds, as pgx.Rows does, and counts its Close.
type spyRows struct {
	pgx.Rows
	rows   [][]any
	at     int
	closed *int
	err    error
}

func (r *spyRows) Next() bool {
	if r.err != nil || r.at >= len(r.rows) {
		return false
	}
	r.at++
	return true
}
func (r *spyRows) Err() error { return r.err }
func (r *spyRows) Close()     { *r.closed++ }
func (r *spyRows) Scan(dest ...any) error {
	return assign(dest, r.rows[r.at-1])
}

type spyRow struct {
	values []any
	err    error
}

func (r spyRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	return assign(dest, r.values)
}

// assign copies the values into the destinations of the types the store scans into: text, text arrays, timestamps and integers.
func assign(dest, values []any) error {
	if len(dest) != len(values) {
		return fmt.Errorf("scan: %d destinations for %d values", len(dest), len(values))
	}
	for i, d := range dest {
		switch p := d.(type) {
		case *string:
			*p = values[i].(string)
		case *[]string:
			*p = values[i].([]string)
		case *time.Time:
			*p = values[i].(time.Time)
		case *int64:
			*p = values[i].(int64)
		case *int32:
			*p = values[i].(int32)
		case *int:
			*p = values[i].(int)
		default:
			return fmt.Errorf("scan: destination %T is not one the spy knows", d)
		}
	}
	return nil
}

type queryLog struct {
	sql  string
	args []any
}

// spyTx is a transaction of one tenant's login.
type spyTx struct {
	pgx.Tx
	snapshot  spyRow
	items     [][]any
	itemsErr  error
	queries   []queryLog
	rolled    int
	committed int
	closed    int
}

func (x *spyTx) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	x.queries = append(x.queries, queryLog{sql, args})
	return x.snapshot
}
func (x *spyTx) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	x.queries = append(x.queries, queryLog{sql, args})
	return &spyRows{rows: x.items, closed: &x.closed, err: x.itemsErr}, nil
}
func (x *spyTx) Rollback(context.Context) error { x.rolled++; return nil }
func (x *spyTx) Commit(context.Context) error   { x.committed++; return nil }

// spyPool begins transactions of one tenant's login.
type spyPool struct {
	tx      *spyTx
	opts    []pgx.TxOptions
	beginTo error
	begun   int
}

func (p *spyPool) BeginTx(_ context.Context, o pgx.TxOptions) (pgx.Tx, error) {
	p.begun++
	p.opts = append(p.opts, o)
	if p.beginTo != nil {
		return nil, p.beginTo
	}
	return p.tx, nil
}

var (
	t1 = time.Date(2026, 11, 17, 9, 0, 0, 123456000, time.FixedZone("WAT", 3600)) // a stored timestamp comes back in the session's zone
	t2 = time.Date(2026, 11, 17, 8, 59, 0, 0, time.UTC)
)

func snapRow(tenantID string) spyRow {
	return spyRow{values: []any{tenantID, snapID, []string{"evr_a", "evr_b"}, "rec_synthetic", "SUCCESS_CONFIRMED", "sha256:" + strings.Repeat("ab", 32), sealedAt}}
}

// the items as the database may return them: in no order, and the payload hash and the rest as stored
func itemRows() [][]any {
	return [][]any{
		{"evr_b", "pay_b", "sha256:" + strings.Repeat("02", 32), sw, "KNOWN", t1, t2, int64(300)},
		{"evr_a", "pay_a", "sha256:" + strings.Repeat("01", 32), ledger, "STALE", t1, t2, int64(60)},
	}
}

func newSpy(tenantID string) *spyPool {
	return &spyPool{tx: &spyTx{snapshot: snapRow(tenantID), items: itemRows()}}
}

func storeOf(t *testing.T, pools map[string]reconcilerport.Beginner) *reconcilerport.Store {
	t.Helper()
	s, err := reconcilerport.NewStore(pools)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// REQ-CTX-004, INV-12: the store reads as the tenant's own login, in one read-only repeatable-read transaction (the snapshot and its items are one view), binds the tenant in both
// queries, and always ends the transaction without committing anything.
func TestTheStoreReadsOneSnapshotInOneReadOnlyTransactionBoundToTheTenant(t *testing.T) {
	mine, theirs := newSpy(tenant), newSpy(foreign)
	s := storeOf(t, map[string]reconcilerport.Beginner{tenant: mine, foreign: theirs})
	got, err := s.Sealed(ctx(t), tenant, snapID)
	if err != nil {
		t.Fatal(err)
	}
	if theirs.begun != 0 {
		t.Fatal("another tenant's login was used")
	}
	if mine.begun != 1 || mine.opts[0].IsoLevel != pgx.RepeatableRead || mine.opts[0].AccessMode != pgx.ReadOnly {
		t.Fatalf("begun %d times with %+v, want one read-only repeatable-read transaction", mine.begun, mine.opts)
	}
	x := mine.tx
	if len(x.queries) != 2 || x.rolled != 1 || x.committed != 0 || x.closed != 1 {
		t.Fatalf("queries %d, rollbacks %d, commits %d, rows closed %d; want 2, 1, 0, 1", len(x.queries), x.rolled, x.committed, x.closed)
	}
	if a := x.queries[0].args; len(a) != 2 || a[0] != tenant || a[1] != snapID || !strings.Contains(x.queries[0].sql, "context_snapshot") || !strings.Contains(x.queries[0].sql, "tenant_id = $1") {
		t.Errorf("the snapshot query: %q %v", x.queries[0].sql, a)
	}
	if a := x.queries[1].args; len(a) != 2 || a[0] != tenant || !strings.Contains(x.queries[1].sql, "evidence_item") || !strings.Contains(x.queries[1].sql, "tenant_id = $1") {
		t.Errorf("the items query: %q %v", x.queries[1].sql, a)
	} else if ids, ok := a[1].([]string); !ok || strings.Join(ids, ",") != "evr_a,evr_b" {
		t.Errorf("the items query asked for %v, want the snapshot's ids", a[1])
	}
	if got.TenantID != tenant || got.SnapshotID != snapID || got.ReconciliationID != "rec_synthetic" || got.Classification != "SUCCESS_CONFIRMED" || got.SealedAt != sealedAt ||
		strings.Join(got.EvidenceIDs, ",") != "evr_a,evr_b" || got.EvidenceSetHash != "sha256:"+strings.Repeat("ab", 32) {
		t.Errorf("the snapshot: %+v", got)
	}
}

// The items come back in evidence-id order whatever the database's collation did, in UTC, with the status as stored and the source's own freshness limit.
func TestTheStoreReturnsTheItemsSortedInUTCAsStored(t *testing.T) {
	s := storeOf(t, map[string]reconcilerport.Beginner{tenant: newSpy(tenant)})
	got, err := s.Sealed(ctx(t), tenant, snapID)
	if err != nil {
		t.Fatal(err)
	}
	want := []layer.Item{
		{EvidenceID: "evr_a", PayloadRef: "pay_a", PayloadHash: "sha256:" + strings.Repeat("01", 32), Status: "STALE", Source: ledger, ReadAt: t1.UTC(), EffectiveAt: t2, EffectiveFreshnessSeconds: 60},
		{EvidenceID: "evr_b", PayloadRef: "pay_b", PayloadHash: "sha256:" + strings.Repeat("02", 32), Status: "KNOWN", Source: sw, ReadAt: t1.UTC(), EffectiveAt: t2, EffectiveFreshnessSeconds: 300},
	}
	if len(got.Items) != 2 {
		t.Fatalf("%d items", len(got.Items))
	}
	for i, it := range got.Items {
		if it != want[i] || it.ReadAt.Location() != time.UTC {
			t.Errorf("item %d = %+v, want %+v", i, it, want[i])
		}
	}
}

// REQ-TRU-003: no row for the tenant (another tenant's id, an id that does not exist) is ErrNotFound; an id that is not the shape of a snapshot id never reaches the database.
func TestTheStoreAnswersNotFoundForAnAbsentRowAndNeverAsksForAMalformedID(t *testing.T) {
	p := newSpy(tenant)
	p.tx.snapshot = spyRow{err: pgx.ErrNoRows}
	s := storeOf(t, map[string]reconcilerport.Beginner{tenant: p})
	if _, err := s.Sealed(ctx(t), tenant, "evs_nosuch01"); !errors.Is(err, reconcilerport.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if p.tx.rolled != 1 {
		t.Error("the transaction was not ended")
	}
	p.begun = 0
	for _, bad := range []string{"", "evs_", "evs_a", "evs_" + strings.Repeat("a", 33), "snap_synth01", "evs_syn th", "evs_syn;--", "EVS_synth01", "evs_synth01\n"} {
		if _, err := s.Sealed(ctx(t), tenant, bad); !errors.Is(err, reconcilerport.ErrNotFound) {
			t.Errorf("%q: err = %v, want ErrNotFound", bad, err)
		}
	}
	if p.begun != 0 {
		t.Errorf("%d transactions for malformed ids", p.begun)
	}
}

// INV-11: a row that is not the tenant's (it cannot be, with row-level security; this is the second lock) is refused as not found, and its items are never read.
func TestTheStoreRefusesARowOfAnotherTenantAsNotFound(t *testing.T) {
	p := newSpy(foreign)
	s := storeOf(t, map[string]reconcilerport.Beginner{tenant: p})
	if _, err := s.Sealed(ctx(t), tenant, snapID); !errors.Is(err, reconcilerport.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if len(p.tx.queries) != 1 {
		t.Errorf("%d queries: the items of another tenant's snapshot were read", len(p.tx.queries))
	}
}

// INV-12, AGENT-16: a tenant with no login is ErrNoLogin and never falls back to another tenant's login; no tenant at all and no deadline are refused and the database is not asked.
func TestTheStoreHasNoFallbackLoginAndNeedsATenantAndADeadline(t *testing.T) {
	p := newSpy(tenant)
	s := storeOf(t, map[string]reconcilerport.Beginner{tenant: p})
	if _, err := s.Sealed(ctx(t), foreign, snapID); !errors.Is(err, reconcilerport.ErrNoLogin) {
		t.Errorf("a tenant with no login: err = %v, want ErrNoLogin", err)
	}
	if _, err := s.Sealed(ctx(t), "", snapID); !errors.Is(err, reconcilerport.ErrNoLogin) {
		t.Errorf("no tenant: err = %v, want ErrNoLogin", err)
	}
	if _, err := s.Sealed(context.Background(), tenant, snapID); !errors.Is(err, reconcilerport.ErrNoDeadline) {
		t.Errorf("no deadline: err = %v, want ErrNoDeadline", err)
	}
	if p.begun != 0 {
		t.Errorf("%d transactions were begun for refused calls", p.begun)
	}
}

// INV-19, AGENT-13: a database that fails (begin, the snapshot read, the items read, a row that cannot be scanned) is ErrStore: the cause is reachable and never printed, and the transaction
// is ended whenever it was begun.
func TestTheStoreErrorsAreFixedTextWithTheCauseBehindThem(t *testing.T) {
	cause := errors.New("connect " + dsnLeak)
	for name, mutate := range map[string]func(*spyPool){
		"begin":         func(p *spyPool) { p.beginTo = cause },
		"snapshot read": func(p *spyPool) { p.tx.snapshot = spyRow{err: cause} },
		"items read":    func(p *spyPool) { p.tx.itemsErr = cause },
		"items scan":    func(p *spyPool) { p.tx.items = [][]any{{"evr_a"}} },
	} {
		p := newSpy(tenant)
		mutate(p)
		_, err := storeOf(t, map[string]reconcilerport.Beginner{tenant: p}).Sealed(ctx(t), tenant, snapID)
		if !errors.Is(err, reconcilerport.ErrStore) || errors.Is(err, reconcilerport.ErrNotFound) {
			t.Errorf("%s: err = %v, want ErrStore and not ErrNotFound", name, err)
			continue
		}
		if name != "items scan" && !errors.Is(err, cause) {
			t.Errorf("%s: the cause is not reachable: %v", name, err)
		}
		for _, s := range []string{"hunter2", "db.internal", "trust_rw", tenant, snapID, "evr_a"} {
			if strings.Contains(err.Error(), s) {
				t.Errorf("%s: the error carries %q: %v", name, s, err)
			}
		}
		if name != "begin" && p.tx.rolled != 1 {
			t.Errorf("%s: the transaction was not ended", name)
		}
	}
}

// AGENT-16: a store with no login, an empty one or one with a nil login is not usable.
func TestTheStoreNeedsALoginForEveryTenantItNames(t *testing.T) {
	for name, pools := range map[string]map[string]reconcilerport.Beginner{
		"none":       nil,
		"empty":      {},
		"a nil pool": {tenant: nil},
		"a bad id":   {"ten x": newSpy(tenant)},
	} {
		if _, err := reconcilerport.NewStore(pools); !errors.Is(err, reconcilerport.ErrConfig) {
			t.Errorf("%s: err = %v, want ErrConfig", name, err)
		}
	}
	var nilStore *reconcilerport.Store
	if _, err := nilStore.Sealed(ctx(t), tenant, snapID); !errors.Is(err, reconcilerport.ErrConfig) {
		t.Errorf("a nil store: err = %v, want ErrConfig", err)
	}
}

// The store is a Snapshots; a set of pgx pools is its logins.
var _ reconcilerport.Snapshots = (*reconcilerport.Store)(nil)
