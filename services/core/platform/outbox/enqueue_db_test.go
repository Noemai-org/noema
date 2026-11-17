package outbox_test

// PLT-08, REQ-VER-005, PERD §56 and §36.1 (transactional outbox, never dual writes), EC-E16, INV-11, INV-12: an event is written in the transaction of the state
// change it belongs to. These tests run on a real PostgreSQL server (REQ-TST-004). All data is SYNTHETIC.

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/Noemai-org/noema/services/core/platform/outbox"
)

func (w *world) insertState(tx pgx.Tx, tenant, id, v string) error {
	_, err := tx.Exec(ctx(w.t), `INSERT INTO plt08_state (tenant_id, id, v) VALUES ($1, $2, $3)`, tenant, id, v)
	return err
}

func (w *world) event(n int, tenant, topic string) outbox.Event {
	return outbox.Event{OutboxID: oid(n), TenantID: tenant, Topic: topic, Payload: payload(n), CreatedAt: w.clk.Now()}
}

// The event commits with its state change.
func TestAnEventCommitsWithItsStateChange(t *testing.T) {
	w := newWorld(t)
	tx, err := w.execA.Begin(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := w.insertState(tx, tenantA, "st_1", "ok"); err != nil {
		t.Fatal(err)
	}
	if err := outbox.Enqueue(ctx(t), tx, w.event(1, tenantA, topicV)); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	// Before COMMIT neither is visible to another session.
	if s, e := w.count(`SELECT count(*) FROM plt08_state`), w.count(`SELECT count(*) FROM outbox_event`); s != 0 || e != 0 {
		t.Fatalf("before commit: %d state rows and %d events are visible, want none", s, e)
	}
	if err := tx.Commit(ctx(t)); err != nil {
		t.Fatal(err)
	}
	if s, e := w.count(`SELECT count(*) FROM plt08_state`), w.count(`SELECT count(*) FROM outbox_event WHERE dispatched_at IS NULL`); s != 1 || e != 1 {
		t.Fatalf("after commit: %d state rows and %d pending events, want one each", s, e)
	}
	var topic, tenant string
	var raw []byte
	if err := w.admin.QueryRow(ctx(t), `SELECT topic, tenant_id, payload FROM outbox_event WHERE outbox_id = $1`, oid(1)).Scan(&topic, &tenant, &raw); err != nil {
		t.Fatal(err)
	}
	var got, want map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(payload(1), &want); err != nil {
		t.Fatal(err)
	}
	if topic != topicV || tenant != tenantA || got["grant_id"] != want["grant_id"] || got["note"] != want["note"] {
		t.Fatalf("stored (%q, %q, %v), want (%q, %q, %v)", topic, tenant, got, topicV, tenantA, want)
	}
}

// A rollback takes the event with the state change.
func TestARollbackTakesTheEventWithTheStateChange(t *testing.T) {
	w := newWorld(t)
	tx, err := w.execA.Begin(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := w.insertState(tx, tenantA, "st_1", "ok"); err != nil {
		t.Fatal(err)
	}
	if err := outbox.Enqueue(ctx(t), tx, w.event(1, tenantA, topicV)); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(ctx(t)); err != nil {
		t.Fatal(err)
	}
	if s, e := w.count(`SELECT count(*) FROM plt08_state`), w.count(`SELECT count(*) FROM outbox_event`); s != 0 || e != 0 {
		t.Fatalf("after rollback: %d state rows and %d events, want none", s, e)
	}
}

// A state change that fails after the event was written takes the event with it: the transaction is aborted and cannot commit.
func TestAStateChangeThatFailsAfterTheEnqueueLeavesNoEvent(t *testing.T) {
	w := newWorld(t)
	tx, err := w.execA.Begin(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := outbox.Enqueue(ctx(t), tx, w.event(1, tenantA, topicV)); err != nil {
		t.Fatal(err)
	}
	if err := w.insertState(tx, tenantA, "st_1", "refuse"); err == nil {
		t.Fatal("the state change was accepted: the test table's CHECK did not fire")
	}
	if err := tx.Commit(ctx(t)); err == nil {
		t.Fatal("an aborted transaction committed")
	}
	if s, e := w.count(`SELECT count(*) FROM plt08_state`), w.count(`SELECT count(*) FROM outbox_event`); s != 0 || e != 0 {
		t.Fatalf("%d state rows and %d events remain, want none", s, e)
	}
}

// An event that is refused takes the state change with it: no state change commits without its event.
func TestAnEventThatIsRefusedTakesTheStateChangeWithIt(t *testing.T) {
	w := newWorld(t)
	w.enqueue(w.execA, tenantA, 1, topicV) // the ID is taken
	tx, err := w.execA.Begin(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := w.insertState(tx, tenantA, "st_1", "ok"); err != nil {
		t.Fatal(err)
	}
	err = outbox.Enqueue(ctx(t), tx, w.event(1, tenantA, topicV))
	if !errors.Is(err, outbox.ErrDuplicateEvent) {
		t.Fatalf("Enqueue = %v, want ErrDuplicateEvent", err)
	}
	if err := tx.Commit(ctx(t)); err == nil {
		t.Fatal("the transaction committed the state change although its event was refused")
	}
	if s, e := w.count(`SELECT count(*) FROM plt08_state`), w.count(`SELECT count(*) FROM outbox_event`); s != 0 || e != 1 {
		t.Fatalf("%d state rows and %d events, want 0 state rows and the one earlier event", s, e)
	}
}

// INV-12, REQ-SEC-004: a login of tenant A cannot write an event of tenant B (row security binds the row to the login's tenant).
func TestALoginCannotEnqueueAnotherTenantsEvent(t *testing.T) {
	w := newWorld(t)
	tx, err := w.execA.Begin(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx(t)) }()
	err = outbox.Enqueue(ctx(t), tx, w.event(1, tenantB, topicV))
	if err == nil {
		t.Fatal("tenant A's login wrote an event for tenant B")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("the error carries the payload: %q", err)
	}
	_ = tx.Rollback(ctx(t))
	if n := w.count(`SELECT count(*) FROM outbox_event`); n != 0 {
		t.Fatalf("%d events stored", n)
	}
}

// AGENT-13: an error from the database that quotes the row (a CHECK violation prints "Failing row contains (...)", payload included) must not reach the error text.
func TestADatabaseRefusalNeverQuotesThePayload(t *testing.T) {
	w := newWorld(t)
	if _, err := w.admin.Exec(ctx(t), `ALTER TABLE outbox_event ADD CONSTRAINT plt08_test_never CHECK (topic <> 'outcome.never')`); err != nil {
		t.Fatal(err)
	}
	tx, err := w.execA.Begin(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx(t)) }()
	err = outbox.Enqueue(ctx(t), tx, w.event(1, tenantA, "outcome.never"))
	if err == nil {
		t.Fatal("the CHECK did not refuse the row")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("the error quotes the payload: %q", err)
	}
	if !errors.Is(err, outbox.ErrWrite) {
		t.Fatalf("error %v, want ErrWrite (with the SQLSTATE, not the row)", err)
	}
	if !strings.Contains(err.Error(), "23514") {
		t.Fatalf("error %q does not name the SQLSTATE 23514", err)
	}
}

// Two events of one tenant and topic are two rows.
func TestTwoEventsOfOneTopicAreTwoRows(t *testing.T) {
	w := newWorld(t)
	w.enqueue(w.execA, tenantA, 1, topicV)
	w.enqueue(w.execA, tenantA, 2, topicV)
	if got := w.pending(tenantA); len(got) != 2 || got[0] != oid(1) || got[1] != oid(2) {
		t.Fatalf("pending = %v", got)
	}
}
