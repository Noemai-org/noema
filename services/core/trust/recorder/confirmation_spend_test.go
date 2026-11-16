package recorder_test

// TRU-07 follow-up (noema#572, EL's answer to Q-C; migration 0028; REQ-TRU-008, REQ-TRU-009; INV-03, INV-17, INV-25, INV-21): a given confirmation is spent
// once. The spend is written in the Decision Record's own transaction, by the decision that the confirmation authorised, and the database refuses a second
// spend of the same confirmation event of the same tenant. A retry of the same input returns the stored decision and spends nothing. Real PostgreSQL, real
// tenant logins, the real hash chain, as in recorder_test.go. All data is SYNTHETIC.

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/trust/decide"
	"github.com/Noemai-org/noema/services/core/trust/recorder"
)

const given = "cfe_01SPENT"

// allowWith is an ALLOW request on the given evidence snapshot that follows the confirmation event (empty: no confirmation).
func allowWith(t *testing.T, snapshot, event string) recorder.Request {
	t.Helper()
	b := evaluatedWith(t, snapshot, "3.1")
	r := request(t, decision(t, decide.Allow, b), b, step(t))
	r.ConfirmationEventID = event
	if event != "" {
		r.ConfirmationPlanHash = h(4) // the plan hash of step(t): the confirmation was given for the plan T14 built
	}
	return r
}

// newTenantTrust is a Trust login mapped to another tenant of the same database.
func newTenantTrust(t *testing.T, w *world, tenantID string) *pgxpool.Pool {
	t.Helper()
	return testpg.TenantLogin(t, w.admin, w.dsn, "trust_rw", tenantID)
}

func (w *world) spends(t *testing.T) int {
	t.Helper()
	return w.count(t, `SELECT count(*) FROM confirmation_spend`)
}

// The confirmation is spent by the decision it authorised, in the same transaction, and the grant names it.
func TestAGivenConfirmationIsSpentByTheDecisionItAuthorisedAndTheGrantNamesIt(t *testing.T) {
	w := newWorld(t)
	res, err := w.write(t, allowWith(t, "evs_01ABC", given), true)
	if err != nil {
		t.Fatal(err)
	}
	if n := w.spends(t); n != 1 {
		t.Fatalf("%d spends, want 1", n)
	}
	var decisionID, event string
	if err := w.admin.QueryRow(ctx(t), `SELECT decision_id, confirmation_event_id FROM confirmation_spend WHERE tenant_id = $1`, tenant).Scan(&decisionID, &event); err != nil {
		t.Fatal(err)
	}
	if decisionID != string(res.Record.DecisionId) || event != given {
		t.Errorf("the spend names decision %q and event %q; want %q and %q", decisionID, event, res.Record.DecisionId, given)
	}
	if len(res.Grants) != 1 || res.Grants[0].ConfirmationEventId == nil || string(*res.Grants[0].ConfirmationEventId) != given {
		t.Errorf("the grant does not follow the confirmation: %+v", res.Grants)
	}
}

// A second decision (another snapshot, so another input) that names the same confirmation is refused with the typed error, and nothing of it remains: no
// record, no grant, no spend, no event.
func TestASecondDecisionThatNamesTheSameConfirmationIsRefusedAndLeavesNothing(t *testing.T) {
	w := newWorld(t)
	if _, err := w.write(t, allowWith(t, "evs_01ABC", given), true); err != nil {
		t.Fatal(err)
	}
	eventsBefore := w.events(t)
	_, err := w.write(t, allowWith(t, "evs_02XYZ", given), true)
	if !errors.Is(err, recorder.ErrConfirmationSpent) {
		t.Fatalf("a second spend of the confirmation: err %v, want ErrConfirmationSpent", err)
	}
	if n := w.count(t, `SELECT count(*) FROM decision_record`); n != 1 {
		t.Errorf("%d decision records, want 1", n)
	}
	if n := w.count(t, `SELECT count(*) FROM decision_grant`); n != 1 {
		t.Errorf("%d grants, want 1", n)
	}
	if n := w.spends(t); n != 1 {
		t.Errorf("%d spends, want 1", n)
	}
	if after := w.events(t); len(after) != len(eventsBefore) {
		t.Errorf("the Record gained events from a refused spend: before %v, after %v", eventsBefore, after)
	}
}

