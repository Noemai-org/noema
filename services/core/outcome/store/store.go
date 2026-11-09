package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/outcome/verifier"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/crypto"
	"github.com/Noemai-org/noema/services/core/record/chain"
)

const (
	errPrefix = "outcome store: "
	writer    = "core-outcome"

	eventRead     = "VERIFICATION_READ"
	eventVerdict  = "VERDICT"
	eventRecovery = "RECOVERY_REQUESTED"
	eventWatch    = "WATCH_CREATED"
	eventSecurity = "SECURITY_EVENT"
	securityKind  = "SIGNATURE_INVALID"
	topicRecovery = "recovery.requested"
	idOutbox      = "obx"

	pgUniqueViolation  = "23505"
	verificationKeyCon = "outcome_verdict_verification_key"
)

// IDSource makes the prefixed ULIDs of PERD 32.3.
type IDSource interface {
	ID(prefix string) (string, error)
}

// Store implements verifier.Store on PostgreSQL. Its Pool must run as the outcome_rw role (db.OpenAsRole): the database,
// not this code, bounds what Outcome may write (REQ-DATA-001). RecordVerdict is one transaction: the database function
// outcome_record_verdict writes the verdict, its RecoveryRequest and its watch, and the Record events and the outbox
// event go in beside them, so a failed append fails the whole verdict and nothing is stored (EC-E16, PERD 32.4).
type Store struct {
	Pool  *pgxpool.Pool
	Clock clock.Clock
	IDs   IDSource
	Rec   *chain.Appender
	// RollbackTimeout bounds the rollback after a failed write. It must be positive: the value is a safety parameter that the deployment owner
	// sets (no default is chosen here, REQ-AGT-005), and a store without one refuses to write rather than roll back without a limit.
	RollbackTimeout time.Duration
}

var _ verifier.Store = (*Store)(nil)

func needDeadline(ctx context.Context) error {
	if _, ok := ctx.Deadline(); !ok {
		return errors.New(errPrefix + "the context needs a deadline")
	}
	return nil
}

// LoadReceipt reads the receipt Outcome starts from, with the correlation ID of its grant. The receipt is a pointer to
// what was dispatched; it is not evidence that anything happened (INV-04).
func (s *Store) LoadReceipt(ctx context.Context, tenantID, receiptID string) (verifier.Receipt, error) {
	if err := needDeadline(ctx); err != nil {
		return verifier.Receipt{}, err
	}
	var r verifier.Receipt
	var corr, principal *string
	err := s.Pool.QueryRow(ctx, `SELECT r.receipt_id, r.tenant_id, r.grant_id, r.attempt, r.idempotency_key, r.logical_action, r.execution_state,
			g.json->>'correlation_id', g.json->>'principal_id', r.dispatched_at, r.raw_status, r.external_ack, r.json, r.sig_key_id, r.sig_value
		FROM execution_receipt r JOIN decision_grant g ON g.grant_id = r.grant_id AND g.tenant_id = r.tenant_id
		WHERE r.tenant_id = $1 AND r.receipt_id = $2`, tenantID, receiptID).
		Scan(&r.ReceiptID, &r.TenantID, &r.GrantID, &r.Attempt, &r.IdempotencyKey, &r.LogicalAction, &r.State, &corr, &principal, &r.DispatchedAt, &r.RawStatus, &r.ExternalAck, &r.Document, &r.SigKeyID, &r.SigValue)
	if errors.Is(err, pgx.ErrNoRows) {
		return verifier.Receipt{}, fmt.Errorf("%sreceipt %s does not exist for tenant %s", errPrefix, receiptID, tenantID)
	}
	if err != nil {
		return verifier.Receipt{}, fmt.Errorf("%sload receipt: %w", errPrefix, err)
	}
	if corr == nil || *corr == "" {
		return verifier.Receipt{}, fmt.Errorf("%sthe grant of receipt %s carries no correlation_id", errPrefix, receiptID)
	}
	if principal == nil || *principal == "" {
		return verifier.Receipt{}, fmt.Errorf("%sthe grant of receipt %s names no principal", errPrefix, receiptID)
	}
	r.CorrelationID, r.PrincipalID = *corr, *principal
	r.DispatchedAt = r.DispatchedAt.UTC()
	return r, nil
}

