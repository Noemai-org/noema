package chain_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"pgregory.net/rapid"

	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/migrations"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/db"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/record/chain"
)

// REC-01 done-when: "Chain property test passes" (REQ-AUD-001, INV-10, INV-11). Property tests with rapid, a test-only dependency
// (AGENT-14, approved by the Architect on 3 Nov 2026). Each property runs against a real PostgreSQL, through the real appender as
// execution_rw, with events that are valid by construction: any rejection is therefore a finding.

type propDB struct {
	ctx         context.Context
	admin, exec *pgxpool.Pool
	app         *chain.Appender
	clk         *clock.Manual
	next        atomic.Int64
}

func newPropDB(t *testing.T) *propDB {
	t.Helper()
	admin, dsn := testpg.NewDatabase(t)
	c, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	t.Cleanup(cancel)
	if _, err := db.Migrate(c, admin, migrations.FS); err != nil {
		t.Fatal(err)
	}
	exec, err := db.OpenAsRole(c, dsn, "execution_rw")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(exec.Close)
	clk := clock.NewManual(time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC))
	return &propDB{ctx: c, admin: admin, exec: exec, clk: clk, app: &chain.Appender{Clock: clk, IDs: ids.New(clk, nil)}}
}

// tenant makes a tenant that no other iteration uses, so every iteration starts a fresh chain in the one database.
func (p *propDB) tenant(t *rapid.T) string {
	id := fmt.Sprintf("ten_prop%d", p.next.Add(1))
	if _, err := p.admin.Exec(p.ctx, `INSERT INTO tenant (tenant_id, name) VALUES ($1, $1)`, id); err != nil {
		t.Fatalf("tenant: %v", err)
	}
	return id
}

var (
	securityKinds    = []string{"INJECTION_DETECTED", "PRINCIPAL_OVERRIDE_ATTEMPT", "SIGNATURE_INVALID", "CROSS_SCOPE_ATTEMPT", "REQUEST_ID_REUSED", "IDEMPOTENCY_COLLISION", "CHAIN_BREAK"}
	failoverReasons  = []string{"PROVIDER_ERROR", "TIMEOUT", "REFUSED_REPLY"}
	noNulCharacters  = `[^\x00]{0,30}`
	maxSafeInteger   = int64(1)<<53 - 1
	principalIDShape = `[A-Za-z0-9][A-Za-z0-9_.-]{0,20}`
)

// jsonValue draws a value a payload may hold: integers within 2^53-1, strings, booleans, null, arrays and objects (PERD 32.1).
func jsonValue(depth int) *rapid.Generator[any] {
	scalar := rapid.OneOf(
		rapid.Map(rapid.Int64Range(-maxSafeInteger, maxSafeInteger), func(v int64) any { return v }),
		rapid.Map(rapid.StringMatching(noNulCharacters), func(v string) any { return v }),
		rapid.Map(rapid.Bool(), func(v bool) any { return v }),
		rapid.Just[any](nil),
	)
	if depth <= 0 {
		return scalar
	}
	return rapid.OneOf(
		scalar,
		rapid.Map(rapid.SliceOfN(jsonValue(depth-1), 0, 4), func(v []any) any { return v }),
		rapid.Map(rapid.MapOfN(rapid.StringMatching(`[a-z_]{1,8}`), jsonValue(depth-1), 0, 4), func(v map[string]any) any { return v }),
	)
}

// eventFor draws an event for tenant that Append must accept.
func eventFor(t *rapid.T, tenant string) chain.Event {
	typ := rapid.SampledFrom(chain.EventTypes()).Draw(t, "type")
	by := rapid.SampledFrom(chain.Emitters()).Draw(t, "written_by")
	actor := by
	if rapid.Bool().Draw(t, "person") {
		actor = rapid.SampledFrom(chain.PrincipalKinds()).Draw(t, "kind") + ":" + rapid.StringMatching(principalIDShape).Draw(t, "principal")
	}
	ev := chain.Event{
		TenantID: tenant, CorrelationID: "corr_" + rapid.StringMatching(`[0-9A-Za-z]{2,12}`).Draw(t, "corr"),
		WrittenBy: by, Actor: actor, EventType: typ,
		ObjectRef: rapid.StringMatching(`[a-z]{2,6}_[0-9A-Za-z_]{2,20}`).Draw(t, "object"),
	}
	switch typ {
	case "SECURITY_EVENT":
		ev.Kind = rapid.SampledFrom(securityKinds).Draw(t, "security_kind")
	case "GRANT_VOIDED":
		ev.Reason = rapid.SampledFrom(chain.GrantVoidReasons()).Draw(t, "void_reason")
	case "PROVIDER_FAILOVER":
		ev.Reason = rapid.SampledFrom(failoverReasons).Draw(t, "failover_reason")
	}
	if rapid.Bool().Draw(t, "has_payload") {
		ev.Payload = rapid.MapOfN(rapid.StringMatching(`[a-z_]{1,8}`), jsonValue(2), 0, 5).Draw(t, "payload")
	}
	return ev
}

