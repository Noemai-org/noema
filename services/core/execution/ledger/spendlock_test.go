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
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/execution/controller"
	"github.com/Noemai-org/noema/services/core/execution/ledger"
	"github.com/Noemai-org/noema/services/core/internal/testpg"
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
func lockCounts(t *testing.T, w *world, name string) (granted, waiting int) {
	t.Helper()
	q := `SELECT count(*) FILTER (WHERE granted), count(*) FILTER (WHERE NOT granted) FROM pg_locks
		WHERE locktype = 'advisory' AND objsubid = 2 AND classid::bigint = (hashtext($1)::bigint & 4294967295) AND ($2 = '' OR objid::bigint = (hashtext($2)::bigint & 4294967295))`
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
		_ = w.admin.QueryRow(c, `SELECT count(*) FROM pg_locks WHERE locktype = 'advisory' AND NOT granted`).Scan(&n)
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
