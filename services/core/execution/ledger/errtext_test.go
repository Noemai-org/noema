package ledger_test

// noema#748 review, Architect ruling A347 (relayed; interim), AGENT-13, REQ-EXE-001, REQ-EXE-002, INV-03, INV-07: an error that crosses the ledger's public boundary has a FIXED
// PUBLIC TEXT. The tenant's ID and every other identifier (grant, idempotency key, nonce), the database driver's message (SQLSTATE text, server, login and role names) and
// paths are not in Error(); the cause stays reachable through Unwrap, so errors.Is and errors.As (the database's *pgconn.PgError, the context's errors, the sentinels) still
// answer. Messages are no longer what a test asserts on: the sentinel and the cause are. Everything is SYNTHETIC.
//
// Each class of failure of Spend is produced on the real stack: faults raised by the database itself through triggers (the way retry_test.go does it), a backend terminated while
// it works, a pool that is closed, contexts that are expired or cancelled.

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/execution/controller"
	"github.com/Noemai-org/noema/services/core/execution/ledger"
)

// leaks are fragments no public text may carry: the tenant, a grant, a key, a nonce, the database's wording and the connection's details.
var leaks = []string{tenant, "ten_", "grt_", "idem-", "n_ab3", "SQLSTATE", "ERROR", "FATAL", "injected fault", "secret detail", "terminating", "password", "host=", "user=",
	"127.0.0.1", "execution_rw", "noema_", "/tmp", "unix", "dial", "socket", "connection reset", "pgx", "pgconn"}

func assertFixedText(t *testing.T, err error, prefix string, extra ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("no error")
	}
	text := err.Error()
	if !strings.HasPrefix(text, prefix) {
		t.Errorf("the error text %q does not start with %q", text, prefix)
	}
	for _, leak := range append(append([]string{}, leaks...), extra...) {
		if strings.Contains(text, leak) {
			t.Errorf("the error text carries %q: %q", leak, text)
		}
	}
	if len(text) > 200 {
		t.Errorf("the error text is %d bytes: a fixed public text is a short sentence, got %q", len(text), text)
	}
}

func wantPg(t *testing.T, err error, code string) {
	t.Helper()
	var pe *pgconn.PgError
	if !errors.As(err, &pe) || pe.Code != code {
		t.Errorf("the cause is not the database's error with SQLSTATE %s: errors.As = %v, %+v", code, errors.As(err, &pe), pe)
	}
}

// failOn installs a trigger on table that raises sqlstate for the rows that match cond (empty: all). deferred makes it a constraint trigger that fires at the commit. The
// message carries a marker that must never reach a caller.
func failOn(t *testing.T, w *world, table, event, cond, sqlstate string, deferred bool) {
	t.Helper()
	when := ""
	if cond != "" {
		when = " WHEN (" + cond + ")"
	}
	kind := "CREATE TRIGGER aa_text_fault BEFORE " + event + " ON " + table
	if deferred {
		kind = "CREATE CONSTRAINT TRIGGER aa_text_fault AFTER " + event + " ON " + table + " DEFERRABLE INITIALLY DEFERRED"
	}
	ddl := fmt.Sprintf(`CREATE FUNCTION text_fault() RETURNS trigger LANGUAGE plpgsql AS $f$
		BEGIN RAISE EXCEPTION 'injected fault: secret detail' USING ERRCODE = '%s'; END $f$;
		%s FOR EACH ROW%s EXECUTE FUNCTION text_fault();`, sqlstate, kind, when)
	if _, err := w.admin.Exec(ctx(t), ddl); err != nil {
		t.Fatal(err)
	}
}

// ownPool is a pool of the same tenant login as the world's, so that a test can close it, or leak a connection of it, without the world's cleanup waiting on that.
func ownPool(t *testing.T, w *world) *pgxpool.Pool {
	t.Helper()
	p, err := pgxpool.NewWithConfig(ctx(t), w.exec.Config())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		done := make(chan struct{})
		go func() { p.Close(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Log("pool.Close is blocked: a connection of the pool was never returned to it")
		}
	})
	return p
}