type op struct {
	tenant   int
	event    chain.Event
	rollback bool
	advance  time.Duration
}

func (p *propDB) append(t *rapid.T, ev chain.Event, rollback bool) (chain.Stored, error) {
	tx, err := p.exec.Begin(p.ctx)
	if err != nil {
		t.Fatal(err)
	}
	st, err := p.app.Append(p.ctx, tx, ev)
	if err != nil || rollback {
		_ = tx.Rollback(p.ctx)
		return st, err
	}
	if err := tx.Commit(p.ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	return st, nil
}

// For any interleaving of events across tenants, some of them rolled back (EC-E16): each tenant's chain is contiguous from seq 1,
// each event extends the one before it (the first extends the tenant's genesis hash), the rolled-back events left nothing, and
// Verify recomputes every hash and accepts the chain. Tenants never disturb one another.
func TestEveryInterleavingOfAppendsLeavesAContiguousVerifiableChainPerTenant(t *testing.T) {
	p := newPropDB(t)
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(1, 3).Draw(t, "tenants")
		tenants := make([]string, n)
		last := make([]string, n)
		count := make([]int64, n)
		for i := range tenants {
			tenants[i] = p.tenant(t)
			last[i] = chain.GenesisHash(tenants[i])
		}
		ops := rapid.SliceOfN(rapid.Custom(func(t *rapid.T) op {
			i := rapid.IntRange(0, n-1).Draw(t, "tenant")
			return op{tenant: i, event: eventFor(t, tenants[i]), rollback: rapid.IntRange(0, 5).Draw(t, "rollback") == 0,
				advance: time.Duration(rapid.IntRange(0, 5000).Draw(t, "advance_ms")) * time.Millisecond}
		}), 1, 25).Draw(t, "ops")
		for _, o := range ops {
			p.clk.Advance(o.advance)
			st, err := p.append(t, o.event, o.rollback)
			if err != nil {
				t.Fatalf("an event valid by construction was refused: %v\nevent: %+v", err, o.event)
			}
			if o.rollback {
				continue
			}
			count[o.tenant]++
			if st.Seq != count[o.tenant] || st.PrevHash != last[o.tenant] {
				t.Fatalf("tenant %d: appended seq %d (want %d) extending %s (want %s)", o.tenant, st.Seq, count[o.tenant], st.PrevHash, last[o.tenant])
			}
			last[o.tenant] = st.Hash
		}
		for i, tn := range tenants {
			var rows int64
			if err := p.admin.QueryRow(p.ctx, `SELECT count(*) FROM record_event WHERE tenant_id = $1`, tn).Scan(&rows); err != nil {
				t.Fatal(err)
			}
			if rows != count[i] {
				t.Fatalf("tenant %d holds %d events, want exactly the %d committed", i, rows, count[i])
			}
			checked, err := chain.Verify(p.ctx, p.admin, tn)
			if err != nil || checked != count[i] {
				t.Fatalf("tenant %d: Verify = %d, %v; want %d, nil", i, checked, err, count[i])
			}
		}
	})
}

type tamper struct {
	name string
	sql  string // $1 tenant, $2 seq
	want chain.BreakKind
	skip func(seq, last int64) bool
}

