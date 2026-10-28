package ledger

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/Noemai-org/noema/services/core/execution/controller"
	"github.com/Noemai-org/noema/services/core/platform/crypto"
)

const (
	eventGrantVoided = "GRANT_VOIDED"
	eventReceipt     = "RECEIPT"

	topicRecoveryRequested = "recovery.requested"
	topicOutcomeVerify     = "outcome.verify"
)

// Void records a void grant (REQ-EXE-001): the grant is marked VOID, one RecoveryRequest addressed to Trust is
// inserted (unique per grant), an outbox event wakes Trust's side, and GRANT_VOIDED goes on the Record, all in one
// transaction. It happens once per grant: a grant that is no longer ISSUED (already void, or spent) is left alone.
func (l *Ledger) Void(ctx context.Context, g controller.Grant, reason controller.VoidReason) error {
	return l.inTx(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		var status string
		err := tx.QueryRow(ctx, `SELECT status FROM decision_grant WHERE tenant_id = $1 AND grant_id = $2 FOR UPDATE`, g.TenantID, g.GrantID).Scan(&status)
		if err != nil {
			return fmt.Errorf("%sread grant %s to void it: %w", errPrefix, g.GrantID, err)
		}
		if status != statusIssued {
			return nil
		}
		if _, err := tx.Exec(ctx, `UPDATE decision_grant SET status = 'VOID' WHERE tenant_id = $1 AND grant_id = $2`, g.TenantID, g.GrantID); err != nil {
			return fmt.Errorf("%smark grant void: %w", errPrefix, err)
		}
		return l.addRecoveryRequest(ctx, tx, g.TenantID, g.CorrelationID, g.GrantID, reason, voidChange{from: statusIssued})
	})
}

