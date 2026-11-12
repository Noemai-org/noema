package ledger_test

// RCV-02 slice 2, ruling A153 (RCV-02 enforcement lives in the database), A3 and A62 (the dispatch budget is 2, per tenant, correlation, capability and
// target, counted over DISPATCH_ATTEMPTED events), REQ-RCV-003, INV-03, AGENT-08, EC-E06: a third dispatch of one key is impossible at the database, not
// only refused by a caller that asks. Whatever writes the event (Spend, a direct append, a concurrent run), the Record holds at most two
// DISPATCH_ATTEMPTED events of a key, and a refused Spend writes nothing and leaves the grant to be voided, never dispatched.

import (
	"errors"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/execution/controller"
	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/record/chain"
)

const budgetSQLState = "NS002"

func sqlState(err error) string {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return pe.Code
	}
	return ""
}

func dispatches(t *testing.T, w *world, corrID, capability, target string) int {
	t.Helper()
	return w.count(t, `SELECT count(*) FROM record_event WHERE tenant_id = $1 AND correlation_id = $2 AND event_type = 'DISPATCH_ATTEMPTED'
		AND payload->>'capability' = $3 AND payload->>'target_ref' = $4`, tenant, corrID, capability, target)
}

// A153: the first two dispatches of a key run; the third, under a new grant, is refused by the database, writes nothing and voids the grant.
func TestAThirdDispatchOfOneKeyIsRefusedAtTheDatabaseAndNothingIsWritten(t *testing.T) {
	w := newWorld(t)
	w.target.answer = controller.TransportResult{Class: controller.TransportTimeout}
	for n := 1; n <= 2; n++ {
		g := w.issue(t, n)
		if out := mustExecute(t, w, g.GrantID); out.Void != "" || out.Receipt == nil {
			t.Fatalf("dispatch %d: %+v", n, out)
		}
	}
	g3 := w.issue(t, 3)
	before := w.events(t)
	out := mustExecute(t, w, g3.GrantID)
	if out.Void != controller.VoidBudgetSpent || out.Receipt != nil {
		t.Fatalf("third dispatch: %+v, want the grant voided as BUDGET_SPENT with no receipt", out)
	}
	if w.target.count() != 2 {
		t.Fatalf("the target was called %d times, want 2", w.target.count())
	}
	if n := dispatches(t, w, corr, capab, "TX1"); n != 2 {
		t.Fatalf("%d DISPATCH_ATTEMPTED events for the key, want 2", n)
	}
	if w.count(t, `SELECT count(*) FROM grant_spend WHERE grant_id = $1`, g3.GrantID) != 0 {
		t.Fatal("the refused grant was spent")
	}
	if w.count(t, `SELECT count(*) FROM decision_grant WHERE grant_id = $1 AND status = 'VOID'`, g3.GrantID) != 1 {
		t.Fatal("the refused grant is not void")
	}
	if w.count(t, `SELECT attempt FROM idempotency_record WHERE tenant_id = $1 LIMIT 1`, tenant) != 2 {
		t.Fatal("the idempotency attempt moved for a refused dispatch")
	}
	for _, e := range w.events(t)[len(before):] {
		if e == "GRANT_SPENT" || e == "DISPATCH_ATTEMPTED" || e == "RECEIPT" {
			t.Fatalf("the refused dispatch wrote %s", e)
		}
	}
	verifyChain(t, w)
}

// A3: the key is tenant, correlation, capability and target; a dispatch of another key has a budget of its own.
func TestEachKeyHasItsOwnBudget(t *testing.T) {
	w := newWorld(t)
	w.target.answer = controller.TransportResult{Class: controller.TransportTimeout}
	n := 0
	run := func(mut func(*controller.Grant)) controller.Outcome {
		n++
		return mustExecute(t, w, w.issue(t, n, mut).GrantID)
	}
	for _, k := range []struct {
		name string
		mut  func(*controller.Grant)
	}{
		{"the first key", func(*controller.Grant) {}},
		{"another target", func(g *controller.Grant) { g.TargetRef = "TX9" }},
		{"another correlation", func(g *controller.Grant) { g.CorrelationID = "corr_other1" }},
	} {
		for i := 1; i <= 2; i++ {
			if out := run(k.mut); out.Void != "" {
				t.Fatalf("%s, dispatch %d: voided as %s", k.name, i, out.Void)
			}
		}
		if out := run(k.mut); out.Void != controller.VoidBudgetSpent {
			t.Fatalf("%s, third dispatch: %+v", k.name, out)
		}
	}
	if w.target.count() != 6 {
		t.Fatalf("%d dispatches, want 6 (two for each of three keys)", w.target.count())
	}
}

// putDispatch appends one DISPATCH_ATTEMPTED event as the tenant's own Execution login, outside Spend, and commits it.
func putDispatch(t *testing.T, w *world, pool *pgxpool.Pool, tenantID, corrID string, payload any) error {
	t.Helper()
	tx, err := pool.Begin(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx(t)) }()
	if _, err := w.led.Rec.Append(ctx(t), tx, chain.Event{TenantID: tenantID, CorrelationID: corrID, WrittenBy: "core-execution", Actor: "core-execution",
		EventType: "DISPATCH_ATTEMPTED", ObjectRef: "grt_direct", Payload: payload}); err != nil {
		return err
	}
	return tx.Commit(ctx(t))
}

