package store

import (
	"context"
	"encoding/json"
	"time"
)

// Snapshot is the authoritative state, as the truth endpoint exports it for the benchmark oracle.
type Snapshot struct {
	Cases      []Case         `json:"cases"`
	Complaints []Complaint    `json:"complaints"`
	Evidence   []Attachment   `json:"evidence"`
	Counters   map[string]int `json:"counters"`
}

// CommandRecord is one row of command_log.
type CommandRecord struct {
	Seq                int64           `json:"seq"`
	At                 time.Time       `json:"at"`
	ScenarioID         string          `json:"scenario_id"`
	Seed               int64           `json:"seed"`
	FaultPlanID        string          `json:"fault_plan_id"`
	Operation          string          `json:"operation"`
	CallNo             int64           `json:"call_no"`
	TenantID           *string         `json:"tenant_id"`
	CaseID             *string         `json:"case_id"`
	IdempotencyKey     *string         `json:"idempotency_key"`
	CorrelationID      *string         `json:"correlation_id"`
	StatusCode         int             `json:"status_code"`
	ErrorCode          *string         `json:"error_code"`
	Committed          bool            `json:"committed"`
	Replay             bool            `json:"replay"`
	FaultIndex         *int            `json:"fault_index"`
	FaultEffect        *string         `json:"fault_effect"`
	FaultParams        json.RawMessage `json:"fault_params"`
	ResponseSuppressed bool            `json:"response_suppressed"`
}

// Snapshot reads all state in one transaction, so it is a consistent cut.
func (s *Store) Snapshot(ctx context.Context) (Snapshot, error) {
	snap := Snapshot{Cases: []Case{}, Complaints: []Complaint{}, Evidence: []Attachment{}, Counters: map[string]int{}}
	tx, err := s.app.Begin(ctx)
	if err != nil {
		return snap, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, `SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY`); err != nil {
		return snap, err
	}
	rows, err := tx.Query(ctx, `SELECT `+caseColumns+` FROM cases ORDER BY seq`)
	if err != nil {
		return snap, err
	}
	for rows.Next() {
		c, _, err := scanCase(rows)
		if err != nil {
			rows.Close()
			return snap, err
		}
		snap.Cases = append(snap.Cases, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return snap, err
	}
	rows, err = tx.Query(ctx, `SELECT `+complaintColumns+` FROM complaints ORDER BY seq`)
	if err != nil {
		return snap, err
	}
	for rows.Next() {
		c, _, err := scanComplaint(rows)
		if err != nil {
			rows.Close()
			return snap, err
		}
		snap.Complaints = append(snap.Complaints, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return snap, err
	}
	rows, err = tx.Query(ctx, `SELECT `+evidenceColumns+` FROM evidence ORDER BY seq`)
	if err != nil {
		return snap, err
	}
	for rows.Next() {
		a, _, err := scanAttachment(rows)
		if err != nil {
			rows.Close()
			return snap, err
		}
		snap.Evidence = append(snap.Evidence, a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return snap, err
	}
	rows, err = tx.Query(ctx, `SELECT kind, value FROM counters`)
	if err != nil {
		return snap, err
	}
	for rows.Next() {
		var kind string
		var v int
		if err := rows.Scan(&kind, &v); err != nil {
			rows.Close()
			return snap, err
		}
		snap.Counters[kind] = v
	}
	rows.Close()
	return snap, rows.Err()
}

// Commands returns command_log rows after afterSeq in order; faultsOnly keeps the rows where a fault applied.
func (s *Store) Commands(ctx context.Context, afterSeq int64, limit int, faultsOnly bool) ([]CommandRecord, error) {
	rows, err := s.app.Query(ctx, `
SELECT seq, at, scenario_id, seed, fault_plan_id, operation, call_no, tenant_id, case_id, idempotency_key, correlation_id,
       status_code, error_code, committed, replay, fault_index, fault_effect, fault_params, response_suppressed
FROM command_log WHERE seq > $1 AND (NOT $3 OR fault_effect IS NOT NULL) ORDER BY seq LIMIT $2`, afterSeq, limit, faultsOnly)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CommandRecord{}
	for rows.Next() {
		var r CommandRecord
		var params []byte
		if err := rows.Scan(&r.Seq, &r.At, &r.ScenarioID, &r.Seed, &r.FaultPlanID, &r.Operation, &r.CallNo, &r.TenantID, &r.CaseID,
			&r.IdempotencyKey, &r.CorrelationID, &r.StatusCode, &r.ErrorCode, &r.Committed, &r.Replay, &r.FaultIndex, &r.FaultEffect,
			&params, &r.ResponseSuppressed); err != nil {
			return nil, err
		}
		r.At = r.At.UTC()
		if params != nil {
			r.FaultParams = json.RawMessage(params)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
