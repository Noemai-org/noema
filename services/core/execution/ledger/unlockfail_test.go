package ledger_test

// noema#748, Architect ruling A348 as refined (relayed by the user; interim, not committed): when the spend COMMITTED and only the unlock fails, Spend returns the committed
// result and no error; the failure is logged as ONE line of fixed text and the connection is closed (closing the session ends the lock), so nothing leaks. The release runs on the
// same dedicated connection, in a defer on every path, with a fresh bounded context (never the caller's, which may be cancelled). When the spend did NOT commit and the unlock
// also fails, the spend's own error is returned. REQ-EXE-001, REQ-EXE-002, INV-03, INV-07, AGENT-13, AGENT-16. Everything is SYNTHETIC.
//
// The faults are the real database's: EXECUTE on pg_advisory_unlock revoked from PUBLIC (the unlock is refused with 42501), a trigger that releases the session's locks in the
// middle of the spend (the unlock then finds nothing to release), and a pool whose connections cancel the caller's context the moment the commit is acknowledged (so the
// unlock runs after the caller's context is done). Nothing is mocked.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/execution/controller"
	"github.com/Noemai-org/noema/services/core/execution/ledger"
)

const (
	wantReleaseLogMsg   = "ledger: the spend lock could not be released cleanly: the connection was closed"
	wantReleaseLogEvent = "SPEND_LOCK_RELEASE_FAILED"
)

// logSink collects what a logger writes.
type logSink struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *logSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *logSink) lines() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, l := range strings.Split(s.buf.String(), "\n") {
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

func newSinkLogger() (*slog.Logger, *logSink) {
	sink := &logSink{}
	return slog.New(slog.NewJSONHandler(sink, &slog.HandlerOptions{Level: slog.LevelDebug})), sink
}

// requireReleaseLog fails unless the sink holds exactly n lines and each is the one line of fixed text: the level, the message and the event name, and nothing else (the timestamp
// aside). No tenant, grant, key, nonce, SQLSTATE or driver text can be in it, because no attribute carries one.
func requireReleaseLog(t *testing.T, sink *logSink, n int) {
	t.Helper()
	lines := sink.lines()
	if len(lines) != n {
		t.Fatalf("%d log lines, want %d: %q", len(lines), n, lines)
	}
	for _, line := range lines {
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("log line is not JSON: %q", line)
		}
		delete(rec, "time")
		want := map[string]any{"level": "WARN", "msg": wantReleaseLogMsg, "event": wantReleaseLogEvent}
		if len(rec) != len(want) || rec["level"] != want["level"] || rec["msg"] != want["msg"] || rec["event"] != want["event"] {
			t.Errorf("log line %v, want exactly %v", rec, want)
		}
		for _, leak := range append(append([]string{}, leaks...), "42501", "pg_advisory", "permission denied") {
			if strings.Contains(line, leak) {
				t.Errorf("the log line carries %q: %q", leak, line)
			}
		}
	}
}

// revokeUnlock makes the database refuse every pg_advisory_unlock of the two-key form with 42501, and shows it does.
func revokeUnlock(t *testing.T, w *world) {
	t.Helper()
	if _, err := w.admin.Exec(ctx(t), `REVOKE EXECUTE ON FUNCTION pg_advisory_unlock(int4, int4) FROM PUBLIC`); err != nil {
		t.Fatal(err)
	}
	_, probe := w.exec.Exec(ctx(t), `SELECT pg_advisory_unlock(1, 2)`)
	wantPg(t, probe, "42501")
}

// ledgerOn is a copy of the world's ledger on its own pool, logging to a sink, so that the pool's connections can be counted and the log read.
func ledgerOn(t *testing.T, w *world, pool *pgxpool.Pool) (*ledger.Ledger, *logSink) {
	t.Helper()
	logger, sink := newSinkLogger()
	led := *w.led
	led.Pool = pool
	led.Log = logger
	return &led, sink
}

// requireNoConnectionKept fails unless the pool holds no connection at all: one that had to be closed is not in the pool, idle or out.
func requireNoConnectionKept(t *testing.T, pool *pgxpool.Pool, way string) {
	t.Helper()
	if s := pool.Stat(); s.TotalConns() != 0 || s.AcquiredConns() != 0 || s.IdleConns() != 0 {
		t.Errorf("after %s the pool holds %d connections (%d out, %d idle); a connection that could not be shown clean must be closed, not returned", way, s.TotalConns(), s.AcquiredConns(), s.IdleConns())
	}
}

