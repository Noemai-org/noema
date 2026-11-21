package interpreter_test

// UND-04, core half (REQ-INT-001, REQ-INP-002, REQ-AUD-001; INV-11, INV-12, EC-E16): the Interpreter's two events on the real Record, as the orchestrator's emitter through
// the tenant's own login, signed (REC-08). All data is SYNTHETIC.

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/internal/recordsig"
	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/migrations"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/db"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/record/chain"
	"github.com/Noemai-org/noema/services/core/understanding"
	"github.com/Noemai-org/noema/services/core/understanding/interpreter"
)

func TestMain(m *testing.M) { testpg.Main(m) }

const (
	tenantA = "ten_aa"
	tenantB = "ten_bb"
)

func ctx(t testing.TB) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return c
}

// failingIDs fails the Record event IDs while fail is set, so that the append fails (EC-E16).
type failingIDs struct {
	mu    sync.Mutex
	inner chain.IDSource
	fail  bool
}

func (f *failingIDs) ID(prefix string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail && prefix == "recev" {
		return "", errors.New("synthetic: the Record refused the event")
	}
	return f.inner.ID(prefix)
}

func (f *failingIDs) set(v bool) { f.mu.Lock(); f.fail = v; f.mu.Unlock() }

type dbWorld struct {
	t     *testing.T
	admin *pgxpool.Pool
	dsn   string
	clk   *clock.Manual
	fail  *failingIDs
	sign  map[string]chain.Signer
	pools map[string]*pgxpool.Pool
}

