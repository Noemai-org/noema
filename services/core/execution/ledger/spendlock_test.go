package ledger_test

// noema#714, Architect ruling A339 (relayed; interim): "advisory lock + typed retryable error". The spends of one tenant take turns on a per-tenant advisory lock instead of
// racing on the Record chain head, so spends of different keys no longer refuse one another with 40001; the bounded retry stays as the second layer; when 40001 still
// persists after the last attempt the caller gets a typed retryable error, not a refusal. REQ-EXE-001, REQ-EXE-002, INV-03, INV-07, AGENT-08, AGENT-16. Everything is
// SYNTHETIC.
//
// The lock is real PostgreSQL: its holders and waiters are read from pg_locks. The slow trigger below only makes the spends overlap for long enough that, without the lock,
// they cannot help but race; it injects no failure.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/execution/controller"
	"github.com/Noemai-org/noema/services/core/execution/ledger"
	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/platform/clock"
)

// slowSpends makes every insert into grant_spend take sleepMS milliseconds, inside the spend's transaction.
func slowSpends(t *testing.T, w *world, sleepMS int) {
	t.Helper()
	ddl := fmt.Sprintf(`CREATE FUNCTION spend_slow() RETURNS trigger LANGUAGE plpgsql AS $f$
		BEGIN PERFORM pg_sleep(%d / 1000.0); RETURN NEW; END $f$;
		CREATE TRIGGER aa_spend_slow BEFORE INSERT ON grant_spend FOR EACH ROW EXECUTE FUNCTION spend_slow();`, sleepMS)
	if _, err := w.admin.Exec(ctx(t), ddl); err != nil {
		t.Fatal(err)
	}
}

// distinct returns n grants of one tenant whose dispatch keys (target) and idempotency keys all differ, so that no budget and no idempotency record is shared.
func distinct(t *testing.T, w *world, n int) ([]controller.Grant, func(controller.Grant) controller.SpendCommand) {
	t.Helper()
	gs := make([]controller.Grant, n)
	for i := range gs {
		gs[i] = w.issue(t, i+1, func(g *controller.Grant) { g.TargetRef = fmt.Sprintf("TX%d", i+1) })
	}
	return gs, func(g controller.Grant) controller.SpendCommand {
		return controller.SpendCommand{Grant: g, IdempotencyKey: "idem-" + g.GrantID, LogicalAction: "ESCALATE_CASE"}
	}
}

func spendEach(t *testing.T, w *world, gs []controller.Grant, cmd func(controller.Grant) controller.SpendCommand) func() []spendOutcome {
	t.Helper()
	start := make(chan struct{})
	outs := make([]spendOutcome, len(gs))
	done := make(chan struct{}, len(gs))
	c := ctx(t)
	for i := range gs {
		go func() {
			<-start
			res, err := w.led.Spend(c, cmd(gs[i]))
			outs[i] = spendOutcome{res, err}
			done <- struct{}{}
		}()
	}
	close(start)
	return func() []spendOutcome {
		for range gs {
			<-done
		}
		return outs
	}
}

