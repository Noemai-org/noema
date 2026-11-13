package admission_test

// STM-01 (A161, A169; the Record-backed Admissions read for noema#465): the orchestrator runs a turn only if the Record confirms the INPUT_RECEIVED event the input gateway
// wrote for it. This reader finds one event by its ID in the tenant's chain, as the tenant's own record_read login (the database binds the tenant, DEBT-04), verifies the
// chain before it trusts a row, and returns the facts the orchestrator compares: the event type, the tenant, the principal (the actor after its kind), the correlation and the
// request ID from the payload. It decides nothing. The tests that append signed events need the signing extension (the CI database has it). All data is SYNTHETIC.

import (
	"context"
	"errors"
	"math/rand"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/internal/recordsig"
	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/migrations"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/db"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/record/admission"
	"github.com/Noemai-org/noema/services/core/record/chain"
	"github.com/Noemai-org/noema/services/core/record/reconstruct"
)

func TestMain(m *testing.M) { testpg.Main(m) }

const (
	tenantA, tenantB = "ten_aa", "ten_bb"
	reqID            = "3f2b8c1e-5d4a-4b7e-9a10-2c6d8e0f1a23"
)

var epoch = time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)

func ctx(t testing.TB) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	return c
}

type pools map[string]*pgxpool.Pool

func (p pools) For(tenant string) (reconstruct.TxBeginner, error) {
	pool, ok := p[tenant]
	if !ok {
		return nil, errors.New("no pool for tenant " + tenant)
	}
	return pool, nil
}

type world struct {
	admin  *pgxpool.Pool
	app    *chain.Appender
	edgeA  *pgxpool.Pool
	edgeB  *pgxpool.Pool
	reader *admission.Reader
}

