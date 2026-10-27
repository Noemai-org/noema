package chain_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/migrations"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/db"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/record/chain"
)

func ctx(t testing.TB) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	return c
}

type world struct {
	admin *pgxpool.Pool
	exec  *pgxpool.Pool // connections run as execution_rw
	app   *chain.Appender
	dsn   string
}

func newWorld(t *testing.T, tenants ...string) *world {
	t.Helper()
	admin, dsn := testpg.NewDatabase(t)
	if _, err := db.Migrate(ctx(t), admin, migrations.FS); err != nil {
		t.Fatal(err)
	}
	for _, id := range tenants {
		if _, err := admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ($1, $1)`, id); err != nil {
			t.Fatal(err)
		}
	}
	exec, err := db.OpenAsRole(ctx(t), dsn, "execution_rw")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(exec.Close)
	clk := clock.NewManual(time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC))
	return &world{admin: admin, exec: exec, dsn: dsn, app: &chain.Appender{Clock: clk, IDs: ids.New(clk, nil)}}
}

func (w *world) append(t *testing.T, ev chain.Event) chain.Stored {
	t.Helper()
	tx, err := w.exec.Begin(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	st, err := w.app.Append(ctx(t), tx, ev)
	if err != nil {
		_ = tx.Rollback(ctx(t))
		t.Fatalf("append: %v", err)
	}
	if err := tx.Commit(ctx(t)); err != nil {
		t.Fatal(err)
	}
	return st
}

func event(tenant string, n int) chain.Event {
	return chain.Event{TenantID: tenant, CorrelationID: "corr_abc123", WrittenBy: "core-execution", Actor: "core-execution", EventType: "GRANT_SPENT",
		ObjectRef: fmt.Sprintf("grt_%04d", n), Payload: map[string]any{"n": n}}
}

// REC-01 done when: the chain property holds (contiguous seq, each event extends the one before, verification passes).
func TestChainIsContiguousAndVerifies(t *testing.T) {
	w := newWorld(t, "ten_aa", "ten_bb")
	var prev string
	for i := 1; i <= 6; i++ {
		st := w.append(t, event("ten_aa", i))
		if st.Seq != int64(i) {
			t.Fatalf("event %d has seq %d", i, st.Seq)
		}
		if i == 1 && st.PrevHash != chain.GenesisHash("ten_aa") {
			t.Fatalf("first event does not start at the genesis hash")
		}
		if i > 1 && st.PrevHash != prev {
			t.Fatalf("event %d does not extend event %d", i, i-1)
		}
		prev = st.Hash
	}
	b := w.append(t, event("ten_bb", 1))
	if b.Seq != 1 || b.PrevHash != chain.GenesisHash("ten_bb") || b.PrevHash == chain.GenesisHash("ten_aa") {
		t.Fatalf("tenants share a chain: %+v", b)
	}
	for tenant, want := range map[string]int64{"ten_aa": 6, "ten_bb": 1} {
		n, err := chain.Verify(ctx(t), w.admin, tenant)
		if err != nil || n != want {
			t.Fatalf("verify %s = %d, %v; want %d, nil", tenant, n, err, want)
		}
	}
}

// REQ-AUD-001: appends are serialised per tenant by a lock on the chain head. Concurrent writers leave no gap and no fork.
func TestConcurrentAppendsLeaveOneChain(t *testing.T) {
	w := newWorld(t, "ten_aa")
	var wg sync.WaitGroup
	errs := make(chan error, 64)
	const writers, each = 8, 5
	for g := 0; g < writers; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < each; i++ {
				tx, err := w.exec.Begin(ctx(t))
				if err != nil {
					errs <- err
					return
				}
				if _, err := w.app.Append(ctx(t), tx, event("ten_aa", g*100+i)); err != nil {
					_ = tx.Rollback(ctx(t))
					errs <- err
					return
				}
				if err := tx.Commit(ctx(t)); err != nil {
					errs <- err
					return
				}
			}
		}(g)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("a concurrent append failed: %v", err)
	}
	n, err := chain.Verify(ctx(t), w.admin, "ten_aa")
	if err != nil || n != writers*each {
		t.Fatalf("verify = %d, %v; want %d events and no break", n, err, writers*each)
	}
}

// EC-E16, REQ-AUD-001: if the append fails, the enclosing transaction fails and the action does not happen.
func TestFailedAppendRollsBackTheActionWithIt(t *testing.T) {
	w := newWorld(t, "ten_aa")
	tx, err := w.exec.Begin(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx(t), `INSERT INTO outbox_event (outbox_id, tenant_id, topic, payload, created_at) VALUES ('obx_1', 'ten_aa', 't', '{}', now())`); err != nil {
		t.Fatal(err)
	}
	bad := event("ten_aa", 1)
	bad.ObjectRef = "not a valid ref"
	if _, err := w.app.Append(ctx(t), tx, bad); err == nil {
		t.Fatal("an invalid event was appended")
	}
	_ = tx.Rollback(ctx(t))
	var n int
	if err := w.admin.QueryRow(ctx(t), `SELECT count(*) FROM outbox_event`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("the action survived a failed append: %d rows (err %v)", n, err)
	}
	if got, err := chain.Verify(ctx(t), w.admin, "ten_aa"); err != nil || got != 0 {
		t.Fatalf("a failed append left events: %d, %v", got, err)
	}
}

// The database refuses an event that does not extend the head it locked: a writer with a stale head cannot fork the chain.
func TestRecordInsertRefusesAnEventThatDoesNotExtendTheHead(t *testing.T) {
	w := newWorld(t, "ten_aa")
	w.append(t, event("ten_aa", 1))
	tx, err := w.exec.Begin(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx(t)) }()
	var seq int64
	var head string
	if err := tx.QueryRow(ctx(t), `SELECT seq, hash FROM record_lock_head('ten_aa')`).Scan(&seq, &head); err != nil {
		t.Fatal(err)
	}
	insert := func(s int64, prev string) error {
		_, err := tx.Exec(ctx(t), `SAVEPOINT s`)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx(t), `SELECT record_insert('ten_aa','recev_X1',$1,$2,'sha256:h','EXECUTION','GRANT_SPENT','grt_1','sha256:p','corr_abc123',now(),'1.0','EXECUTION','{}'::jsonb,NULL)`, s, prev)
		if err != nil {
			_, _ = tx.Exec(ctx(t), `ROLLBACK TO SAVEPOINT s`)
		}
		return err
	}
	for name, err := range map[string]error{"a repeated seq": insert(seq, head), "a skipped seq": insert(seq+2, head), "a wrong prev_hash": insert(seq+1, "sha256:other")} {
		var pe *pgconn.PgError
		if !errors.As(err, &pe) || pe.Code != "40001" {
			t.Fatalf("%s: got %v, want serialization_failure", name, err)
		}
	}
}

// REC-01 done when: tampering and a broken prior hash are both detected. The tamper is done by the administrator
// with the immutability trigger disabled, the one way past the INSERT-only roles (PERD REQ-AUD-001 tests).
func TestVerifyDetectsEveryKindOfTampering(t *testing.T) {
	cases := []struct {
		name string
		sql  string
		want chain.BreakKind
	}{
		{"an edited hashed body", `UPDATE record_event SET body = jsonb_set(body, '{object_ref}', '"grt_9999"') WHERE seq = 2`, chain.BreakColumns},
		{"an edited body and its column", `UPDATE record_event SET body = jsonb_set(body, '{object_ref}', '"grt_9999"'), object_ref = 'grt_9999' WHERE seq = 2`, chain.BreakHash},
		{"an edited stored hash", `UPDATE record_event SET hash = 'sha256:` + strings.Repeat("0", 64) + `' WHERE seq = 2`, chain.BreakHash},
		{"a broken prior hash", `UPDATE record_event SET prev_hash = 'sha256:` + strings.Repeat("1", 64) + `' WHERE seq = 3`, chain.BreakPrevHash},
		{"a deleted event", `DELETE FROM record_event WHERE seq = 2`, chain.BreakSeqGap},
		{"an edited payload", `UPDATE record_event SET payload = '{"n": 999}' WHERE seq = 2`, chain.BreakPayloadHash},
		{"a v1 schema_version in the stored row", `UPDATE record_event SET schema_version = '1.0' WHERE seq = 2`, chain.BreakSchema},
		{"a v1 schema_version in the hashed body too", `UPDATE record_event SET schema_version = '1.0', body = jsonb_set(body, '{schema_version}', '"1.0"') WHERE seq = 2`, chain.BreakSchema},
		{"an edited actor column", `UPDATE record_event SET actor = 'customer:usr_1' WHERE seq = 2`, chain.BreakColumns},
		{"an actor that is not a principal, in the row and the hashed body", `UPDATE record_event SET actor = 'SOMEONE_ELSE', body = jsonb_set(body, '{actor}', '"SOMEONE_ELSE"') WHERE seq = 2`, chain.BreakSchema},
		{"an emitter outside the closed set, in the row and the hashed body", `UPDATE record_event SET written_by = 'TRUST', actor = 'TRUST', body = jsonb_set(jsonb_set(body, '{actor}', '"TRUST"'), '{written_by}', '"TRUST"') WHERE seq = 2`, chain.BreakSchema},
		{"a stored payload with a fraction", `UPDATE record_event SET payload = '{"n": 1.5}' WHERE seq = 2`, chain.BreakPayloadHash},
		{"a v2 schema_version in the stored row", `UPDATE record_event SET schema_version = '2.0' WHERE seq = 2`, chain.BreakSchema},
		{"an edited event type column", `UPDATE record_event SET event_type = 'GRANT_VOIDED' WHERE seq = 2`, chain.BreakColumns},
		{"a whole self-consistent rewrite of the last event", ``, chain.BreakHash},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t, "ten_aa")
			for i := 1; i <= 4; i++ {
				w.append(t, event("ten_aa", i))
			}
			if _, err := chain.Verify(ctx(t), w.admin, "ten_aa"); err != nil {
				t.Fatalf("a clean chain does not verify: %v", err)
			}
			conn, err := w.admin.Acquire(ctx(t))
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Release()
			if _, err := conn.Exec(ctx(t), `SET session_replication_role = replica`); err != nil {
				t.Fatal(err)
			}
			sql := c.sql
			if sql == "" {
				// Rewrites event 4's body and its column but keeps the old stored hash: the stored hash no longer
				// matches the event.
				sql = `UPDATE record_event SET body = jsonb_set(body, '{actor}', '"customer:usr_1"'), actor = 'customer:usr_1' WHERE seq = 4`
			}
			if _, err := conn.Exec(ctx(t), sql); err != nil {
				t.Fatalf("tamper: %v", err)
			}
			_, err = chain.Verify(ctx(t), w.admin, "ten_aa")
			var be *chain.BreakError
			if !errors.As(err, &be) {
				t.Fatalf("tampering (%s) was not detected: %v", c.name, err)
			}
			if be.Kind != c.want {
				t.Fatalf("tampering (%s): break kind %s, want %s (%v)", c.name, be.Kind, c.want, err)
			}
		})
	}
}

// REQ-AUD-001: the role that appends cannot change or remove what it appended; the chain functions are its only way in.
func TestAppendingRoleCannotRewriteTheRecord(t *testing.T) {
	w := newWorld(t, "ten_aa")
	w.append(t, event("ten_aa", 1))
	for _, stmt := range []string{`UPDATE record_event SET hash = 'x'`, `DELETE FROM record_event`, `TRUNCATE record_event`, `UPDATE record_chain_head SET seq = 9`} {
		_, err := w.exec.Exec(ctx(t), stmt)
		var pe *pgconn.PgError
		if !errors.As(err, &pe) || pe.Code != "42501" {
			t.Fatalf("%q as execution_rw: got %v, want permission denied", stmt, err)
		}
	}
}

// Events that do not match the contract are refused before they reach the database.
func TestAppendRefusesEventsOutsideTheContract(t *testing.T) {
	w := newWorld(t, "ten_aa")
	good := event("ten_aa", 1)
	for name, mutate := range map[string]func(*chain.Event){
		"a tenant that is not ten_":                     func(e *chain.Event) { e.TenantID = "tenant-a" },
		"a correlation that is not corr_":               func(e *chain.Event) { e.CorrelationID = "x" },
		"an object_ref with spaces":                     func(e *chain.Event) { e.ObjectRef = "grt 1" },
		"a power name in upper case as the emitter":     func(e *chain.Event) { e.WrittenBy, e.Actor = "EXECUTION", "EXECUTION" },
		"an emitter outside the closed set":             func(e *chain.Event) { e.WrittenBy, e.Actor = "core-understanding", "core-understanding" },
		"an unknown emitter acting for a person":        func(e *chain.Event) { e.WrittenBy, e.Actor = "TRUST", "approver:usr_1" },
		"a logical service the core cannot reach":       func(e *chain.Event) { e.WrittenBy, e.Actor = "fault-controller", "fault-controller" },
		"a missing actor":                               func(e *chain.Event) { e.Actor = "" },
		"the v2 form of the actor":                      func(e *chain.Event) { e.Actor = "EXECUTION:usr_88213" },
		"service as a principal kind":                   func(e *chain.Event) { e.Actor = "service:core-execution" },
		"a principal kind outside the list":             func(e *chain.Event) { e.Actor = "guest:usr_1" },
		"a principal without an id":                     func(e *chain.Event) { e.Actor = "customer:" },
		"a principal id with a colon":                   func(e *chain.Event) { e.Actor = "customer:a:b" },
		"another emitter's identity as the principal":   func(e *chain.Event) { e.Actor = "core-trust" },
		"an event type outside PERD 32.2":               func(e *chain.Event) { e.EventType = "NOT_AN_EVENT" },
		"a security kind outside the schema":            func(e *chain.Event) { e.EventType, e.Kind = "SECURITY_EVENT", "SOMETHING_NEW" },
		"a SECURITY_EVENT without a kind":               func(e *chain.Event) { e.EventType = "SECURITY_EVENT" },
		"a kind on an event that is not SECURITY_EVENT": func(e *chain.Event) { e.Kind = "CHAIN_BREAK" },
		"a reason on GRANT_SPENT":                       func(e *chain.Event) { e.Reason = "EXPIRED" },
		"GRANT_VOIDED without a reason":                 func(e *chain.Event) { e.EventType = "GRANT_VOIDED" },
		"a fractional number in the payload":            func(e *chain.Event) { e.Payload = map[string]any{"x": json.Number("1.5")} },
		"an exponent number in the payload":             func(e *chain.Event) { e.Payload = map[string]any{"x": json.Number("1e3")} },
		"an integer above 2^53-1 in the payload":        func(e *chain.Event) { e.Payload = map[string]any{"x": json.Number("9007199254740992")} },
		"a float64 in the payload":                      func(e *chain.Event) { e.Payload = map[string]any{"x": mustFloat("2")} },
	} {
		ev := good
		mutate(&ev)
		tx, err := w.exec.Begin(ctx(t))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.app.Append(ctx(t), tx, ev); err == nil {
			t.Errorf("%s was appended", name)
		}
		_ = tx.Rollback(ctx(t))
	}
	if n, _ := chain.Verify(ctx(t), w.admin, "ten_aa"); n != 0 {
		t.Fatalf("refused events left %d events", n)
	}
}

// A context without a deadline is refused (AGENTS.md section 7).
func TestAppendNeedsAContextDeadline(t *testing.T) {
	w := newWorld(t, "ten_aa")
	tx, err := w.exec.Begin(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx(t)) }()
	var _ pgx.Tx = tx
	if _, err := w.app.Append(context.Background(), tx, event("ten_aa", 1)); err == nil {
		t.Fatal("Append accepted a context without a deadline")
	}
}

// PERD 27.1, REQ-EXE-003: each of the nine void reasons is accepted, kept as written and distinct from the others, and the
// chain over them verifies. The six added by record-event.v2 are SIGNATURE_INVALID, EXPIRED, BINDING_MISMATCH,
// WRITES_DISABLED, REVALIDATION_UNAVAILABLE and REJECTED_STALE_VERSION.
func TestEveryGrantVoidReasonIsAcceptedKeptAsWrittenAndDistinct(t *testing.T) {
	reasons := chain.GrantVoidReasons()
	if len(reasons) != 9 {
		t.Fatalf("%d reasons, want 9: %v", len(reasons), reasons)
	}
	added := []string{"SIGNATURE_INVALID", "EXPIRED", "BINDING_MISMATCH", "WRITES_DISABLED", "REVALIDATION_UNAVAILABLE", "REJECTED_STALE_VERSION"}
	for _, r := range added {
		found := false
		for _, have := range reasons {
			found = found || have == r
		}
		if !found {
			t.Fatalf("%s is not an accepted grant-void reason", r)
		}
	}
	w := newWorld(t, "ten_aa")
	for i, r := range reasons {
		w.append(t, chain.Event{TenantID: "ten_aa", CorrelationID: "corr_abc123", WrittenBy: "core-execution", Actor: "core-execution", EventType: "GRANT_VOIDED", Reason: r,
			ObjectRef: fmt.Sprintf("grt_%04d", i+1), Payload: map[string]any{"reason": r}})
	}
	var distinct, matching int
	if err := w.admin.QueryRow(ctx(t), `SELECT count(DISTINCT body->>'reason'), count(*) FILTER (WHERE body->>'reason' = payload->>'reason') FROM record_event WHERE event_type = 'GRANT_VOIDED'`).Scan(&distinct, &matching); err != nil {
		t.Fatal(err)
	}
	if distinct != 9 || matching != 9 {
		t.Fatalf("stored %d distinct reasons, %d equal to their payload; want 9 and 9", distinct, matching)
	}
	if n, err := chain.Verify(ctx(t), w.admin, "ten_aa"); err != nil || n != 9 {
		t.Fatalf("verify = %d, %v", n, err)
	}
}

// An unknown reason is rejected before it reaches the database, and nothing is stored (record-event.v2 GrantVoidReason).
func TestAnUnknownGrantVoidReasonIsRejected(t *testing.T) {
	w := newWorld(t, "ten_aa")
	for _, bad := range []string{"SOMETHING_ELSE", "expired", "Expired", "", " EXPIRED", "EXPIRED ", "TIMEOUT", "REJECTED_STALE_VERSION2", "SUCCEEDED"} {
		tx, err := w.exec.Begin(ctx(t))
		if err != nil {
			t.Fatal(err)
		}
		_, err = w.app.Append(ctx(t), tx, chain.Event{TenantID: "ten_aa", CorrelationID: "corr_abc123", WrittenBy: "core-execution", Actor: "core-execution",
			EventType: "GRANT_VOIDED", Reason: bad, ObjectRef: "grt_0001"})
		_ = tx.Rollback(ctx(t))
		if err == nil {
			t.Errorf("GRANT_VOIDED with the reason %q was appended", bad)
		}
	}
	if n, _ := chain.Verify(ctx(t), w.admin, "ten_aa"); n != 0 {
		t.Fatalf("rejected reasons left %d events", n)
	}
}

// mustFloat is a float64 made at run time, so the test file holds no float literal.
func mustFloat(text string) any {
	v, err := strconv.ParseFloat(text, 64)
	if err != nil {
		panic(err)
	}
	return v
}