// lockCounts reads the advisory locks of the spend key space (the two-key form, objsubid 2, namespace SpendLockNamespace), for one tenant's name or, with an empty name, for all.
// pg_locks is cluster-wide and the packages of one test run share the server, so only the locks of this test's own database are counted (noema#748: another package's spend of a
// tenant of the same name was counted as this test's).
func lockCounts(t *testing.T, w *world, name string) (granted, waiting int) {
	t.Helper()
	q := `SELECT count(*) FILTER (WHERE granted), count(*) FILTER (WHERE NOT granted) FROM pg_locks
		WHERE locktype = 'advisory' AND database = (SELECT oid FROM pg_database WHERE datname = current_database()) AND objsubid = 2 AND classid::bigint = (hashtext($1)::bigint & 4294967295) AND ($2 = '' OR objid::bigint = (hashtext($2)::bigint & 4294967295))`
	if err := w.admin.QueryRow(ctx(t), q, ledger.SpendLockNamespace, name).Scan(&granted, &waiting); err != nil {
		t.Fatal(err)
	}
	return granted, waiting
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.After(20 * time.Second)
	for !ok() {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for %s", what)
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// holdSpendLock takes the spend lock of a tenant from outside, as a spend in flight would hold it, and returns the function that releases it.
func holdSpendLock(t *testing.T, w *world, tenantID string) func() {
	t.Helper()
	conn, err := w.admin.Acquire(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx(t), `SELECT pg_advisory_lock(hashtext($1), hashtext($2))`, ledger.SpendLockNamespace, ledger.SpendLockName(tenantID)); err != nil {
		t.Fatal(err)
	}
	released := false
	release := func() {
		if released {
			return
		}
		released = true
		if _, err := conn.Exec(ctx(t), `SELECT pg_advisory_unlock(hashtext($1), hashtext($2))`, ledger.SpendLockNamespace, ledger.SpendLockName(tenantID)); err != nil {
			t.Error(err)
		}
		conn.Release()
	}
	t.Cleanup(release)
	return release
}

// The tenant's spends take turns. Eight grants of eight distinct keys, every spend slow enough to overlap the others, three attempts: all eight commit and none of them
// is refused, which three attempts alone could not do (status/EXE-RETRY-714.md, open item 1 before this ruling). No 40001 reaches a caller and no typed error either.
func TestSpendsOfDistinctKeysOfOneTenantTakeTurnsAndNoneIsRefused(t *testing.T) {
	for run := 1; run <= 3; run++ {
		t.Run(fmt.Sprintf("run%d", run), func(t *testing.T) {
			w := newWorld(t)
			w.led.SerializationAttempts = 3
			slowSpends(t, w, 30)
			const n = 8
			gs, cmd := distinct(t, w, n)
			for i, o := range spendEach(t, w, gs, cmd)() {
				if o.err != nil || !o.res.Spent {
					t.Errorf("spend %d = %+v, %v; want spent with no error", i, o.res, o.err)
				}
			}
			for q, want := range map[string]int{
				`SELECT count(*) FROM grant_spend`:                                          n,
				`SELECT count(*) FROM record_event WHERE event_type = 'GRANT_SPENT'`:        n,
				`SELECT count(*) FROM record_event WHERE event_type = 'DISPATCH_ATTEMPTED'`: n,
				`SELECT count(*) FROM decision_grant WHERE status = 'ISSUED'`:               0,
			} {
				if got := w.count(t, q); got != want {
					t.Errorf("%s = %d, want %d", q, got, want)
				}
			}
			verifyChain(t, w)
		})
	}
}

// The lock is per tenant: while the lock of tenant A is held, a spend of tenant A waits for it and a spend of tenant B goes through. When A's lock is released A's spend
// commits.
func TestTheSpendLockIsPerTenant(t *testing.T) {
	w := newWorld(t)
	w.led.SerializationAttempts = 3
	if _, err := w.admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ('ten_bb', 'B')`); err != nil {
		t.Fatal(err)
	}
	other := *w.led
	other.Pool = testpg.TenantLogin(t, w.admin, w.admin.Config().ConnString(), "execution_rw", "ten_bb")
	gA := w.issue(t, 1)
	gB := w.issue(t, 2, func(g *controller.Grant) {
		g.TenantID, g.DecisionID, g.GrantID, g.Nonce = "ten_bb", "dec_0002b", "grt_0002b", "n_ab3000b"
	})
	release := holdSpendLock(t, w, tenant)

	aDone := make(chan spendOutcome, 1)
	go func() {
		res, err := w.led.Spend(ctx(t), cmdFor(gA))
		aDone <- spendOutcome{res, err}
	}()
	waitFor(t, "tenant A's spend to queue on its lock", func() bool { _, waiting := lockCounts(t, w, ledger.SpendLockName(tenant)); return waiting == 1 })

	res, err := other.Spend(ctx(t), cmdFor(gB))
	if err != nil || !res.Spent {
		t.Fatalf("tenant B's spend while A's lock is held = %+v, %v; want spent", res, err)
	}
	select {
	case o := <-aDone:
		t.Fatalf("tenant A's spend ran while its lock was held elsewhere: %+v, %v", o.res, o.err)
	case <-time.After(300 * time.Millisecond):
	}
	release()
	select {
	case o := <-aDone:
		if o.err != nil || !o.res.Spent {
			t.Fatalf("tenant A's spend after the release = %+v, %v; want spent", o.res, o.err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("tenant A's spend did not run after the lock was released")
	}
}

// While a spend is in its transaction it holds exactly one lock of the spend key space, the one of its own tenant; when it returns, on every way out (committed, replayed,
// refused, rolled back), none is held: a connection goes back to the pool with no lock on it.
func TestTheLockIsHeldForTheSpendAndReleasedOnEveryWayOut(t *testing.T) {
	w := newWorld(t)
	w.led.SerializationAttempts = 1
	slowSpends(t, w, 400)
	g := w.issue(t, 1)
	done := make(chan spendOutcome, 1)
	go func() {
		res, err := w.led.Spend(ctx(t), cmdFor(g))
		done <- spendOutcome{res, err}
	}()
	waitFor(t, "the spend to hold its tenant's lock", func() bool { g, _ := lockCounts(t, w, ledger.SpendLockName(tenant)); return g == 1 })
	if all, _ := lockCounts(t, w, ""); all != 1 {
		t.Fatalf("%d locks of the spend key space are held, want exactly the tenant's", all)
	}
	if o := <-done; o.err != nil || !o.res.Spent {
		t.Fatalf("Spend = %+v, %v", o.res, o.err)
	}
	assertNoSpendLock := func(way string) {
		t.Helper()
		if g, wt := lockCounts(t, w, ""); g != 0 || wt != 0 {
			t.Errorf("after %s: %d held and %d awaited locks of the spend key space, want none", way, g, wt)
		}
		if n := w.exec.Stat().AcquiredConns(); n != 0 {
			t.Errorf("after %s: %d connections are still out of the pool", way, n)
		}
	}
	assertNoSpendLock("a commit")
	if res, err := w.led.Spend(ctx(t), cmdFor(g)); err != nil || !res.Replay {
		t.Fatalf("second Spend = %+v, %v; want a replay", res, err)
	}
	assertNoSpendLock("a replay")

	g2 := w.issue(t, 2, func(g *controller.Grant) { g.TargetRef = "TX2" })
	w2 := cmdFor(g2)
	w2.IdempotencyKey = "idem-2"
	if _, err := w.admin.Exec(ctx(t), `DROP TRIGGER aa_spend_slow ON grant_spend`); err != nil {
		t.Fatal(err)
	}
	faultOnSpend(t, w, "XX000", 1000, 0)
	if _, err := w.led.Spend(ctx(t), w2); err == nil {
		t.Fatal("the injected fault did not fail the spend")
	}
	assertNoSpendLock("a rollback")
}

// A lock wait is bounded by the context: with the tenant's lock held elsewhere for good, a spend with a deadline returns, with the context's error, shortly after it, its
// grant ISSUED, nothing written, no lock waiter left behind and its connection back in the pool; and the next spend, once the lock is free, commits.
func TestTheWaitForTheLockEndsWithTheContext(t *testing.T) {
	w := newWorld(t)
	w.led.SerializationAttempts = 3
	g := w.issue(t, 1)
	release := holdSpendLock(t, w, tenant)
	c, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	done := make(chan spendOutcome, 1)
	go func() {
		res, err := w.led.Spend(c, cmdFor(g))
		done <- spendOutcome{res, err}
	}()
	var o spendOutcome
	select {
	case o = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Spend went on waiting for the lock after its context deadline")
	}
	if o.err == nil || o.res.Spent || !errors.Is(o.err, context.DeadlineExceeded) {
		t.Fatalf("Spend = %+v, %v; want an error that wraps the context's deadline", o.res, o.err)
	}
	if errors.Is(o.err, controller.ErrSpendRetryable) {
		t.Errorf("a spend that ran out of time waiting for the lock is the caller's own deadline, not a retryable refusal: %v", o.err)
	}
	waitFor(t, "the abandoned waiter to leave the lock queue", func() bool { _, waiting := lockCounts(t, w, ""); return waiting == 0 })
	waitFor(t, "the connection to return to the pool", func() bool { return w.exec.Stat().AcquiredConns() == 0 })
	if got := w.count(t, `SELECT count(*) FROM decision_grant WHERE grant_id = $1 AND status = 'ISSUED'`, g.GrantID); got != 1 {
		t.Fatal("the grant is not ISSUED")
	}
	if got := w.count(t, `SELECT count(*) FROM grant_spend`) + len(w.events(t)); got != 0 {
		t.Fatalf("%d rows were written by a spend that never got its turn", got)
	}
	release()
	if res, err := w.led.Spend(ctx(t), cmdFor(g)); err != nil || !res.Spent {
		t.Fatalf("Spend after the lock was free = %+v, %v; want spent", res, err)
	}
}

// The lock cannot be the same lock as any other lock of the system: it lives in the two-key key space, and a one-key lock whose bits are the same two numbers is another
// lock. Holding that one does not make a spend wait.
func TestAOneKeyAdvisoryLockWithTheSameNumbersIsAnotherLock(t *testing.T) {
	w := newWorld(t)
	w.led.SerializationAttempts = 3
	g := w.issue(t, 1)
	conn, err := w.admin.Acquire(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	const oneKey = `SELECT pg_advisory_lock(((hashtext($1)::bigint & 4294967295) << 32) | (hashtext($2)::bigint & 4294967295))`
	if _, err := conn.Exec(ctx(t), oneKey, ledger.SpendLockNamespace, ledger.SpendLockName(tenant)); err != nil {
		t.Fatal(err)
	}
	c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if res, err := w.led.Spend(c, cmdFor(g)); err != nil || !res.Spent {
		t.Fatalf("Spend with the one-key twin held = %+v, %v; want spent at once", res, err)
	}
}

// Without the lock first, the transaction-scoped form does not do the job, which is why the lock is a session lock taken on the connection before the transaction begins:
// a SERIALIZABLE transaction takes its snapshot at its first statement, and pg_advisory_xact_lock is a statement, so a transaction that waited for it behind a writer reads
// the world as it was before that writer committed, and its first write to a row the writer changed is refused with 40001. This test is the evidence; it uses no code of
// the ledger.
func TestAnAdvisoryXactLockAsTheFirstStatementOfASerializableTransactionDoesNotRefreshItsSnapshot(t *testing.T) {
	w := newWorld(t)
	c := ctx(t)
	if _, err := w.admin.Exec(c, `CREATE TABLE lock_probe (id int PRIMARY KEY, v int); INSERT INTO lock_probe VALUES (1, 0)`); err != nil {
		t.Fatal(err)
	}
	first, err := w.admin.Begin(c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Exec(c, `SELECT pg_advisory_xact_lock(1, 2)`); err != nil {
		t.Fatal(err)
	}
	second := make(chan error, 1)
	go func() {
		tx, err := w.admin.Begin(c)
		if err != nil {
			second <- err
			return
		}
		defer func() { _ = tx.Rollback(c) }()
		if _, err := tx.Exec(c, `SET TRANSACTION ISOLATION LEVEL SERIALIZABLE`); err != nil {
			second <- err
			return
		}
		if _, err := tx.Exec(c, `SELECT pg_advisory_xact_lock(1, 2)`); err != nil {
			second <- err
			return
		}
		_, err = tx.Exec(c, `UPDATE lock_probe SET v = 2 WHERE id = 1`)
		second <- err
	}()
	waitFor(t, "the second transaction to wait for the lock", func() bool {
		var n int
		_ = w.admin.QueryRow(c, `SELECT count(*) FROM pg_locks WHERE locktype = 'advisory' AND NOT granted AND database = (SELECT oid FROM pg_database WHERE datname = current_database())`).Scan(&n)
		return n == 1
	})
	if _, err := first.Exec(c, `UPDATE lock_probe SET v = 1 WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	if err := first.Commit(c); err != nil {
		t.Fatal(err)
	}
	if got := sqlState(<-second); got != "40001" {
		t.Fatalf("the waiter's write after the lock was granted: SQLSTATE %q, want 40001", got)
	}
}

// After the last attempt a persistent serialization failure is the typed retryable error. The grant is still ISSUED, nothing is on the Record, nothing is in grant_spend,
// and the database's own error is still in the chain, so a caller that matched its SQLSTATE still sees it. The error is typed for 40001 only: a unique violation, a deadlock
// and any other error are not "try again" (the ruling names 40001 alone).
func TestAPersistentSerializationFailureIsTheTypedRetryableError(t *testing.T) {
	for _, attempts := range []int{1, 3} {
		t.Run(fmt.Sprintf("attempts%d", attempts), func(t *testing.T) {
			w := newWorld(t)
			w.led.SerializationAttempts = attempts
			probe := faultOnSpend(t, w, "40001", 1000, 0)
			g := w.issue(t, 1)
			res, err := w.led.Spend(ctx(t), cmdFor(g))
			if !errors.Is(err, controller.ErrSpendRetryable) || res.Spent || res.Replay || res.BudgetSpent {
				t.Fatalf("Spend = %+v, %v; want controller.ErrSpendRetryable", res, err)
			}
			if !isDidNotCommit40001(err) {
				t.Errorf("the typed error lost the database's 40001 or the did-not-commit text: %v", err)
			}
			if got := probe(); got != attempts {
				t.Fatalf("the spend was attempted %d times, want %d", got, attempts)
			}
			if got := w.count(t, `SELECT count(*) FROM decision_grant WHERE grant_id = $1 AND status = 'ISSUED'`, g.GrantID); got != 1 {
				t.Fatal("the grant is not ISSUED")
			}
			for q, want := range map[string]int{`SELECT count(*) FROM grant_spend`: 0, `SELECT count(*) FROM record_event`: 0, `SELECT count(*) FROM idempotency_record`: 0} {
				if got := w.count(t, q); got != want {
					t.Errorf("%s = %d, want %d", q, got, want)
				}
			}
		})
	}
	for _, code := range []string{"23505", "40P01", "P0001"} {
		t.Run("not typed "+code, func(t *testing.T) {
			w := newWorld(t)
			w.led.SerializationAttempts = 3
			faultOnSpend(t, w, code, 1000, 0)
			_, err := w.led.Spend(ctx(t), cmdFor(w.issue(t, 1)))
			if err == nil || errors.Is(err, controller.ErrSpendRetryable) {
				t.Fatalf("SQLSTATE %s: %v; want an error that is not the retryable one", code, err)
			}
		})
	}
}

// The typed error also reaches a caller through the controller (execution/controller/retryable_test.go proves the mapping). Here, the real thing: a controller over this
// ledger, a grant whose spend is refused for good, comes back as an error that is the typed one, with the grant ISSUED and nothing dispatched.
func TestTheControllerReturnsTheTypedErrorAndDispatchesNothing(t *testing.T) {
	w := newWorld(t)
	w.led.SerializationAttempts = 2
	faultOnSpend(t, w, "40001", 1000, 0)
	g := w.issue(t, 1)
	out, err := w.ctl.Execute(ctx(t), tenant, g.GrantID)
	if !errors.Is(err, controller.ErrSpendRetryable) || out.Receipt != nil || out.Void != "" {
		t.Fatalf("Execute = %+v, %v; want controller.ErrSpendRetryable and no receipt or void", out, err)
	}
	if got := w.target.count(); got != 0 {
		t.Fatalf("%d dispatches were made for a spend that did not commit", got)
	}
	if got := w.count(t, `SELECT count(*) FROM decision_grant WHERE grant_id = $1 AND status = 'ISSUED'`, g.GrantID); got != 1 {
		t.Fatal("the grant is not ISSUED")
	}
}

// noema#748 (CI): pg_locks is cluster-wide and the test packages of one run share the server, so a spend of another package's tenant of the same name was counted as this
// test's lock. The count is of this database only: a lock of the same two numbers taken in another database is not counted.
func TestTheLockCountsIgnoreTheLocksOfAnotherDatabase(t *testing.T) {
	w := newWorld(t)
	c := ctx(t)
	const other = "noema_lockcount_other"
	if _, err := w.admin.Exec(c, "CREATE DATABASE "+other); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = w.admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+other+" WITH (FORCE)") })
	cfg := w.admin.Config().ConnConfig.Copy()
	cfg.Database = other
	conn, err := pgx.ConnectConfig(c, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	if _, err := conn.Exec(c, `SELECT pg_advisory_lock(hashtext($1), hashtext($2))`, ledger.SpendLockNamespace, ledger.SpendLockName(tenant)); err != nil {
		t.Fatal(err)
	}
	if g, wt := lockCounts(t, w, ""); g != 0 || wt != 0 {
		t.Fatalf("a lock of another database was counted: %d held, %d awaited, want none", g, wt)
	}
	if g, _ := lockCounts(t, w, ledger.SpendLockName(tenant)); g != 0 {
		t.Fatalf("a lock of another database was counted for the tenant: %d held, want none", g)
	}
}