func TestACommittedSpendWhoseUnlockFailsReturnsTheCommittedResultAndLogsOneFixedLine(t *testing.T) {
	w := newWorld(t)
	pool := ownPool(t, w)
	led, sink := ledgerOn(t, w, pool)

	// Control: a clean spend keeps its connection in the pool and logs nothing, so that the counts below mean something.
	clean, cerr := led.Spend(ctx(t), cmdFor(w.issue(t, 1)))
	if cerr != nil || !clean.Spent || clean.Attempt != 1 {
		t.Fatalf("clean Spend = %+v, %v", clean, cerr)
	}
	if s := pool.Stat(); s.TotalConns() != 1 || s.IdleConns() != 1 {
		t.Fatalf("after a clean spend the pool holds %d connections (%d idle), want the one connection back", s.TotalConns(), s.IdleConns())
	}
	requireReleaseLog(t, sink, 0)

	revokeUnlock(t, w)
	g := w.issue(t, 2)
	cmd := controller.SpendCommand{Grant: g, IdempotencyKey: "idem-2", LogicalAction: "ESCALATE_CASE"}
	res, err := led.Spend(ctx(t), cmd)
	if err != nil {
		t.Fatalf("Spend error = %v; the spend committed and only the unlock failed, so the committed result is returned and no error", err)
	}
	if res != (controller.SpendResult{Spent: true, Attempt: 1}) {
		t.Fatalf("Spend = %+v; want the result a clean spend gives (Spent, attempt 1)", res)
	}
	requireReleaseLog(t, sink, 1)
	requireNoConnectionKept(t, pool, "an unlock that failed")
	requireReleased(t, w, "an unlock that failed", 5*time.Second, pool)
	if got := w.count(t, `SELECT count(*) FROM decision_grant WHERE grant_id = $1 AND status = 'SPENT'`, g.GrantID); got != 1 {
		t.Fatalf("the grant is not SPENT (%d)", got)
	}
	for _, ev := range []string{"GRANT_SPENT", "DISPATCH_ATTEMPTED"} {
		if got := w.count(t, `SELECT count(*) FROM record_event WHERE event_type = $1 AND tenant_id = $2 AND payload->>'idempotency_key' = 'idem-2'`, ev, tenant); got != 1 {
			t.Errorf("%d %s events of the committed spend in the Record, want 1", got, ev)
		}
	}

	// The tenant's next spend is not held up by the failed release.
	next, nerr := led.Spend(ctx(t), controller.SpendCommand{Grant: w.issue(t, 3), IdempotencyKey: "idem-3", LogicalAction: "ESCALATE_CASE"})
	if nerr != nil || !next.Spent {
		t.Fatalf("the next spend of the tenant = %+v, %v; want spent", next, nerr)
	}
	requireReleaseLog(t, sink, 2)
}

// A Ledger with no Log (the default) logs nothing and still answers with the committed result.
func TestACommittedSpendWhoseUnlockFailsWithNoLoggerStillReturnsTheCommittedResult(t *testing.T) {
	w := newWorld(t)
	pool := ownPool(t, w)
	led := *w.led
	led.Pool = pool
	if led.Log != nil {
		t.Fatal("the world's ledger has a logger")
	}
	revokeUnlock(t, w)
	res, err := led.Spend(ctx(t), cmdFor(w.issue(t, 1)))
	if err != nil || !res.Spent {
		t.Fatalf("Spend = %+v, %v; want the committed result and no error", res, err)
	}
	requireNoConnectionKept(t, pool, "an unlock that failed, with no logger")
	requireReleased(t, w, "an unlock that failed, with no logger", 5*time.Second, pool)
}

// The panic path: the panic reaches the caller with its own stack, nothing is held, and the failed release is logged once.
func TestAPanicInsideTheSpendWhoseUnlockAlsoFailsLogsOneLineAndLeavesNothing(t *testing.T) {
	w := newWorld(t)
	pool := ownPool(t, w)
	led, sink := ledgerOn(t, w, pool)
	led.Clock = panicClock{}
	revokeUnlock(t, w)
	g := w.issue(t, 1)
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		_, _ = led.Spend(ctx(t), cmdFor(g))
	}()
	if recovered == nil {
		t.Fatal("the panic did not reach the caller")
	}
	requireReleaseLog(t, sink, 1)
	requireNoConnectionKept(t, pool, "a panic whose unlock failed")
	requireReleased(t, w, "a panic whose unlock failed", 5*time.Second, pool)
	if got := w.count(t, `SELECT count(*) FROM grant_spend`); got != 0 {
		t.Fatalf("%d rows in grant_spend after a spend that panicked", got)
	}
}

