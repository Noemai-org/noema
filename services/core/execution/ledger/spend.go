package ledger

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/Noemai-org/noema/services/core/execution/controller"
)

const (
	eventGrantSpent        = "GRANT_SPENT"
	eventDispatchAttempted = "DISPATCH_ATTEMPTED"
	eventSecurity          = "SECURITY_EVENT"
	kindCollision          = "IDEMPOTENCY_COLLISION"
)

// Spend is E8 (PERD 27.1): in one SERIALIZABLE transaction, check the grant is still unspent, read the kill switch,
// upsert the idempotency record (DISPATCHING, attempt + 1), insert grant_spend (unique grant ID and nonce), mark
// the grant SPENT and append GRANT_SPENT and DISPATCH_ATTEMPTED. All or nothing: if any step fails, nothing is
// dispatched. A grant that was already spent or voided, or that loses a race to a concurrent spend, is a Replay.
func (l *Ledger) Spend(ctx context.Context, cmd controller.SpendCommand) (controller.SpendResult, error) {
	var res controller.SpendResult
	err := l.inTx(ctx, pgx.Serializable, func(tx pgx.Tx) error {
		r, err := l.spend(ctx, tx, cmd)
		res = r
		if err == nil && !r.Spent {
			return errNothingWritten
		}
		return err
	})
	switch {
	case errors.Is(err, errNothingWritten):
		return res, nil
	case errors.Is(err, controller.ErrKeyCollision):
		return controller.SpendResult{}, l.raiseCollision(ctx, cmd, err)
	case pgCode(err) == pgUniqueViolation || pgCode(err) == pgSerializationFailure || pgCode(err) == pgDeadlockDetected:
		// Another spend got there first, or the database could not serialise this one. If the grant is no longer
		// ISSUED the answer is a replay; if it still is, nothing was spent and the caller may try again.
		if final, ferr := l.grantIsFinal(ctx, cmd.Grant.TenantID, cmd.Grant.GrantID); ferr == nil && final {
			return controller.SpendResult{Replay: true}, nil
		}
		return controller.SpendResult{}, fmt.Errorf("%sspend %s did not commit: %w", errPrefix, cmd.Grant.GrantID, err)
	case err != nil:
		return controller.SpendResult{}, err
	}
	return res, nil
}

// errNothingWritten rolls back a transaction that has nothing to commit and still returns a result.
var errNothingWritten = errors.New(errPrefix + "nothing written")

func (l *Ledger) spend(ctx context.Context, tx pgx.Tx, cmd controller.SpendCommand) (controller.SpendResult, error) {
	g := cmd.Grant
	var status, nonce string
	err := tx.QueryRow(ctx, `SELECT status, nonce FROM decision_grant WHERE tenant_id = $1 AND grant_id = $2`, g.TenantID, g.GrantID).Scan(&status, &nonce)
	if errors.Is(err, pgx.ErrNoRows) {
		return controller.SpendResult{}, fmt.Errorf("%sgrant %s does not exist", errPrefix, g.GrantID)
	}
	if err != nil {
		return controller.SpendResult{}, fmt.Errorf("%sread grant: %w", errPrefix, err)
	}
	if status != statusIssued {
		return controller.SpendResult{Replay: true}, nil
	}
	if nonce != g.Nonce {
		return controller.SpendResult{}, fmt.Errorf("%sgrant %s: the nonce differs from the stored grant", errPrefix, g.GrantID)
	}
	var killed bool
	if err := tx.QueryRow(ctx, `SELECT kill_switch FROM tenant WHERE tenant_id = $1`, g.TenantID).Scan(&killed); err != nil {
		return controller.SpendResult{}, fmt.Errorf("%sread kill switch: %w", errPrefix, err)
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
		return controller.SpendResult{}, fmt.Errorf("%sidempotency record: %w", errPrefix, err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO grant_spend (grant_id, tenant_id, nonce, idempotency_key, attempt, spent_at) VALUES ($1, $2, $3, $4, $5, $6)`,
		g.GrantID, g.TenantID, g.Nonce, cmd.IdempotencyKey, attempt, now); err != nil {
		return controller.SpendResult{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE decision_grant SET status = 'SPENT' WHERE tenant_id = $1 AND grant_id = $2`, g.TenantID, g.GrantID); err != nil {
		return controller.SpendResult{}, fmt.Errorf("%smark grant spent: %w", errPrefix, err)
	}
	if err := l.record(ctx, tx, g.TenantID, g.CorrelationID, g.GrantID, eventGrantSpent, "", "",
		map[string]any{"grant_id": g.GrantID, "nonce": g.Nonce, "idempotency_key": cmd.IdempotencyKey, "attempt": attempt}); err != nil {
		return controller.SpendResult{}, err
	}
	if err := l.record(ctx, tx, g.TenantID, g.CorrelationID, g.GrantID, eventDispatchAttempted, "", "",
		map[string]any{"idempotency_key": cmd.IdempotencyKey, "attempt": attempt, "params_hash": g.ParamsHash, "logical_action": cmd.LogicalAction}); err != nil {
		return controller.SpendResult{}, err
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
		return errors.Join(sentinel, fmt.Errorf("%sraise the collision event: %w", errPrefix, werr))
	}
	return sentinel
}
