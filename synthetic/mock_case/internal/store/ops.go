package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Noemai-org/noema/synthetic/mock_case/internal/ids"
)

const caseColumns = `case_id, seq, tenant_id, reference, principal_id, transaction_ref, reason_code, status, owner_queue,
  priority, idempotency_key, version, created_at, request_hash, created_by_intent, epoch`

func scanCase(row pgx.Row) (Case, string, error) {
	var c Case
	var hash string
	err := row.Scan(&c.CaseID, &c.Seq, &c.TenantID, &c.Reference, &c.PrincipalID, &c.TransactionRef, &c.ReasonCode, &c.Status,
		&c.OwnerQueue, &c.Priority, &c.IdempotencyKey, &c.Version, &c.CreatedAt, &hash, &c.CreatedByIntent, &c.Epoch)
	c.VersionToken = VersionToken(c.Version)
	c.CreatedAt = c.CreatedAt.UTC()
	return c, hash, err
}

// CreateCaseInput is a create-case command with defaults already applied: the idempotency comparison uses
// the effective values, so omitting the queue and naming the default queue are the same payload.
type CreateCaseInput struct {
	TenantID       string
	PrincipalID    string
	TransactionRef string
	ReasonCode     string
	OwnerQueue     string
	Priority       string
	IdempotencyKey string

	CreatedByIntent string
	Epoch           int64
}

func (in CreateCaseInput) requestHash() string {
	return hashJSON(struct {
		Op             string `json:"op"`
		TenantID       string `json:"tenant_id"`
		PrincipalID    string `json:"principal_id"`
		TransactionRef string `json:"transaction_ref"`
		ReasonCode     string `json:"reason_code"`
		OwnerQueue     string `json:"owner_queue"`
		Priority       string `json:"priority"`
		Intent         string `json:"created_by_intent"`
		Epoch          int64  `json:"epoch"`
	}{"create_case", in.TenantID, in.PrincipalID, in.TransactionRef, in.ReasonCode, in.OwnerQueue, in.Priority, in.CreatedByIntent, in.Epoch})
}

// subjectEpochIndex is the unique index that makes create-if-absent atomic (migration 0003).
const subjectEpochIndex = "cases_subject_epoch_uq"

