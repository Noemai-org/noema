package ledger_test

// TST-05 slice 2 (noema#498, noema#506): a boundary inside the receipt transaction of Ledger.Complete, after its writes and before commit, so the crash harness can kill the
// process while the receipt, the idempotency record, the outbox event and the RECEIPT Record event are written and not yet committed. REQ-TST-008, REQ-EXE-006, INV-07, EC-E09.
// A hook is nil in production. All data is SYNTHETIC.

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Noemai-org/noema/services/core/execution/controller"
	"github.com/Noemai-org/noema/services/core/execution/ledger"
)

func receiptRows(t *testing.T, w *world, grant string) map[string]int {
	t.Helper()
	return map[string]int{
		"execution_receipt":             w.count(t, `SELECT count(*) FROM execution_receipt WHERE grant_id = $1`, grant),
		"RECEIPT events":                w.count(t, `SELECT count(*) FROM record_event WHERE event_type = 'RECEIPT' AND object_ref = $1`, grant),
		"GRANT_SPENT events":            w.count(t, `SELECT count(*) FROM record_event WHERE event_type = 'GRANT_SPENT' AND object_ref = $1`, grant),
		"DISPATCH_ATTEMPTED events":     w.count(t, `SELECT count(*) FROM record_event WHERE event_type = 'DISPATCH_ATTEMPTED' AND object_ref = $1`, grant),
		"receipted idempotency records": w.count(t, `SELECT count(*) FROM idempotency_record WHERE last_grant_id = $1 AND status = 'RECEIPTED'`, grant),
		"outcome.verify outbox events":  w.count(t, `SELECT count(*) FROM outbox_event WHERE topic = 'outcome.verify' AND payload->>'grant_id' = $1`, grant),
		"voided grants":                 w.count(t, `SELECT count(*) FROM decision_grant WHERE grant_id = $1 AND status = 'VOID'`, grant),
		"recovery requests":             w.count(t, `SELECT count(*) FROM recovery_request WHERE grant_id = $1`, grant),
	}
}

// lockedAtTheHook is a probe the hook runs from another connection: a row the transaction has already updated is locked until it ends, so a NOWAIT lock attempt fails with
// 55P03. It proves the boundary is after the writes, not merely somewhere in the function (a hook placed before them would find the row free).
func lockedAtTheHook(t *testing.T, w *world, query string, args ...any) bool {
	t.Helper()
	_, err := w.admin.Exec(ctx(t), query+" FOR UPDATE NOWAIT", args...)
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "55P03"
}

func wantRows(t *testing.T, got map[string]int, want map[string]int, when string) {
	t.Helper()
	for k, n := range want {
		if got[k] != n {
			t.Errorf("%s: %s = %d, want %d (all: %v)", when, k, got[k], n, got)
		}
	}
}

// A hook that fails inside the receipt transaction rolls the whole transaction back: no receipt, no idempotency update, no outbox event and no RECEIPT event remain, the spend and
// the dispatch attempt (committed earlier) do, and the attempt is the sweeper's input. A sweep afterwards writes the one OUTCOME_UNKNOWN receipt and dispatches nothing.
func TestAKillInsideTheReceiptTransactionLeavesNoReceiptAndTheSweeperRecordsOneUnknown(t *testing.T) {
	w := newWorld(t)
	g := w.issue(t, 1)
	locked := false
	w.led.Boundary = func(p ledger.Point) error {
		if p == ledger.PointInCompleteTxBeforeCommit {
			locked = lockedAtTheHook(t, w, `SELECT 1 FROM idempotency_record WHERE last_grant_id = $1`, g.GrantID)
			return ledger.ErrCrash
		}
		return nil
	}
	if _, err := w.ctl.Execute(ctx(t), tenant, g.GrantID); !errors.Is(err, ledger.ErrCrash) {
		t.Fatalf("Execute: %v, want the injected crash", err)
	}
	if !locked {
		t.Fatal("at the hook the idempotency record was not yet updated by the receipt transaction: the boundary is not after the writes")
	}
	wantRows(t, receiptRows(t, w, g.GrantID), map[string]int{"execution_receipt": 0, "RECEIPT events": 0, "GRANT_SPENT events": 1, "DISPATCH_ATTEMPTED events": 1,
		"receipted idempotency records": 0, "outcome.verify outbox events": 0}, "after the kill")
	if w.target.count() != 1 {
		t.Fatalf("%d dispatches, want 1", w.target.count())
	}
	pending, err := w.led.Dispatching(ctx(t), tenant)
	if err != nil || len(pending) != 1 {
		t.Fatalf("the sweeper's input %+v, %v; want the one attempt left without a receipt", pending, err)
	}
	w.led.Boundary = nil // the process is back
	if n, err := w.ctl.Recover(ctx(t), tenant); err != nil || n != 1 {
		t.Fatalf("Recover: %d receipts, %v; want one", n, err)
	}
	wantRows(t, receiptRows(t, w, g.GrantID), map[string]int{"execution_receipt": 1, "RECEIPT events": 1, "GRANT_SPENT events": 1, "DISPATCH_ATTEMPTED events": 1}, "after the sweep")
	if w.target.count() != 1 {
		t.Fatalf("recovery dispatched: %d dispatches, want 1", w.target.count())
	}
	verifyChain(t, w)
}

// The stale-version receipt writes more inside the same transaction (the VOID of the spent grant, the RecoveryRequest and GRANT_VOIDED): a kill at the boundary keeps none of them.
func TestAKillInsideTheStaleVersionReceiptTransactionKeepsNeitherTheVoidNorTheRecoveryRequest(t *testing.T) {
	w := newWorld(t)
	w.target.answer = staleAnswer()
	g := w.issue(t, 1)
	voidWritten := false
	w.led.Boundary = func(ledger.Point) error {
		voidWritten = lockedAtTheHook(t, w, `SELECT 1 FROM decision_grant WHERE grant_id = $1`, g.GrantID)
		return ledger.ErrCrash
	}
	if _, err := w.ctl.Execute(ctx(t), tenant, g.GrantID); !errors.Is(err, ledger.ErrCrash) {
		t.Fatalf("Execute: %v, want the injected crash", err)
	}
	if !voidWritten {
		t.Fatal("at the hook the grant was not yet voided by the receipt transaction: the boundary is not after the stale-version writes")
	}
	wantRows(t, receiptRows(t, w, g.GrantID), map[string]int{"execution_receipt": 0, "RECEIPT events": 0, "voided grants": 0, "recovery requests": 0, "GRANT_SPENT events": 1}, "after the kill")
	if n := w.count(t, `SELECT count(*) FROM record_event WHERE event_type = 'GRANT_VOIDED' AND object_ref = $1`, g.GrantID); n != 0 {
		t.Fatalf("%d GRANT_VOIDED events after the kill, want 0", n)
	}
	verifyChain(t, w)
}

// The boundary exists and is named: one point, with a name for the harness's log, and a nil hook changes nothing (the other tests of this package run with none).
func TestTheLedgerBoundaryHasOnePointAndAName(t *testing.T) {
	if got := ledger.Points(); len(got) != 1 || got[0] != ledger.PointInCompleteTxBeforeCommit {
		t.Fatalf("Points() = %v", got)
	}
	if s := ledger.PointInCompleteTxBeforeCommit.String(); s != "IN_COMPLETE_TX_BEFORE_COMMIT" {
		t.Fatalf("name %q", s)
	}
	var _ = controller.Receipt{} // the hook belongs to the Ledger, not to the controller's Point list: the matrices that range over controller.Points() are not changed
}
