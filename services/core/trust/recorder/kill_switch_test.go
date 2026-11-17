package recorder_test

// EXE-06, Trust side (PUS-06; REQ-EXE-008, REQ-OPS-002; INV-03, INV-17): while a tenant's kill switch is on, the Writer issues no grant and writes nothing for a
// decision that carries write steps. A decision with no step (a read, an explanation, a refusal) still records. The switch is the tenant's own and is read inside the
// caller's transaction, at the time of the write. Real PostgreSQL, real tenant logins, the real hash chain. All data is SYNTHETIC.

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/trust/decide"
	"github.com/Noemai-org/noema/services/core/trust/grants"
	"github.com/Noemai-org/noema/services/core/trust/recorder"
)

const otherTenant = "ten_bb"

// setSwitch sets one tenant's kill switch as the administrator (the control plane is not built yet; EXE-06 is the Trust side only).
func (w *world) setSwitch(t *testing.T, tenantID string, on bool) {
	t.Helper()
	if _, err := w.admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ($1, 'X') ON CONFLICT (tenant_id) DO NOTHING`, tenantID); err != nil {
		t.Fatal(err)
	}
	if _, err := w.admin.Exec(ctx(t), `UPDATE tenant SET kill_switch = $2 WHERE tenant_id = $1`, tenantID, on); err != nil {
		t.Fatal(err)
	}
}

func (w *world) countFor(t *testing.T, q, tenantID string) int {
	t.Helper()
	var n int
	if err := w.admin.QueryRow(ctx(t), q, tenantID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// writeVia is write through a given pool (the login of another tenant).
func (w *world) writeVia(t *testing.T, pool *pgxpool.Pool, r recorder.Request) (recorder.Result, error) {
	t.Helper()
	tx, err := pool.Begin(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	res, werr := w.w.Write(ctx(t), tx, r)
	if werr != nil {
		_ = tx.Rollback(ctx(t))
		return res, werr
	}
	if err := tx.Commit(ctx(t)); err != nil {
		t.Fatal(err)
	}
	return res, nil
}

func (w *world) grantIssuedEvents(t *testing.T, tenantID string) int {
	t.Helper()
	return w.countFor(t, `SELECT count(*) FROM record_event WHERE tenant_id = $1 AND event_type = 'GRANT_ISSUED'`, tenantID)
}

// PUS-06: with the switch on, a decision with steps is refused with ErrWritesDisabled and nothing is written: zero GRANT_ISSUED events, zero grants, zero decision
// rows, zero events of any kind. The refusal is the same for one step and for two, and does not depend on the caller committing.
func TestPUS06AKillSwitchThatIsOnIssuesNoGrantAndWritesNothingForADecisionWithSteps(t *testing.T) {
	w := newWorld(t)
	w.setSwitch(t, tenant, true)
	b := evaluated(t)
	second := step(t)
	second.PlanStep = 2
	for _, c := range []struct {
		name, intent string
		steps        []grants.Step
	}{{"one step", "int_01A", []grants.Step{step(t)}}, {"two steps", "int_01B", []grants.Step{step(t), second}}} {
		name := c.name
		r := request(t, decision(t, decide.Allow, b), b, c.steps...)
		r.IntentID = c.intent // distinct intents, so each is a decision of its own (A186) and a leaked first write cannot hide the second
		res, err := w.write(t, r, true)
		if !errors.Is(err, recorder.ErrWritesDisabled) {
			t.Errorf("%s: err %v, want ErrWritesDisabled", name, err)
		}
		if len(res.Grants) != 0 {
			t.Errorf("%s: %d grants returned", name, len(res.Grants))
		}
	}
	if n := w.grantIssuedEvents(t, tenant); n != 0 {
		t.Fatalf("%d GRANT_ISSUED events while the switch is on, want 0", n)
	}
	w.nothingRemains(t, "the kill switch is on")
	if n := w.count(t, `SELECT count(*) FROM record_event`); n != 0 {
		t.Errorf("%d Record events of any kind after a refusal, want 0", n)
	}
}

// The refusal comes before the first write, not after it with the caller's rollback to clean up: inside the caller's own transaction, at the moment Write returns, no decision row
// and no grant row exists yet.
func TestPUS06TheRefusalIsMadeBeforeTheFirstWriteInTheCallersTransaction(t *testing.T) {
	w := newWorld(t)
	w.setSwitch(t, tenant, true)
	b := evaluated(t)
	tx, err := w.trust.Begin(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx(t)) }()
	if _, err := w.w.Write(ctx(t), tx, request(t, decision(t, decide.Allow, b), b, step(t))); !errors.Is(err, recorder.ErrWritesDisabled) {
		t.Fatalf("err %v, want ErrWritesDisabled", err)
	}
	for _, table := range []string{"decision_record", "decision_grant", "confirmation_spend"} {
		var n int
		if err := tx.QueryRow(ctx(t), `SELECT count(*) FROM `+table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Errorf("%d rows in %s inside the caller's transaction after the refusal, want 0", n, table)
		}
	}
}

// A decision with no write step still records while the switch is on: reads and explanations continue (REQ-EXE-008). An ALLOW with no step, a REJECT and a WAIT each write
// their decision record and their DECISION_MADE event, and never a grant.
func TestPUS06ADecisionWithNoStepStillRecordsWhileTheSwitchIsOn(t *testing.T) {
	w := newWorld(t)
	w.setSwitch(t, tenant, true)
	for i, o := range []decide.Outcome{decide.Allow, decide.Reject, decide.Wait} {
		b := evaluated(t)
		r := request(t, decision(t, o, b), b)
		r.IntentID = []string{"int_01A", "int_01B", "int_01C"}[i] // one decision per input tuple (A186): distinct intents are distinct decisions
		res, err := w.write(t, r, true)
		if err != nil || len(res.Grants) != 0 || string(res.Record.Outcome) != string(o) {
			t.Fatalf("%s with no step: %+v %v", o, res, err)
		}
	}
	if n := w.count(t, `SELECT count(*) FROM decision_record`); n != 3 {
		t.Errorf("%d decision records, want 3", n)
	}
	if n := w.count(t, `SELECT count(*) FROM decision_grant`); n != 0 {
		t.Errorf("%d grants, want 0", n)
	}
	got := w.events(t)
	if len(got) != 3 {
		t.Fatalf("events %v, want three DECISION_MADE", got)
	}
	for _, e := range got {
		if e[:13] != "DECISION_MADE" {
			t.Errorf("event %s, want DECISION_MADE", e)
		}
	}
	if n := w.grantIssuedEvents(t, tenant); n != 0 {
		t.Errorf("%d GRANT_ISSUED events, want 0", n)
	}
}

// Switch off: the write is normal, grants and GRANT_ISSUED events included. The switch is read at the time of the write and is not remembered: the same request is refused
// while the switch is on and written when it is off, and a switch turned on later stops the next write.
func TestPUS06TheSwitchIsReadAtEveryWriteAndOffMeansNormal(t *testing.T) {
	w := newWorld(t)
	b := evaluated(t)
	mk := func(intent string) recorder.Request {
		r := request(t, decision(t, decide.Allow, b), b, step(t))
		r.IntentID = intent
		return r
	}
	w.setSwitch(t, tenant, false)
	if res, err := w.write(t, mk("int_01A"), true); err != nil || len(res.Grants) != 1 {
		t.Fatalf("switch off: %+v %v", res, err)
	}
	w.setSwitch(t, tenant, true)
	if _, err := w.write(t, mk("int_01B"), true); !errors.Is(err, recorder.ErrWritesDisabled) {
		t.Fatalf("switch turned on after a write: %v", err)
	}
	w.setSwitch(t, tenant, false) // a superuser (the operator's credential) can clear; no service login can (migration 0017)
	if res, err := w.write(t, mk("int_01B"), true); err != nil || len(res.Grants) != 1 {
		t.Fatalf("switch cleared: %+v %v", res, err)
	}
	if n := w.grantIssuedEvents(t, tenant); n != 2 {
		t.Errorf("%d GRANT_ISSUED events, want 2 (one per allowed write)", n)
	}
	if n := w.count(t, `SELECT count(*) FROM decision_record`); n != 2 {
		t.Errorf("%d decision records, want 2: the refused write left none", n)
	}
	if n := w.count(t, `SELECT count(*) FROM decision_grant`); n != 2 {
		t.Errorf("%d grants, want 2", n)
	}
}

// The switch is the tenant's own (INV-12; REQ-SEC-004): another tenant's switch on does not stop this tenant, and this tenant's switch on does not stop the other.
func TestPUS06AnotherTenantsSwitchDoesNotAffectThisTenant(t *testing.T) {
	w := newWorld(t)
	w.setSwitch(t, otherTenant, true)
	b := evaluated(t)
	if res, err := w.write(t, request(t, decision(t, decide.Allow, b), b, step(t)), true); err != nil || len(res.Grants) != 1 {
		t.Fatalf("ten_aa with only ten_bb's switch on: %+v %v", res, err)
	}
	if n := w.grantIssuedEvents(t, tenant); n != 1 {
		t.Errorf("%d GRANT_ISSUED events for ten_aa, want 1", n)
	}
	// the other direction: ten_aa's switch on, ten_bb's off; ten_bb writes through its own login
	w.setSwitch(t, tenant, true)
	w.setSwitch(t, otherTenant, false)
	bb := newTenantTrust(t, w, otherTenant)
	rb := request(t, decision(t, decide.Allow, b), b, step(t))
	rb.TenantID = otherTenant
	if res, err := w.writeVia(t, bb, rb); err != nil || len(res.Grants) != 1 {
		t.Fatalf("ten_bb with only ten_aa's switch on: %+v %v", res, err)
	}
	if n := w.grantIssuedEvents(t, otherTenant); n != 1 {
		t.Errorf("%d GRANT_ISSUED events for ten_bb, want 1", n)
	}
	// and ten_bb's switch on stops ten_bb, as ten_aa's stops ten_aa
	w.setSwitch(t, otherTenant, true)
	rb2 := request(t, decision(t, decide.Allow, b), b, step(t))
	rb2.TenantID, rb2.IntentID = otherTenant, "int_01B"
	if _, err := w.writeVia(t, bb, rb2); !errors.Is(err, recorder.ErrWritesDisabled) {
		t.Fatalf("ten_bb with its own switch on: %v", err)
	}
}

// The read is made on the caller's transaction and nowhere else: Write is given a transaction and no pool, so it has no other connection to read from. At READ COMMITTED a
// transaction that is already open when the switch is turned on still sees it, because the read is a statement of its own made at write time. At REPEATABLE READ the read
// sees the transaction's own snapshot, the one its writes see; a flip after that snapshot is not seen by this transaction. That is the window of a check-then-write that
// holds no lock on the tenant row (the Trust login has no UPDATE privilege on it, so it cannot take FOR SHARE); it is closed by Execution, which reads the switch again
// inside the spend (E6, E8) and voids the grant. This test pins both behaviours so that neither changes silently.
func TestPUS06TheSwitchIsReadInsideTheCallersTransactionAtWriteTime(t *testing.T) {
	w := newWorld(t)
	b := evaluated(t)

	// READ COMMITTED: the transaction is open and has made an earlier statement; the switch is turned on and committed after it; the write meets it.
	tx, err := w.trust.Begin(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx(t), `SELECT 1`); err != nil {
		t.Fatal(err)
	}
	w.setSwitch(t, tenant, true)
	if _, err := w.w.Write(ctx(t), tx, request(t, decision(t, decide.Allow, b), b, step(t))); !errors.Is(err, recorder.ErrWritesDisabled) {
		t.Errorf("READ COMMITTED, switch turned on after the transaction began: %v", err)
	}
	_ = tx.Rollback(ctx(t))

	// REPEATABLE READ: the snapshot is taken before the switch is turned on; the write sees the snapshot, so it is written inside this transaction (and rolled back here).
	w.setSwitch(t, tenant, false)
	rr, err := w.trust.BeginTx(ctx(t), pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rr.Exec(ctx(t), `SELECT 1 FROM tenant`); err != nil {
		t.Fatal(err)
	}
	w.setSwitch(t, tenant, true)
	r := request(t, decision(t, decide.Allow, b), b, step(t))
	r.IntentID = "int_01RR"
	if res, err := w.w.Write(ctx(t), rr, r); err != nil || len(res.Grants) != 1 {
		t.Errorf("REPEATABLE READ, switch turned on after the snapshot: %+v %v", res, err)
	}
	_ = rr.Rollback(ctx(t))
	w.nothingRemains(t, "both transactions were rolled back")
}