// CreateCase creates a case if none exists for its subject and epoch, or answers a repeat of the same command.
// Same key and same payload returns the existing case (Replayed); same key and a different payload is
// IdempotencyConflict and changes nothing. A new key for a subject and epoch that already has a case is
// PreconditionFailed: nothing is written, no counter value is spent, and the existing case is returned. The
// precondition is enforced by the unique index, in the transaction that would insert, so two concurrent creators
// cannot both succeed.
func (s *Store) CreateCase(ctx context.Context, cmd Cmd, in CreateCaseInput) (Case, Outcome, error) {
	var out Case
	hash := in.requestHash()
	oc, err := s.run(ctx, cmd, func(tx pgx.Tx) (result, error) {
		res := result{tenant: in.TenantID, key: in.IdempotencyKey, reqHash: hash}
		existing, existingHash, err := scanCase(tx.QueryRow(ctx, `SELECT `+caseColumns+` FROM cases WHERE tenant_id = $1 AND idempotency_key = $2`, in.TenantID, in.IdempotencyKey))
		if err == nil {
			res.caseID, res.replay = existing.CaseID, true
			if existingHash == hash {
				out, res.outcome = existing, Replayed
			} else {
				out, res.outcome = Case{}, IdempotencyConflict
			}
			return res, nil
		}
		if err != pgx.ErrNoRows {
			return res, err
		}
		if _, err := tx.Exec(ctx, `SAVEPOINT create_case`); err != nil {
			return res, err
		}
		n, err := nextCounter(ctx, tx, "case")
		if err != nil {
			return res, err
		}
		at := Micro(cmd.At)
		id := ids.CaseID(cmd.Seed, n, at)
		tag, err := tx.Exec(ctx, `
INSERT INTO cases (case_id, seq, tenant_id, reference, principal_id, transaction_ref, reason_code, status, owner_queue, priority,
  idempotency_key, request_hash, version, created_at, updated_at, created_by_intent, epoch)
VALUES ($1,$2,$3,$4,$5,$6,$7,'OPEN',$8,$9,$10,$11,1,$12,$12,$13,$14)
ON CONFLICT (tenant_id, idempotency_key) DO NOTHING`,
			id, n, in.TenantID, ids.CaseReference(at, n), in.PrincipalID, in.TransactionRef, in.ReasonCode, in.OwnerQueue, in.Priority,
			in.IdempotencyKey, hash, at, in.CreatedByIntent, in.Epoch)
		var pe *pgconn.PgError
		if errors.As(err, &pe) && pe.Code == "23505" && pe.ConstraintName == subjectEpochIndex {
			// Back out to before the counter was taken, so the references stay gapless, then answer with the case that exists.
			if _, rerr := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT create_case`); rerr != nil {
				return res, rerr
			}
			found, _, qerr := scanCase(tx.QueryRow(ctx, `SELECT `+caseColumns+` FROM cases
WHERE tenant_id = $1 AND transaction_ref = $2 AND epoch = $3 AND status IN ('OPEN', 'ROUTED')`, in.TenantID, in.TransactionRef, in.Epoch))
			if qerr != nil {
				return res, qerr
			}
			out, res.outcome, res.caseID = found, PreconditionFailed, found.CaseID
			return res, nil
		}
		if err != nil {
			return res, err
		}
		if tag.RowsAffected() == 0 {
			return res, errRetry
		}
		out, _, err = scanCase(tx.QueryRow(ctx, `SELECT `+caseColumns+` FROM cases WHERE case_id = $1`, id))
		res.outcome, res.committed, res.caseID = Created, true, id
		return res, err
	})
	return out, oc, err
}

// GetCase reads one case of a tenant.
func (s *Store) GetCase(ctx context.Context, cmd Cmd, tenant, caseID string) (Case, Outcome, error) {
	var out Case
	oc, err := s.run(ctx, cmd, func(tx pgx.Tx) (result, error) {
		res := result{tenant: tenant, caseID: caseID}
		c, _, err := scanCase(tx.QueryRow(ctx, `SELECT `+caseColumns+` FROM cases WHERE tenant_id = $1 AND case_id = $2`, tenant, caseID))
		if err == pgx.ErrNoRows {
			res.outcome = NotFound
			return res, nil
		}
		out, res.outcome = c, Found
		return res, err
	})
	return out, oc, err
}

// CaseFilter selects cases for a list. TenantID is always required; the API requires either Key or PrincipalID.
type CaseFilter struct {
	TenantID       string
	IdempotencyKey string
	PrincipalID    string
	TransactionRef string
}

// ListCases returns up to limit cases in creation order after afterSeq, and whether more remain.
func (s *Store) ListCases(ctx context.Context, cmd Cmd, f CaseFilter, afterSeq int64, limit int) ([]Case, bool, error) {
	var items []Case
	var more bool
	_, err := s.run(ctx, cmd, func(tx pgx.Tx) (result, error) {
		res := result{tenant: f.TenantID, key: f.IdempotencyKey, outcome: Found}
		rows, err := tx.Query(ctx, `SELECT `+caseColumns+` FROM cases
WHERE tenant_id = $1 AND seq > $2
  AND ($3 = '' OR idempotency_key = $3)
  AND ($4 = '' OR principal_id = $4)
  AND ($5 = '' OR transaction_ref = $5)
ORDER BY seq LIMIT $6`, f.TenantID, afterSeq, f.IdempotencyKey, f.PrincipalID, f.TransactionRef, limit+1)
		if err != nil {
			return res, err
		}
		defer rows.Close()
		items = items[:0]
		for rows.Next() {
			c, _, err := scanCase(rows)
			if err != nil {
				return res, err
			}
			items = append(items, c)
		}
		if err := rows.Err(); err != nil {
			return res, err
		}
		more = len(items) > limit
		if more {
			items = items[:limit]
		}
		return res, nil
	})
	return items, more, err
}

// RouteCase moves a case to a queue if expectedVersion is still its version. A stale version returns
// VersionMismatch with the case as it stands and applies nothing. With touch set, a competing modification
// is applied first (the CONCURRENT_MODIFICATION fault): the version advances by one and the route is refused.
func (s *Store) RouteCase(ctx context.Context, cmd Cmd, tenant, caseID, queue string, expectedVersion int64, touch bool) (Case, Outcome, error) {
	var out Case
	oc, err := s.run(ctx, cmd, func(tx pgx.Tx) (result, error) {
		res := result{tenant: tenant, caseID: caseID}
		cur, _, err := scanCase(tx.QueryRow(ctx, `SELECT `+caseColumns+` FROM cases WHERE tenant_id = $1 AND case_id = $2 FOR NO KEY UPDATE`, tenant, caseID))
		if err == pgx.ErrNoRows {
			res.outcome = NotFound
			return res, nil
		}
		if err != nil {
			return res, err
		}
		at := Micro(cmd.At)
		if touch {
			if _, err := tx.Exec(ctx, `UPDATE cases SET version = version + 1, updated_at = $2 WHERE case_id = $1`, caseID, at); err != nil {
				return res, err
			}
			cur.Version++
			cur.VersionToken = VersionToken(cur.Version)
			out, res.outcome, res.committed = cur, VersionMismatch, true
			return res, nil
		}
		if cur.Version != expectedVersion {
			out, res.outcome = cur, VersionMismatch
			return res, nil
		}
		if _, err := tx.Exec(ctx, `UPDATE cases SET status = 'ROUTED', owner_queue = $2, version = version + 1, updated_at = $3 WHERE case_id = $1`, caseID, queue, at); err != nil {
			return res, err
		}
		cur.Status, cur.OwnerQueue, cur.Version = "ROUTED", queue, cur.Version+1
		cur.VersionToken = VersionToken(cur.Version)
		out, res.outcome, res.committed = cur, Routed, true
		return res, nil
	})
	return out, oc, err
}

// ---- complaints ----

func scanComplaint(row pgx.Row) (Complaint, string, error) {
	var c Complaint
	var hash string
	err := row.Scan(&c.ComplaintRef, &c.Seq, &c.TenantID, &c.CaseID, &c.IdempotencyKey, &c.LoggedAt, &c.ClockStartedAt, &hash)
	c.LoggedAt, c.ClockStartedAt = c.LoggedAt.UTC(), c.ClockStartedAt.UTC()
	return c, hash, err
}

const complaintColumns = `complaint_ref, seq, tenant_id, case_id, idempotency_key, logged_at, clock_started_at, request_hash`

// CreateComplaint logs a complaint reference against a case, with the same idempotency rules as a case.
func (s *Store) CreateComplaint(ctx context.Context, cmd Cmd, tenant, caseID, key string) (Complaint, Outcome, error) {
	var out Complaint
	hash := hashJSON(struct {
		Op     string `json:"op"`
		Tenant string `json:"tenant_id"`
		CaseID string `json:"case_id"`
	}{"create_complaint", tenant, caseID})
	oc, err := s.run(ctx, cmd, func(tx pgx.Tx) (result, error) {
		res := result{tenant: tenant, caseID: caseID, key: key, reqHash: hash}
		existing, existingHash, err := scanComplaint(tx.QueryRow(ctx, `SELECT `+complaintColumns+` FROM complaints WHERE tenant_id = $1 AND idempotency_key = $2`, tenant, key))
		if err == nil {
			res.replay = true
			if existingHash == hash {
				out, res.outcome = existing, Replayed
			} else {
				out, res.outcome = Complaint{}, IdempotencyConflict
			}
			return res, nil
		}
		if err != pgx.ErrNoRows {
			return res, err
		}
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM cases WHERE tenant_id = $1 AND case_id = $2)`, tenant, caseID).Scan(&exists); err != nil {
			return res, err
		}
		if !exists {
			res.outcome = NotFound
			return res, nil
		}
		n, err := nextCounter(ctx, tx, "complaint")
		if err != nil {
			return res, err
		}
		at := Micro(cmd.At)
		ref := ids.ComplaintRef(at, n)
		tag, err := tx.Exec(ctx, `
INSERT INTO complaints (complaint_ref, seq, tenant_id, case_id, idempotency_key, request_hash, logged_at, clock_started_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$7) ON CONFLICT (tenant_id, idempotency_key) DO NOTHING`, ref, n, tenant, caseID, key, hash, at)
		if err != nil {
			return res, err
		}
		if tag.RowsAffected() == 0 {
			return res, errRetry
		}
		out, _, err = scanComplaint(tx.QueryRow(ctx, `SELECT `+complaintColumns+` FROM complaints WHERE complaint_ref = $1`, ref))
		res.outcome, res.committed = Created, true
		return res, err
	})
	return out, oc, err
}

