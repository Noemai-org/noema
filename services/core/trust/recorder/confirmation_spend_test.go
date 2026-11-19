package recorder_test

// TRU-07 follow-up (noema#572, EL's answer to Q-C; migration 0028; REQ-TRU-008, REQ-TRU-009; INV-03, INV-17, INV-25, INV-21): a given confirmation is spent
// once. The spend is written in the Decision Record's own transaction, by the decision that the confirmation authorised, and the database refuses a second
// spend of the same confirmation event of the same tenant. A retry of the same input returns the stored decision and spends nothing. Real PostgreSQL, real
// tenant logins, the real hash chain, as in recorder_test.go. All data is SYNTHETIC.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/record/chain"
	"github.com/Noemai-org/noema/services/core/trust/confirm"
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

// realNow is the database's wall clock as the test sees it: the expiry check of a confirmation is made by the database (statement_timestamp), so a confirmation's times are real ones. It goes
// through platform/clock, the one place that reads the system clock (lint [clock]).
func realNow() time.Time { return clock.System{}.Now() }

// policyVersion is the policy version a confirmation binds for evaluatedWith(t, _, "3.1") (A341, interim): the composite of every policy version of the basis, "ps:" and the SHA-256
// hex of the RFC 8785 form of the sorted, distinct list of "policy_id@version" strings, here ["fin.pol@3.1"]. The literal was computed by an independent Python implementation, not by
// the function under test. (The decision record and the grant still bind the single policy fin.pol@3.1.)
const policyVersion = "ps:c1c966ac3b0b555cd16913245fcdf40ebe103d30ea356053aca7a1be0b76967e"

// policyVersionTwo is the composite for a basis of two policies, fin.pol@3.1 and fin.other@2.0: ["fin.other@2.0","fin.pol@3.1"] (independent Python).
const policyVersionTwo = "ps:ec91c19602ebb2caf6db5ffb9113f0c26e54328be6d9c03c1c064485ac94a2cb"

// validBinding is the binding of the one step of allowWith, as the confirmation store computes it when it creates the request (SoT 43.2a, trust/confirm).
func validBinding() confirm.BindingInput {
	return confirm.BindingInput{PrincipalID: "usr_88213", IntentHash: string(h(1)), CapabilityID: capability, TargetRef: target, ParamsHash: string(h(2)),
		EvidenceSetHash: string(h(3)), PlanHash: string(h(4)), PolicyVersion: policyVersion}
}

