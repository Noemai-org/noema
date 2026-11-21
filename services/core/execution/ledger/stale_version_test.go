package ledger_test

import (
	"reflect"
	"testing"

	"github.com/Noemai-org/noema/services/core/execution/controller"
)

func staleAnswer() controller.TransportResult {
	return controller.TransportResult{Class: controller.TransportVersionConflict, RawCode: "409"}
}

// REQ-EXE-004, INV-27, US-19, EC-E11: the write carries the grant's target version, the target refuses it, and the grant is
// VOID: the receipt, the status change, the RecoveryRequest addressed to Trust and GRANT_VOIDED{REJECTED_STALE_VERSION} are
// committed together, there is one dispatch and no retry.
func TestAStaleVersionVoidsTheSpentGrantTogetherWithItsReceipt(t *testing.T) {
	w := newWorld(t)
	w.target.answer = staleAnswer()
	g := w.issue(t, 1)
	out := mustExecute(t, w, g.GrantID)
	if out.Receipt == nil || out.Receipt.State != controller.StateRejectedStaleVersion || out.Void != "" || out.Raced || out.Replayed {
		t.Fatalf("outcome %+v, want one REJECTED_STALE_VERSION receipt", out)
	}
	if w.target.count() != 1 {
		t.Fatalf("%d dispatches, want 1", w.target.count())
	}
	if got := w.target.calls[0].ExpectedFrom; !reflect.DeepEqual(got, g.TargetVersions) {
		t.Fatalf("the write carried %+v, want the grant's target versions %+v", got, g.TargetVersions)
	}
	want := []string{"REVALIDATION_PASSED", "GRANT_SPENT", "DISPATCH_ATTEMPTED", "RECEIPT", "GRANT_VOIDED"}
	if got := tail(w.events(t), len(want)); !reflect.DeepEqual(got, want) {
		t.Fatalf("Record events %v, want the tail %v (the receipt first, then the void)", got, want)
	}
	if n := w.count(t, `SELECT count(*) FROM recovery_request WHERE grant_id = $1 AND reason = 'REJECTED_STALE_VERSION' AND status = 'OPEN'`, g.GrantID); n != 1 {
		t.Fatalf("%d open RecoveryRequests with reason REJECTED_STALE_VERSION, want 1", n)
	}
	if n := w.count(t, `SELECT count(*) FROM outbox_event WHERE topic = 'recovery.requested' AND payload->>'grant_id' = $1 AND payload->>'reason' = 'REJECTED_STALE_VERSION'`, g.GrantID); n != 1 {
		t.Fatalf("%d recovery.requested outbox events, want 1", n)
	}
	// PERD v1.18 38.2: SPENT -> VOID for a confirmed refusal (migration 0002). The Record keeps both states: GRANT_SPENT
	// is still there, and GRANT_VOIDED names the change from SPENT to VOID and the receipt that confirmed it.
	var status string
	if err := w.admin.QueryRow(ctx(t), `SELECT status FROM decision_grant WHERE grant_id = $1`, g.GrantID).Scan(&status); err != nil || status != "VOID" {
		t.Fatalf("grant status %q, %v; want VOID", status, err)
	}
	if n := w.count(t, `SELECT count(*) FROM record_event WHERE event_type = 'GRANT_SPENT' AND object_ref = $1`, g.GrantID); n != 1 {
		t.Fatalf("%d GRANT_SPENT events for the voided grant, want 1: the SPENT state must stay on the Record", n)
	}
	if n := w.count(t, `SELECT count(*) FROM record_event WHERE event_type = 'GRANT_VOIDED' AND object_ref = $1 AND payload->>'from_status' = 'SPENT' AND payload->>'to_status' = 'VOID'
		AND payload->>'receipt_id' = $2 AND payload->>'reason' = 'REJECTED_STALE_VERSION'`, g.GrantID, out.Receipt.ReceiptID); n != 1 {
		t.Fatalf("%d GRANT_VOIDED events naming SPENT -> VOID and the receipt, want 1", n)
	}
	verifyChain(t, w)
}