// Every single tampering the chain is meant to expose, run with the triggers bypassed (the attack: an administrator who can write
// the table). Deleting the last event is not here: see TestTruncatingTheTailIsNotDetectedByTheChainAlone.
var tampers = []tamper{
	{"a hashed body field edited, its column left", `UPDATE record_event SET body = jsonb_set(body, '{object_ref}', '"zz_forged"') WHERE tenant_id = $1 AND seq = $2`, chain.BreakColumns, nil},
	{"a body field and its column edited", `UPDATE record_event SET body = jsonb_set(body, '{object_ref}', '"zz_forged"'), object_ref = 'zz_forged' WHERE tenant_id = $1 AND seq = $2`, chain.BreakHash, nil},
	{"the stored hash edited", `UPDATE record_event SET hash = 'sha256:` + strings.Repeat("0", 64) + `' WHERE tenant_id = $1 AND seq = $2`, chain.BreakHash, nil},
	{"the prior hash edited", `UPDATE record_event SET prev_hash = 'sha256:` + strings.Repeat("1", 64) + `' WHERE tenant_id = $1 AND seq = $2`, chain.BreakPrevHash, nil},
	{"an event deleted from before the head", `DELETE FROM record_event WHERE tenant_id = $1 AND seq = $2`, chain.BreakSeqGap, func(seq, last int64) bool { return seq == last }},
	{"the stored payload edited", `UPDATE record_event SET payload = '{"forged": 1}' WHERE tenant_id = $1 AND seq = $2`, chain.BreakPayloadHash, nil},
	{"the event type column edited", `UPDATE record_event SET event_type = CASE WHEN event_type = 'VERDICT' THEN 'GRANT_SPENT' ELSE 'VERDICT' END WHERE tenant_id = $1 AND seq = $2`, chain.BreakColumns, nil},
	{"the actor column edited", `UPDATE record_event SET actor = CASE WHEN actor = 'customer:usr_forged' THEN 'operator:usr_forged' ELSE 'customer:usr_forged' END WHERE tenant_id = $1 AND seq = $2`, chain.BreakColumns, nil},
	{"the schema version edited", `UPDATE record_event SET schema_version = '2.0' WHERE tenant_id = $1 AND seq = $2`, chain.BreakSchema, nil},
}

// Whatever the chain holds, any one of these tamperings at any event is found, and Verify names the first event it breaks.
func TestAnySingleTamperingIsDetectedAtTheEventItTouches(t *testing.T) {
	p := newPropDB(t)
	rapid.Check(t, func(t *rapid.T) {
		tn := p.tenant(t)
		n := rapid.IntRange(2, 12).Draw(t, "events")
		for i := 0; i < n; i++ {
			p.clk.Advance(time.Duration(rapid.IntRange(0, 3000).Draw(t, "advance_ms")) * time.Millisecond)
			if _, err := p.append(t, eventFor(t, tn), false); err != nil {
				t.Fatalf("an event valid by construction was refused: %v", err)
			}
		}
		if _, err := chain.Verify(p.ctx, p.admin, tn); err != nil {
			t.Fatalf("a clean chain does not verify: %v", err)
		}
		var usable []tamper
		seq := int64(rapid.IntRange(1, n).Draw(t, "seq"))
		for _, tp := range tampers {
			if tp.skip == nil || !tp.skip(seq, int64(n)) {
				usable = append(usable, tp)
			}
		}
		tp := rapid.SampledFrom(usable).Draw(t, "tampering")
		if rows, err := p.tamper(t, tp.sql, tn, seq); err != nil || rows != 1 {
			t.Fatalf("tamper %q: %v (%d rows)", tp.name, err, rows)
		}
		_, err := chain.Verify(p.ctx, p.admin, tn)
		var be *chain.BreakError
		if !errors.As(err, &be) {
			t.Fatalf("%s at seq %d of %d was not detected: %v", tp.name, seq, n, err)
		}
		if be.Kind != tp.want || be.Seq != seq && !(tp.want == chain.BreakSeqGap && be.Seq == seq+1) {
			t.Fatalf("%s at seq %d: Verify reported %s at seq %d, want %s at seq %d", tp.name, seq, be.Kind, be.Seq, tp.want, seq)
		}
	})
}