// Two evaluations of two inputs race for one confirmation: exactly one ALLOW commits and the other gets ErrConfirmationSpent (INV-17, real PostgreSQL).
func TestTwoDecisionsRacingForOneConfirmationLeaveExactlyOneAllow(t *testing.T) {
	w := newWorld(t)
	type result struct{ err error }
	start := make(chan struct{})
	out := make(chan result, 2)
	var wg sync.WaitGroup
	for _, snapshot := range []string{"evs_01ABC", "evs_02XYZ"} {
		r := allowWith(t, snapshot, given)
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			tx, err := w.trust.Begin(ctx(t))
			if err != nil {
				out <- result{err}
				return
			}
			if _, werr := w.w.Write(ctx(t), tx, r); werr != nil {
				_ = tx.Rollback(ctx(t))
				out <- result{werr}
				return
			}
			out <- result{tx.Commit(ctx(t))}
		}()
	}
	close(start)
	wg.Wait()
	close(out)
	var won, spent int
	for r := range out {
		switch {
		case r.err == nil:
			won++
		case errors.Is(r.err, recorder.ErrConfirmationSpent):
			spent++
		default:
			t.Errorf("unexpected error: %v", r.err)
		}
	}
	if won != 1 || spent != 1 {
		t.Fatalf("%d decisions committed and %d refused as spent; want 1 and 1", won, spent)
	}
	if n := w.count(t, `SELECT count(*) FROM decision_record`); n != 1 {
		t.Errorf("%d decision records, want 1", n)
	}
	if n := w.count(t, `SELECT count(*) FROM decision_grant`); n != 1 {
		t.Errorf("%d grants, want 1", n)
	}
	if n := w.spends(t); n != 1 {
		t.Errorf("%d spends, want 1", n)
	}
}

// A186: a retry of the same input returns the stored decision. It must not meet the spend's unique key, and the count stays 1.
func TestARetryOfTheSameInputReturnsTheStoredDecisionAndSpendsNothingMore(t *testing.T) {
	w := newWorld(t)
	first, err := w.write(t, allowWith(t, "evs_01ABC", given), true)
	if err != nil {
		t.Fatal(err)
	}
	second, err := w.write(t, allowWith(t, "evs_01ABC", given), true)
	if err != nil {
		t.Fatalf("the retry of the same input: %v", err)
	}
	if second.Record.DecisionId != first.Record.DecisionId {
		t.Errorf("the retry returned decision %s, not the stored %s", second.Record.DecisionId, first.Record.DecisionId)
	}
	if n := w.spends(t); n != 1 {
		t.Errorf("%d spends, want 1", n)
	}
}

// Only an ALLOW spends a confirmation, and only when it names one: a refusal that names a confirmation leaves it unspent, an ALLOW without one writes no spend.
func TestAConfirmationIsSpentOnlyByAnAllowThatNamesIt(t *testing.T) {
	w := newWorld(t)
	b := evaluatedWith(t, "evs_01ABC", "3.1")
	refusal := request(t, decision(t, decide.Reject, b), b)
	refusal.ConfirmationEventID = given
	if _, err := w.write(t, refusal, true); err != nil {
		t.Fatal(err)
	}
	if n := w.spends(t); n != 0 {
		t.Fatalf("a REJECT spent the confirmation: %d spends", n)
	}
	if _, err := w.write(t, allowWith(t, "evs_02XYZ", ""), true); err != nil {
		t.Fatal(err)
	}
	if n := w.spends(t); n != 0 {
		t.Fatalf("an ALLOW naming no confirmation wrote %d spends", n)
	}
	if _, err := w.write(t, allowWith(t, "evs_03DEF", given), true); err != nil {
		t.Fatalf("the confirmation the REJECT did not spend: %v", err)
	}
	if n := w.spends(t); n != 1 {
		t.Errorf("%d spends, want 1", n)
	}
}

