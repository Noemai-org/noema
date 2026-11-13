package ledger

import "errors"

// Point is a step boundary inside a ledger transaction, at which the crash harness can kill the process (TST-05, rulings A30 and A40; RESIST S9.2). The controller's own
// boundaries (controller.Point) are between the steps of the protocol; these are inside one step's transaction, which only the ledger can reach.
type Point int

// PointInCompleteTxBeforeCommit is inside the receipt transaction of Complete: the signed receipt, the idempotency record, the outbox event, the RECEIPT Record event and,
// for a stale-version refusal, the VOID, the RecoveryRequest and GRANT_VOIDED are written and the transaction is not yet committed. A crash here must leave none of them.
const PointInCompleteTxBeforeCommit Point = iota

// Points lists every boundary.
func Points() []Point { return []Point{PointInCompleteTxBeforeCommit} }

func (p Point) String() string {
	if p == PointInCompleteTxBeforeCommit {
		return "IN_COMPLETE_TX_BEFORE_COMMIT"
	}
	return "UNKNOWN_POINT"
}

// ErrCrash is returned by a Boundary hook to stand for the process dying at that point: the transaction is rolled back at once and the error is returned as it is, so only
// what was committed before survives.
var ErrCrash = errors.New(errPrefix + "crash injected")

func (l *Ledger) at(p Point) error {
	if l.Boundary == nil {
		return nil
	}
	return l.Boundary(p)
}
