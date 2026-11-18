package ledger_test

// noema#714, Architect rulings A317 to A328 (relayed, interim), REQ-EXE-001, REQ-EXE-002, INV-03, INV-07, AGENT-08, AGENT-16: a spend the database refuses with a
// serialization failure (SQLSTATE 40001) wrote nothing, so the ledger tries the whole transaction again, a bounded number of times that comes from configuration and
// has no default. Each attempt is a fresh transaction whose first statement reads the grant again; a spend that an earlier winner made final becomes a replay. Only 40001
// is retried. The context deadline bounds all attempts. After the last attempt the error is the one the ledger always returned. Everything is SYNTHETIC.
//
// The contention is real: concurrent spends of one tenant on a real PostgreSQL, all appending to one Record chain head. The exact attempt counts are measured with a
// fault the database itself raises (a trigger on grant_spend that counts its calls in a sequence, which a rollback does not undo, and raises the SQLSTATE it is
// given), because a real race cannot be made to fail an exact number of times, nor to raise a deadlock or a unique violation on demand. Nothing in the Go code is
// mocked.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/execution/controller"
	"github.com/Noemai-org/noema/services/core/execution/ledger"
	"github.com/Noemai-org/noema/services/core/record/chain"
)

const settingName = "NOEMA_DB_SERIALIZATION_ATTEMPTS"

func cmdFor(g controller.Grant) controller.SpendCommand {
	return controller.SpendCommand{Grant: g, IdempotencyKey: "idem-1", LogicalAction: "ESCALATE_CASE"}
}

// faultOnSpend installs a trigger on grant_spend that counts every insert in a sequence (a sequence is not rolled back, so the count survives the failed attempt) and,
// for the first failFirst of them, raises sqlstate after sleeping sleepMS milliseconds. It returns the function that reads the count.
func faultOnSpend(t *testing.T, w *world, sqlstate string, failFirst, sleepMS int) func() int {
	t.Helper()
	ddl := fmt.Sprintf(`CREATE SEQUENCE spend_probe;
		GRANT USAGE ON SEQUENCE spend_probe TO execution_rw;
		CREATE FUNCTION spend_probe_fault() RETURNS trigger LANGUAGE plpgsql AS $f$
		BEGIN
		  IF nextval('spend_probe') <= %d THEN
		    PERFORM pg_sleep(%d / 1000.0);
		    RAISE EXCEPTION 'injected fault' USING ERRCODE = '%s';
		  END IF;
		  RETURN NEW;
		END $f$;
		CREATE TRIGGER aa_spend_probe BEFORE INSERT ON grant_spend FOR EACH ROW EXECUTE FUNCTION spend_probe_fault();`, failFirst, sleepMS, sqlstate)
	if _, err := w.admin.Exec(ctx(t), ddl); err != nil {
		t.Fatal(err)
	}
	return func() int {
		var last int
		var called bool
		if err := w.admin.QueryRow(ctx(t), `SELECT last_value, is_called FROM spend_probe`).Scan(&last, &called); err != nil {
			t.Fatal(err)
		}
		if !called {
			return 0
		}
		return last
	}
}

type spendOutcome struct {
	res controller.SpendResult
	err error
}

// spendAll starts one Spend per grant, each on its own goroutine, all released together, and returns what they come to.
func spendAll(t *testing.T, w *world, grants []controller.Grant) func() []spendOutcome {
	t.Helper()
	start := make(chan struct{})
	outs := make([]spendOutcome, len(grants))
	var wg sync.WaitGroup
	c := ctx(t)
	for i := range grants {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			res, err := w.led.Spend(c, cmdFor(grants[i]))
			outs[i] = spendOutcome{res, err}
		}()
	}
	close(start)
	return func() []spendOutcome {
		wg.Wait()
		return outs
	}
}

func issueN(t *testing.T, w *world, n int) []controller.Grant {
	t.Helper()
	gs := make([]controller.Grant, n)
	for i := range gs {
		gs[i] = w.issue(t, i+1)
	}
	return gs
}

func isDidNotCommit40001(err error) bool {
	return err != nil && sqlState(err) == "40001" && strings.Contains(err.Error(), "did not commit")
}