// A confirmation event ID that is not in the contract's form is refused before anything is written.
func TestAMalformedConfirmationEventIDIsRefusedBeforeAnythingIsWritten(t *testing.T) {
	w := newWorld(t)
	for _, bad := range []string{"cfe_", "evt_01SPENT", "cfe_01 SPENT", "CFE_01SPENT"} {
		_, err := w.write(t, allowWith(t, "evs_01ABC", bad), true)
		if !errors.Is(err, recorder.ErrRequest) {
			t.Errorf("%q: err %v, want ErrRequest", bad, err)
		}
	}
	w.nothingRemains(t, "a malformed confirmation event ID")
	if n := w.spends(t); n != 0 {
		t.Errorf("%d spends remain", n)
	}
}

// pgRefusal is the SQLSTATE and the message of a database refusal; the test fails when err is not one.
func pgRefusal(t *testing.T, what string, err error) (string, string) {
	t.Helper()
	var pg *pgconn.PgError
	if !errors.As(err, &pg) {
		t.Fatalf("%s: err %v, want a database refusal", what, err)
	}
	return pg.Code, pg.Message
}

const (
	sqlstateIntegrity  = "23000" // integrity_constraint_violation
	sqlstatePrivilege  = "42501" // insufficient_privilege
	insertSpendOfOther = `INSERT INTO confirmation_spend (tenant_id, confirmation_event_id, decision_id, plan_hash) VALUES ($1, 'cfe_01OTHER', $2, $3)`
)

// The spend is insert-only. The administrator is refused by the trigger itself (reject_mutation says so); the Trust login is refused by its privileges (it has SELECT and INSERT only).
func TestTheSpendIsInsertOnly(t *testing.T) {
	w := newWorld(t)
	if _, err := w.write(t, allowWith(t, "evs_01ABC", given), true); err != nil {
		t.Fatal(err)
	}
	for name, stmt := range map[string]string{
		"update":   `UPDATE confirmation_spend SET confirmation_event_id = 'cfe_01OTHER'`,
		"delete":   `DELETE FROM confirmation_spend`,
		"truncate": `TRUNCATE confirmation_spend`,
	} {
		_, err := w.admin.Exec(ctx(t), stmt)
		if code, msg := pgRefusal(t, name+" by the administrator", err); code != sqlstateIntegrity || !strings.Contains(msg, "insert-only") {
			t.Errorf("%s by the administrator: %s %q, want %s from the insert-only trigger", name, code, msg, sqlstateIntegrity)
		}
		_, err = w.trust.Exec(ctx(t), stmt)
		if code, _ := pgRefusal(t, name+" by the Trust login", err); code != sqlstatePrivilege {
			t.Errorf("%s by the Trust login: %s, want %s (no privilege)", name, code, sqlstatePrivilege)
		}
	}
	if n := w.spends(t); n != 1 {
		t.Errorf("%d spends remain after the refused changes, want 1", n)
	}
}

// Only a recorded ALLOW can be spent: a spend that names a REJECT decision is refused by the requires-allow trigger, with its own code and message.
func TestASpendCanNameOnlyARecordedAllowDecision(t *testing.T) {
	w := newWorld(t)
	b := evaluatedWith(t, "evs_02XYZ", "3.1")
	refused, err := w.write(t, request(t, decision(t, decide.Reject, b), b), true)
	if err != nil {
		t.Fatal(err)
	}
	_, err = w.trust.Exec(ctx(t), insertSpendOfOther, tenant, string(refused.Record.DecisionId), string(h(4)))
	if code, msg := pgRefusal(t, "a spend for a REJECT decision", err); code != sqlstateIntegrity || !strings.Contains(msg, "not a recorded ALLOW decision") {
		t.Errorf("a spend for a REJECT decision: %s %q, want %s from the requires-allow trigger", code, msg, sqlstateIntegrity)
	}
	if _, err := w.trust.Exec(ctx(t), insertSpendOfOther, tenant, "dec_01NOSUCH", string(h(4))); err == nil {
		t.Error("a spend for a decision that does not exist was accepted")
	}
	if n := w.spends(t); n != 0 {
		t.Errorf("%d spends exist, want 0", n)
	}
}