// A panic with a working unlock: the unlock runs on the same connection, but the transaction was left open, so the connection is closed, not returned, and that is logged once.
func TestAPanicInsideTheSpendClosesTheConnectionWhoseTransactionWasLeftOpen(t *testing.T) {
	w := newWorld(t)
	pool := ownPool(t, w)
	led, sink := ledgerOn(t, w, pool)
	led.Clock = panicClock{}
	func() {
		defer func() { _ = recover() }()
		_, _ = led.Spend(ctx(t), cmdFor(w.issue(t, 1)))
	}()
	requireReleaseLog(t, sink, 1)
	requireNoConnectionKept(t, pool, "a panic inside the transaction")
	requireReleased(t, w, "a panic inside the transaction", 5*time.Second, pool)
}

type ctxKey struct{}

// cancelAfterCommit is a pgx tracer that cancels the caller's context the moment the database has acknowledged a COMMIT, so that the release that follows runs when the caller's
// context is already done.
type cancelAfterCommit struct {
	cancel context.CancelFunc
	fired  atomic.Bool
}

func (c *cancelAfterCommit) TraceQueryStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	if strings.EqualFold(d.SQL, "commit") {
		return context.WithValue(ctx, ctxKey{}, true)
	}
	return ctx
}

func (c *cancelAfterCommit) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryEndData) {
	if ctx.Value(ctxKey{}) != nil && d.Err == nil {
		c.fired.Store(true)
		c.cancel()
	}
}