// GetComplaint reads one complaint reference of a tenant.
func (s *Store) GetComplaint(ctx context.Context, cmd Cmd, tenant, ref string) (Complaint, Outcome, error) {
	var out Complaint
	oc, err := s.run(ctx, cmd, func(tx pgx.Tx) (result, error) {
		res := result{tenant: tenant}
		c, _, err := scanComplaint(tx.QueryRow(ctx, `SELECT `+complaintColumns+` FROM complaints WHERE tenant_id = $1 AND complaint_ref = $2`, tenant, ref))
		if err == pgx.ErrNoRows {
			res.outcome = NotFound
			return res, nil
		}
		out, res.outcome, res.caseID = c, Found, c.CaseID
		return res, err
	})
	return out, oc, err
}

// ListComplaints returns complaints by idempotency key or by case, in order, with the same paging as cases.
func (s *Store) ListComplaints(ctx context.Context, cmd Cmd, tenant, key, caseID string, afterSeq int64, limit int) ([]Complaint, bool, error) {
	var items []Complaint
	var more bool
	_, err := s.run(ctx, cmd, func(tx pgx.Tx) (result, error) {
		res := result{tenant: tenant, key: key, caseID: caseID, outcome: Found}
		rows, err := tx.Query(ctx, `SELECT `+complaintColumns+` FROM complaints
WHERE tenant_id = $1 AND seq > $2 AND ($3 = '' OR idempotency_key = $3) AND ($4 = '' OR case_id = $4)
ORDER BY seq LIMIT $5`, tenant, afterSeq, key, caseID, limit+1)
		if err != nil {
			return res, err
		}
		defer rows.Close()
		items = items[:0]
		for rows.Next() {
			c, _, err := scanComplaint(rows)
			if err != nil {
				return res, err
			}
			items = append(items, c)
		}
		if err := rows.Err(); err != nil {
			return res, err
		}
		more = len(items) > limit
		if more {
			items = items[:limit]
		}
		return res, nil
	})
	return items, more, err
}