// The classes of failure of Spend. Each one: how it is made, what the cause must still answer, and (through assertFixedText) that its text is fixed.
func TestEverySpendFailureHasAFixedPublicTextAndKeepsItsCause(t *testing.T) {
	deadlineCtx := func(d time.Duration) (context.Context, context.CancelFunc) {
		return context.WithTimeout(context.Background(), d)
	}
	cases := []struct {
		name  string
		setup func(t *testing.T, w *world)
		run   func(t *testing.T, w *world, g controller.Grant) error
		cause func(t *testing.T, err error)
	}{
		{
			name:  "serialization failure on every attempt",
			setup: func(t *testing.T, w *world) { faultOnSpend(t, w, "40001", 1000, 0) },
			cause: func(t *testing.T, err error) {
				if !errors.Is(err, controller.ErrSpendRetryable) {
					t.Errorf("the typed retryable error is not matched: %v", err)
				}
				wantPg(t, err, "40001")
			},
		},
		{
			name:  "serialization failure at the commit on every attempt",
			setup: func(t *testing.T, w *world) { failOn(t, w, "grant_spend", "INSERT", "", "40001", true) },
			cause: func(t *testing.T, err error) {
				if !errors.Is(err, controller.ErrSpendRetryable) {
					t.Errorf("the typed retryable error is not matched: %v", err)
				}
				wantPg(t, err, "40001")
			},
		},
		{name: "unique violation", setup: func(t *testing.T, w *world) { faultOnSpend(t, w, "23505", 1000, 0) }, cause: func(t *testing.T, err error) { wantPg(t, err, "23505") }},
		{name: "deadlock", setup: func(t *testing.T, w *world) { faultOnSpend(t, w, "40P01", 1000, 0) }, cause: func(t *testing.T, err error) { wantPg(t, err, "40P01") }},
		{name: "any other fault at the insert", setup: func(t *testing.T, w *world) { faultOnSpend(t, w, "XX000", 1000, 0) }, cause: func(t *testing.T, err error) { wantPg(t, err, "XX000") }},
		{name: "fault at the commit", setup: func(t *testing.T, w *world) { failOn(t, w, "grant_spend", "INSERT", "", "P0001", true) }, cause: func(t *testing.T, err error) { wantPg(t, err, "P0001") }},
		{
			name:  "idempotency record cannot be written",
			setup: func(t *testing.T, w *world) { failOn(t, w, "idempotency_record", "INSERT", "", "P0001", false) },
			cause: func(t *testing.T, err error) { wantPg(t, err, "P0001") },
		},
		{
			name:  "grant cannot be marked spent",
			setup: func(t *testing.T, w *world) { failOn(t, w, "decision_grant", "UPDATE", "", "P0001", false) },
			cause: func(t *testing.T, err error) { wantPg(t, err, "P0001") },
		},
		{
			name: "the Record event cannot be appended",
			setup: func(t *testing.T, w *world) {
				failOn(t, w, "record_event", "INSERT", "NEW.event_type = 'GRANT_SPENT'", "P0001", false)
			},
			cause: func(t *testing.T, err error) { wantPg(t, err, "P0001") },
		},
		{
			name: "the grant cannot be read",
			setup: func(t *testing.T, w *world) {
				if _, err := w.admin.Exec(ctx(t), `REVOKE SELECT ON decision_grant FROM execution_rw`); err != nil {
					t.Fatal(err)
				}
			},
			cause: func(t *testing.T, err error) { wantPg(t, err, "42501") },
		},
		{
			name: "the kill switch cannot be read",
			setup: func(t *testing.T, w *world) {
				if _, err := w.admin.Exec(ctx(t), `REVOKE SELECT ON tenant FROM execution_rw`); err != nil {
					t.Fatal(err)
				}
			},
			cause: func(t *testing.T, err error) { wantPg(t, err, "42501") },
		},
		{
			name: "the grant does not exist",
			run: func(t *testing.T, w *world, g controller.Grant) error {
				g.GrantID = "grt_9999"
				_, err := w.led.Spend(ctx(t), cmdFor(g))
				return err
			},
			cause: func(t *testing.T, err error) {},
		},
		{
			name: "the nonce differs from the stored grant",
			run: func(t *testing.T, w *world, g controller.Grant) error {
				g.Nonce = "n_ab39999"
				_, err := w.led.Spend(ctx(t), cmdFor(g))
				return err
			},
			cause: func(t *testing.T, err error) {},
		},
		{
			name: "the backend is terminated while the transaction runs",
			setup: func(t *testing.T, w *world) {
				slowSpends(t, w, 5000)
			},
			run: func(t *testing.T, w *world, g controller.Grant) error {
				done := make(chan error, 1)
				go func() { _, err := w.led.Spend(ctx(t), cmdFor(g)); done <- err }()
				waitFor(t, "the spend to be inside its slow insert", func() bool {
					return w.count(t, `SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND state = 'active' AND query LIKE 'INSERT INTO grant_spend%'`) == 1
				})
				if _, err := w.admin.Exec(ctx(t), `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = current_database() AND query LIKE 'INSERT INTO grant_spend%'`); err != nil {
					t.Fatal(err)
				}
				select {
				case err := <-done:
					return err
				case <-time.After(20 * time.Second):
					t.Fatal("the spend did not return after its backend was terminated")
					return nil
				}
			},
			cause: func(t *testing.T, err error) { wantPg(t, err, "57P01") },
		},
		{
			name: "the pool is closed",
			run: func(t *testing.T, w *world, g controller.Grant) error {
				led := *w.led
				led.Pool = ownPool(t, w)
				led.Pool.Close()
				_, err := led.Spend(ctx(t), cmdFor(g))
				return err
			},
			cause: func(t *testing.T, err error) {
				if errors.Unwrap(err) == nil {
					t.Error("the cause of the closed pool is not kept")
				}
			},
		},
		{
			name: "the context has expired",
			run: func(t *testing.T, w *world, g controller.Grant) error {
				c, cancel := deadlineCtx(time.Nanosecond)
				defer cancel()
				<-c.Done()
				_, err := w.led.Spend(c, cmdFor(g))
				return err
			},
			cause: func(t *testing.T, err error) {
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Errorf("the deadline is not the cause: %v", err)
				}
			},
		},
		{
			name: "the context was cancelled",
			run: func(t *testing.T, w *world, g controller.Grant) error {
				c, cancel := deadlineCtx(time.Minute)
				cancel()
				_, err := w.led.Spend(c, cmdFor(g))
				return err
			},
			cause: func(t *testing.T, err error) {
				if !errors.Is(err, context.Canceled) {
					t.Errorf("the cancellation is not the cause: %v", err)
				}
			},
		},
		{
			name:  "the context expires while the transaction runs",
			setup: func(t *testing.T, w *world) { slowSpends(t, w, 5000) },
			run: func(t *testing.T, w *world, g controller.Grant) error {
				c, cancel := deadlineCtx(400 * time.Millisecond)
				defer cancel()
				_, err := w.led.Spend(c, cmdFor(g))
				return err
			},
			cause: func(t *testing.T, err error) {
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Errorf("the deadline is not the cause: %v", err)
				}
			},
		},
		{
			name: "the context has no deadline",
			run: func(t *testing.T, w *world, g controller.Grant) error {
				_, err := w.led.Spend(context.Background(), cmdFor(g))
				return err
			},
			cause: func(t *testing.T, err error) {},
		},
		{
			name: "the ledger has no rollback bound",
			run: func(t *testing.T, w *world, g controller.Grant) error {
				led := *w.led
				led.RollbackTimeout = 0
				_, err := led.Spend(ctx(t), cmdFor(g))
				return err
			},
			cause: func(t *testing.T, err error) {},
		},
		{
			name: "the ledger has no serialization attempts",
			run: func(t *testing.T, w *world, g controller.Grant) error {
				led := *w.led
				led.SerializationAttempts = 0
				_, err := led.Spend(ctx(t), cmdFor(g))
				return err
			},
			cause: func(t *testing.T, err error) {},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			w.led.SerializationAttempts = 3
			g := w.issue(t, 1)
			if tc.setup != nil {
				tc.setup(t, w)
			}
			var err error
			if tc.run != nil {
				err = tc.run(t, w, g)
			} else {
				_, err = w.led.Spend(ctx(t), cmdFor(g))
			}
			assertFixedText(t, err, "ledger: ")
			tc.cause(t, err)
		})
	}
}