// A153: the rule is the database's. Written directly (not through Spend), the third DISPATCH_ATTEMPTED of a key is refused, and so is one whose key
// cannot be read, because an unreadable key could be any key of its correlation (RCV-02 slice 1).
func TestTheDatabaseRefusesAThirdDispatchRecordWhateverWritesIt(t *testing.T) {
	w := newWorld(t)
	put := func(payload any) error { return putDispatch(t, w, w.exec, tenant, corr, payload) }
	key := map[string]any{"capability": capab, "target_ref": "TX1", "attempt": 1}
	for i := 1; i <= 2; i++ {
		if err := put(key); err != nil {
			t.Fatalf("direct dispatch record %d: %v", i, err)
		}
	}
	if err := put(key); sqlState(err) != budgetSQLState {
		t.Fatalf("third direct record: %v (SQLSTATE %q), want %s", err, sqlState(err), budgetSQLState)
	}
	for name, p := range map[string]any{
		"no capability":                   map[string]any{"target_ref": "TX7"},
		"no target":                       map[string]any{"capability": capab},
		"an empty target":                 map[string]any{"capability": capab, "target_ref": ""},
		"an empty capability":             map[string]any{"capability": "", "target_ref": "TX7"},
		"no payload":                      nil,
		"a payload that is not an object": []any{"x"},
	} {
		if err := put(p); sqlState(err) != budgetSQLState {
			t.Errorf("%s: %v (SQLSTATE %q), want %s", name, err, sqlState(err), budgetSQLState)
		}
	}
	if err := put(map[string]any{"capability": capab, "target_ref": "TX2"}); err != nil {
		t.Fatalf("another target: %v", err)
	}
	if n := w.count(t, `SELECT count(*) FROM record_event WHERE event_type = 'DISPATCH_ATTEMPTED'`); n != 3 {
		t.Fatalf("%d DISPATCH_ATTEMPTED events, want 3", n)
	}
}

// A3: the budget is per tenant. Another tenant's dispatches of the same correlation, capability and target do not count, and are not counted.
func TestTheBudgetIsPerTenant(t *testing.T) {
	w := newWorld(t)
	if _, err := w.admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ('ten_bb', 'B')`); err != nil {
		t.Fatal(err)
	}
	other := testpg.TenantLogin(t, w.admin, w.admin.Config().ConnString(), "execution_rw", "ten_bb")
	key := map[string]any{"capability": capab, "target_ref": "TX1"}
	for i := 1; i <= 2; i++ {
		if err := putDispatch(t, w, w.exec, tenant, corr, key); err != nil {
			t.Fatalf("tenant A, record %d: %v", i, err)
		}
	}
	for i := 1; i <= 2; i++ {
		if err := putDispatch(t, w, other, "ten_bb", corr, key); err != nil {
			t.Fatalf("tenant B, record %d (tenant A's two do not count): %v", i, err)
		}
	}
	if err := putDispatch(t, w, other, "ten_bb", corr, key); sqlState(err) != budgetSQLState {
		t.Fatalf("tenant B, third record: %v", err)
	}
	if err := putDispatch(t, w, w.exec, tenant, corr, key); sqlState(err) != budgetSQLState {
		t.Fatalf("tenant A, third record: %v", err)
	}
}

// AGENT-08, REQ-RCV-003, "no third dispatch ever": grants of one key executed at once never dispatch more than twice, and the Record never holds
// more than two DISPATCH_ATTEMPTED events of the key, whatever order the database serialises them in. A serialisation failure is a refusal to
// commit, not a dispatch.
func TestConcurrentExecutesOfOneKeyNeverDispatchMoreThanTwice(t *testing.T) {
	w := newWorld(t)
	w.target.answer = controller.TransportResult{Class: controller.TransportTimeout}
	const grants = 8
	ids := make([]string, grants)
	for i := range ids {
		ids[i] = w.issue(t, i+1).GrantID
	}
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = w.ctl.Execute(ctx(t), tenant, id) // an error is a transaction that did not commit
		}()
	}
	wg.Wait()
	if got := w.target.count(); got > 2 {
		t.Fatalf("the target was dispatched to %d times, want at most 2", got)
	}
	if n := dispatches(t, w, corr, capab, "TX1"); n > 2 || n != w.target.count() {
		t.Fatalf("%d DISPATCH_ATTEMPTED events for %d dispatches, want equal and at most 2", n, w.target.count())
	}
	// Run the rest one by one: the budget ends at exactly two.
	for _, id := range ids {
		_, _ = w.ctl.Execute(ctx(t), tenant, id)
	}
	if n := dispatches(t, w, corr, capab, "TX1"); n != 2 {
		t.Fatalf("%d DISPATCH_ATTEMPTED events after all grants ran, want exactly 2", n)
	}
	verifyChain(t, w)
}
