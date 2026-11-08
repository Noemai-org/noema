package export

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// ErrNotImplemented is returned by the stub; the implementation commit removes it.
var ErrNotImplemented = errors.New("export: not implemented")

// Errors the export returns. Each is a refusal: nothing is exported with it.
var (
	ErrNoSession      = errors.New("export: no session tenant")
	ErrNotFound       = errors.New("export: no Record event for this correlation")
	ErrIntegrity      = errors.New("export: the Record chain did not verify")
	ErrTenantMismatch = errors.New("export: the Record handle belongs to another tenant than the session")
	ErrUnmapped       = errors.New("export: a Record event type has no row in the step table")
	ErrUnprojectable  = errors.New("export: a Record event cannot be exported under the contract")
)

// TxBeginner is what the export needs: a pool whose connections run as the session tenant's record_read login.
type TxBeginner interface {
	BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error)
}

// Session is what the edge's session check established. The tenant is never an argument of its own (INV-12).
type Session struct{ TenantID string }

// Entry is one entry of the neutral audit_record.
type Entry struct {
	Seq           int      `json:"seq"`
	At            string   `json:"at"`
	Step          string   `json:"step"`
	Actor         string   `json:"actor"`
	EvidenceIDs   []string `json:"evidence_ids"`
	InputsDigest  string   `json:"inputs_digest"`
	OutputsDigest string   `json:"outputs_digest"`
	Decision      *string  `json:"decision"`
	Outcome       *string  `json:"outcome"`
}

// AuditRecord is the neutral audit_record: the entries and a digest of the whole.
type AuditRecord struct {
	Entries []Entry `json:"entries"`
	Digest  string  `json:"digest"`
}

// StepOf returns the neutral step of a Record event type. inCase is false for the system-level types the export leaves out.
func StepOf(eventType string) (step string, inCase, known bool) { return "", false, false }

// Digest is the digest of the whole: sha256 of the canonical JSON of the entries array alone.
func Digest(entries []Entry) (string, error) { return "", ErrNotImplemented }

// JSON is the canonical JSON of the audit_record, the same bytes for the same Record.
func (a AuditRecord) JSON() ([]byte, error) { return nil, ErrNotImplemented }

// AuditRecordOf exports one correlation of the session tenant's Record.
func AuditRecordOf(ctx context.Context, db TxBeginner, s Session, correlationID string) (AuditRecord, error) {
	return AuditRecord{}, ErrNotImplemented
}