// An idempotency key reused for other parameters is the sentinel the controller voids the grant on. It keeps its identity and its short text; and when the security event that
// records the collision cannot itself be written, the answer is still the sentinel, with a fixed text, the database's error kept as a cause.
func TestAKeyCollisionKeepsItsSentinelAndItsEventFailureHasAFixedText(t *testing.T) {
	collide := func(t *testing.T, w *world) controller.SpendCommand {
		t.Helper()
		g1 := w.issue(t, 1)
		if res, err := w.led.Spend(ctx(t), cmdFor(g1)); err != nil || !res.Spent {
			t.Fatalf("setup: %+v, %v", res, err)
		}
		g2 := w.issue(t, 2, func(g *controller.Grant) { g.ParamsHash = "sha256:" + strings.Repeat("a", 64) })
		return cmdFor(g2)
	}
	t.Run("event recorded", func(t *testing.T) {
		w := newWorld(t)
		_, err := w.led.Spend(ctx(t), collide(t, w))
		if !errors.Is(err, controller.ErrKeyCollision) {
			t.Fatalf("Spend error = %v; want controller.ErrKeyCollision", err)
		}
		assertFixedText(t, err, "")
	})
	t.Run("event not recorded", func(t *testing.T) {
		w := newWorld(t)
		cmd := collide(t, w)
		failOn(t, w, "record_event", "INSERT", "NEW.event_type = 'SECURITY_EVENT'", "P0001", false)
		_, err := w.led.Spend(ctx(t), cmd)
		if !errors.Is(err, controller.ErrKeyCollision) {
			t.Fatalf("Spend error = %v; want controller.ErrKeyCollision even when the event cannot be written", err)
		}
		wantPg(t, err, "P0001")
		assertFixedText(t, err, "ledger: ")
	})
}