func newDBWorld(t *testing.T) *dbWorld {
	t.Helper()
	admin, dsn := testpg.NewDatabase(t)
	if _, err := db.Migrate(ctx(t), admin, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	clk := clock.NewManual(time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC))
	w := &dbWorld{t: t, admin: admin, dsn: dsn, clk: clk, fail: &failingIDs{inner: ids.New(clk, nil)}, pools: map[string]*pgxpool.Pool{}}
	w.sign = recordsig.All(t, admin, "interpreter", nil)
	for _, id := range []string{tenantA, tenantB} {
		if _, err := admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ($1, 'Synthetic')`, id); err != nil {
			t.Fatal(err)
		}
	}
	return w
}

func (w *dbWorld) recorder(tenant string) *interpreter.RecordRecorder {
	w.t.Helper()
	if w.pools[tenant] == nil {
		w.pools[tenant] = testpg.TenantLogin(w.t, w.admin, w.dsn, "orchestrator_rw", tenant)
	}
	return &interpreter.RecordRecorder{Pool: w.pools[tenant], Appender: &chain.Appender{Clock: w.clk, IDs: w.fail, Signers: w.sign}, Timeout: 5 * time.Second}
}

type stored struct {
	WrittenBy, Actor, EventType, ObjectRef, Corr string
	Payload, Body                                map[string]any
}

func (w *dbWorld) events() []stored {
	w.t.Helper()
	rows, err := w.admin.Query(ctx(w.t), `SELECT written_by, actor, event_type, object_ref, correlation_id, payload::text, body::text FROM record_event ORDER BY tenant_id, seq`)
	if err != nil {
		w.t.Fatal(err)
	}
	defer rows.Close()
	var out []stored
	for rows.Next() {
		var s stored
		var p, b string
		if err := rows.Scan(&s.WrittenBy, &s.Actor, &s.EventType, &s.ObjectRef, &s.Corr, &p, &b); err != nil {
			w.t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(p), &s.Payload); err != nil {
			w.t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(b), &s.Body); err != nil {
			w.t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

func subject(tenant string) interpreter.Subject {
	return interpreter.Subject{TenantID: tenant, Actor: actor, CorrelationID: corr}
}

// A refusal at the gate is INTENT_REJECTED_AT_GATE with the reason codes the reconstruction reads (record/reconstruct/project.go: reason_codes), written by the
// orchestrator's emitter for the login's tenant, and nothing else.
func TestARejectionIsAppendedAsIntentRejectedAtGateWithItsReasonCodesOnly(t *testing.T) {
	w := newDBWorld(t)
	if err := w.recorder(tenantA).Rejected(ctx(t), subject(tenantA), []understanding.Reason{understanding.ReasonSig, understanding.ReasonUnknownNode}); err != nil {
		t.Fatal(err)
	}
	evs := w.events()
	if len(evs) != 1 {
		t.Fatalf("%d events, want 1", len(evs))
	}
	e := evs[0]
	if e.EventType != "INTENT_REJECTED_AT_GATE" || e.WrittenBy != "core-workflows" || e.Actor != actor || e.Corr != corr || e.Body["tenant_id"] != tenantA || e.ObjectRef != "gat_4c21f9" {
		t.Errorf("event %+v", e)
	}
	if want := map[string]any{"reason_codes": []any{"SIG", "UNKNOWN_NODE"}}; !reflect.DeepEqual(e.Payload, want) {
		t.Errorf("payload %v, want only the codes %v", e.Payload, want)
	}
}

// A real override is SECURITY_EVENT of kind PRINCIPAL_OVERRIDE_ATTEMPT with the member names, never a value.
func TestAnOverrideAttemptIsAppendedAsASecurityEventWithFieldNamesOnly(t *testing.T) {
	w := newDBWorld(t)
	if err := w.recorder(tenantA).OverrideAttempt(ctx(t), subject(tenantA), []string{"tenant_id", "principal_id"}); err != nil {
		t.Fatal(err)
	}
	evs := w.events()
	if len(evs) != 1 || evs[0].EventType != "SECURITY_EVENT" || evs[0].Body["kind"] != "PRINCIPAL_OVERRIDE_ATTEMPT" || evs[0].WrittenBy != "core-workflows" {
		t.Fatalf("events %+v", evs)
	}
	if want := map[string]any{"fields": []any{"tenant_id", "principal_id"}}; !reflect.DeepEqual(evs[0].Payload, want) {
		t.Errorf("payload %v, want only the names %v", evs[0].Payload, want)
	}
}

// EC-E16: when the Record refuses the append the recorder reports it and nothing stands.
func TestAnAppendTheRecordRefusesStoresNothing(t *testing.T) {
	w := newDBWorld(t)
	w.fail.set(true)
	r := w.recorder(tenantA)
	if err := r.Rejected(ctx(t), subject(tenantA), []understanding.Reason{understanding.ReasonSig}); err == nil {
		t.Error("a rejection was reported recorded although the append failed")
	}
	if err := r.OverrideAttempt(ctx(t), subject(tenantA), []string{"tenant_id"}); err == nil {
		t.Error("an override attempt was reported recorded although the append failed")
	}
	if n := len(w.events()); n != 0 {
		t.Errorf("%d events stored for appends that failed", n)
	}
}

// INV-12: the tenant is the login's. A subject that names another tenant is refused by the database, not by this code.
func TestASubjectOfAnotherTenantIsRefusedByTheDatabase(t *testing.T) {
	w := newDBWorld(t)
	if err := w.recorder(tenantA).Rejected(ctx(t), subject(tenantB), []understanding.Reason{understanding.ReasonSig}); err == nil {
		t.Error("tenant A's login wrote an event for tenant B")
	}
	if n := len(w.events()); n != 0 {
		t.Errorf("%d events stored", n)
	}
}

// What the recorder will not write at all, checked before the database is asked: a malformed subject, no reason, a reason off its shape, no field, a field the gate does not report.
func TestWhatTheRecorderCannotServeIsRefusedBeforeItIsWritten(t *testing.T) {
	w := newDBWorld(t)
	r := w.recorder(tenantA)
	bad := func(f func(s *interpreter.Subject)) interpreter.Subject { s := subject(tenantA); f(&s); return s }
	for name, s := range map[string]interpreter.Subject{
		"no tenant":               bad(func(s *interpreter.Subject) { s.TenantID = "" }),
		"an actor off shape":      bad(func(s *interpreter.Subject) { s.Actor = "end_user:usr_1" }),
		"a correlation off shape": bad(func(s *interpreter.Subject) { s.CorrelationID = "corr x" }),
	} {
		if err := r.Rejected(ctx(t), s, []understanding.Reason{understanding.ReasonSig}); err == nil {
			t.Errorf("%s: written", name)
		}
	}
	if err := r.Rejected(ctx(t), subject(tenantA), nil); err == nil {
		t.Error("a rejection with no reason was written")
	}
	if err := r.Rejected(ctx(t), subject(tenantA), []understanding.Reason{"sig"}); err == nil {
		t.Error("a lower-case reason was written")
	}
	if err := r.OverrideAttempt(ctx(t), subject(tenantA), nil); err == nil {
		t.Error("an override attempt with no field was written")
	}
	if err := r.OverrideAttempt(ctx(t), subject(tenantA), []string{"principal_type"}); err == nil {
		t.Error("a field the gate does not report was written")
	}
	if n := len(w.events()); n != 0 {
		t.Errorf("%d events stored for events that must be refused", n)
	}
	if err := (&interpreter.RecordRecorder{}).Rejected(ctx(t), subject(tenantA), []understanding.Reason{understanding.ReasonSig}); err == nil {
		t.Error("a recorder with no pool, appender or timeout wrote")
	}
}