// poolCancellingAfterCommit is a pool of the world's login whose connections cancel cancel() once a commit is acknowledged.
func poolCancellingAfterCommit(t *testing.T, w *world, cancel context.CancelFunc) (*pgxpool.Pool, *cancelAfterCommit) {
	t.Helper()
	tr := &cancelAfterCommit{cancel: cancel}
	cfg := w.exec.Config()
	cfg.ConnConfig.Tracer = tr
	p, err := pgxpool.NewWithConfig(ctx(t), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return p, tr
}

// The caller's context is cancelled while the spend's commit is acknowledged, before the release. The unlock uses a fresh bounded context, so it still succeeds: the committed
// result, no error, nothing logged, and the connection goes back to the pool.
func TestTheUnlockRunsOnAFreshContextWhenTheCallersContextIsCancelledAfterTheCommit(t *testing.T) {
	w := newWorld(t)
	c, cancel := context.WithCancel(ctx(t))
	defer cancel()
	pool, tr := poolCancellingAfterCommit(t, w, cancel)
	led, sink := ledgerOn(t, w, pool)
	res, err := led.Spend(c, cmdFor(w.issue(t, 1)))
	if !tr.fired.Load() || c.Err() == nil {
		t.Fatal("the scenario did not happen: the caller's context was not cancelled after the commit")
	}
	if err != nil || !res.Spent {
		t.Fatalf("Spend = %+v, %v; want the committed result and no error", res, err)
	}
	requireReleaseLog(t, sink, 0)
	if s := pool.Stat(); s.TotalConns() != 1 || s.IdleConns() != 1 {
		t.Errorf("the pool holds %d connections (%d idle), want the one connection back: the unlock on a fresh context succeeded", s.TotalConns(), s.IdleConns())
	}
	requireReleased(t, w, "a cancelled caller context during the unlock", 0, pool)
}

// The same, with the unlock failing as well: the committed result, no error, one line, the connection closed.
func TestACancelledCallerContextAndAFailedUnlockStillReturnTheCommittedResult(t *testing.T) {
	w := newWorld(t)
	c, cancel := context.WithCancel(ctx(t))
	defer cancel()
	pool, tr := poolCancellingAfterCommit(t, w, cancel)
	led, sink := ledgerOn(t, w, pool)
	revokeUnlock(t, w)
	res, err := led.Spend(c, cmdFor(w.issue(t, 1)))
	if !tr.fired.Load() || c.Err() == nil {
		t.Fatal("the scenario did not happen: the caller's context was not cancelled after the commit")
	}
	if err != nil || !res.Spent {
		t.Fatalf("Spend = %+v, %v; want the committed result and no error", res, err)
	}
	requireReleaseLog(t, sink, 1)
	requireNoConnectionKept(t, pool, "a failed unlock after the caller's context was cancelled")
	requireReleased(t, w, "a failed unlock after the caller's context was cancelled", 5*time.Second, pool)
}

// An unlock that finds nothing to release (the session's locks were released in the middle of the spend, by a trigger of the real database) is a release that cannot be shown
// clean: the committed result, no error, one line, the connection closed.
func TestAnUnlockThatFindsNothingToReleaseClosesTheConnectionAndLogsOnce(t *testing.T) {
	w := newWorld(t)
	if _, err := w.admin.Exec(ctx(t), `CREATE FUNCTION spend_drop_locks() RETURNS trigger LANGUAGE plpgsql AS $f$
		BEGIN PERFORM pg_advisory_unlock_all(); RETURN NEW; END $f$;
		CREATE TRIGGER aa_spend_drop_locks AFTER INSERT ON grant_spend FOR EACH ROW EXECUTE FUNCTION spend_drop_locks();`); err != nil {
		t.Fatal(err)
	}
	pool := ownPool(t, w)
	led, sink := ledgerOn(t, w, pool)
	res, err := led.Spend(ctx(t), cmdFor(w.issue(t, 1)))
	if err != nil || !res.Spent {
		t.Fatalf("Spend = %+v, %v; want the committed result and no error", res, err)
	}
	requireReleaseLog(t, sink, 1)
	requireNoConnectionKept(t, pool, "an unlock that found nothing to release")
	requireReleased(t, w, "an unlock that found nothing to release", 5*time.Second, pool)
}

// The spend did NOT commit and the unlock also fails: the spend's own error is returned (with its fixed public text and its own cause), never the lock's; one line is logged and
// the connection is closed.
func TestAnUnlockThatFailsAfterASpendThatDidNotCommitReturnsTheSpendsError(t *testing.T) {
	t.Run("a refusal that is not a serialization failure", func(t *testing.T) {
		w := newWorld(t)
		pool := ownPool(t, w)
		led, sink := ledgerOn(t, w, pool)
		faultOnSpend(t, w, "XX000", 1000, 0)
		revokeUnlock(t, w)
		g := w.issue(t, 1)
		res, err := led.Spend(ctx(t), cmdFor(g))
		if err == nil || res.Spent {
			t.Fatalf("Spend = %+v, %v; want the spend's error", res, err)
		}
		assertFixedText(t, err, "ledger: the spend could not be completed")
		wantPg(t, err, "XX000")
		requireReleaseLog(t, sink, 1)
		requireNoConnectionKept(t, pool, "a failed spend whose unlock failed")
		requireReleased(t, w, "a failed spend whose unlock failed", 5*time.Second, pool)
		if got := w.count(t, `SELECT count(*) FROM decision_grant WHERE grant_id = $1 AND status = 'ISSUED'`, g.GrantID); got != 1 {
			t.Fatal("the grant is not ISSUED after a spend that did not commit")
		}
	})
	t.Run("a serialization failure on every attempt", func(t *testing.T) {
		w := newWorld(t)
		pool := ownPool(t, w)
		led, sink := ledgerOn(t, w, pool)
		faultOnSpend(t, w, "40001", 1000, 0)
		revokeUnlock(t, w)
		res, err := led.Spend(ctx(t), cmdFor(w.issue(t, 1)))
		if res.Spent || !errors.Is(err, controller.ErrSpendRetryable) {
			t.Fatalf("Spend = %+v, %v; want the typed retryable error of the spend", res, err)
		}
		wantPg(t, err, "40001")
		requireReleaseLog(t, sink, 1)
		requireNoConnectionKept(t, pool, "three refused attempts whose unlock failed")
		requireReleased(t, w, "three refused attempts whose unlock failed", 5*time.Second, pool)
	})
	t.Run("a replay", func(t *testing.T) {
		w := newWorld(t)
		pool := ownPool(t, w)
		led, sink := ledgerOn(t, w, pool)
		g := w.issue(t, 1)
		if res, err := led.Spend(ctx(t), cmdFor(g)); err != nil || !res.Spent {
			t.Fatalf("first Spend = %+v, %v", res, err)
		}
		revokeUnlock(t, w)
		res, err := led.Spend(ctx(t), cmdFor(g))
		if err != nil || !res.Replay || res.Spent {
			t.Fatalf("second Spend = %+v, %v; want the replay and no error", res, err)
		}
		requireReleaseLog(t, sink, 1)
		requireNoConnectionKept(t, pool, "a replay whose unlock failed")
	})
}
