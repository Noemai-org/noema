package portread

// The epoch of a new intent about a transaction (DEBT-54 part 2; ruling A520, interim; PERD 28.2: the logical resolution epoch "increments only when the prior logical action's
// case is verified CLOSED in the case system and a new intent is admitted" [REC]; INV-12, INV-19, INV-21, AGENT-09).
//
// The epoch is derived from the transaction's case list: the highest epoch of a case whose status is CLOSED, plus one, and 1 when no case is closed. The read is NOT recorded: it
// happens at admission, before the Evidence stage, and the Evidence stage's own ReadCases records the case list it uses. A list that cannot be relied on (the read failed, the list
// is incomplete, a case is not this tenant's, principal's or transaction's, a case id repeats or is off its shape, a status is empty, a closed case has no positive epoch) gives no
// epoch: the answer is ErrEpochUnknown, never 1, because a guessed 1 would reuse an earlier epoch's idempotency key.

import (
	"context"
	"errors"
	"fmt"

	"github.com/Noemai-org/noema/services/core/adapters"
)

// statusClosed is the status of a case verified closed in the case system (PERD 28.2). The case system's own word, never a Noema state.
const statusClosed = "CLOSED"

// ErrEpochUnknown means the case list gave no usable answer, so no epoch can be named. Callers fail closed.
var ErrEpochUnknown = errors.New(prefix + "the case list gives no usable epoch")

// NextEpoch reads the transaction's case list once, in the session's scope, and returns the logical resolution epoch of a new intent about it. It records nothing. An error that is
// not ErrEpochUnknown is a request or configuration the reader refuses before reading.
func (r *Reader) NextEpoch(ctx context.Context, scope adapters.Scope, transactionRef string) (int, error) {
	if _, ok := ctx.Deadline(); !ok {
		return 0, errors.New(prefix + "the context needs a deadline")
	}
	switch {
	case r.Cases == nil:
		return 0, errors.New(prefix + "the reader has no case port")
	case r.ReadTimeout <= 0:
		return 0, errors.New(prefix + "the reader needs a positive read timeout from configuration")
	}
	if err := scope.Validate(); err != nil {
		return 0, err
	}
	if !principalRe.MatchString(scope.PrincipalID) {
		return 0, fmt.Errorf("%sthe session's principal does not match the contract", prefix)
	}
	if !recordRefRe.MatchString(transactionRef) {
		return 0, fmt.Errorf("%sthe transaction reference does not match the contract", prefix)
	}
	cctx, cancel := r.call(ctx)
	defer cancel()
	page, err := r.Cases.FindCases(cctx, adapters.CaseQuery{TenantID: scope.TenantID, PrincipalID: scope.PrincipalID, TransactionRef: transactionRef})
	if err != nil || !page.Complete || !markerAgrees(page.Complete, page.NextCursor) {
		return 0, ErrEpochUnknown
	}
	var highest int64
	seen := make(map[string]bool, len(page.Items))
	for _, c := range page.Items {
		if !recordRefRe.MatchString(c.CaseID) || seen[c.CaseID] || c.Status == "" || c.TenantID != scope.TenantID || c.PrincipalID != scope.PrincipalID || c.TransactionRef != transactionRef {
			return 0, ErrEpochUnknown
		}
		seen[c.CaseID] = true
		if c.Status != statusClosed {
			continue
		}
		if c.Epoch < 1 || c.Epoch >= maxEpoch {
			return 0, ErrEpochUnknown
		}
		highest = max(highest, c.Epoch)
	}
	return int(highest) + 1, nil
}

// maxEpoch keeps the answer inside the column's range (an integer): a closed case whose epoch is at the range's edge is not a usable answer.
const maxEpoch = 1<<31 - 1
