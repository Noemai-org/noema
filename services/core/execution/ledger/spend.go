package ledger

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/Noemai-org/noema/services/core/execution/controller"
)

// pgDispatchBudget is the SQLSTATE of migration 0023: the key's dispatch budget is spent, or the event's key cannot be read.
const pgDispatchBudget = "NS002"

const (
	eventGrantSpent        = "GRANT_SPENT"
	eventDispatchAttempted = "DISPATCH_ATTEMPTED"
	eventSecurity          = "SECURITY_EVENT"
	kindCollision          = "IDEMPOTENCY_COLLISION"
)

// Spend is E8 (PERD 27.1): in one SERIALIZABLE transaction, check the grant is still unspent, read the kill switch,
// upsert the idempotency record (DISPATCHING, attempt + 1), insert grant_spend (unique grant ID and nonce), mark
// the grant SPENT and append GRANT_SPENT and DISPATCH_ATTEMPTED. All or nothing: if any step fails, nothing is
// dispatched. A grant that was already spent or voided, or that loses a race to a concurrent spend, is a Replay. The spends of one tenant take turns on that tenant's
// advisory lock (withSpendLock), so they do not race each other on the Record chain head. A transaction the database still refuses with a serialization failure is run
// again as a fresh transaction, up to SerializationAttempts attempts in all (spendWithRetry); after the last, the error is controller.ErrSpendRetryable around the
// database's own: nothing was spent and the grant is still ISSUED.
func (l *Ledger) Spend(ctx context.Context, cmd controller.SpendCommand) (controller.SpendResult, error) {
	res, err := l.spendOnce(ctx, cmd)
	return res, publicSpendError(err)
}

// spendOnce is Spend before its error is given its fixed public text (publicSpendError): the errors inside it still carry what the classification below reads.
func (l *Ledger) spendOnce(ctx context.Context, cmd controller.SpendCommand) (controller.SpendResult, error) {
	if l.SerializationAttempts < 1 {
		return controller.SpendResult{}, errNoSerializationAttempts
	}
	res, err := l.spendWithRetry(ctx, cmd)
	switch {
	case errors.Is(err, errNothingWritten):
		return res, nil
	case pgCode(err) == pgDispatchBudget:
		return controller.SpendResult{BudgetSpent: true}, nil // the transaction rolled back as a whole: nothing was spent, written or dispatched
	case errors.Is(err, controller.ErrKeyCollision):
		return controller.SpendResult{}, l.raiseCollision(ctx, cmd, err)
	case pgCode(err) == pgUniqueViolation || pgCode(err) == pgSerializationFailure || pgCode(err) == pgDeadlockDetected:
		// Another spend got there first, or the database could not serialise this one. If the grant is no longer
		// ISSUED the answer is a replay; if it still is, nothing was spent and the caller may try again (a serialization failure only after the last attempt).
		if final, ferr := l.grantIsFinal(ctx, cmd.Grant.TenantID, cmd.Grant.GrantID); ferr == nil && final {
			return controller.SpendResult{Replay: true}, nil
		}
		if pgCode(err) == pgSerializationFailure {
			// The database kept refusing the spend through every attempt: the typed answer "present the grant again" (ruling A339), with the database's own error as a cause.
			return controller.SpendResult{}, fixed("the spend did not commit and the grant is unspent: present it again", errors.Join(controller.ErrSpendRetryable, err))
		}
		return controller.SpendResult{}, fixed("the spend did not commit", err)
	case err != nil:
		return controller.SpendResult{}, err
	}
	return res, nil
}

// errNonceDiffers is the refusal of a spend whose grant carries another nonce than the stored one. It names no grant: the caller knows which one it presented.
var errNonceDiffers = newFixed("the nonce differs from the stored grant")

// errNothingWritten rolls back a transaction that has nothing to commit and still returns a result.
var errNothingWritten = errors.New(errPrefix + "nothing written")