// A known limit, recorded so it cannot be forgotten: a chain that loses its newest events still verifies, because nothing after
// them refers back. Only a head recorded elsewhere reveals it. REC-07 (anchoring to a write-once store, REQ-AUD-007) closes this.
// Until then the Record is tamper-evident within the POC threat model, never tamper-proof (SoT OD-06).
func TestTruncatingTheTailIsNotDetectedByTheChainAlone(t *testing.T) {
	p := newPropDB(t)
	rapid.Check(t, func(t *rapid.T) {
		tn := p.tenant(t)
		n := rapid.IntRange(2, 8).Draw(t, "events")
		for i := 0; i < n; i++ {
			if _, err := p.append(t, eventFor(t, tn), false); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := p.tamper(t, `DELETE FROM record_event WHERE tenant_id = $1 AND seq = $2`, tn, n); err != nil {
			t.Fatal(err)
		}
		checked, err := chain.Verify(p.ctx, p.admin, tn)
		if err != nil || checked != int64(n-1) {
			t.Fatalf("after dropping the newest event Verify = %d, %v; the documented limit is that it accepts the shorter chain (%d)", checked, err, n-1)
		}
	})
}

// A U+0000 in a payload cannot be stored (PostgreSQL jsonb refuses it). The append fails and the transaction leaves no gap: the
// chain is still contiguous and verifies, and the next event extends the last one that was stored.
func TestANulCharacterInAPayloadFailsTheAppendAndLeavesNoGap(t *testing.T) {
	p := newPropDB(t)
	tn := "ten_nul"
	if _, err := p.admin.Exec(p.ctx, `INSERT INTO tenant (tenant_id, name) VALUES ($1, $1)`, tn); err != nil {
		t.Fatal(err)
	}
	ev := func(payload map[string]any) chain.Event {
		return chain.Event{TenantID: tn, CorrelationID: "corr_nul", WrittenBy: "core-execution", Actor: "core-execution", EventType: "GRANT_SPENT", ObjectRef: "grt_nul1", Payload: payload}
	}
	tx, _ := p.exec.Begin(p.ctx)
	first, err := p.app.Append(p.ctx, tx, ev(map[string]any{"a": 1}))
	if err != nil || tx.Commit(p.ctx) != nil {
		t.Fatal(err)
	}
	tx, _ = p.exec.Begin(p.ctx)
	if _, err := p.app.Append(p.ctx, tx, ev(map[string]any{"a": "x\x00y"})); err == nil {
		t.Fatal("a payload with U+0000 was accepted")
	}
	_ = tx.Rollback(p.ctx)
	tx, _ = p.exec.Begin(p.ctx)
	third, err := p.app.Append(p.ctx, tx, ev(map[string]any{"a": 2}))
	if err != nil || tx.Commit(p.ctx) != nil {
		t.Fatal(err)
	}
	if third.Seq != 2 || third.PrevHash != first.Hash {
		t.Fatalf("after the refused append the next event is seq %d extending %s; want seq 2 extending %s", third.Seq, third.PrevHash, first.Hash)
	}
	if n, err := chain.Verify(p.ctx, p.admin, tn); err != nil || n != 2 {
		t.Fatalf("Verify = %d, %v; want 2, nil", n, err)
	}
}

// testingT is what the helpers need of a test; *testing.T and *rapid.T both have it.
type testingT interface {
	Helper()
	Fatal(args ...any)
	Fatalf(format string, args ...any)
}

// tamper edits the Record as an administrator would, past the immutability triggers, and leaves nothing behind on the pooled
// connection. The setting is SET LOCAL inside a transaction, so it ends with the transaction: the connection that goes back to the
// pool is never one with triggers disabled, whatever the statement does (Codex review 5763774796). It returns the rows affected.
func (p *propDB) tamper(t testingT, sql string, args ...any) (int64, error) {
	t.Helper()
	tx, err := p.admin.Begin(p.ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(p.ctx) }()
	if _, err := tx.Exec(p.ctx, `SET LOCAL session_replication_role = replica`); err != nil {
		return 0, err
	}
	tag, err := tx.Exec(p.ctx, sql, args...)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(p.ctx); err != nil {
		return 0, err
	}
	p.assertTriggersEnabled(t)
	return tag.RowsAffected(), nil
}

// assertTriggersEnabled fails unless every connection the pool can hand out next runs with triggers enabled. It takes the whole
// pool's idle connections at once, so a connection left in replica mode cannot hide behind one that is not.
func (p *propDB) assertTriggersEnabled(t testingT) {
	t.Helper()
	var held []*pgxpool.Conn
	defer func() {
		for _, c := range held {
			c.Release()
		}
	}()
	// The bound is the number of connections the pool has, taken once before any is held. Two ways to get it wrong, both seen here:
	// reading the idle count inside the loop (every Acquire lowers it, so the loop can stop before every connection has been looked
	// at: Codex review 5763835671), and asking for more than the pool can give (every connection is held until the loop ends, so one
	// Acquire past MaxConns waits forever). The test is single-threaded, so every existing connection is idle and can be taken.
	n := int(p.admin.Stat().TotalConns())
	if n < 1 {
		n = 1
	}
	for i := 0; i < n; i++ {
		c, err := p.admin.Acquire(p.ctx)
		if err != nil {
			t.Fatal(err)
		}
		held = append(held, c)
		var role string
		if err := c.QueryRow(p.ctx, `SHOW session_replication_role`).Scan(&role); err != nil {
			t.Fatal(err)
		}
		if role != "origin" {
			t.Fatalf("a pooled connection has session_replication_role = %q: the triggers are disabled for the next user of the pool", role)
		}
	}
}

// The helper itself: after a tamper, no connection of the pool has triggers disabled, and the immutability trigger still refuses an
// ordinary administrator statement (so the setting did not leak into the next test of the pool).
func TestTamperingLeavesNoConnectionWithTriggersDisabled(t *testing.T) {
	p := newPropDB(t)
	tn := "ten_leak"
	if _, err := p.admin.Exec(p.ctx, `INSERT INTO tenant (tenant_id, name) VALUES ($1, $1)`, tn); err != nil {
		t.Fatal(err)
	}
	tx, err := p.exec.Begin(p.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.app.Append(p.ctx, tx, chain.Event{TenantID: tn, CorrelationID: "corr_leak", WrittenBy: "core-execution", Actor: "core-execution",
		EventType: "GRANT_SPENT", ObjectRef: "grt_leak1", Payload: map[string]any{"a": 1}}); err != nil || tx.Commit(p.ctx) != nil {
		t.Fatalf("append: %v", err)
	}
	if rows, err := p.tamper(t, `UPDATE record_event SET event_type = 'GRANT_VOIDED' WHERE tenant_id = $1 AND seq = 1`, tn); err != nil || rows != 1 {
		t.Fatalf("tamper = %d, %v", rows, err)
	}
	if _, err := p.admin.Exec(p.ctx, `UPDATE record_event SET event_type = 'GRANT_SPENT' WHERE tenant_id = $1 AND seq = 1`, tn); err == nil {
		t.Fatal("an ordinary UPDATE of the Record succeeded: the triggers were left disabled on a pooled connection")
	}
}

// recorder stands in for a test so a check that is expected to fail can be observed failing. Fatalf stops the caller the way a real
// one does (it does not return).
type recorder struct{ failed bool }

type stopped struct{}

func (r *recorder) Helper()               {}
func (r *recorder) Fatal(args ...any)     { r.failed = true; panic(stopped{}) }
func (r *recorder) Fatalf(string, ...any) { r.failed = true; panic(stopped{}) }

// The check sees a connection left with the triggers disabled wherever it sits among the idle ones (Codex review 5763835671: a
// bound that shrank during the loop could skip some). Four connections are made idle; each one in turn is the one left in replica
// mode; the check must fail every time.
func TestTheTriggerCheckSeesALeakOnAnyIdleConnection(t *testing.T) {
	p := newPropDB(t)
	const conns = 4
	hold := func() []*pgxpool.Conn {
		var held []*pgxpool.Conn
		for i := 0; i < conns; i++ {
			c, err := p.admin.Acquire(p.ctx)
			if err != nil {
				t.Fatal(err)
			}
			held = append(held, c)
		}
		return held
	}
	release := func(held []*pgxpool.Conn) {
		for _, c := range held {
			c.Release()
		}
	}
	for leaked := 0; leaked < conns; leaked++ {
		held := hold()
		if _, err := held[leaked].Exec(p.ctx, `SET session_replication_role = replica`); err != nil {
			t.Fatal(err)
		}
		release(held)

		r := &recorder{}
		func() {
			defer func() {
				if v := recover(); v != nil {
					if _, ok := v.(stopped); !ok {
						panic(v)
					}
				}
			}()
			p.assertTriggersEnabled(r)
		}()
		if !r.failed {
			t.Fatalf("a connection left in replica mode (the %d-th of %d idle ones) was not noticed", leaked+1, conns)
		}

		held = hold() // put the pool back to origin for the next round
		for _, c := range held {
			if _, err := c.Exec(p.ctx, `RESET session_replication_role`); err != nil {
				t.Fatal(err)
			}
		}
		release(held)
	}
}