// noema#748 review (P2, AGENT-13): a spend-lock failure reaches callers with a fixed public text. The tenant's ID and the driver's message (server and login details) are not in
// it; the cause is kept for errors.Is and errors.As and is not printed.
func TestASpendLockFailureNamesNeitherTheTenantNorTheDriver(t *testing.T) {
	w := newWorld(t)
	w.led.SerializationAttempts = 3
	g := w.issue(t, 1)
	release := holdSpendLock(t, w, tenant)
	defer release()
	c, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	done := make(chan spendOutcome, 1)
	go func() {
		res, err := w.led.Spend(c, cmdFor(g))
		done <- spendOutcome{res, err}
	}()
	waitFor(t, "the spend to queue on its lock", func() bool { _, waiting := lockCounts(t, w, ledger.SpendLockName(tenant)); return waiting == 1 })
	if _, err := w.admin.Exec(ctx(t), `SELECT pg_terminate_backend(pid) FROM pg_locks WHERE locktype = 'advisory' AND NOT granted
		AND database = (SELECT oid FROM pg_database WHERE datname = current_database())`); err != nil {
		t.Fatal(err)
	}
	var o spendOutcome
	select {
	case o = <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("the spend did not return after its waiting session was terminated")
	}
	if o.err == nil || o.res.Spent {
		t.Fatalf("Spend = %+v, %v; want an error", o.res, o.err)
	}
	var pgErr *pgconn.PgError
	if !errors.As(o.err, &pgErr) {
		t.Errorf("the driver's error is not kept as the cause: %v", o.err)
	}
	for _, leak := range []string{tenant, "SQLSTATE", "terminating", "57P01", "ERROR", "FATAL"} {
		if strings.Contains(o.err.Error(), leak) {
			t.Errorf("the error text carries %q: %q", leak, o.err.Error())
		}
	}
}