// FindVerdict returns the verdict already stored for a verification, so a redelivery of outcome.verify finds it (INV-04 needs
// one verdict per verification, and a second RecoveryRequest would send Trust the same case twice).
func (s *Store) FindVerdict(ctx context.Context, tenantID, verificationID string) (verifier.Found, bool, error) {
	if err := needDeadline(ctx); err != nil {
		return verifier.Found{}, false, err
	}
	var f verifier.Found
	var verdict, reason string
	err := s.Pool.QueryRow(ctx, `SELECT verdict_id, receipt_id, verdict, reason FROM outcome_verdict WHERE tenant_id = $1 AND verification_id = $2`,
		tenantID, verificationID).Scan(&f.VerdictID, &f.ReceiptID, &verdict, &reason)
	if errors.Is(err, pgx.ErrNoRows) {
		return verifier.Found{}, false, nil
	}
	if err != nil {
		return verifier.Found{}, false, fmt.Errorf("%sfind verdict: %w", errPrefix, err)
	}
	f.Verdict, f.Reason = verifier.Verdict(verdict), verifier.Reason(reason)
	return f, true, nil
}

// rollback rolls the transaction back on a context detached from the caller's cancellation but with a deadline of its own, so that a stalled
// database cannot hold the connection after the answer is decided. Its error is returned for the caller to report with the original one.
func rollback(ctx context.Context, tx pgx.Tx, limit time.Duration) error {
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), limit)
	defer cancel()
	if err := tx.Rollback(rctx); err != nil {
		return fmt.Errorf("%srollback: %w", errPrefix, err)
	}
	return nil
}

// RecordVerdict stores a verification: see Store. If another delivery of the same verification stored its verdict first, it
// returns verifier.ErrAlreadyVerified and stores nothing.
func (s *Store) RecordVerdict(ctx context.Context, w verifier.Write) error {
	if err := needDeadline(ctx); err != nil {
		return err
	}
	if s.RollbackTimeout <= 0 {
		return errors.New(errPrefix + "the store needs a positive rollback timeout")
	}
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("%sbegin: %w", errPrefix, err)
	}
	if err := s.write(ctx, tx, w); err != nil {
		rbErr := rollback(ctx, tx, s.RollbackTimeout)
		var pe *pgconn.PgError
		if errors.As(err, &pe) && pe.Code == pgUniqueViolation && pe.ConstraintName == verificationKeyCon {
			if rbErr != nil { // the duplicate is real, but so is the failed cleanup: report both, with the classification kept for errors.Is
				return errors.Join(verifier.ErrAlreadyVerified, rbErr)
			}
			return verifier.ErrAlreadyVerified
		}
		return errors.Join(err, rbErr)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("%scommit verdict %s: %w", errPrefix, w.Verdict.VerdictID, err)
	}
	return nil
}

func (s *Store) record(ctx context.Context, tx pgx.Tx, v verifier.VerdictDoc, eventType, kind, objectRef string, payload any) error {
	_, err := s.Rec.Append(ctx, tx, chain.Event{TenantID: v.TenantID, CorrelationID: v.CorrelationID, WrittenBy: writer, Actor: writer, EventType: eventType,
		Kind: kind, ObjectRef: objectRef, Payload: payload})
	if err != nil {
		return fmt.Errorf("%sappend %s: %w", errPrefix, eventType, err)
	}
	return nil
}