// The tenant binding, each layer on its own (Security Reviewer on noema#603): the requires-allow trigger would also refuse ten_aa's login writing for ten_bb (the decision is
// ten_aa's), so it is switched off in turn, the way a hand-written or mistaken change could, and the guard and then row-level security must each refuse by themselves.
func TestTheTenantBindingIsEnforcedByTheGuardAndByRowSecurityEachOnItsOwn(t *testing.T) {
	w := newWorld(t)
	allowed, err := w.write(t, allowWith(t, "evs_01ABC", given), true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ('ten_bb', 'B')`); err != nil {
		t.Fatal(err)
	}
	forTenantBB := func() error {
		_, err := w.trust.Exec(ctx(t), `INSERT INTO confirmation_spend (tenant_id, confirmation_event_id, decision_id, plan_hash) VALUES ('ten_bb', 'cfe_01OTHER', $1, $2)`, string(allowed.Record.DecisionId), string(h(4)))
		return err
	}
	switchTrigger := func(name, state string) {
		t.Helper()
		if _, err := w.admin.Exec(ctx(t), `ALTER TABLE confirmation_spend `+state+` TRIGGER `+name); err != nil {
			t.Fatal(err)
		}
	}
	// 1. nothing is switched off: refused (by whichever layer is first)
	if err := forTenantBB(); err == nil {
		t.Fatal("ten_aa's login wrote a spend for ten_bb")
	}
	// 2. the requires-allow trigger off: the tenant guard refuses, by its own message
	switchTrigger("confirmation_spend_requires_allow", "DISABLE")
	code, msg := pgRefusal(t, "the tenant guard", forTenantBB())
	if code != sqlstatePrivilege || !strings.Contains(msg, "may not spend confirmations for tenant ten_bb") {
		t.Errorf("with the requires-allow trigger off: %s %q, want %s from the tenant guard", code, msg, sqlstatePrivilege)
	}
	// 3. the guard off too: row-level security refuses, by its own message
	switchTrigger("confirmation_spend_tenant_guard", "DISABLE")
	code, msg = pgRefusal(t, "row-level security", forTenantBB())
	if code != sqlstatePrivilege || !strings.Contains(msg, "row-level security") {
		t.Errorf("with the guard off too: %s %q, want %s from row-level security", code, msg, sqlstatePrivilege)
	}
	switchTrigger("confirmation_spend_tenant_guard", "ENABLE")
	switchTrigger("confirmation_spend_requires_allow", "ENABLE")
	if n := w.count(t, `SELECT count(*) FROM confirmation_spend WHERE tenant_id = 'ten_bb'`); n != 0 {
		t.Errorf("%d spends exist for ten_bb", n)
	}
	// ten_bb's own login sees none of ten_aa's spends
	other := newTenantTrust(t, w, "ten_bb")
	var seen int
	if err := other.QueryRow(ctx(t), `SELECT count(*) FROM confirmation_spend`).Scan(&seen); err != nil {
		t.Fatal(err)
	}
	if seen != 0 {
		t.Errorf("ten_bb's Trust login sees %d of ten_aa's spends", seen)
	}
}

// A233 (interim): a retry of the same input that names a different confirmation than the stored decision followed is refused as a typed conflict (INV-21: the same input gives the
// same decision), not returned as if nothing differed. The stored decision stays, and nothing is spent or written.
func TestARetryNamingAnotherConfirmationThanTheStoredDecisionIsATypedConflict(t *testing.T) {
	w := newWorld(t)
	if _, err := w.write(t, allowWith(t, "evs_01ABC", given), true); err != nil {
		t.Fatal(err)
	}
	eventsBefore := w.events(t)
	for name, event := range map[string]string{"another confirmation": "cfe_01OTHER", "none": ""} {
		_, err := w.write(t, allowWith(t, "evs_01ABC", event), true)
		if !errors.Is(err, recorder.ErrDecisionConflict) {
			t.Errorf("a retry naming %s: err %v, want ErrDecisionConflict", name, err)
		}
	}
	if n := w.spends(t); n != 1 {
		t.Errorf("%d spends, want 1", n)
	}
	if n := w.count(t, `SELECT count(*) FROM decision_record`); n != 1 {
		t.Errorf("%d decision records, want 1", n)
	}
	if after := w.events(t); len(after) != len(eventsBefore) {
		t.Errorf("the Record gained events from a refused retry: before %v, after %v", eventsBefore, after)
	}
	// a stored refusal that named a confirmation (it spends none) is returned for its own retry, as before
	b := evaluatedWith(t, "evs_05GHI", "3.1")
	refusal := request(t, decision(t, decide.Reject, b), b)
	refusal.ConfirmationEventID = "cfe_01REFUSED"
	first, err := w.write(t, refusal, true)
	if err != nil {
		t.Fatal(err)
	}
	if again, err := w.write(t, refusal, true); err != nil || again.Record.DecisionId != first.Record.DecisionId {
		t.Errorf("the retry of a stored refusal: %v", err)
	}
}

// A230 (interim): the writer refuses an ALLOW whose confirmation was given for another plan than the one T14 built. Nothing is written, so no spend, no record, no grant.
func TestAnAllowWhoseConfirmationWasGivenForAnotherPlanIsRefused(t *testing.T) {
	w := newWorld(t)
	for name, mutate := range map[string]func(*recorder.Request){
		"another plan hash": func(r *recorder.Request) { r.ConfirmationPlanHash = h(9) },
		"no plan hash":      func(r *recorder.Request) { r.ConfirmationPlanHash = "" },
		"malformed hash":    func(r *recorder.Request) { r.ConfirmationPlanHash = "sha256:abc" },
		"a second step of another plan": func(r *recorder.Request) {
			second := step(t)
			second.PlanHash, second.PlanStep = h(5), 2
			r.Steps = append(r.Steps, second)
		},
		"no steps (a confirmation is for a write)": func(r *recorder.Request) { r.Steps = nil },
	} {
		r := allowWith(t, "evs_01ABC", given)
		mutate(&r)
		if _, err := w.write(t, r, true); !errors.Is(err, recorder.ErrConfirmationPlanMismatch) {
			t.Errorf("%s: err %v, want ErrConfirmationPlanMismatch", name, err)
		}
	}
	w.nothingRemains(t, "a confirmation given for another plan")
	if n := w.spends(t); n != 0 {
		t.Errorf("%d spends remain", n)
	}
	// a plan hash with no confirmation is a malformed request
	r := allowWith(t, "evs_01ABC", "")
	r.ConfirmationPlanHash = h(4)
	if _, err := w.write(t, r, true); !errors.Is(err, recorder.ErrRequest) {
		t.Errorf("a plan hash with no confirmation: err %v, want ErrRequest", err)
	}
	// the matching plan is accepted, and the spend records the plan the confirmation was given for
	if _, err := w.write(t, allowWith(t, "evs_01ABC", given), true); err != nil {
		t.Fatalf("the matching plan: %v", err)
	}
	var plan string
	if err := w.admin.QueryRow(ctx(t), `SELECT plan_hash FROM confirmation_spend WHERE tenant_id = $1`, tenant).Scan(&plan); err != nil {
		t.Fatal(err)
	}
	if plan != string(h(4)) {
		t.Errorf("the spend names plan %q, want %q", plan, h(4))
	}
}