// The same for a wait that ends with the caller's deadline: the tenant's ID is not in the text, the deadline is still the cause.
func TestALockWaitThatEndsWithTheDeadlineNamesNoTenant(t *testing.T) {
	w := newWorld(t)
	w.led.SerializationAttempts = 3
	g := w.issue(t, 1)
	release := holdSpendLock(t, w, tenant)
	defer release()
	c, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	res, err := w.led.Spend(c, cmdFor(g))
	if err == nil || res.Spent || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Spend = %+v, %v; want an error that wraps the deadline", res, err)
	}
	if strings.Contains(err.Error(), tenant) {
		t.Errorf("the error text names the tenant: %q", err.Error())
	}
}

// noema#748 review, Architect ruling A348 (relayed; the text was not available to the author, the conditions below are inferred from the request): the spend lock is shown
// RELEASED on every path, with the real pg_locks of this database and the pool's AcquiredConns back to 0. "Released" is read from the server (lockCounts) and from the pool
// (Stat().AcquiredConns), never from the code's own bookkeeping. A path that ends the connection (an unlock that fails, a context that ends mid-transaction, a terminated
// backend) releases the lock when the backend ends, so those paths are polled with a deadline; the others are checked once, the moment the call returns.

// requireReleased fails unless, after the call, no lock of the spend key space is held or awaited in this database and none of the pools has a connection out. within is how long the
// server may take to end a session that the client closed; zero checks once.
func requireReleased(t *testing.T, w *world, way string, within time.Duration, pools ...*pgxpool.Pool) {
	t.Helper()
	if len(pools) == 0 {
		pools = []*pgxpool.Pool{w.exec}
	}
	state := func() (held, awaited, out int) {
		held, awaited = lockCounts(t, w, "")
		for _, p := range pools {
			out += int(p.Stat().AcquiredConns())
		}
		return held, awaited, out
	}
	deadline := time.After(within)
	for {
		held, awaited, out := state()
		if held == 0 && awaited == 0 && out == 0 {
			return
		}
		select {
		case <-deadline:
			t.Errorf("after %s: %d held and %d awaited locks of the spend key space, %d connections out of the pool(s): the lock or the connection leaked", way, held, awaited, out)
			return
		case <-time.After(10 * time.Millisecond):
		}
	}
}