// ---- evidence ----

const evidenceColumns = `attachment_id, seq, tenant_id, case_id, payload, payload_hash, idempotency_key, attached_at, request_hash`

func scanAttachment(row pgx.Row) (Attachment, string, error) {
	var a Attachment
	var hash string
	err := row.Scan(&a.AttachmentID, &a.Seq, &a.TenantID, &a.CaseID, &a.Payload, &a.PayloadHash, &a.IdempotencyKey, &a.AttachedAt, &hash)
	a.AttachedAt = a.AttachedAt.UTC()
	return a, hash, err
}

// AppendEvidence appends an attachment to a case. The payload hash is the SHA-256 of the exact bytes received.
// Evidence is append-only: there is no update or delete, here or in the database.
func (s *Store) AppendEvidence(ctx context.Context, cmd Cmd, tenant, caseID, key, payload string) (Attachment, Outcome, error) {
	var out Attachment
	payloadHash := PayloadHash(payload)
	hash := hashJSON(struct {
		Op          string `json:"op"`
		Tenant      string `json:"tenant_id"`
		CaseID      string `json:"case_id"`
		PayloadHash string `json:"payload_hash"`
	}{"append_evidence", tenant, caseID, payloadHash})
	oc, err := s.run(ctx, cmd, func(tx pgx.Tx) (result, error) {
		res := result{tenant: tenant, caseID: caseID, key: key, reqHash: hash}
		existing, existingHash, err := scanAttachment(tx.QueryRow(ctx, `SELECT `+evidenceColumns+` FROM evidence WHERE tenant_id = $1 AND idempotency_key = $2`, tenant, key))
		if err == nil {
			res.replay = true
			if existingHash == hash {
				out, res.outcome = existing, Replayed
			} else {
				out, res.outcome = Attachment{}, IdempotencyConflict
			}
			return res, nil
		}
		if err != pgx.ErrNoRows {
			return res, err
		}
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM cases WHERE tenant_id = $1 AND case_id = $2)`, tenant, caseID).Scan(&exists); err != nil {
			return res, err
		}
		if !exists {
			res.outcome = NotFound
			return res, nil
		}
		n, err := nextCounter(ctx, tx, "evidence")
		if err != nil {
			return res, err
		}
		at := Micro(cmd.At)
		id := ids.AttachmentID(cmd.Seed, n, at)
		tag, err := tx.Exec(ctx, `
INSERT INTO evidence (attachment_id, seq, tenant_id, case_id, payload, payload_hash, idempotency_key, request_hash, attached_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT (tenant_id, idempotency_key) DO NOTHING`,
			id, n, tenant, caseID, payload, payloadHash, key, hash, at)
		if err != nil {
			return res, err
		}
		if tag.RowsAffected() == 0 {
			return res, errRetry
		}
		out, _, err = scanAttachment(tx.QueryRow(ctx, `SELECT `+evidenceColumns+` FROM evidence WHERE attachment_id = $1`, id))
		res.outcome, res.committed = Created, true
		return res, err
	})
	return out, oc, err
}

// GetEvidence reads one attachment of a tenant.
func (s *Store) GetEvidence(ctx context.Context, cmd Cmd, tenant, id string) (Attachment, Outcome, error) {
	var out Attachment
	oc, err := s.run(ctx, cmd, func(tx pgx.Tx) (result, error) {
		res := result{tenant: tenant}
		a, _, err := scanAttachment(tx.QueryRow(ctx, `SELECT `+evidenceColumns+` FROM evidence WHERE tenant_id = $1 AND attachment_id = $2`, tenant, id))
		if err == pgx.ErrNoRows {
			res.outcome = NotFound
			return res, nil
		}
		out, res.outcome, res.caseID = a, Found, a.CaseID
		return res, err
	})
	return out, oc, err
}

// ListEvidence returns attachments by idempotency key or by case, in order, with the same paging as cases.
func (s *Store) ListEvidence(ctx context.Context, cmd Cmd, tenant, key, caseID string, afterSeq int64, limit int) ([]Attachment, bool, error) {
	var items []Attachment
	var more bool
	_, err := s.run(ctx, cmd, func(tx pgx.Tx) (result, error) {
		res := result{tenant: tenant, key: key, caseID: caseID, outcome: Found}
		rows, err := tx.Query(ctx, `SELECT `+evidenceColumns+` FROM evidence
WHERE tenant_id = $1 AND seq > $2 AND ($3 = '' OR idempotency_key = $3) AND ($4 = '' OR case_id = $4)
ORDER BY seq LIMIT $5`, tenant, afterSeq, key, caseID, limit+1)
		if err != nil {
			return res, err
		}
		defer rows.Close()
		items = items[:0]
		for rows.Next() {
			a, _, err := scanAttachment(rows)
			if err != nil {
				return res, err
			}
			items = append(items, a)
		}
		if err := rows.Err(); err != nil {
			return res, err
		}
		more = len(items) > limit
		if more {
			items = items[:limit]
		}
		return res, nil
	})
	return items, more, err
}