// waitForLockWaiters waits until n backends of the test database are blocked on a lock, as the spends are when they queue behind an open transaction.
func waitForLockWaiters(t *testing.T, w *world, n int) {
	t.Helper()
	deadline := time.After(20 * time.Second)
	for {
		var got int
		if err := w.admin.QueryRow(ctx(t), `SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND wait_event_type = 'Lock'`).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got >= n {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("only %d backends are waiting on a lock, want %d", got, n)
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// The contention is real. A transaction holds the Record chain head of the tenant and commits an event; eight spends of eight distinct unspent grants of that tenant
// are started behind it. The pool has a fixed number of connections, one of them the holder's, so as many spends as the pool has other connections are in their
// transactions, queued on the lock, when the holder commits. With one attempt, every one of those is refused with 40001 although its grant is still ISSUED: the defect of
// noema#714, reproduced without an injected fault. (A spend that had not yet got a connection starts after the commit and is not refused.)
func TestWithOneAttemptASpendThatLosesTheHeadRaceIsRefusedWhileItsGrantIsStillIssued(t *testing.T) {
	w := newWorld(t)
	w.led.SerializationAttempts = 1
	const n = 8
	queued := int(w.exec.Config().MaxConns) - 1
	if queued < 2 || queued > n {
		t.Fatalf("the pool has %d connections: the test needs between 3 and %d", queued+1, n+1)
	}
	grants := issueN(t, w, n)
	blocker, err := w.exec.Begin(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = blocker.Rollback(ctx(t)) }()
	if _, err := w.led.Rec.Append(ctx(t), blocker, chain.Event{TenantID: tenant, CorrelationID: corr, WrittenBy: "core-execution", Actor: "core-execution",
		EventType: "REVALIDATION_PASSED", ObjectRef: grants[0].GrantID}); err != nil {
		t.Fatal(err)
	}
	wait := spendAll(t, w, grants)
	waitForLockWaiters(t, w, queued)
	if err := blocker.Commit(ctx(t)); err != nil {
		t.Fatal(err)
	}
	refused := 0
	for i, o := range wait() {
		switch {
		case isDidNotCommit40001(o.err):
			refused++
		case o.err != nil:
			t.Errorf("spend %d: %v; want success or the 40001 refusal", i, o.err)
		}
	}
	if refused < queued {
		t.Fatalf("%d spends were refused with 40001, want at least the %d that were queued on the lock", refused, queued)
	}
	if got := w.count(t, `SELECT count(*) FROM decision_grant WHERE status = 'ISSUED'`); got < refused {
		t.Fatalf("%d grants are ISSUED but %d spends were refused: a refused spend must leave its grant ISSUED", got, refused)
	}
	verifyChain(t, w)
}

// The same eight spends, free-running, with the attempts the ruling gives. The grants share one dispatch key, whose budget is two (A3, A62, A153): the Record chain takes
// two commits from them at most, so a spend that loses the head race twice meets a key whose budget is spent on its third attempt. Two spend, six are refused by the
// budget, none is refused with 40001, no grant is spent twice.
func TestWithThreeAttemptsNoSerializationFailureReachesACallerOfAnUnspentGrant(t *testing.T) {
	for run := 1; run <= 5; run++ {
		t.Run(fmt.Sprintf("run%d", run), func(t *testing.T) {
			w := newWorld(t)
			w.led.SerializationAttempts = 3
			const n = 8
			grants := issueN(t, w, n)
			spent, budget := 0, 0
			for i, o := range spendAll(t, w, grants)() {
				switch {
				case o.err != nil:
					t.Errorf("spend %d reached its caller with %v", i, o.err)
				case o.res.Spent:
					spent++
				case o.res.BudgetSpent:
					budget++
				default:
					t.Errorf("spend %d: %+v, want spent or over budget", i, o.res)
				}
			}
			if spent != 2 || budget != n-2 {
				t.Fatalf("%d spent, %d over budget; want 2 and %d", spent, budget, n-2)
			}
			for q, want := range map[string]int{
				`SELECT count(*) FROM grant_spend`:                                                                                 2,
				`SELECT count(*) FROM record_event WHERE event_type = 'GRANT_SPENT'`:                                               2,
				`SELECT count(*) FROM record_event WHERE event_type = 'DISPATCH_ATTEMPTED'`:                                        2,
				`SELECT count(*) FROM decision_grant WHERE status = 'ISSUED'`:                                                      n - 2,
				`SELECT attempt FROM idempotency_record WHERE idempotency_key = 'idem-1'`:                                          2,
				`SELECT count(*) FROM decision_grant WHERE status IN ('SPENT') AND grant_id IN (SELECT grant_id FROM grant_spend)`: 2,
			} {
				if got := w.count(t, q); got != want {
					t.Errorf("%s = %d, want %d", q, got, want)
				}
			}
			verifyChain(t, w)
		})
	}
}

// A spend that loses a race to another spend of the same grant is tried again in a fresh transaction, whose first statement reads the grant as SPENT: it is a replay, not an
// error. Eight callers, one grant: one spends, seven replay, none fails.
func TestACallerThatLosesTheRaceForTheSameGrantGetsAReplayOnTheNextAttempt(t *testing.T) {
	w := newWorld(t)
	w.led.SerializationAttempts = 3
	g := w.issue(t, 1)
	grants := []controller.Grant{g, g, g, g, g, g, g, g}
	spent, replay := 0, 0
	for i, o := range spendAll(t, w, grants)() {
		switch {
		case o.err != nil:
			t.Errorf("spend %d: %v", i, o.err)
		case o.res.Spent:
			spent++
		case o.res.Replay:
			replay++
		default:
			t.Errorf("spend %d: %+v", i, o.res)
		}
	}
	if spent != 1 || replay != len(grants)-1 {
		t.Fatalf("%d spent and %d replays; want 1 and %d", spent, replay, len(grants)-1)
	}
	if got := w.count(t, `SELECT count(*) FROM record_event WHERE event_type = 'GRANT_SPENT'`); got != 1 {
		t.Fatalf("%d GRANT_SPENT events, want 1", got)
	}
	verifyChain(t, w)
}

// Two attempts are refused and the third is not: the spend commits on its third try, in a transaction of its own. The refused attempts left nothing behind: the
// idempotency attempt is 1, one GRANT_SPENT and one DISPATCH_ATTEMPTED are on the Record, one row is in grant_spend.
func TestASpendRefusedTwiceWithAFailureToSerializeCommitsOnTheThirdAttempt(t *testing.T) {
	w := newWorld(t)
	w.led.SerializationAttempts = 3
	probe := faultOnSpend(t, w, "40001", 2, 0)
	g := w.issue(t, 1)
	res, err := w.led.Spend(ctx(t), cmdFor(g))
	if err != nil || !res.Spent || res.Attempt != 1 {
		t.Fatalf("Spend = %+v, %v; want spent, attempt 1", res, err)
	}
	if got := probe(); got != 3 {
		t.Fatalf("the spend was attempted %d times, want 3", got)
	}
	for q, want := range map[string]int{
		`SELECT count(*) FROM grant_spend`:                                          1,
		`SELECT count(*) FROM record_event WHERE event_type = 'GRANT_SPENT'`:        1,
		`SELECT count(*) FROM record_event WHERE event_type = 'DISPATCH_ATTEMPTED'`: 1,
		`SELECT attempt FROM idempotency_record WHERE idempotency_key = 'idem-1'`:   1,
	} {
		if got := w.count(t, q); got != want {
			t.Errorf("%s = %d, want %d", q, got, want)
		}
	}
	verifyChain(t, w)
}

// After the last attempt the error is the one the ledger always returned for a spend that did not commit, and the grant is still ISSUED. The number of attempts made is
// exactly the configured number: not one more, not one less.
func TestAfterTheLastAttemptTheErrorIsTheDidNotCommitError(t *testing.T) {
	for _, attempts := range []int{1, 2, 3, 4} {
		t.Run(fmt.Sprintf("attempts%d", attempts), func(t *testing.T) {
			w := newWorld(t)
			w.led.SerializationAttempts = attempts
			probe := faultOnSpend(t, w, "40001", 1000, 0)
			g := w.issue(t, 1)
			res, err := w.led.Spend(ctx(t), cmdFor(g))
			if !isDidNotCommit40001(err) || res.Spent || res.Replay || res.BudgetSpent {
				t.Fatalf("Spend = %+v, %v; want the 40001 'did not commit' error", res, err)
			}
			if got := probe(); got != attempts {
				t.Fatalf("the spend was attempted %d times, want %d", got, attempts)
			}
			if got := w.count(t, `SELECT count(*) FROM decision_grant WHERE grant_id = $1 AND status = 'ISSUED'`, g.GrantID); got != 1 {
				t.Fatal("the grant is not ISSUED")
			}
			if got := w.count(t, `SELECT count(*) FROM grant_spend`); got != 0 {
				t.Fatalf("%d rows in grant_spend, want 0", got)
			}
		})
	}
}

// Only a serialization failure is retried. A unique violation, a deadlock and any other error make exactly one attempt (a deadlock stays as it is: the ruling names 40001
// alone).
func TestOnlyASerializationFailureIsRetried(t *testing.T) {
	for _, code := range []string{"23505", "40P01", "P0001", "XX000"} {
		t.Run(code, func(t *testing.T) {
			w := newWorld(t)
			w.led.SerializationAttempts = 3
			probe := faultOnSpend(t, w, code, 1000, 0)
			g := w.issue(t, 1)
			res, err := w.led.Spend(ctx(t), cmdFor(g))
			if err == nil || res.Spent {
				t.Fatalf("Spend = %+v, %v; want the error", res, err)
			}
			if got := sqlState(err); got != code {
				t.Errorf("the error carries SQLSTATE %q, want %q", got, code)
			}
			if got := probe(); got != 1 {
				t.Fatalf("the spend was attempted %d times, want 1", got)
			}
		})
	}
}

// The context deadline bounds all the attempts: with a thousand attempts allowed, every one of them slow and refused, the spend returns when the deadline does, an error,
// and nothing goes on trying after it.
func TestTheContextDeadlineStopsTheAttempts(t *testing.T) {
	w := newWorld(t)
	w.led.SerializationAttempts = 1000
	probe := faultOnSpend(t, w, "40001", 1000000, 50)
	g := w.issue(t, 1)
	c, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
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
		t.Fatal("Spend ran on after its context deadline")
	}
	if o.err == nil || o.res.Spent {
		t.Fatalf("Spend = %+v, %v; want an error", o.res, o.err)
	}
	if !isDidNotCommit40001(o.err) && !errors.Is(o.err, context.DeadlineExceeded) && !errors.Is(o.err, context.Canceled) {
		t.Errorf("the error is neither the did-not-commit error nor the context's: %v", o.err)
	}
	made := probe()
	if made < 1 || made >= 1000 {
		t.Fatalf("%d attempts were made inside a 500 ms deadline, want at least one and far fewer than 1000", made)
	}
	time.Sleep(300 * time.Millisecond)
	if later := probe(); later != made {
		t.Fatalf("attempts went on after Spend returned: %d, then %d", made, later)
	}
}

// AGENT-16: the number of attempts has no default. A Ledger without a value of at least one refuses to spend, names the setting, and writes nothing.
func TestALedgerWithoutSerializationAttemptsRefusesToSpend(t *testing.T) {
	for _, attempts := range []int{0, -1} {
		t.Run(fmt.Sprint(attempts), func(t *testing.T) {
			w := newWorld(t)
			w.led.SerializationAttempts = attempts
			probe := faultOnSpend(t, w, "P0001", 0, 0)
			g := w.issue(t, 1)
			res, err := w.led.Spend(ctx(t), cmdFor(g))
			if err == nil || res.Spent || !strings.Contains(err.Error(), settingName) {
				t.Fatalf("Spend = %+v, %v; want a refusal that names %s", res, err, settingName)
			}
			if got := probe(); got != 0 {
				t.Fatalf("%d transactions reached the insert, want none", got)
			}
			if got := w.count(t, `SELECT count(*) FROM decision_grant WHERE grant_id = $1 AND status = 'ISSUED'`, g.GrantID); got != 1 {
				t.Fatal("the grant is not ISSUED")
			}
		})
	}
}

// AGENT-16: the setting is read with no default. A missing or invalid value is an error that names the setting, so that the process refuses to configure the ledger.
func TestTheSerializationAttemptsSettingHasNoDefaultAndIsStrict(t *testing.T) {
	if ledger.EnvSerializationAttempts != settingName {
		t.Fatalf("EnvSerializationAttempts = %q, want %q", ledger.EnvSerializationAttempts, settingName)
	}
	for raw, want := range map[string]int{"1": 1, "2": 2, "3": 3, "10": 10} {
		if got, err := ledger.ParseSerializationAttempts(raw); err != nil || got != want {
			t.Errorf("ParseSerializationAttempts(%q) = %d, %v; want %d", raw, got, err, want)
		}
	}
	for _, raw := range []string{"", " ", "0", "-1", "+3", "03", "abc", "3.5", "3 ", " 3", "3a", "0x3", "99999999999999999999999"} {
		got, err := ledger.ParseSerializationAttempts(raw)
		if err == nil || got != 0 || !strings.Contains(err.Error(), settingName) {
			t.Errorf("ParseSerializationAttempts(%q) = %d, %v; want 0 and an error that names %s", raw, got, err, settingName)
		}
	}
}