func newWorld(t *testing.T) *world {
	t.Helper()
	admin, dsn := testpg.NewDatabase(t)
	if _, err := db.Migrate(ctx(t), admin, migrations.FS); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{tenantA, tenantB} {
		if _, err := admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ($1, 'T')`, id); err != nil {
			t.Fatal(err)
		}
	}
	clk := clock.NewManual(epoch)
	w := &world{admin: admin, app: &chain.Appender{Clock: clk, IDs: ids.New(clk, rand.New(rand.NewSource(3))), Signers: recordsig.For(t, admin, "admission", nil, "core-edge")},
		edgeA: testpg.EmitterLogin(t, admin, dsn, tenantA, "core-edge"), edgeB: testpg.EmitterLogin(t, admin, dsn, tenantB, "core-edge")}
	readers := pools{tenantA: testpg.TenantLogin(t, admin, dsn, "record_read", tenantA), tenantB: testpg.TenantLogin(t, admin, dsn, "record_read", tenantB)}
	w.reader = &admission.Reader{DBs: readers}
	return w
}

func (w *world) append(t *testing.T, p *pgxpool.Pool, tenant string, e chain.Event) string {
	t.Helper()
	tx, err := p.Begin(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	e.TenantID = tenant
	st, err := w.app.Append(ctx(t), tx, e)
	if err != nil {
		_ = tx.Rollback(ctx(t))
		t.Fatal(err)
	}
	if err := tx.Commit(ctx(t)); err != nil {
		t.Fatal(err)
	}
	return st.EventID
}

func input(corr, actor string, payload map[string]any) chain.Event {
	return chain.Event{CorrelationID: corr, WrittenBy: "core-edge", Actor: actor, EventType: "INPUT_RECEIVED", ObjectRef: "inp_" + corr[len(corr)-4:], Payload: payload}
}

// The reader returns the facts of the event the gateway wrote: its type, the tenant (the pool's), the principal (the actor after its kind), the correlation and the request ID.
func TestItFindsTheInputReceivedOfTheTenantWithItsIdentity(t *testing.T) {
	w := newWorld(t)
	id := w.append(t, w.edgeA, tenantA, input("corr_AA11", "customer:usr_01", map[string]any{"channel": "text", "request_id": reqID}))
	got, err := w.reader.Lookup(ctx(t), tenantA, id)
	want := admission.Found{EventType: "INPUT_RECEIVED", TenantID: tenantA, PrincipalID: "usr_01", CorrelationID: "corr_AA11", RequestID: reqID}
	if err != nil || got != want {
		t.Fatalf("got %+v (%v), want %+v", got, err, want)
	}
}

// Another tenant's login neither finds the event nor learns that it exists; an ID that does not exist is the same answer.
func TestAnotherTenantsReaderAndAnUnknownIdAreNotFound(t *testing.T) {
	w := newWorld(t)
	id := w.append(t, w.edgeA, tenantA, input("corr_AA11", "customer:usr_01", map[string]any{"channel": "text", "request_id": reqID}))
	w.append(t, w.edgeB, tenantB, input("corr_BB22", "customer:usr_02", map[string]any{"channel": "text", "request_id": reqID}))
	if _, err := w.reader.Lookup(ctx(t), tenantB, id); !errors.Is(err, admission.ErrNotFound) {
		t.Errorf("tenant B reading tenant A's event: %v; want ErrNotFound", err)
	}
	if _, err := w.reader.Lookup(ctx(t), tenantA, "recev_ZZZZZZ99"); !errors.Is(err, admission.ErrNotFound) {
		t.Errorf("an unknown ID: %v; want ErrNotFound", err)
	}
}

// The reader reports what the event is; it does not decide. Another event type comes back as it is, and a missing request ID or an actor with no kind comes back empty, so the
// orchestrator's comparison refuses it.
func TestItReportsWhatTheEventIsAndLeavesEmptyWhatTheEventDoesNotCarry(t *testing.T) {
	w := newWorld(t)
	other := w.append(t, w.edgeA, tenantA, chain.Event{CorrelationID: "corr_AA11", WrittenBy: "core-edge", Actor: "core-edge", EventType: "INPUT_DEDUPLICATED", ObjectRef: "inp_AA11", Payload: map[string]any{"request_id": reqID}})
	noReq := w.append(t, w.edgeA, tenantA, input("corr_AA22", "customer:usr_01", map[string]any{"channel": "text"}))
	got, err := w.reader.Lookup(ctx(t), tenantA, other)
	if err != nil || got.EventType != "INPUT_DEDUPLICATED" || got.PrincipalID != "" {
		t.Errorf("another type: %+v (%v)", got, err)
	}
	got, err = w.reader.Lookup(ctx(t), tenantA, noReq)
	if err != nil || got.EventType != "INPUT_RECEIVED" || got.RequestID != "" {
		t.Errorf("no request id: %+v (%v)", got, err)
	}
}

// Fail closed: a pool the tenant has no reader for is an error; a malformed ID or tenant, no deadline and an unconfigured reader are ErrRequest and read nothing.
func TestItRefusesWhatItCannotHonour(t *testing.T) {
	w := newWorld(t)
	if _, err := w.reader.Lookup(ctx(t), "ten_zz", "recev_AA11"); err == nil || errors.Is(err, admission.ErrNotFound) {
		t.Errorf("a tenant with no pool: %v; want an error that is not ErrNotFound", err)
	}
	for name, c := range map[string][2]string{"a bad event id": {tenantA, "evt_AA11"}, "an id with a newline": {tenantA, "recev_AA11\n"}, "a bad tenant": {"tenant", "recev_AA11"}, "an empty id": {tenantA, ""}} {
		if _, err := w.reader.Lookup(ctx(t), c[0], c[1]); !errors.Is(err, admission.ErrRequest) {
			t.Errorf("%s: %v; want ErrRequest", name, err)
		}
	}
	if _, err := w.reader.Lookup(context.Background(), tenantA, "recev_AA11"); !errors.Is(err, admission.ErrRequest) {
		t.Errorf("no deadline: %v; want ErrRequest", err)
	}
	if _, err := (&admission.Reader{}).Lookup(ctx(t), tenantA, "recev_AA11"); !errors.Is(err, admission.ErrRequest) {
		t.Errorf("an unconfigured reader: %v; want ErrRequest", err)
	}
}

// tamper changes one stored row the way a hostile writer with superuser rights would (triggers off), so the chain no longer verifies.
func (w *world) tamper(t *testing.T, sql string, args ...any) {
	t.Helper()
	conn, err := w.admin.Acquire(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx(t), `SET session_replication_role = replica`); err != nil {
		t.Fatal(err)
	}
	tag, err := conn.Exec(ctx(t), sql, args...)
	if err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("tamper %q: %v (%d rows)", sql, err, tag.RowsAffected())
	}
}

// REQ-AUD-001, INV-12 (the one security claim of this package): a chain that does not verify serves no row of itself, not even an event that was not touched. The control is the
// untouched chain, which is served. Deleting the VerifyRows call turns this test red.
func TestAChainThatDoesNotVerifyServesNothing(t *testing.T) {
	w := newWorld(t)
	first := w.append(t, w.edgeA, tenantA, input("corr_AA11", "customer:usr_01", map[string]any{"channel": "text", "request_id": reqID}))
	second := w.append(t, w.edgeA, tenantA, input("corr_AA22", "customer:usr_01", map[string]any{"channel": "text", "request_id": reqID}))
	if _, err := w.reader.Lookup(ctx(t), tenantA, first); err != nil {
		t.Fatalf("control: the untouched chain is served: %v", err)
	}
	w.tamper(t, `UPDATE record_event SET payload = '{"channel":"voice","request_id":"tampered"}' WHERE event_id = $1`, second)
	for name, id := range map[string]string{"the tampered event": second, "an untouched event of the same chain": first} {
		got, err := w.reader.Lookup(ctx(t), tenantA, id)
		if !errors.Is(err, admission.ErrIntegrity) || got != (admission.Found{}) {
			t.Errorf("%s: got %+v (%v); want ErrIntegrity and nothing found", name, got, err)
		}
	}
}
