package outbox_test

// PLT-08, migration 0031 (proposed number), PERD §38.2 (`outbox_event`: PENDING to DISPATCHED, mutable), REQ-VER-005, INV-12. The column grants of migration 0001 let a
// producer insert a row that is already dispatched (an event that commits and is never delivered) and let the dispatcher set `dispatched_at` back to NULL (a redelivery
// nobody asked for). The trigger makes the lifecycle one way, for every role including the owner. All data is SYNTHETIC.

import (
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func sqlstate(err error) string {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return pe.Code
	}
	return ""
}

// 23000 is integrity_constraint_violation, the class the other guards of this schema raise (idempotency_record_guard, reject_mutation).
const integrity = "23000"

func TestAnEventCannotBeBornDispatched(t *testing.T) {
	w := newWorld(t)
	for name, pool := range map[string]*pgxpool.Pool{"the execution login": w.execA, "the migrating superuser": w.admin} {
		_, err := pool.Exec(ctx(t), `INSERT INTO outbox_event (outbox_id, tenant_id, topic, payload, created_at, dispatched_at) VALUES ($1, $2, $3, '{}', now(), now())`, oid(1), tenantA, topicV)
		if sqlstate(err) != integrity {
			t.Errorf("%s: inserting a dispatched event: %v, want SQLSTATE %s", name, err, integrity)
		}
	}
	if n := w.count(`SELECT count(*) FROM outbox_event`); n != 0 {
		t.Fatalf("%d events stored", n)
	}
}

// The one legal move: the dispatcher sets dispatched_at once, from NULL.
func TestTheDispatcherMayMarkAPendingEventOnce(t *testing.T) {
	w := newWorld(t)
	w.enqueue(w.execA, tenantA, 1, topicV)
	if _, err := w.orchA.Exec(ctx(t), `UPDATE outbox_event SET dispatched_at = now() WHERE outbox_id = $1`, oid(1)); err != nil {
		t.Fatalf("the legal mark was refused: %v", err)
	}
	if n := w.count(`SELECT count(*) FROM outbox_event WHERE dispatched_at IS NOT NULL`); n != 1 {
		t.Fatalf("%d dispatched events, want 1", n)
	}
}

// A dispatched event is final: it cannot be set back to pending (a delivery nobody asked for) and its instant cannot be rewritten.
func TestADispatchedEventNeverChangesAgain(t *testing.T) {
	w := newWorld(t)
	w.enqueue(w.execA, tenantA, 1, topicV)
	if _, err := w.orchA.Exec(ctx(t), `UPDATE outbox_event SET dispatched_at = now() WHERE outbox_id = $1`, oid(1)); err != nil {
		t.Fatal(err)
	}
	for name, q := range map[string]string{
		"back to pending":      `UPDATE outbox_event SET dispatched_at = NULL WHERE outbox_id = $1`,
		"a second instant":     `UPDATE outbox_event SET dispatched_at = now() + interval '1 hour' WHERE outbox_id = $1`,
		"the same instant":     `UPDATE outbox_event SET dispatched_at = dispatched_at WHERE outbox_id = $1`,
		"a payload edit":       `UPDATE outbox_event SET payload = '{"x":1}' WHERE outbox_id = $1`,
		"a topic edit":         `UPDATE outbox_event SET topic = 'a.b' WHERE outbox_id = $1`,
		"a tenant edit":        `UPDATE outbox_event SET tenant_id = '` + tenantB + `' WHERE outbox_id = $1`,
		"a created_at edit":    `UPDATE outbox_event SET created_at = created_at + interval '1 day' WHERE outbox_id = $1`,
		"an ID edit":           `UPDATE outbox_event SET outbox_id = 'obx_` + strings.Repeat("9", 26) + `' WHERE outbox_id = $1`,
		"the owner's rewrite":  `UPDATE outbox_event SET dispatched_at = NULL WHERE outbox_id = $1`,
		"a whole-row rewrite":  `UPDATE outbox_event SET dispatched_at = dispatched_at, payload = payload WHERE outbox_id = $1`,
		"the unchanged update": `UPDATE outbox_event SET topic = topic WHERE outbox_id = $1`,
	} {
		pool := w.admin // the owner and the superuser are bound by the trigger too
		if name == "back to pending" || name == "a second instant" || name == "the same instant" {
			pool = w.orchA // what the dispatcher's own grant allows
		}
		if _, err := pool.Exec(ctx(t), q, oid(1)); sqlstate(err) != integrity {
			t.Errorf("%s: %v, want SQLSTATE %s", name, err, integrity)
		}
	}
	if n := w.count(`SELECT count(*) FROM outbox_event WHERE dispatched_at IS NOT NULL`); n != 1 {
		t.Fatalf("%d dispatched events after the attempts, want 1", n)
	}
}

