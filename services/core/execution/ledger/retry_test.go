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

// endTheLoginsBackends ends every server-side connection of the tenant's execution login and returns once none is left. Spend returned on the deadline, but a statement the
// client sent just before it can still be waiting at the server, and a loaded server may begin it after Spend returned: the probe counts it, and it is one attempt in flight,
// not a new one. With every connection gone nothing the client sent earlier can be counted, so any attempt counted afterwards was started by the client. It polls every 25 ms,
// counting polls and not reading the clock (the clock rule), and fails the test if connections remain after about ten seconds (a client that keeps reconnecting).
func endTheLoginsBackends(t *testing.T, w *world) {
	t.Helper()
	const polls = 400
	for i := 0; i < polls; i++ {
		var left int
		if err := w.admin.QueryRow(ctx(t), `SELECT count(*) FROM pg_stat_activity
			WHERE datname = current_database() AND pid <> pg_backend_pid() AND usename LIKE 'execution_rw%'`).Scan(&left); err != nil {
			t.Fatal(err)
		}
		if left == 0 {
			return
		}
		if _, err := w.admin.Exec(ctx(t), `SELECT pg_terminate_backend(pid) FROM pg_stat_activity
			WHERE datname = current_database() AND pid <> pg_backend_pid() AND usename LIKE 'execution_rw%'`); err != nil {
			t.Fatal(err)
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("connections of the execution login were still there about ten seconds after Spend returned")
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

// isDidNotCommit40001 reads the answer from the chain, not from the message (noema#748 review, A347): the database's 40001 as the cause, under the typed retryable error.
func isDidNotCommit40001(err error) bool {
	return err != nil && sqlState(err) == "40001" && errors.Is(err, controller.ErrSpendRetryable)
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

// The contention is real. A transaction outside the spends holds the Record chain head of the tenant and commits an event; eight spends of eight distinct unspent grants of
// that tenant are started behind it. With one attempt and the tenant's spend lock (ruling A339), exactly one of them is on the head when the writer commits, the one whose
// turn it is: it is refused with 40001 although its grant is still ISSUED (the defect of noema#714, reproduced without an injected fault, for the one writer the lock does
// not cover). The other seven were queued on the lock, not on the head: they begin their transactions after the commit, see it, and spend. Before the lock every spend
// that had a connection was on the head and was refused.
func TestWithOneAttemptOnlyTheSpendWhoseTurnItIsLosesTheHeadToAWriterOutsideTheLock(t *testing.T) {
	w := newWorld(t)
	w.led.SerializationAttempts = 1
	const n = 8
	queued := int(w.exec.Config().MaxConns) - 1
	if queued < 2 || queued > n {
		t.Fatalf("the pool has %d connections: the test needs between 3 and %d", queued+1, n+1)
	}
	grants, cmd := distinct(t, w, n)
	blocker, err := w.exec.Begin(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = blocker.Rollback(ctx(t)) }()
	if _, err := w.led.Rec.Append(ctx(t), blocker, chain.Event{TenantID: tenant, CorrelationID: corr, WrittenBy: "core-execution", Actor: "core-execution",
		EventType: "REVALIDATION_PASSED", ObjectRef: grants[0].GrantID}); err != nil {
		t.Fatal(err)
	}
	wait := spendEach(t, w, grants, cmd)
	waitForLockWaiters(t, w, queued)
	if err := blocker.Commit(ctx(t)); err != nil {
		t.Fatal(err)
	}
	refused, spent := 0, 0
	for i, o := range wait() {
		switch {
		case isDidNotCommit40001(o.err):
			refused++
		case o.err != nil:
			t.Errorf("spend %d: %v; want success or the 40001 refusal", i, o.err)
		case o.res.Spent:
			spent++
		}
	}
	if refused != 1 || spent != n-1 {
		t.Fatalf("%d spends were refused with 40001 and %d spent, want exactly 1 and %d", refused, spent, n-1)
	}
	if got := w.count(t, `SELECT count(*) FROM decision_grant WHERE status = 'ISSUED'`); got != 1 {
		t.Fatalf("%d grants are ISSUED, want exactly the refused one", got)
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
	// Spend returned on the deadline. The one connection that held the spend runs one statement at a time, so at most ONE statement sent just before the deadline may still
	// begin at the server after Spend returned, and the probe counts it (the failure this replaces read "8, then 9"). Anything more is a client that went on trying. So: take the
	// count at the return, end the login's server-side connections (endTheLoginsBackends), allow the count to have moved by at most one, then check that nothing more comes.
	atReturn := probe()
	endTheLoginsBackends(t, w)
	made := probe()
	if made < 1 || made >= 1000 {
		t.Fatalf("%d attempts were made inside a 500 ms deadline, want at least one and far fewer than 1000", made)
	}
	if made-atReturn > 1 {
		t.Fatalf("%d attempts were started after Spend returned (%d at the return, %d once the statements ended), want at most the one already sent", made-atReturn, atReturn, made)
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