// REQ-EXE-004, REQ-EXE-006, AGENT-08: no automatic retry. Executing the voided grant again, voiding it again and running the
// recovery sweeper dispatch nothing, add no second RecoveryRequest and no second void.
func TestAStaleVersionIsNeverRetriedAndIsVoidedOnce(t *testing.T) {
	w := newWorld(t)
	w.target.answer = staleAnswer()
	g := w.issue(t, 1)
	mustExecute(t, w, g.GrantID)
	if again := mustExecute(t, w, g.GrantID); !again.Replayed || again.Receipt != nil {
		t.Fatalf("the second execution of a spent grant: %+v, want a replay", again)
	}
	if err := w.led.Void(ctx(t), g, controller.VoidTargetVersionStale); err != nil {
		t.Fatal(err)
	}
	if pending, err := w.led.Dispatching(ctx(t), tenant); err != nil || len(pending) != 0 {
		t.Fatalf("the sweeper's input %+v, %v; the stale receipt must leave nothing to sweep", pending, err)
	}
	if w.target.count() != 1 {
		t.Fatalf("%d dispatches after a replay, a second void and a sweep, want 1", w.target.count())
	}
	for q, want := range map[string]int{
		`SELECT count(*) FROM recovery_request`:                               1,
		`SELECT count(*) FROM record_event WHERE event_type = 'GRANT_VOIDED'`: 1,
		`SELECT count(*) FROM execution_receipt`:                              1,
	} {
		if got := w.count(t, q); got != want {
			t.Fatalf("%s = %d, want %d", q, got, want)
		}
	}
}

// REQ-EXE-004, PERD 32.4: the void and the receipt are one transaction. When the void cannot be written, no receipt is
// written either, the attempt stays DISPATCHING and the sweeper turns it into OUTCOME_UNKNOWN for Outcome to verify. The
// fault is a real one: a RecoveryRequest for the grant already exists, so the unique constraint refuses the second.
func TestIfTheVoidCannotBeWrittenNoStaleReceiptIsWrittenEither(t *testing.T) {
	w := newWorld(t)
	w.target.answer = staleAnswer()
	g := w.issue(t, 1)
	if _, err := w.admin.Exec(ctx(t), `INSERT INTO recovery_request (recovery_request_id, tenant_id, grant_id, reason, created_at) VALUES ('rrq_planted', $1, $2, 'STATE_CHANGED', now())`, tenant, g.GrantID); err != nil {
		t.Fatal(err)
	}
	if _, err := w.ctl.Execute(ctx(t), tenant, g.GrantID); err == nil {
		t.Fatal("Execute reported success although the void could not be written")
	}
	if n := w.count(t, `SELECT count(*) FROM execution_receipt`); n != 0 {
		t.Fatalf("%d receipts, want none: the receipt must roll back with the void", n)
	}
	for _, e := range w.events(t) {
		if e == "RECEIPT" || e == "GRANT_VOIDED" {
			t.Fatalf("Record has %s although the transaction failed", e)
		}
	}
	var status string
	if err := w.admin.QueryRow(ctx(t), `SELECT status FROM decision_grant WHERE grant_id = $1`, g.GrantID).Scan(&status); err != nil || status != "SPENT" {
		t.Fatalf("grant status %q, %v; want SPENT: the status change must roll back with the rest", status, err)
	}
	pending, err := w.led.Dispatching(ctx(t), tenant)
	if err != nil || len(pending) != 1 || pending[0].GrantID != g.GrantID {
		t.Fatalf("sweeper input %+v, %v; want the one attempt left DISPATCHING", pending, err)
	}
	verifyChain(t, w)
}

// REQ-EXE-004: only a refused version voids a spent grant. Every other receipt state leaves the grant spent and sends no
// RecoveryRequest from Execution (Outcome decides what follows; PERD 30.2).
func TestOnlyAStaleVersionVoidsAGrantThatWasDispatched(t *testing.T) {
	answers := map[controller.ExecutionState]controller.TransportResult{
		controller.StateAcknowledged:     {Class: controller.TransportAck2xx, ExternalAck: "CASE-1", RawCode: "201"},
		controller.StateRejectedByTarget: {Class: controller.TransportRejected4xx, RawCode: "422"},
		controller.StateOutcomeUnknown:   {Class: controller.TransportTimeout},
		controller.StateFailedTerminal:   {Class: controller.TransportDefect},
	}
	for state, answer := range answers {
		t.Run(string(state), func(t *testing.T) {
			w := newWorld(t)
			w.target.answer = answer
			g := w.issue(t, 1)
			out := mustExecute(t, w, g.GrantID)
			if out.Receipt == nil || out.Receipt.State != state {
				t.Fatalf("outcome %+v, want a %s receipt", out, state)
			}
			if n := w.count(t, `SELECT count(*) FROM recovery_request`) + w.count(t, `SELECT count(*) FROM record_event WHERE event_type = 'GRANT_VOIDED'`); n != 0 {
				t.Fatalf("a %s receipt produced a void or a RecoveryRequest", state)
			}
			if n := w.count(t, `SELECT count(*) FROM decision_grant WHERE grant_id = $1 AND status = 'SPENT'`, g.GrantID); n != 1 {
				t.Fatalf("a %s receipt changed the grant's status", state)
			}
		})
	}
}

func tail(s []string, n int) []string {
	if len(s) < n {
		return s
	}
	return s[len(s)-n:]
}