// A pending event's content never changes either, for anyone; only the mark moves.
func TestAPendingEventsContentNeverChanges(t *testing.T) {
	w := newWorld(t)
	w.enqueue(w.execA, tenantA, 1, topicV)
	for name, q := range map[string]string{
		"a payload edit":    `UPDATE outbox_event SET payload = '{"x":1}' WHERE outbox_id = $1`,
		"a topic edit":      `UPDATE outbox_event SET topic = 'a.b' WHERE outbox_id = $1`,
		"a tenant edit":     `UPDATE outbox_event SET tenant_id = '` + tenantB + `' WHERE outbox_id = $1`,
		"a created_at edit": `UPDATE outbox_event SET created_at = created_at + interval '1 day' WHERE outbox_id = $1`,
		"an ID edit":        `UPDATE outbox_event SET outbox_id = 'obx_` + strings.Repeat("9", 26) + `' WHERE outbox_id = $1`,
		"a no-op":           `UPDATE outbox_event SET topic = topic WHERE outbox_id = $1`,
	} {
		if _, err := w.admin.Exec(ctx(t), q, oid(1)); sqlstate(err) != integrity {
			t.Errorf("%s: %v, want SQLSTATE %s", name, err, integrity)
		}
	}
	if got := w.pending(tenantA); len(got) != 1 {
		t.Fatalf("pending = %v", got)
	}
}

// The grants of migration 0001 still hold: no producer updates or deletes an event, and the dispatcher updates the mark and nothing else.
func TestTheGrantsOfTheOutboxAreUnchanged(t *testing.T) {
	w := newWorld(t)
	w.enqueue(w.execA, tenantA, 1, topicV)
	for name, tc := range map[string]struct {
		pool *pgxpool.Pool
		q    string
	}{
		"a producer updates":       {w.execA, `UPDATE outbox_event SET dispatched_at = now()`},
		"a producer deletes":       {w.execA, `DELETE FROM outbox_event`},
		"the dispatcher deletes":   {w.orchA, `DELETE FROM outbox_event`},
		"the dispatcher's payload": {w.orchA, `UPDATE outbox_event SET payload = '{}'`},
		"the dispatcher inserts":   {w.orchA, `INSERT INTO outbox_event (outbox_id, tenant_id, topic, payload, created_at) VALUES ('obx_` + strings.Repeat("8", 26) + `', 'ten_aa', 'a.b', '{}', now())`},
	} {
		if _, err := tc.pool.Exec(ctx(t), tc.q); sqlstate(err) != "42501" {
			t.Errorf("%s: %v, want SQLSTATE 42501 (permission denied)", name, err)
		}
	}
}

// The trigger functions of this schema pin their search_path (SR's rule of noema#234, REQ-SEC-004): the one added here does too.
func TestTheOutboxGuardPinsItsSearchPath(t *testing.T) {
	w := newWorld(t)
	var names []string
	rows, err := w.admin.Query(ctx(t), `SELECT p.proname, coalesce(p.proconfig, ARRAY[]::text[]) FROM pg_trigger tg JOIN pg_proc p ON p.oid = tg.tgfoid
		WHERE tg.tgrelid = 'public.outbox_event'::regclass AND NOT tg.tgisinternal`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		var cfg []string
		if err := rows.Scan(&name, &cfg); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
		pinned := false
		for _, c := range cfg {
			if strings.EqualFold(strings.ReplaceAll(c, `"`, ""), "search_path=pg_catalog, pg_temp") {
				pinned = true
			}
		}
		if !pinned {
			t.Errorf("%s does not pin search_path to pg_catalog, pg_temp (proconfig %v)", name, cfg)
		}
	}
	if len(names) == 0 {
		t.Fatal("outbox_event has no lifecycle trigger")
	}
}
