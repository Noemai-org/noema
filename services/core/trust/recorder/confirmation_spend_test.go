package recorder_test

// TRU-07 follow-up (noema#572, EL's answer to Q-C; migration 0028; REQ-TRU-008, REQ-TRU-009; INV-03, INV-17, INV-25, INV-21): a given confirmation is spent
// once. The spend is written in the Decision Record's own transaction, by the decision that the confirmation authorised, and the database refuses a second
// spend of the same confirmation event of the same tenant. A retry of the same input returns the stored decision and spends nothing. Real PostgreSQL, real
// tenant logins, the real hash chain, as in recorder_test.go. All data is SYNTHETIC.

import (
	"errors"
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

// The spend is insert-only for every role, the table is scoped to the login's tenant, and a spend can name only an ALLOW decision of its own tenant.
func TestTheSpendIsInsertOnlyScopedToItsTenantAndNamesOnlyAnAllow(t *testing.T) {
	w := newWorld(t)
	allowed, err := w.write(t, allowWith(t, "evs_01ABC", given), true)
	if err != nil {
		t.Fatal(err)
	}
	b := evaluatedWith(t, "evs_02XYZ", "3.1")
	refused, err := w.write(t, request(t, decision(t, decide.Reject, b), b), true)
	if err != nil {
		t.Fatal(err)
	}
	for name, stmt := range map[string]string{
		"update":   `UPDATE confirmation_spend SET confirmation_event_id = 'cfe_01OTHER'`,
		"delete":   `DELETE FROM confirmation_spend`,
		"truncate": `TRUNCATE confirmation_spend`,
	} {
		if _, err := w.admin.Exec(ctx(t), stmt); err == nil {
			t.Errorf("%s by the administrator was not refused", name)
		}
		if _, err := w.trust.Exec(ctx(t), stmt); err == nil {
			t.Errorf("%s by the Trust login was not refused", name)
		}
	}
	if n := w.spends(t); n != 1 {
		t.Fatalf("%d spends remain after the refused changes, want 1", n)
	}
	// a spend naming a decision that is not an ALLOW is refused by the database, whoever writes it
	_, err = w.trust.Exec(ctx(t), `INSERT INTO confirmation_spend (tenant_id, confirmation_event_id, decision_id) VALUES ($1, 'cfe_01OTHER', $2)`, tenant, string(refused.Record.DecisionId))
	var pg *pgconn.PgError
	if !errors.As(err, &pg) {
		t.Fatalf("a spend for a REJECT decision: err %v, want a database refusal", err)
	}
	// another tenant's login sees none of this tenant's spends
	if _, err := w.admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ('ten_bb', 'B')`); err != nil {
		t.Fatal(err)
	}
	other := newTenantTrust(t, w, "ten_bb")
	var seen int
	if err := other.QueryRow(ctx(t), `SELECT count(*) FROM confirmation_spend`).Scan(&seen); err != nil {
		t.Fatal(err)
	}
	if seen != 0 {
		t.Errorf("ten_bb's Trust login sees %d of ten_aa's spends", seen)
	}
	// and ten_aa's login cannot write a spend for ten_bb
	if _, err := w.trust.Exec(ctx(t), `INSERT INTO confirmation_spend (tenant_id, confirmation_event_id, decision_id) VALUES ('ten_bb', 'cfe_01OTHER', $1)`, string(allowed.Record.DecisionId)); err == nil {
		t.Error("ten_aa's login wrote a spend for ten_bb")
	}
	if n := w.count(t, `SELECT count(*) FROM confirmation_spend WHERE tenant_id = 'ten_bb'`); n != 0 {
		t.Errorf("%d spends exist for ten_bb", n)
	}
}
