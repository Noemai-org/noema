package export

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Noemai-org/noema/services/core/platform/crypto"
	"github.com/Noemai-org/noema/services/core/record/chain"
)

// rollbackTimeout bounds the cleanup of a request: the rollback runs on a context detached from the request's deadline, and never
// without a limit (as in record/reconstruct).
const rollbackTimeout = 5 * time.Second

// Errors the export returns. Each is a refusal: nothing is exported with it.
var (
	ErrNoSession      = errors.New("export: no session tenant")
	ErrNoConnection   = errors.New("export: no connection to the Record")
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

// Session is what the edge's session check established (EDG-01). The tenant is never an argument of its own (INV-12).
type Session struct{ TenantID string }

// Entry is one entry of the neutral audit_record. The fields are those of AuditEntry in benchmark-arm-result.v1.
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

// AuditRecord is the neutral audit_record: the entries and the digest of the whole.
type AuditRecord struct {
	Entries []Entry `json:"entries"`
	Digest  string  `json:"digest"`
}

var (
	hashRe  = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	atRe    = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]+)?Z$`)
	labelRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
)

// sha256Of is the digest of the canonical JSON of v: "sha256:" and 64 lower-case hex digits.
func sha256Of(v any) (string, error) {
	b, err := crypto.CanonicalJSON(v)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// digestLabel is the domain label of the whole-export digest (the Benchmark Owner's decision on noema#503, option 1; the card, docs#142, the Security Reviewer's finding 4; the
// pattern of A53 for grants): a digest of this structure cannot be replayed or confused as the digest of another structure (a per-entry digest, a Record hash).
const digestLabel = "noema/audit-export/v1"

// Digest is the digest of the whole: sha256 of the label, a zero byte and the canonical JSON of the entries array alone ("sha256:" and 64 lower-case hex digits).
func Digest(entries []Entry) (string, error) {
	if entries == nil {
		entries = []Entry{}
	}
	b, err := crypto.CanonicalJSON(entries)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	h.Write([]byte(digestLabel))
	h.Write([]byte{0})
	h.Write(b)
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

// JSON is the canonical JSON of the audit_record: the same bytes for the same Record.
func (a AuditRecord) JSON() ([]byte, error) { return crypto.CanonicalJSON(a) }

// AuditRecordOf exports one correlation of the session tenant's Record as the neutral audit_record. In one read-only REPEATABLE
// READ transaction it reads the tenant's whole chain through record_read (the database takes the tenant from the login), checks
// the handle is the session tenant's, verifies the chain from genesis and only then builds the entries from those same rows. A
// chain that does not verify, another tenant's handle, an unknown event type or a payload that cannot be read is a refusal.
func AuditRecordOf(ctx context.Context, db TxBeginner, s Session, correlationID string) (AuditRecord, error) {
	if s.TenantID == "" {
		return AuditRecord{}, ErrNoSession
	}
	if noConnection(db) {
		return AuditRecord{}, ErrNoConnection
	}
	if _, ok := ctx.Deadline(); !ok {
		return AuditRecord{}, errors.New("export: the context needs a deadline")
	}
	rows, err := readChain(ctx, db)
	if err != nil {
		return AuditRecord{}, err
	}
	if len(rows) == 0 {
		return AuditRecord{}, ErrNotFound
	}
	if t, err := tenantOf(rows[0]); err != nil || t != s.TenantID {
		return AuditRecord{}, fmt.Errorf("%w (%v)", ErrTenantMismatch, err)
	}
	if _, err := chain.VerifyRows(s.TenantID, rows); err != nil {
		return AuditRecord{}, fmt.Errorf("%w: %w", ErrIntegrity, err)
	}
	var span []chain.Row
	for _, r := range rows {
		if r.CorrelationID == correlationID {
			span = append(span, r)
		}
	}
	if len(span) == 0 {
		return AuditRecord{}, ErrNotFound
	}
	entries, err := entriesOf(span)
	if err != nil {
		return AuditRecord{}, err
	}
	digest, err := Digest(entries)
	if err != nil {
		return AuditRecord{}, err
	}
	return AuditRecord{Entries: entries, Digest: digest}, nil
}

// noConnection is true for a nil interface and for a typed nil pointer (a nil *pgxpool.Pool), either of which would panic at the first call.
func noConnection(db TxBeginner) bool {
	if db == nil {
		return true
	}
	v := reflect.ValueOf(db)
	return v.Kind() == reflect.Pointer && v.IsNil()
}

func readChain(ctx context.Context, db TxBeginner) ([]chain.Row, error) {
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, fmt.Errorf("export: begin: %w", err)
	}
	defer func() {
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout)
		defer cancel()
		_ = tx.Rollback(rctx)
	}()
	return chain.ReadChain(ctx, tx)
}

func tenantOf(r chain.Row) (string, error) {
	var b struct {
		TenantID string `json:"tenant_id"`
	}
	if err := json.Unmarshal([]byte(r.Body), &b); err != nil {
		return "", err
	}
	return b.TenantID, nil
}

// commitmentLabel is the format label of ruling A115 (the Architect, 10 Nov 2026, noema#383): outputs_digest is
// sha256(JCS(["noema/record-export/v1", event_id, payload_hash])). The event id is not exported, so a consumer of the export cannot confirm a guessed
// payload by hashing it (the Record's payload_hash alone is a deterministic, unkeyed function of the payload); a holder of the Record, which has the
// event ids, can recompute every digest.
const commitmentLabel = "noema/record-export/v1"

// entriesOf builds the entries of one verified span. seq is renumbered 1..n; inputs_digest links each entry to the outputs_digest of
// the previous exported one (never to a payload hash, which would reopen the guessing oracle); ids tc_1, tc_2 ... follow the EVIDENCE_READ and
// VERIFICATION_READ events in one chain-order sequence (noema#355, Benchmark Owner, 10 Nov).
func entriesOf(span []chain.Row) ([]Entry, error) {
	entries := make([]Entry, 0, len(span))
	prevOut, reads := []string{}, readIDs{}
	for _, r := range span {
		step, inCase, known := StepOf(r.EventType)
		if !known {
			return nil, fmt.Errorf("%w: %s", ErrUnmapped, r.EventType)
		}
		if !inCase {
			continue
		}
		reads.note(r.EventType)
		at, err := atOf(r)
		if err != nil {
			return nil, err
		}
		if !hashRe.MatchString(r.PayloadHash) {
			return nil, fmt.Errorf("%w: event %s has no payload hash", ErrUnprojectable, r.EventID)
		}
		in, err := sha256Of(prevOut)
		if err != nil {
			return nil, err
		}
		out, err := sha256Of([]string{commitmentLabel, r.EventID, r.PayloadHash})
		if err != nil {
			return nil, err
		}
		e := Entry{Seq: len(entries) + 1, At: at, Step: step, Actor: neutralActor(r.EventType, r.WrittenBy, r.Actor), EvidenceIDs: reads.listFor(step),
			InputsDigest: in, OutputsDigest: out}
		if err := setLabels(&e, r); err != nil {
			return nil, err
		}
		entries = append(entries, e)
		prevOut = []string{out}
	}
	return entries, nil
}

// readIDs numbers the reads of one case in chain order: an evidence read and a verification read take the next tc_n of the same sequence.
type readIDs struct{ evidence, verification, all []string }

func (r *readIDs) note(eventType string) {
	if eventType != "EVIDENCE_READ" && eventType != "VERIFICATION_READ" {
		return
	}
	id := "tc_" + strconv.Itoa(len(r.all)+1)
	r.all = append(r.all, id)
	if eventType == "EVIDENCE_READ" {
		r.evidence = append(r.evidence, id)
	} else {
		r.verification = append(r.verification, id)
	}
}

// listFor: READ_EVIDENCE and DECIDE list the evidence reads so far (a verification read cannot precede a decision); VERIFY lists the verification reads
// at or before it; EXPLAIN lists every id read so far; every other step lists none. The list is empty, never null.
func (r *readIDs) listFor(step string) []string {
	var src []string
	switch step {
	case "READ_EVIDENCE", "DECIDE":
		src = r.evidence
	case "VERIFY":
		src = r.verification
	case "EXPLAIN":
		src = r.all
	}
	return append([]string{}, src...)
}

func atOf(r chain.Row) (string, error) {
	var b struct {
		At string `json:"at"`
	}
	if err := json.Unmarshal([]byte(r.Body), &b); err != nil || !atRe.MatchString(b.At) {
		return "", fmt.Errorf("%w: event %s has no readable time", ErrUnprojectable, r.EventID)
	}
	return b.At, nil
}

// setLabels sets `decision` on DECISION_MADE and `outcome` on VERDICT, and nothing on any other entry (noema#355 item 4).
func setLabels(e *Entry, r chain.Row) error {
	switch r.EventType {
	case "DECISION_MADE":
		var p struct {
			Outcome string `json:"decision_outcome"`
		}
		if err := payloadInto(r, &p); err != nil || !dispositions[p.Outcome] {
			return fmt.Errorf("%w: event %s carries no legal disposition", ErrUnprojectable, r.EventID)
		}
		e.Decision = &p.Outcome
	case "VERDICT":
		label, ok := verdictLabel(r)
		if !ok {
			return fmt.Errorf("%w: event %s carries no readable verdict", ErrUnprojectable, r.EventID)
		}
		e.Outcome = &label
	}
	return nil
}

func payloadInto(r chain.Row, v any) error {
	if r.Payload == nil {
		return errors.New("no payload")
	}
	return json.Unmarshal([]byte(*r.Payload), v)
}

// verdictLabel reads the verdict enum. Outcome writes the verdict document nested under "verdict" (outcome/store), the enum at
// verdict.verdict; the flat form {"verdict": "<enum>"} that the reconstruction reads is accepted too.
func verdictLabel(r chain.Row) (string, bool) {
	var p struct {
		Verdict json.RawMessage `json:"verdict"`
	}
	if payloadInto(r, &p) != nil {
		return "", false
	}
	var flat string
	if json.Unmarshal(p.Verdict, &flat) == nil && labelRe.MatchString(flat) {
		return flat, true
	}
	var nested struct {
		Verdict string `json:"verdict"`
	}
	d := json.NewDecoder(bytes.NewReader(p.Verdict))
	if d.Decode(&nested) == nil && labelRe.MatchString(nested.Verdict) {
		return nested.Verdict, true
	}
	return "", false
}