type panicClock struct{}

func (panicClock) Now() time.Time { panic("synthetic panic inside the spend") }

var _ clock.Clock = panicClock{}

func TestTheSpendLockIsReleasedOnEveryPath(t *testing.T) {
	t.Run("commit", func(t *testing.T) {
		w := newWorld(t)
		if res, err := w.led.Spend(ctx(t), cmdFor(w.issue(t, 1))); err != nil || !res.Spent {
			t.Fatalf("Spend = %+v, %v", res, err)
		}
		requireReleased(t, w, "a commit", 0)
	})
	t.Run("replay", func(t *testing.T) {
		w := newWorld(t)
		g := w.issue(t, 1)
		if res, err := w.led.Spend(ctx(t), cmdFor(g)); err != nil || !res.Spent {
			t.Fatalf("Spend = %+v, %v", res, err)
		}
		if res, err := w.led.Spend(ctx(t), cmdFor(g)); err != nil || !res.Replay {
			t.Fatalf("second Spend = %+v, %v; want a replay", res, err)
		}
		requireReleased(t, w, "a replay", 0)
	})
	t.Run("rolled back by a fault that is not a serialization failure", func(t *testing.T) {
		w := newWorld(t)
		faultOnSpend(t, w, "XX000", 1000, 0)
		if _, err := w.led.Spend(ctx(t), cmdFor(w.issue(t, 1))); err == nil {
			t.Fatal("the injected fault did not fail the spend")
		}
		requireReleased(t, w, "a rollback", 0)
	})
	t.Run("a serialization failure on every attempt", func(t *testing.T) {
		w := newWorld(t)
		w.led.SerializationAttempts = 3
		faultOnSpend(t, w, "40001", 1000, 0)
		if _, err := w.led.Spend(ctx(t), cmdFor(w.issue(t, 1))); !errors.Is(err, controller.ErrSpendRetryable) {
			t.Fatalf("Spend error = %v; want the typed retryable error", err)
		}
		requireReleased(t, w, "three refused attempts", 0)
	})
	t.Run("the context expires while the transaction runs", func(t *testing.T) {
		w := newWorld(t)
		slowSpends(t, w, 5000)
		c, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
		defer cancel()
		g := w.issue(t, 1)
		done := make(chan error, 1)
		go func() { _, err := w.led.Spend(c, cmdFor(g)); done <- err }()
		select {
		case err := <-done:
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("Spend error = %v; want the context's deadline", err)
			}
		case <-time.After(4 * time.Second):
			t.Fatal("Spend returned long after its 400 ms deadline, or not at all")
		}
		requireReleased(t, w, "a deadline that ended the transaction", 3*time.Second)
		if got := w.count(t, `SELECT count(*) FROM grant_spend`); got != 0 {
			t.Fatalf("%d rows in grant_spend after an abandoned spend", got)
		}
	})
	t.Run("the context was already cancelled", func(t *testing.T) {
		w := newWorld(t)
		c, cancel := context.WithTimeout(context.Background(), time.Minute)
		cancel()
		if _, err := w.led.Spend(c, cmdFor(w.issue(t, 1))); !errors.Is(err, context.Canceled) {
			t.Fatalf("Spend error = %v; want the cancellation", err)
		}
		requireReleased(t, w, "a cancelled context", 0)
	})
	t.Run("a panic inside the spend", func(t *testing.T) {
		w := newWorld(t)
		pool := ownPool(t, w)
		led := *w.led
		led.Pool = pool
		led.Clock = panicClock{}
		g := w.issue(t, 1)
		var recovered any
		func() {
			defer func() { recovered = recover() }()
			_, _ = led.Spend(ctx(t), cmdFor(g))
		}()
		if recovered == nil {
			t.Fatal("the panic did not reach the caller: Spend must not swallow it")
		}
		requireReleased(t, w, "a panic inside the spend", 3*time.Second, pool)
		if got := w.count(t, `SELECT count(*) FROM grant_spend`); got != 0 {
			t.Fatalf("%d rows in grant_spend after a spend that panicked", got)
		}
		if got := w.count(t, `SELECT count(*) FROM decision_grant WHERE grant_id = $1 AND status = 'ISSUED'`, g.GrantID); got != 1 {
			t.Fatal("the grant is not ISSUED after a spend that panicked")
		}
		if res, err := w.led.Spend(ctx(t), cmdFor(g)); err != nil || !res.Spent {
			t.Fatalf("the next spend of the tenant = %+v, %v; want spent (the lock must not stay behind)", res, err)
		}
	})
	t.Run("the unlock itself fails", func(t *testing.T) {
		w := newWorld(t)
		if _, err := w.admin.Exec(ctx(t), `REVOKE EXECUTE ON FUNCTION pg_advisory_unlock(int4, int4) FROM PUBLIC`); err != nil {
			t.Fatal(err)
		}
		_, probe := w.exec.Exec(ctx(t), `SELECT pg_advisory_unlock(1, 2)`)
		wantPg(t, probe, "42501")
		g := w.issue(t, 1)
		_, err := w.led.Spend(ctx(t), cmdFor(g))
		if err != nil {
			wantPg(t, err, "42501")
		}
		requireReleased(t, w, "an unlock that failed", 5*time.Second)
		if got := w.count(t, `SELECT count(*) FROM decision_grant WHERE grant_id = $1 AND status = 'SPENT'`, g.GrantID); got != 1 {
			t.Fatalf("the spend committed before the unlock failed, but the grant is not SPENT (%d)", got)
		}
	})
	t.Run("the backend is terminated while the transaction runs", func(t *testing.T) {
		w := newWorld(t)
		slowSpends(t, w, 5000)
		done := make(chan error, 1)
		g := w.issue(t, 1)
		go func() { _, err := w.led.Spend(ctx(t), cmdFor(g)); done <- err }()
		waitFor(t, "the spend to be inside its slow insert", func() bool {
			return w.count(t, `SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND state = 'active' AND query LIKE 'INSERT INTO grant_spend%'`) == 1
		})
		if _, err := w.admin.Exec(ctx(t), `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = current_database() AND query LIKE 'INSERT INTO grant_spend%'`); err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-done:
			if err == nil {
				t.Fatal("a spend whose backend was terminated reported success")
			}
		case <-time.After(20 * time.Second):
			t.Fatal("the spend did not return after its backend was terminated")
		}
		requireReleased(t, w, "a terminated backend", 5*time.Second)
	})
}