// addRecoveryRequest is the part of a void that is the same whether the grant was void before its write or after it: one
// RecoveryRequest addressed to Trust (unique per grant), the outbox event that wakes Trust's side, and GRANT_VOIDED on the
// Record. It runs inside the caller's transaction.
func (l *Ledger) addRecoveryRequest(ctx context.Context, tx pgx.Tx, tenantID, correlationID, grantID string, reason controller.VoidReason, change voidChange) error {
	now := l.Clock.Now()
	rrq, err := l.IDs.ID("rrq")
	if err != nil {
		return fmt.Errorf("%srecovery request id: %w", errPrefix, err)
	}
	obx, err := l.IDs.ID("obx")
	if err != nil {
		return fmt.Errorf("%soutbox id: %w", errPrefix, err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO recovery_request (recovery_request_id, tenant_id, grant_id, reason, created_at) VALUES ($1, $2, $3, $4, $5)`,
		rrq, tenantID, grantID, string(reason), now); err != nil {
		return fmt.Errorf("%sinsert recovery request: %w", errPrefix, err)
	}
	payload := map[string]any{"recovery_request_id": rrq, "grant_id": grantID, "reason": string(reason)}
	if err := l.outbox(ctx, tx, obx, tenantID, topicRecoveryRequested, payload); err != nil {
		return err
	}
	// The payload names the change of status, so that the earlier state stays visible next to GRANT_SPENT (PERD v1.18 REQ-EXE-004).
	body := map[string]any{"grant_id": grantID, "reason": string(reason), "from_status": change.from, "to_status": statusVoid}
	if change.receiptID != "" {
		body["receipt_id"] = change.receiptID
	}
	return l.record(ctx, tx, tenantID, correlationID, grantID, eventGrantVoided, "", string(reason), body)
}

// voidChange is the status a void leaves and, for a void made by a receipt, the receipt that confirmed it.
type voidChange struct {
	from      string
	receiptID string
}

func (l *Ledger) outbox(ctx context.Context, tx pgx.Tx, id, tenantID, topic string, payload any) error {
	body, err := crypto.CanonicalJSON(payload)
	if err != nil {
		return fmt.Errorf("%soutbox payload: %w", errPrefix, err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO outbox_event (outbox_id, tenant_id, topic, payload, created_at) VALUES ($1, $2, $3, $4::jsonb, $5)`,
		id, tenantID, topic, string(body), l.Clock.Now()); err != nil {
		return fmt.Errorf("%sinsert outbox event: %w", errPrefix, err)
	}
	return nil
}

// Complete is E11: in one transaction, insert the signed receipt (one per grant and attempt; the first writer
// wins), mark the idempotency record RECEIPTED (only when this is its latest attempt), append RECEIPT and enqueue
// the Outcome verification. It returns false, and writes nothing else, when a receipt for this grant and attempt
// already exists.
//
// A REJECTED_STALE_VERSION receipt also voids the grant (REQ-EXE-004, INV-27, US-19; PERD v1.18 38.2): in the same
// transaction, after RECEIPT, the grant moves SPENT -> VOID and the RecoveryRequest to Trust, its outbox event and
// GRANT_VOIDED{REJECTED_STALE_VERSION} are written. The trigger decision_grant_guard (migration 0002) permits the move
// only because the receipt is a confirmed refusal. If any write fails, none is kept, and the attempt stays DISPATCHING
// for the sweeper (OUTCOME_UNKNOWN). GRANT_SPENT stays on the Record.
func (l *Ledger) Complete(ctx context.Context, r controller.Receipt) (bool, error) {
	written := false
	err := l.inTx(ctx, pgx.ReadCommitted, func(tx pgx.Tx) error {
		doc := r.Document()
		doc["sig"] = map[string]any{"key_id": r.Sig.KeyID, "value": r.Sig.Value}
		body, err := crypto.CanonicalJSON(doc)
		if err != nil {
			return fmt.Errorf("%sreceipt json: %w", errPrefix, err)
		}
		raw := r.TransportResult
		if r.RawCode != "" {
			raw += ":" + r.RawCode
		}
		ack := r.ExternalAck
		now := l.Clock.Now()
		tag, err := tx.Exec(ctx, `INSERT INTO execution_receipt (receipt_id, tenant_id, grant_id, attempt, idempotency_key, logical_action, execution_state,
			raw_status, external_ack, dispatched_at, json, sig_key_id, sig_value, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::jsonb, $12, $13, $14)
			ON CONFLICT (grant_id, attempt) DO NOTHING`,
			r.ReceiptID, r.TenantID, r.GrantID, r.Attempt, r.IdempotencyKey, r.LogicalAction, string(r.State), raw, ack, r.DispatchedAt, string(body),
			r.Sig.KeyID, r.Sig.Value, now)
		if err != nil {
			return fmt.Errorf("%sinsert receipt: %w", errPrefix, err)
		}
		if tag.RowsAffected() == 0 {
			return errNothingWritten
		}
		if _, err := tx.Exec(ctx, `UPDATE idempotency_record SET status = 'RECEIPTED', external_ref = COALESCE($4, external_ref), updated_at = $5
			WHERE tenant_id = $1 AND idempotency_key = $2 AND attempt = $3`, r.TenantID, r.IdempotencyKey, r.Attempt, ack, now); err != nil {
			return fmt.Errorf("%supdate idempotency record: %w", errPrefix, err)
		}
		obx, err := l.IDs.ID("obx")
		if err != nil {
			return fmt.Errorf("%soutbox id: %w", errPrefix, err)
		}
		if err := l.outbox(ctx, tx, obx, r.TenantID, topicOutcomeVerify,
			map[string]any{"receipt_id": r.ReceiptID, "grant_id": r.GrantID, "attempt": r.Attempt, "idempotency_key": r.IdempotencyKey}); err != nil {
			return err
		}
		written = true
		doc["raw_status"] = raw
		if err := l.record(ctx, tx, r.TenantID, r.CorrelationID, r.GrantID, eventReceipt, "", "", doc); err != nil {
			return err
		}
		if r.State == controller.StateRejectedStaleVersion {
			// PERD v1.18 38.2: SPENT -> VOID. The guard (migration 0002) lets the update through only because the receipt
			// inserted above is a confirmed refusal; it refuses any other receipt, and the whole transaction with it.
			tag, err := tx.Exec(ctx, `UPDATE decision_grant SET status = 'VOID' WHERE tenant_id = $1 AND grant_id = $2 AND status = 'SPENT'`, r.TenantID, r.GrantID)
			if err != nil {
				return fmt.Errorf("%svoid the spent grant: %w", errPrefix, err)
			}
			if tag.RowsAffected() != 1 {
				return fmt.Errorf("%svoid the spent grant %s: it is not SPENT", errPrefix, r.GrantID)
			}
			return l.addRecoveryRequest(ctx, tx, r.TenantID, r.CorrelationID, r.GrantID, controller.VoidTargetVersionStale, voidChange{from: statusSpent, receiptID: r.ReceiptID})
		}
		return nil
	})
	if errors.Is(err, errNothingWritten) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return written, nil
}

// Dispatching lists spent attempts that have no receipt: the sweeper's input (PERD 27.1, EC-E09).
func (l *Ledger) Dispatching(ctx context.Context, tenantID string) ([]controller.Pending, error) {
	if err := needDeadline(ctx); err != nil {
		return nil, err
	}
	rows, err := l.Pool.Query(ctx, `SELECT gs.grant_id, gs.tenant_id, dg.json->>'correlation_id', gs.idempotency_key, ir.logical_action, gs.attempt
		FROM grant_spend gs
		JOIN decision_grant dg ON dg.grant_id = gs.grant_id
		JOIN idempotency_record ir ON ir.tenant_id = gs.tenant_id AND ir.idempotency_key = gs.idempotency_key
		WHERE gs.tenant_id = $1
		  AND NOT EXISTS (SELECT 1 FROM execution_receipt r WHERE r.grant_id = gs.grant_id AND r.attempt = gs.attempt)
		ORDER BY gs.spent_at, gs.grant_id`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("%slist dispatching: %w", errPrefix, err)
	}
	defer rows.Close()
	var out []controller.Pending
	for rows.Next() {
		var p controller.Pending
		var corr *string
		if err := rows.Scan(&p.GrantID, &p.TenantID, &corr, &p.IdempotencyKey, &p.LogicalAction, &p.Attempt); err != nil {
			return nil, fmt.Errorf("%sscan dispatching: %w", errPrefix, err)
		}
		if corr != nil {
			p.CorrelationID = *corr
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
