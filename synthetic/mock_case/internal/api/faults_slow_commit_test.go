package api_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// holdInserts blocks every insert into cases (reads still run) until the returned release function is called, by
// holding a real PostgreSQL table lock from a separate connection. It is how a test makes the commit slow without
// replacing anything: the command is genuinely waiting in the database when the caller gives up.
func (e *env) holdInserts() (release func()) {
	e.t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, e.db.AdminDSN)
	if err != nil {
		e.t.Fatalf("connect to hold the lock: %v", err)
	}
	if _, err := conn.Exec(ctx, `BEGIN`); err != nil {
		e.t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `LOCK TABLE cases IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		e.t.Fatalf("lock cases: %v", err)
	}
	released := false
	release = func() {
		if released {
			return
		}
		released = true
		if _, err := conn.Exec(ctx, `COMMIT`); err != nil {
			e.t.Errorf("release the lock: %v", err)
		}
		if err := conn.Close(ctx); err != nil {
			e.t.Logf("close the lock connection: %v", err)
		}
	}
	e.t.Cleanup(release)
	return release
}

// waitForCount polls the observable state until the number of objects of a kind is want, or fails after within.
// It waits only on what the truth endpoint reports; it never assumes a commit and never returns early on a miss.
func (e *env) waitForCount(kind string, want int, within time.Duration, why string) {
	e.t.Helper()
	deadline := time.Now().Add(within)
	for {
		n := e.count(kind)
		if n == want {
			return
		}
		if time.Now().After(deadline) {
			e.t.Fatalf("%s: want %d %s, have %d after %v", why, want, kind, n, within)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// REQ-ENV-008, REQ-ENV-004, US-06, EC-E02, EC-T09 (AGENT-23: regression case for the flaky
// TestTimeoutAfterCommitHoldsThenDrops on main, run 41226045727). Every effect whose contract is "the command
// commits and only the answer is affected" must commit even when the caller gives up first. Here the commit is made
// slower than the caller's timeout, so the caller has gone while the command is still in the database. The case must
// be committed once the database lets it through, and the retry must find it.
func TestAfterCommitEffectsCommitWhenTheCallerGivesUpBeforeTheCommit(t *testing.T) {
	for _, c := range []struct{ effect, params string }{
		{"TIMEOUT_AFTER_COMMIT", `{"delay_ms":1000}`},
		{"DROP_RESPONSE", `{}`},
		{"NEAR_TIMEOUT_AFTER", `{"delay_ms":1000}`},
		{"HTTP_5XX", `{"status":502,"after_commit":true}`},
	} {
		t.Run(c.effect, func(t *testing.T) {
			e := newEnv(t)
			e.loadPlan(plan(fault("create_case", `{"nth_call":1}`, c.effect, c.params), ""))
			release := e.holdInserts()
			start := time.Now()
			if _, err := e.createWith(impatient(100*time.Millisecond), "idk_1"); err == nil {
				t.Fatal("the client must time out: the insert is held by the database")
			}
			if took := time.Since(start); took > 800*time.Millisecond {
				t.Fatalf("the client gave up at its own timeout, not after %v", took)
			}
			if n := e.count("cases"); n != 0 {
				t.Fatalf("the insert is still held by the database lock: %d cases", n)
			}
			release()
			e.waitForCount("cases", 1, 5*time.Second, "the case must be committed although the caller gave up first")
			if retry := e.createCase("idk_1"); retry.status != 200 || retry.header.Get("Idempotent-Replay") != "true" {
				t.Fatalf("the retry must find the committed case: %d %s", retry.status, retry.raw)
			}
			if e.count("cases") != 1 {
				t.Fatal("exactly one effect")
			}
		})
	}
}