// A spend that waited for the lock and then got its turn leaves no waiter behind: while it waits there is exactly one waiter and one connection out; when it has spent, no waiter,
// no holder, no backend blocked on a lock, no connection out.
func TestASpendThatWaitedForItsTurnLeavesNoWaiterBehind(t *testing.T) {
	w := newWorld(t)
	w.led.SerializationAttempts = 3
	g := w.issue(t, 1)
	release := holdSpendLock(t, w, tenant)
	done := make(chan spendOutcome, 1)
	go func() {
		res, err := w.led.Spend(ctx(t), cmdFor(g))
		done <- spendOutcome{res, err}
	}()
	waitFor(t, "the spend to queue on its lock", func() bool { _, waiting := lockCounts(t, w, ledger.SpendLockName(tenant)); return waiting == 1 })
	if out := w.exec.Stat().AcquiredConns(); out != 1 {
		t.Fatalf("%d connections are out while one spend waits, want 1", out)
	}
	release()
	select {
	case o := <-done:
		if o.err != nil || !o.res.Spent {
			t.Fatalf("Spend after its turn = %+v, %v; want spent", o.res, o.err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the waiting spend did not run when the lock was released")
	}
	requireReleased(t, w, "a spend that waited and then ran", 0)
	if n := w.count(t, `SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND wait_event_type = 'Lock'`); n != 0 {
		t.Fatalf("%d backends are still blocked on a lock", n)
	}
}

// Fifty spends of five tenants, ten each, all at once, each tenant on its own login and pool as in production: every spend either commits or, after its attempts, gets the typed
// retryable error (the lock orders the spends of one tenant; SERIALIZABLE transactions of different tenants can still refuse one another, see the ledger README), and when the
// last has returned there is no lock held or awaited and no connection out of any pool.
func TestFiftyConcurrentSpendsOfFiveTenantsLeaveNoLockAndNoConnection(t *testing.T) {
	w := newWorld(t)
	const tenants, perTenant = 5, 10
	var pools []*pgxpool.Pool
	type job struct {
		led *ledger.Ledger
		cmd controller.SpendCommand
	}
	var jobs []job
	for k := 1; k <= tenants; k++ {
		id := fmt.Sprintf("ten_t%d", k)
		if _, err := w.admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ($1, $1)`, id); err != nil {
			t.Fatal(err)
		}
		led := *w.led
		led.SerializationAttempts = 3
		led.Pool = testpg.TenantLogin(t, w.admin, w.admin.Config().ConnString(), "execution_rw", id)
		pools = append(pools, led.Pool)
		for i := 1; i <= perTenant; i++ {
			g := w.issue(t, k*100+i, func(g *controller.Grant) {
				g.TenantID, g.DecisionID = id, fmt.Sprintf("dec_000%d", k)
				g.GrantID, g.Nonce, g.TargetRef = fmt.Sprintf("grt_%d%03d", k, i), fmt.Sprintf("n_ab3%d%03d", k, i), fmt.Sprintf("TX%d", i)
			})
			jobs = append(jobs, job{&led, controller.SpendCommand{Grant: g, IdempotencyKey: "idem-" + g.GrantID, LogicalAction: "ESCALATE_CASE"}})
		}
	}
	start := make(chan struct{})
	outs := make([]spendOutcome, len(jobs))
	var wg sync.WaitGroup
	for i := range jobs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			res, err := jobs[i].led.Spend(ctx(t), jobs[i].cmd)
			outs[i] = spendOutcome{res, err}
		}()
	}
	close(start)
	wg.Wait()
	spent := 0
	for i, o := range outs {
		switch {
		case o.err == nil && o.res.Spent:
			spent++
		case errors.Is(o.err, controller.ErrSpendRetryable) && !o.res.Spent:
			if got := w.count(t, `SELECT count(*) FROM decision_grant WHERE grant_id = $1 AND status = 'ISSUED'`, jobs[i].cmd.Grant.GrantID); got != 1 {
				t.Errorf("spend %d was refused as retryable but its grant is not ISSUED", i)
			}
		default:
			t.Errorf("spend %d = %+v, %v; want spent, or the typed retryable error with the grant unspent", i, o.res, o.err)
		}
	}
	if got := w.count(t, `SELECT count(*) FROM grant_spend`); got != spent {
		t.Errorf("%d rows in grant_spend for %d spends that reported success", got, spent)
	}
	t.Logf("%d of %d spends committed; the rest exhausted their attempts on a serialization failure between tenants and got the typed retryable error", spent, len(jobs))
	requireReleased(t, w, "fifty concurrent spends of five tenants", 0, pools...)
}
