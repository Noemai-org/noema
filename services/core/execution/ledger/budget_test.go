package ledger_test

// RCV-02 slice 2, ruling A153 (RCV-02 enforcement lives in the database), A3 and A62 (the dispatch budget is 2, per tenant, correlation, capability and
// target, counted over DISPATCH_ATTEMPTED events), REQ-RCV-003, INV-03, AGENT-08, EC-E06: a third dispatch of one key is impossible at the database, not
// only refused by a caller that asks. Whatever writes the event (Spend, a direct append, a concurrent run), the Record holds at most two
// DISPATCH_ATTEMPTED events of a key, and a refused Spend writes nothing and leaves the grant to be voided, never dispatched.

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
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

// A153: the first two dispatches of a key run; the third, under a new grant, is refused by the database, writes nothing and dispatches nothing. The grant is
// not spent and no event is written: GRANT_VOIDED reasons and SECURITY_EVENT kinds are closed lists of record-event.v3, so a budget reason would be a contract
// change (AGENT-15) and is not made here; the grant stays ISSUED until it expires, and the database refuses it again every time it is tried.
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
	if !out.BudgetSpent || out.Void != "" || out.Receipt != nil || out.Replayed {
		t.Fatalf("third dispatch: %+v, want BudgetSpent with no void, no receipt and no replay", out)
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
	if w.count(t, `SELECT count(*) FROM decision_grant WHERE grant_id = $1 AND status = 'ISSUED'`, g3.GrantID) != 1 {
		t.Fatal("the refused grant is not still ISSUED")
	}
	if again := mustExecute(t, w, g3.GrantID); !again.BudgetSpent || w.target.count() != 2 {
		t.Fatalf("the same grant tried again: %+v, dispatches %d", again, w.target.count())
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
			if out := run(k.mut); out.Void != "" || out.BudgetSpent {
				t.Fatalf("%s, dispatch %d: %+v", k.name, i, out)
			}
		}
		if out := run(k.mut); !out.BudgetSpent {
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

// A153: the rule is the database's and it counts the Record. With two DISPATCH_ATTEMPTED events of a key already in the Record, however they got there, a
// grant of that key is refused at the spend by the database: Spend reports BudgetSpent and writes nothing.
func TestTheSpendOfAKeyWithTwoDispatchesInTheRecordIsRefusedByTheDatabase(t *testing.T) {
	w := newWorld(t)
	key := map[string]any{"capability": capab, "target_ref": "TX1", "attempt": 1}
	for i := 1; i <= 2; i++ {
		if err := putDispatch(t, w, w.exec, tenant, corr, key); err != nil {
			t.Fatalf("history record %d: %v", i, err)
		}
	}
	g := w.issue(t, 1)
	res, err := w.led.Spend(ctx(t), controller.SpendCommand{Grant: g, IdempotencyKey: "idem-1", LogicalAction: "ESCALATE_CASE"})
	if err != nil || !res.BudgetSpent || res.Spent {
		t.Fatalf("spend: %+v %v, want BudgetSpent", res, err)
	}
	if w.count(t, `SELECT count(*) FROM grant_spend`) != 0 || w.count(t, `SELECT count(*) FROM decision_grant WHERE status = 'ISSUED'`) != 1 {
		t.Fatal("a refused spend changed the grant or stored a spend")
	}
	// The database says no on its own, whatever the caller does: an insert into grant_spend by the Execution login is refused too.
	_, err = w.exec.Exec(ctx(t), `INSERT INTO grant_spend (grant_id, tenant_id, nonce, idempotency_key, attempt, spent_at) VALUES ($1, $2, $3, 'idem-1', 1, now())`, g.GrantID, tenant, g.Nonce)
	if sqlState(err) != budgetSQLState {
		t.Fatalf("a direct insert into grant_spend: %v (SQLSTATE %q), want %s", err, sqlState(err), budgetSQLState)
	}
}

// A3: only the key's own dispatches count: another target, another correlation, another capability and another tenant's events do not.
func TestOnlyTheKeysOwnDispatchesCountTowardItsBudget(t *testing.T) {
	w := newWorld(t)
	if _, err := w.admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ('ten_bb', 'B')`); err != nil {
		t.Fatal(err)
	}
	other := testpg.TenantLogin(t, w.admin, w.admin.Config().ConnString(), "execution_rw", "ten_bb")
	history := []struct {
		pool    *pgxpool.Pool
		tenant  string
		corr    string
		payload map[string]any
	}{
		{w.exec, tenant, corr, map[string]any{"capability": capab, "target_ref": "TX9"}},                // another target
		{w.exec, tenant, "corr_other1", map[string]any{"capability": capab, "target_ref": "TX1"}},       // another correlation
		{w.exec, tenant, corr, map[string]any{"capability": "fin.cap.other", "target_ref": "TX1"}},      // another capability
		{other, "ten_bb", corr, map[string]any{"capability": capab, "target_ref": "TX1"}},               // another tenant
		{other, "ten_bb", corr, map[string]any{"capability": capab, "target_ref": "TX1", "attempt": 2}}, // another tenant, a second time
	}
	for i, h := range history {
		if err := putDispatch(t, w, h.pool, h.tenant, h.corr, h.payload); err != nil {
			t.Fatalf("history %d: %v", i, err)
		}
	}
	w.target.answer = controller.TransportResult{Class: controller.TransportTimeout}
	for n := 1; n <= 2; n++ {
		if out := mustExecute(t, w, w.issue(t, n).GrantID); out.BudgetSpent || out.Receipt == nil {
			t.Fatalf("dispatch %d of the key: %+v, want it to run (nothing else counts)", n, out)
		}
	}
	if out := mustExecute(t, w, w.issue(t, 3).GrantID); !out.BudgetSpent {
		t.Fatalf("third dispatch: %+v", out)
	}
}

// RCV-02 slice 1, A3: an earlier dispatch whose key cannot be read might belong to any key of its correlation, so it counts against all of them.
func TestAnEarlierDispatchWithAnUnreadableKeyCountsAgainstEveryKeyOfItsCorrelation(t *testing.T) {
	w := newWorld(t)
	if err := putDispatch(t, w, w.exec, tenant, corr, map[string]any{"attempt": 1}); err != nil { // no capability, no target
		t.Fatal(err)
	}
	if err := putDispatch(t, w, w.exec, tenant, corr, nil); err != nil { // no payload at all
		t.Fatal(err)
	}
	g := w.issue(t, 1)
	res, err := w.led.Spend(ctx(t), controller.SpendCommand{Grant: g, IdempotencyKey: "idem-1", LogicalAction: "ESCALATE_CASE"})
	if err != nil || !res.BudgetSpent {
		t.Fatalf("spend with two unreadable dispatches in the correlation: %+v %v, want BudgetSpent", res, err)
	}
	// One unreadable dispatch leaves room for one more.
	w2 := newWorld(t)
	if err := putDispatch(t, w2, w2.exec, tenant, corr, map[string]any{"attempt": 1}); err != nil {
		t.Fatal(err)
	}
	w2.target.answer = controller.TransportResult{Class: controller.TransportTimeout}
	if out := mustExecute(t, w2, w2.issue(t, 1).GrantID); out.BudgetSpent {
		t.Fatalf("one unreadable dispatch used up the budget: %+v", out)
	}
	if out := mustExecute(t, w2, w2.issue(t, 2).GrantID); !out.BudgetSpent {
		t.Fatalf("the second spend after one unreadable dispatch and one real one: %+v", out)
	}
}

// Fail closed: a grant whose key cannot be read is refused at the spend, not counted against nothing.
func TestAGrantWhoseKeyCannotBeReadIsRefusedAtTheSpend(t *testing.T) {
	w := newWorld(t)
	g := w.issue(t, 1)
	if _, err := w.admin.Exec(ctx(t), `ALTER TABLE decision_grant DISABLE TRIGGER USER`); err != nil {
		t.Fatal(err)
	}
	if _, err := w.admin.Exec(ctx(t), `UPDATE decision_grant SET json = json - 'target_ref' WHERE grant_id = $1`, g.GrantID); err != nil {
		t.Fatal(err)
	}
	res, err := w.led.Spend(ctx(t), controller.SpendCommand{Grant: g, IdempotencyKey: "idem-1", LogicalAction: "ESCALATE_CASE"})
	if err != nil || !res.BudgetSpent {
		t.Fatalf("spend of a grant with no target: %+v %v, want BudgetSpent", res, err)
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

// Security Reviewer's blocking finding on noema#472: the rule must not depend on the isolation level of the spending transaction. Under READ COMMITTED, two spends of two
// grants of one key that run at the same time must not both pass on a count of one: while the first spend (its guard passed, its DISPATCH_ATTEMPTED appended, not yet
// committed) is in flight, the second spend of the key is held or refused, and once the first commits the second is refused by the database. A direct insert by the
// Execution login has the same rule as Spend (AGENT-08, A153).
func TestTwoSpendsOfOneKeyCannotBothPassUnderReadCommitted(t *testing.T) {
	w := newWorld(t)
	key := map[string]any{"capability": capab, "target_ref": "TX1", "attempt": 1}
	if err := putDispatch(t, w, w.exec, tenant, corr, key); err != nil { // one dispatch already in the Record: the key has one left
		t.Fatal(err)
	}
	g1, g2 := w.issue(t, 1), w.issue(t, 2)
	spend := `INSERT INTO grant_spend (grant_id, tenant_id, nonce, idempotency_key, attempt, spent_at) VALUES ($1, $2, $3, $4, 1, now())`

	tx1, err := w.exec.Begin(ctx(t)) // READ COMMITTED, the default: the isolation level is not what protects the key
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx1.Rollback(ctx(t)) }()
	if _, err := tx1.Exec(ctx(t), spend, g1.GrantID, tenant, g1.Nonce, "idem-1"); err != nil {
		t.Fatalf("the first spend of the key: %v", err)
	}
	if _, err := w.led.Rec.Append(ctx(t), tx1, chain.Event{TenantID: tenant, CorrelationID: corr, WrittenBy: "core-execution", Actor: "core-execution",
		EventType: "DISPATCH_ATTEMPTED", ObjectRef: g1.GrantID, Payload: key}); err != nil {
		t.Fatal(err)
	}

	tx2, err := w.exec.Begin(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx2.Rollback(ctx(t)) }()
	second := make(chan error, 1)
	go func() {
		_, err := tx2.Exec(ctx(t), spend, g2.GrantID, tenant, g2.Nonce, "idem-2")
		second <- err
	}()
	var secondErr error
	select {
	case secondErr = <-second: // it answered while the first was in flight: it must not have passed
		if secondErr == nil {
			t.Fatal("a second spend of the key passed while the first was in flight: both would commit on a count of one, and the key would dispatch three times")
		}
	case <-time.After(500 * time.Millisecond): // it waits for the first, as the lock makes it
	}
	if err := tx1.Commit(ctx(t)); err != nil {
		t.Fatal(err)
	}
	if secondErr == nil {
		select {
		case secondErr = <-second:
		case <-time.After(20 * time.Second):
			t.Fatal("the second spend never answered after the first committed")
		}
	}
	if sqlState(secondErr) != budgetSQLState {
		t.Fatalf("the second spend, counted after the first committed: %v (SQLSTATE %q), want %s: it counted before the first's event was in the Record", secondErr, sqlState(secondErr), budgetSQLState)
	}
	_ = tx2.Rollback(ctx(t))
	if n := dispatches(t, w, corr, capab, "TX1"); n != 2 {
		t.Fatalf("%d DISPATCH_ATTEMPTED events, want 2", n)
	}
}

// The Security Reviewer's note 1 on noema#472: the lock makes a waiting spend count after the first has committed only if its snapshot is taken after the wait. A REPEATABLE READ
// transaction keeps the snapshot of its first statement, so it would count a stale one and pass on a count of one. The guard refuses that isolation level outright (SQLSTATE 0A000);
// the only caller, Spend, runs SERIALIZABLE, and READ COMMITTED is proved by the test above. Proves REQ-RCV-003, INV-03.
func TestASpendUnderRepeatableReadIsRefusedBecauseItsSnapshotCouldBeStale(t *testing.T) {
	w := newWorld(t)
	g := w.issue(t, 1)
	tx, err := w.exec.BeginTx(ctx(t), pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx(t)) }()
	_, err = tx.Exec(ctx(t), `INSERT INTO grant_spend (grant_id, tenant_id, nonce, idempotency_key, attempt, spent_at) VALUES ($1, $2, $3, $4, 1, now())`,
		g.GrantID, tenant, g.Nonce, "idem-rr")
	if sqlState(err) != "0A000" {
		t.Fatalf("a spend under REPEATABLE READ: %v (SQLSTATE %q), want 0A000", err, sqlState(err))
	}
}