func bindingHashOf(t *testing.T, in confirm.BindingInput) string {
	t.Helper()
	b, err := confirm.BindingHash(in)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// recordConfirmation records, through the supported path and as the given Trust login, an accepted confirmation that resolved to a fresh evaluation.
func recordConfirmation(t *testing.T, pool *pgxpool.Pool, tenantID, event string, in confirm.BindingInput, expires time.Time) error {
	t.Helper()
	tx, err := pool.Begin(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	err = recorder.RecordConfirmation(ctx(t), tx, recorder.Confirmation{TenantID: tenantID, EventID: event, RequestID: "cfr_" + strings.TrimPrefix(event, "cfe_"),
		BindingHash: bindingHashOf(t, in), AcceptedAt: realNow().Add(-time.Minute), ExpiresAt: expires})
	if err != nil {
		_ = tx.Rollback(ctx(t))
		return err
	}
	return tx.Commit(ctx(t))
}

// confirmed records a valid, unexpired confirmation of ten_aa for the binding of allowWith.
func (w *world) confirmed(t *testing.T, event string) {
	t.Helper()
	if err := recordConfirmation(t, w.trust, tenant, event, validBinding(), realNow().Add(time.Hour)); err != nil {
		t.Fatalf("recording the confirmation %s: %v", event, err)
	}
}

func (w *world) spends(t *testing.T) int {
	t.Helper()
	return w.count(t, `SELECT count(*) FROM confirmation_spend`)
}

// The confirmation is spent by the decision it authorised, in the same transaction, and the grant names it.
func TestAGivenConfirmationIsSpentByTheDecisionItAuthorisedAndTheGrantNamesIt(t *testing.T) {
	w := newWorld(t)
	w.confirmed(t, given)
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
	var bound string
	if err := w.admin.QueryRow(ctx(t), `SELECT binding_hash FROM confirmation_spend WHERE tenant_id = $1`, tenant).Scan(&bound); err != nil {
		t.Fatal(err)
	}
	if want := bindingHashOf(t, validBinding()); bound != want {
		t.Errorf("the spend records binding %q, want %q", bound, want)
	}
}

// The tamper-evident chain shows the spend (Security Reviewer on noema#603): DECISION_MADE names the confirmation event and the binding it was spent against, and GRANT_ISSUED names the
// event its grant follows. PERD 32.2 has no spend event type, so the spend rides on these two events of the same transaction; an ALLOW with no confirmation carries neither member.
func TestTheRecordShowsWhichConfirmationADecisionSpent(t *testing.T) {
	w := newWorld(t)
	w.confirmed(t, given)
	if _, err := w.write(t, allowWith(t, "evs_01ABC", given), true); err != nil {
		t.Fatal(err)
	}
	if _, err := w.write(t, allowWith(t, "evs_02XYZ", ""), true); err != nil {
		t.Fatal(err)
	}
	rows, err := chain.ReadChain(ctx(t), w.reader)
	if err != nil {
		t.Fatal(err)
	}
	var withEvent, withoutEvent int
	for _, r := range rows {
		if r.Payload == nil || (r.EventType != "DECISION_MADE" && r.EventType != "GRANT_ISSUED") {
			continue
		}
		var p map[string]any
		if err := json.Unmarshal([]byte(*r.Payload), &p); err != nil {
			t.Fatal(err)
		}
		event, hasEvent := p["confirmation_event_id"]
		binding, hasBinding := p["confirmation_binding_hash"]
		switch {
		case hasEvent:
			withEvent++
			if event != given {
				t.Errorf("%s names confirmation %v, want %s", r.EventType, event, given)
			}
			if r.EventType == "DECISION_MADE" && binding != bindingHashOf(t, validBinding()) {
				t.Errorf("DECISION_MADE names binding %v, want %s", binding, bindingHashOf(t, validBinding()))
			}
			if r.EventType == "GRANT_ISSUED" && hasBinding {
				t.Errorf("GRANT_ISSUED names a binding: %s", *r.Payload)
			}
		default:
			withoutEvent++
			if hasBinding {
				t.Errorf("an event of a decision with no confirmation names a binding: %s", *r.Payload)
			}
		}
	}
	if withEvent != 2 || withoutEvent != 2 {
		t.Errorf("%d events name the confirmation and %d do not; want 2 (DECISION_MADE and GRANT_ISSUED) and 2 of the other decision", withEvent, withoutEvent)
	}
}

// The writer checks the confirmation against the database (Security Reviewer on noema#603, noema#572 blocking 1): it must exist for this tenant, must not have expired by the
// database's clock, and must have been given for exactly this action: the eight values of SoT 43.2a. Anything else is ErrConfirmationNotValid and nothing is written.
func TestAConfirmationThatDoesNotExistIsExpiredOrWasGivenForAnotherActionIsRefused(t *testing.T) {
	w := newWorld(t)
	if _, err := w.admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ('ten_bb', 'B')`); err != nil {
		t.Fatal(err)
	}
	trustBB := newTenantTrust(t, w, "ten_bb")
	expect := func(name, event string) {
		t.Helper()
		if _, err := w.write(t, allowWith(t, "evs_01ABC", event), true); !errors.Is(err, recorder.ErrConfirmationNotValid) {
			t.Errorf("%s: err %v, want ErrConfirmationNotValid", name, err)
		}
	}
	// one that was never recorded
	expect("a confirmation that does not exist", "cfe_01NOSUCH")
	// one that another tenant recorded
	if err := recordConfirmation(t, trustBB, "ten_bb", "cfe_01OFBB", validBinding(), realNow().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	expect("another tenant's confirmation", "cfe_01OFBB")
	// one that expired (by the database's clock, not the caller's)
	if err := recordConfirmation(t, w.trust, tenant, "cfe_01OLD", validBinding(), realNow().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	expect("an expired confirmation", "cfe_01OLD")
	// one given for another action: each of the eight bound values changed in turn
	for name, mutate := range map[string]func(*confirm.BindingInput){
		"another principal":  func(b *confirm.BindingInput) { b.PrincipalID = "usr_99999" },
		"another intent":     func(b *confirm.BindingInput) { b.IntentHash = string(h(7)) },
		"another capability": func(b *confirm.BindingInput) { b.CapabilityID = "fin.cap.lookup_transaction" },
		"another target":     func(b *confirm.BindingInput) { b.TargetRef = "fin.transaction:TX00001" },
		"other parameters":   func(b *confirm.BindingInput) { b.ParamsHash = string(h(8)) },
		"other evidence":     func(b *confirm.BindingInput) { b.EvidenceSetHash = string(h(9)) },
		"another plan":       func(b *confirm.BindingInput) { b.PlanHash = string(h(10)) },
		"another policy":     func(b *confirm.BindingInput) { b.PolicyVersion = "fin.pol@9.9" },
	} {
		b := validBinding()
		mutate(&b)
		event := "cfe_01" + strings.ToUpper(strings.ReplaceAll(strings.ReplaceAll(name, " ", ""), "other", "oth"))
		if err := recordConfirmation(t, w.trust, tenant, event, b, realNow().Add(time.Hour)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		expect(name, event)
	}
	w.nothingRemains(t, "a confirmation that is not valid for this action")
	if n := w.spends(t); n != 0 {
		t.Errorf("%d spends remain", n)
	}
	// a confirmation approves one action: a plan with two steps cannot follow one
	w.confirmed(t, given)
	r := allowWith(t, "evs_01ABC", given)
	second := step(t)
	second.PlanStep = 2
	r.Steps = append(r.Steps, second)
	if _, err := w.write(t, r, true); !errors.Is(err, recorder.ErrConfirmationPlanMismatch) {
		t.Errorf("two steps after one confirmation: err %v, want ErrConfirmationPlanMismatch", err)
	}
	// and the valid one still works, once
	if _, err := w.write(t, allowWith(t, "evs_01ABC", given), true); err != nil {
		t.Fatalf("the valid confirmation: %v", err)
	}
}

// A second decision (another snapshot, so another input) that names the same confirmation is refused with the typed error, and nothing of it remains: no
// record, no grant, no spend, no event.
func TestASecondDecisionThatNamesTheSameConfirmationIsRefusedAndLeavesNothing(t *testing.T) {
	w := newWorld(t)
	w.confirmed(t, given)
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
	w.confirmed(t, given)
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
	w.confirmed(t, given)
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
	w.confirmed(t, given)
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
	insertSpendOfOther = `INSERT INTO confirmation_spend (tenant_id, confirmation_event_id, decision_id, plan_hash, binding_hash) VALUES ($1, 'cfe_01OTHER', $2, $3, $4)`
)

// The spend is insert-only. The administrator is refused by the trigger itself (reject_mutation says so); the Trust login is refused by its privileges (it has SELECT and INSERT only).
func TestTheSpendIsInsertOnly(t *testing.T) {
	w := newWorld(t)
	w.confirmed(t, given)
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
	_, err = w.trust.Exec(ctx(t), insertSpendOfOther, tenant, string(refused.Record.DecisionId), string(h(4)), bindingHashOf(t, validBinding()))
	if code, msg := pgRefusal(t, "a spend for a REJECT decision", err); code != sqlstateIntegrity || !strings.Contains(msg, "not a recorded ALLOW decision") {
		t.Errorf("a spend for a REJECT decision: %s %q, want %s from the requires-allow trigger", code, msg, sqlstateIntegrity)
	}
	// the same four arguments as above (pgx refuses a call with fewer than the statement has placeholders, on the client, before any trigger runs: the first form of this case passed whatever the database did)
	_, err = w.trust.Exec(ctx(t), insertSpendOfOther, tenant, "dec_01NOSUCH", string(h(4)), bindingHashOf(t, validBinding()))
	if code, msg := pgRefusal(t, "a spend for a decision that does not exist", err); code != sqlstateIntegrity || !strings.Contains(msg, "not a recorded ALLOW decision") {
		t.Errorf("a spend for a decision that does not exist: %s %q, want %s from the requires-allow trigger", code, msg, sqlstateIntegrity)
	}
	if n := w.spends(t); n != 0 {
		t.Errorf("%d spends exist, want 0", n)
	}
}

// The tenant binding, each layer on its own (Security Reviewer on noema#603): the requires-allow trigger would also refuse ten_aa's login writing for ten_bb (the decision is
// ten_aa's), so it is switched off in turn, the way a hand-written or mistaken change could, and the guard and then row-level security must each refuse by themselves.
func TestTheTenantBindingIsEnforcedByTheGuardAndByRowSecurityEachOnItsOwn(t *testing.T) {
	w := newWorld(t)
	w.confirmed(t, given)
	allowed, err := w.write(t, allowWith(t, "evs_01ABC", given), true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ('ten_bb', 'B')`); err != nil {
		t.Fatal(err)
	}
	forTenantBB := func() error {
		_, err := w.trust.Exec(ctx(t), `INSERT INTO confirmation_spend (tenant_id, confirmation_event_id, decision_id, plan_hash, binding_hash) VALUES ('ten_bb', 'cfe_01OTHER', $1, $2, $3)`, string(allowed.Record.DecisionId), string(h(4)), bindingHashOf(t, validBinding()))
		return err
	}
	switchTrigger := func(name, state string) {
		t.Helper()
		if _, err := w.admin.Exec(ctx(t), `ALTER TABLE confirmation_spend `+state+` TRIGGER `+name); err != nil {
			t.Fatal(err)
		}
	}
	// a failure part-way must not leave a trigger off for a test that shares the database (Security Reviewer, note 6): every trigger is switched on again when the test ends
	t.Cleanup(func() {
		for _, name := range []string{"confirmation_spend_tenant_guard", "confirmation_spend_requires_confirmation", "confirmation_spend_requires_allow"} {
			if _, err := w.admin.Exec(context.Background(), `ALTER TABLE confirmation_spend ENABLE TRIGGER `+name); err != nil {
				t.Errorf("switching %s back on: %v", name, err)
			}
		}
	})
	// 1. nothing is switched off: refused (by whichever layer is first)
	if err := forTenantBB(); err == nil {
		t.Fatal("ten_aa's login wrote a spend for ten_bb")
	}
	// 2. the requires-allow trigger off: the tenant guard refuses, by its own message
	switchTrigger("confirmation_spend_requires_allow", "DISABLE")
	switchTrigger("confirmation_spend_requires_confirmation", "DISABLE")
	code, msg := pgRefusal(t, "the tenant guard", forTenantBB())
	if code != sqlstatePrivilege || !strings.Contains(msg, "may not write for tenant ten_bb") {
		t.Errorf("with the requires-allow trigger off: %s %q, want %s from the tenant guard", code, msg, sqlstatePrivilege)
	}
	// 3. the guard off too: row-level security refuses, by its own message
	switchTrigger("confirmation_spend_tenant_guard", "DISABLE")
	code, msg = pgRefusal(t, "row-level security", forTenantBB())
	if code != sqlstatePrivilege || !strings.Contains(msg, "row-level security") {
		t.Errorf("with the guard off too: %s %q, want %s from row-level security", code, msg, sqlstatePrivilege)
	}
	switchTrigger("confirmation_spend_tenant_guard", "ENABLE")
	switchTrigger("confirmation_spend_requires_confirmation", "ENABLE")
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
	w.confirmed(t, given)
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
	w.confirmed(t, given)
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

// allowTwoPolicies is allowWith over a basis of two policies, fin.pol@3.1 and fin.other@2.0.
func allowTwoPolicies(t *testing.T, snapshot, event string) recorder.Request {
	t.Helper()
	b := twoPolicies(t, snapshot)
	r := request(t, decision(t, decide.Allow, b), b, step(t))
	r.ConfirmationEventID, r.ConfirmationPlanHash = event, h(4)
	return r
}

// A341 (interim), INV-25, INV-17: the ALLOW that follows a confirmation is bound to the composite of every policy version of its basis. A confirmation given for the composite of two
// policies is spent by an ALLOW over those two, and the spend records that binding.
func TestAConfirmationGivenForTheCompositeOfTwoPoliciesIsSpentByAnAllowOverThoseTwo(t *testing.T) {
	w := newWorld(t)
	b := validBinding()
	b.PolicyVersion = policyVersionTwo
	if err := recordConfirmation(t, w.trust, tenant, given, b, realNow().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := w.write(t, allowTwoPolicies(t, "evs_01ABC", given), true); err != nil {
		t.Fatalf("the ALLOW over two policies: %v", err)
	}
	var bound string
	if err := w.admin.QueryRow(ctx(t), `SELECT binding_hash FROM confirmation_spend WHERE tenant_id = $1`, tenant).Scan(&bound); err != nil {
		t.Fatal(err)
	}
	if want := bindingHashOf(t, b); bound != want {
		t.Errorf("the spend records binding %q, want %q", bound, want)
	}
}

// A341 (interim): a confirmation given for any other policy version than the composite of the ALLOW's own basis is not valid for it: the old single-policy form, the composite of one of
// two policies, and the composite of two for a basis of one. Nothing remains.
func TestAConfirmationGivenForAnotherPolicyVersionThanTheCompositeIsRefused(t *testing.T) {
	for name, tc := range map[string]struct {
		policy string
		allow  func(t *testing.T, snapshot, event string) recorder.Request
	}{
		"the single-policy form, one policy in the basis": {"fin.pol@3.1", func(t *testing.T, s, e string) recorder.Request { return allowWith(t, s, e) }},
		"the composite of one policy, two in the basis":   {policyVersion, allowTwoPolicies},
		"the composite of two policies, one in the basis": {policyVersionTwo, func(t *testing.T, s, e string) recorder.Request { return allowWith(t, s, e) }},
		"the single-policy form, two in the basis":        {"fin.pol@3.1", allowTwoPolicies},
	} {
		w := newWorld(t)
		b := validBinding()
		b.PolicyVersion = tc.policy
		if err := recordConfirmation(t, w.trust, tenant, given, b, realNow().Add(time.Hour)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if _, err := w.write(t, tc.allow(t, "evs_01ABC", given), true); !errors.Is(err, recorder.ErrConfirmationNotValid) {
			t.Errorf("%s: err %v, want ErrConfirmationNotValid", name, err)
		}
		w.nothingRemains(t, name)
		if n := w.spends(t); n != 0 {
			t.Errorf("%s: %d spends", name, n)
		}
	}
}
