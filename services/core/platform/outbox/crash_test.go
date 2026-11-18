//go:build !windows

package outbox_test

// PLT-08 done-when: "An event commits with its state change or not at all (crash test)". REQ-VER-005, PERD §56, EC-E16, INV-11, INV-30, AGENT-08.
//
// A real process death, as TST-05 does it (adapters/outcome_e2e/crash_test.go; ruling A30: a Go test with real process death and no new CI job): the test binary runs
// itself again (TestCrashChild) as a child that connects to the parent's database through the tenant's own logins, does its work up to a named point, tells the parent it is
// there, and waits. The parent looks at the database while the child is at the point, then kills it with SIGKILL, so no deferred function runs and PostgreSQL rolls
// back what is open by itself. No production code carries a crash hook: the point is a line of test code (the state change and the event are written by the real
// Enqueue; the delivery is the real dispatcher with a handler that stops after it delivered). The parent never sleeps or polls: it reads the child's marker line, and it
// learns that a transaction has ended by a statement that waits for its row lock.
//
// Linux only. All data is SYNTHETIC (AGENT-25).

import (
	"bufio"
	"context"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/outbox"
)

const (
	envMode   = "NOEMA_PLT08_CHILD"
	envDSN    = "NOEMA_PLT08_DSN"
	envFile   = "NOEMA_PLT08_FILE"
	markReady = "NOEMA_PLT08_READY"
)

// childPool connects as the tenant's own login for a power, as testpg.TenantLogin names it: <group>__<tenant>_<database>.
func childPool(t *testing.T, dsn, group string) *pgxpool.Pool {
	t.Helper()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	u.User = url.User(group + "__" + tenantA + "_" + strings.TrimPrefix(u.Path, "/"))
	c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(c, u.String())
	if err != nil {
		t.Fatal(err)
	}
	return pool
}

// ready tells the parent that the child is at its point, then waits for the kill. It blocks on a read of the pipe the parent holds open, so a parent that ended without
// killing it closes the pipe and the child ends too.
func ready() {
	_, _ = os.Stdout.WriteString(markReady + "\n")
	_, _ = os.Stdin.Read(make([]byte, 1))
}

// TestCrashChild is the helper process of the tests below. Run by hand or in an ordinary run it does nothing; the parent fails if the child did not die by SIGKILL.
func TestCrashChild(t *testing.T) {
	mode := os.Getenv(envMode)
	if mode == "" {
		t.Skip("helper process of the crash tests (they run it)")
	}
	dsn, file := os.Getenv(envDSN), os.Getenv(envFile)
	c, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	now := clock.System{}.Now()
	switch mode {
	case "state-and-event-uncommitted", "state-and-event-committed":
		pool := childPool(t, dsn, "execution_rw")
		tx, err := pool.Begin(c)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(c, `INSERT INTO plt08_state (tenant_id, id, v) VALUES ($1, 'st_crash', 'ok')`, tenantA); err != nil {
			t.Fatal(err)
		}
		if err := outbox.Enqueue(c, tx, outbox.Event{OutboxID: oid(1), TenantID: tenantA, Topic: topicV, Payload: payload(1), CreatedAt: now}); err != nil {
			t.Fatal(err)
		}
		if mode == "state-and-event-committed" {
			if err := tx.Commit(c); err != nil {
				t.Fatal(err)
			}
		}
		ready() // the transaction is open (or just committed); the parent kills the process here
	case "deliver-and-stop-on-the-second":
		pool := childPool(t, dsn, "orchestrator_rw")
		delivered := 0
		h := outbox.HandlerFunc(func(hc context.Context, d outbox.Delivery) error {
			f, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
			if err != nil {
				return err
			}
			defer func() { _ = f.Close() }()
			if _, err := f.WriteString(d.IdempotencyKey() + "\n"); err != nil {
				return err
			}
			if err := f.Sync(); err != nil { // the delivery is real before the process dies
				return err
			}
			delivered++
			if delivered == 2 {
				ready() // delivered, not yet marked: the dispatcher's transaction for this event is open
			}
			return nil
		})
		d, err := outbox.New(pool, clock.System{}, outbox.Config{TenantID: tenantA, Handlers: map[string]outbox.Handler{topicV: h}, MaxEvents: 10, DeliveryTimeout: 55 * time.Second})
		if err != nil {
			t.Fatal(err)
		}
		_, _ = d.RunOnce(c)
	default:
		t.Fatalf("unknown mode %q", mode)
	}
}