// Through the controller: its wrapping adds a prefix and no identifier, so the whole text a caller of Execute sees is fixed, and the typed error and the database's error
// are still matched.
func TestTheControllersWrappingOfASpendFailureAddsNoIdentifier(t *testing.T) {
	for _, tc := range []struct {
		name, sqlstate string
		retryable      bool
	}{{"serialization failure", "40001", true}, {"other fault", "XX000", false}} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			w.led.SerializationAttempts = 2
			faultOnSpend(t, w, tc.sqlstate, 1000, 0)
			g := w.issue(t, 1)
			_, err := w.ctl.Execute(ctx(t), tenant, g.GrantID)
			assertFixedText(t, err, "controller: spend: ledger: ")
			wantPg(t, err, tc.sqlstate)
			if errors.Is(err, controller.ErrSpendRetryable) != tc.retryable {
				t.Errorf("errors.Is(err, ErrSpendRetryable) = %v, want %v", !tc.retryable, tc.retryable)
			}
		})
	}
}

// The setting's name is in the error of ParseSerializationAttempts (a setting name is not a secret); its value is not, whatever it is: a wrong value pasted into the wrong
// variable may be a secret. The strconv error of an out-of-range value, which quotes the value, is kept as the cause.
func TestTheSerializationAttemptsErrorsNameTheSettingAndNotItsValue(t *testing.T) {
	for _, raw := range []string{"hunter2-secret-token", "-7", "3.5", "99999999999999999999999", "0x3", " 3"} {
		t.Run(raw, func(t *testing.T) {
			_, err := ledger.ParseSerializationAttempts(raw)
			if err == nil || !strings.Contains(err.Error(), settingName) {
				t.Fatalf("ParseSerializationAttempts(%q) = %v; want an error that names %s", raw, err, settingName)
			}
			if strings.Contains(err.Error(), strings.TrimSpace(raw)) {
				t.Errorf("the error text carries the value %q: %q", raw, err.Error())
			}
		})
	}
	_, err := ledger.ParseSerializationAttempts("99999999999999999999999")
	if !errors.Is(err, strconv.ErrRange) {
		t.Errorf("the out-of-range cause is not kept: %v", err)
	}
}