func (l *Ledger) spend(ctx context.Context, tx pgx.Tx, cmd controller.SpendCommand) (controller.SpendResult, error) {
	g := cmd.Grant
	var status, nonce string
	err := tx.QueryRow(ctx, `SELECT status, nonce FROM decision_grant WHERE tenant_id = $1 AND grant_id = $2`, g.TenantID, g.GrantID).Scan(&status, &nonce)
	if errors.Is(err, pgx.ErrNoRows) {
		return controller.SpendResult{}, fixed("the grant does not exist", err)
	}
	if err != nil {
		return controller.SpendResult{}, fixed("the grant could not be read", err)
	}
	if status != statusIssued {
		return controller.SpendResult{Replay: true}, nil
	}
	if nonce != g.Nonce {
		return controller.SpendResult{}, errNonceDiffers
	}
	var killed bool
	if err := tx.QueryRow(ctx, `SELECT kill_switch FROM tenant WHERE tenant_id = $1`, g.TenantID).Scan(&killed); err != nil {
		return controller.SpendResult{}, fixed("the kill switch could not be read", err)
	}
	if killed {
		return controller.SpendResult{WritesDisabled: true}, nil
	}
	now := l.Clock.Now()
	var attempt int64
	err = tx.QueryRow(ctx, `INSERT INTO idempotency_record AS ir (tenant_id, idempotency_key, logical_action, params_hash, attempt, status, last_grant_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 1, 'DISPATCHING', $5, $6, $6)
		ON CONFLICT (tenant_id, idempotency_key) DO UPDATE
		   SET attempt = ir.attempt + 1, status = 'DISPATCHING', last_grant_id = EXCLUDED.last_grant_id, updated_at = EXCLUDED.updated_at
		 WHERE ir.params_hash = EXCLUDED.params_hash AND ir.logical_action = EXCLUDED.logical_action
		RETURNING attempt`, g.TenantID, cmd.IdempotencyKey, cmd.LogicalAction, g.ParamsHash, g.GrantID, now).Scan(&attempt)
	if errors.Is(err, pgx.ErrNoRows) {
		return controller.SpendResult{}, controller.ErrKeyCollision
	}
	if err != nil {
		return controller.SpendResult{}, fixed("the idempotency record could not be written", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO grant_spend (grant_id, tenant_id, nonce, idempotency_key, attempt, spent_at) VALUES ($1, $2, $3, $4, $5, $6)`,
		g.GrantID, g.TenantID, g.Nonce, cmd.IdempotencyKey, attempt, now); err != nil {
		return controller.SpendResult{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE decision_grant SET status = 'SPENT' WHERE tenant_id = $1 AND grant_id = $2`, g.TenantID, g.GrantID); err != nil {
		return controller.SpendResult{}, fixed("the grant could not be marked spent", err)
	}
	if err := l.record(ctx, tx, g.TenantID, g.CorrelationID, g.GrantID, eventGrantSpent, "", "",
		map[string]any{"grant_id": g.GrantID, "nonce": g.Nonce, "idempotency_key": cmd.IdempotencyKey, "attempt": attempt}); err != nil {
		return controller.SpendResult{}, fixed("the Record event GRANT_SPENT could not be appended", err)
	}
	if err := l.record(ctx, tx, g.TenantID, g.CorrelationID, g.GrantID, eventDispatchAttempted, "", "",
		map[string]any{"idempotency_key": cmd.IdempotencyKey, "attempt": attempt, "params_hash": g.ParamsHash, "logical_action": cmd.LogicalAction,
			"capability": g.CapabilityID, "target_ref": g.TargetRef}); err != nil {
		return controller.SpendResult{}, fixed("the Record event DISPATCH_ATTEMPTED could not be appended", err)
	}
	return controller.SpendResult{Spent: true, Attempt: attempt}, nil
}

// grantIsFinal reports that the grant has left ISSUED (spent, void or expired), read outside the failed transaction.
func (l *Ledger) grantIsFinal(ctx context.Context, tenantID, grantID string) (bool, error) {
	var status string
	if err := l.Pool.QueryRow(ctx, `SELECT status FROM decision_grant WHERE tenant_id = $1 AND grant_id = $2`, tenantID, grantID).Scan(&status); err != nil {
		return false, err
	}
	return status != statusIssued, nil
}

// raiseCollision records SECURITY_EVENT IDEMPOTENCY_COLLISION in its own transaction (the spend was rolled back) and
// returns the sentinel the controller answers by voiding the grant (EC-E06, REQ-EXE-002). If the event cannot be
// written, the sentinel is returned together with that failure.
func (l *Ledger) raiseCollision(ctx context.Context, cmd controller.SpendCommand, sentinel error) error {
	werr := l.inTx(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		return l.record(ctx, tx, cmd.Grant.TenantID, cmd.Grant.CorrelationID, cmd.Grant.GrantID, eventSecurity, kindCollision, "",
			map[string]any{"idempotency_key": cmd.IdempotencyKey, "params_hash": cmd.Grant.ParamsHash, "logical_action": cmd.LogicalAction})
	})
	if werr != nil {
		return fixed("idempotency key collision, and its security event could not be recorded", errors.Join(sentinel, werr))
	}
	return sentinel
}