// startChild runs the child in a mode and returns once it has printed its marker. The caller kills it with kill.
func startChild(t *testing.T, w *world, mode, file string) *exec.Cmd {
	t.Helper()
	cmd := exec.CommandContext(ctx(t), os.Args[0], "-test.run=^TestCrashChild$", "-test.count=1")
	cmd.Env = append(os.Environ(), envMode+"="+mode, envDSN+"="+w.dsn, envFile+"="+file)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cmd.StdinPipe(); err != nil { // held open until the child ends: the child waits on it
		t.Fatal(err)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	var seen []string
	sc := bufio.NewScanner(out)
	for sc.Scan() {
		if sc.Text() == markReady {
			return cmd
		}
		seen = append(seen, sc.Text())
	}
	_ = cmd.Wait()
	t.Fatalf("the child ended before it reached its point (%v):\n%s\n%s", sc.Err(), strings.Join(seen, "\n"), stderr.String())
	return nil
}

// kill ends the child with SIGKILL and requires that it died of it.
func kill(t *testing.T, cmd *exec.Cmd) {
	t.Helper()
	if err := cmd.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	err := cmd.Wait()
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("the child ended with %v, want a death by SIGKILL", err)
	}
	if ws, ok := ee.Sys().(syscall.WaitStatus); !ok || !ws.Signaled() || ws.Signal() != syscall.SIGKILL {
		t.Fatalf("the child did not die by SIGKILL: %v", err)
	}
}

// openTx counts the open transactions of a tenant's login: a session that is idle in transaction.
func (w *world) openTx(group string) int {
	w.t.Helper()
	return w.count(`SELECT count(*) FROM pg_stat_activity WHERE usename LIKE $1 AND state = 'idle in transaction'`, group+"\\_\\_"+tenantA+"\\_%")
}