func (s *Store) write(ctx context.Context, tx pgx.Tx, w verifier.Write) error {
	v := w.Verdict
	if w.SignatureInvalid {
		if err := s.record(ctx, tx, v, eventSecurity, securityKind, v.ReceiptID, map[string]any{"receipt_id": v.ReceiptID, "verdict_id": v.VerdictID}); err != nil {
			return err
		}
	}
	for _, r := range w.Reads {
		payload := map[string]any{"receipt_id": v.ReceiptID, "verdict_id": v.VerdictID, "query": r.Query, "attempt": r.Attempt, "page": r.Page, "status": r.Status,
			"items": r.Items, "read_at": r.At.UTC().Format(verifier.TimestampLayout)}
		if err := s.record(ctx, tx, v, eventRead, "", r.EvidenceID, payload); err != nil {
			return err
		}
	}
	doc, err := crypto.CanonicalJSON(v.Document)
	if err != nil {
		return fmt.Errorf("%scanonical verdict: %w", errPrefix, err)
	}
	var recovery, watchID *string
	var next, deadline any
	var triggers []string
	if w.RecoveryRequestID != "" {
		recovery = &w.RecoveryRequestID
	}
	if w.Watch != nil {
		watchID, next, deadline, triggers = &w.Watch.WatchID, w.Watch.NextCheckAt, w.Watch.DeadlineAt, w.Watch.Triggers
	}
	if _, err := tx.Exec(ctx, `SELECT outcome_record_verdict($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14::jsonb, $15, $16, $17, $18, $19)`,
		v.TenantID, v.VerdictID, v.ReceiptID, v.VerificationID, v.CorrelationID, v.OutcomeID, string(v.Verdict), string(v.Reason), v.PredicateResult, v.EvidenceReads, v.VerifiedAt,
		v.Sig.KeyID, v.Sig.Value, string(doc), recovery, watchID, next, deadline, triggers); err != nil {
		return fmt.Errorf("%swrite verdict %s: %w", errPrefix, v.VerdictID, err)
	}
	verdictPayload := map[string]any{"verdict": v.Document, "reason": string(v.Reason), "receipt_signature": string(v.ReceiptSignature), "verification_id": v.VerificationID}
	if err := s.record(ctx, tx, v, eventVerdict, "", v.VerdictID, verdictPayload); err != nil {
		return err
	}
	if recovery != nil {
		payload := map[string]any{"recovery_request_id": *recovery, "verdict_id": v.VerdictID, "reason": string(v.Reason)}
		if err := s.record(ctx, tx, v, eventRecovery, "", *recovery, payload); err != nil {
			return err
		}
		if err := s.outbox(ctx, tx, v.TenantID, payload); err != nil {
			return err
		}
	}
	if w.Watch != nil {
		payload := map[string]any{"watch_id": w.Watch.WatchID, "verdict_id": v.VerdictID, "next_check_at": w.Watch.NextCheckAt.UTC().Format(verifier.TimestampLayout),
			"deadline_at": w.Watch.DeadlineAt.UTC().Format(verifier.TimestampLayout), "triggers": w.Watch.Triggers}
		return s.record(ctx, tx, v, eventWatch, "", w.Watch.WatchID, payload)
	}
	return nil
}

// outbox enqueues the wake-up for Trust's recovery evaluation, in the verdict's transaction (REQ-VER-005 pattern).
func (s *Store) outbox(ctx context.Context, tx pgx.Tx, tenantID string, payload any) error {
	id, err := s.IDs.ID(idOutbox)
	if err != nil {
		return fmt.Errorf("%soutbox id: %w", errPrefix, err)
	}
	body, err := crypto.CanonicalJSON(payload)
	if err != nil {
		return fmt.Errorf("%soutbox payload: %w", errPrefix, err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO outbox_event (outbox_id, tenant_id, topic, payload, created_at) VALUES ($1, $2, $3, $4::jsonb, $5)`,
		id, tenantID, topicRecovery, string(body), s.Clock.Now().UTC()); err != nil {
		return fmt.Errorf("%sinsert outbox event: %w", errPrefix, err)
	}
	return nil
}