// lockAndCount is a statement that blocks until the transaction that holds the row's lock has ended, and returns how many rows it then holds. It is how the parent learns
// that the killed child's transaction is over without sleeping: PostgreSQL ends it by itself when the connection dies.
func (w *world) lockAndCount(q string, args ...any) int {
	w.t.Helper()
	c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tx, err := w.admin.Begin(c)
	if err != nil {
		w.t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var n int
	if err := tx.QueryRow(c, q, args...).Scan(&n); err != nil {
		w.t.Fatalf("%s: %v", q, err)
	}
	return n
}

// The done-when. A kill after the state change and the event were written and before COMMIT leaves neither: the state change and its event are one transaction.
func TestAKillBeforeCommitLeavesNeitherTheStateChangeNorItsEvent(t *testing.T) {
	w := newWorld(t)
	cmd := startChild(t, w, "state-and-event-uncommitted", "")
	// The child is inside its transaction, after both writes: its session is idle in transaction, and a statement that needs the same keys has to wait for it.
	if n := w.openTx("execution_rw"); n != 1 {
		t.Fatalf("%d open transactions of the execution login at the point, want 1 (the point is not inside the transaction)", n)
	}
	if s, e := w.count(`SELECT count(*) FROM plt08_state`), w.count(`SELECT count(*) FROM outbox_event`); s != 0 || e != 0 {
		t.Fatalf("before the kill, an uncommitted %d state rows and %d events are visible", s, e)
	}
	kill(t, cmd)
	// A reader that takes the same keys waits for the dead transaction to end, then finds them free: nothing committed.
	if n := w.lockAndCount(`WITH s AS (INSERT INTO plt08_state (tenant_id, id, v) VALUES ($1, 'st_crash', 'ok') RETURNING 1),
		e AS (INSERT INTO outbox_event (outbox_id, tenant_id, topic, payload, created_at) VALUES ($2, $1, $3, '{}', now()) RETURNING 1)
		SELECT (SELECT count(*) FROM s) + (SELECT count(*) FROM e)`, tenantA, oid(1), topicV); n != 2 {
		t.Fatalf("the keys were not free after the kill (%d inserted): something of the transaction committed", n)
	}
	if s, e := w.count(`SELECT count(*) FROM plt08_state`), w.count(`SELECT count(*) FROM outbox_event`); s != 0 || e != 0 {
		t.Fatalf("after the kill: %d state rows and %d events, want none", s, e)
	}
}

// A kill after COMMIT keeps both: the event is there for the dispatcher, once.
func TestAKillAfterCommitKeepsTheStateChangeAndItsEvent(t *testing.T) {
	w := newWorld(t)
	cmd := startChild(t, w, "state-and-event-committed", "")
	kill(t, cmd)
	if s, e := w.count(`SELECT count(*) FROM plt08_state`), w.count(`SELECT count(*) FROM outbox_event WHERE dispatched_at IS NULL`); s != 1 || e != 1 {
		t.Fatalf("after the kill: %d state rows and %d pending events, want one each", s, e)
	}
	rec := &recorder{}
	if _, err := w.dispatcher(w.orchA, tenantA, rec, 10).RunOnce(ctx(t)); err != nil || len(rec.ids()) != 1 || rec.ids()[0] != oid(1) {
		t.Fatalf("the survivor's delivery: %v, %v", rec.ids(), err)
	}
}

// A dispatcher that dies after it delivered and before it marked: the event is still pending, the earlier mark of the same pass stands, and the next dispatcher delivers the
// event again with the same key (at least once, never lost), then marks it. A third pass delivers nothing.
func TestAKillAfterDeliveryAndBeforeTheMarkDeliversTheEventAgain(t *testing.T) {
	w := newWorld(t)
	for n := 1; n <= 3; n++ {
		w.enqueue(w.execA, tenantA, n, topicV)
	}
	file := t.TempDir() + "/delivered"
	cmd := startChild(t, w, "deliver-and-stop-on-the-second", file)
	// At the point: event 1 is delivered and marked, event 2 is delivered and held by the open transaction, event 3 is untouched.
	if got := readLines(t, file); strings.Join(got, ",") != oid(1)+","+oid(2) {
		t.Fatalf("the child delivered %v, want events 1 and 2", got)
	}
	if got := w.pending(tenantA); strings.Join(got, ",") != oid(2)+","+oid(3) {
		t.Fatalf("at the point pending = %v, want events 2 and 3 (event 1 is marked, event 2 is delivered and not marked)", got)
	}
	if n := w.openTx("orchestrator_rw"); n != 1 {
		t.Fatalf("%d open transactions of the dispatcher's login at the point, want 1", n)
	}
	kill(t, cmd)
	// The statement waits for the dead dispatcher's lock on event 2 to end, then counts the pending events.
	if n := w.lockAndCount(`SELECT count(*) FROM (SELECT 1 FROM outbox_event WHERE outbox_id = $1 AND dispatched_at IS NULL FOR UPDATE) s`, oid(2)); n != 1 {
		t.Fatalf("event 2 is not pending after the kill (%d): a death between delivery and mark lost the event", n)
	}
	if got := w.pending(tenantA); strings.Join(got, ",") != oid(2)+","+oid(3) {
		t.Fatalf("after the kill pending = %v, want events 2 and 3", got)
	}
	rec := &recorder{}
	res, err := w.dispatcher(w.orchA, tenantA, rec, 10).RunOnce(ctx(t))
	if err != nil || strings.Join(rec.ids(), ",") != oid(2)+","+oid(3) || len(res.Delivered) != 2 {
		t.Fatalf("the new dispatcher: %v, %v, %+v; want events 2 and 3", err, rec.ids(), res)
	}
	all := append(readLines(t, file), rec.ids()...)
	if strings.Join(all, ",") != strings.Join([]string{oid(1), oid(2), oid(2), oid(3)}, ",") {
		t.Fatalf("all deliveries %v: event 2 must be delivered twice with the same key, the others once", all)
	}
	if left := w.pending(tenantA); len(left) != 0 {
		t.Fatalf("still pending: %v", left)
	}
	if _, err := w.dispatcher(w.orchA, tenantA, rec, 10).RunOnce(ctx(t)); err != nil || len(rec.ids()) != 2 {
		t.Fatalf("a third pass delivered again: %v, %v", err, rec.ids())
	}
}

func readLines(t *testing.T, file string) []string {
	t.Helper()
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Fields(string(b))
}
