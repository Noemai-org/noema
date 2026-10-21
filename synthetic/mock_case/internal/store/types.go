package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Case is the fin.case payload of PERD §34.4 plus the keys the adapter filters by.
type Case struct {
	CaseID         string    `json:"case_id"`
	Reference      string    `json:"reference"`
	Status         string    `json:"status"`
	OwnerQueue     string    `json:"owner_queue"`
	Priority       string    `json:"priority"`
	IdempotencyKey string    `json:"idempotency_key"`
	VersionToken   string    `json:"version_token"`
	CreatedAt      time.Time `json:"created_at"`
	TenantID       string    `json:"tenant_id"`
	PrincipalID    string    `json:"principal_id"`
	TransactionRef string    `json:"transaction_ref"`
	ReasonCode     string    `json:"reason_code"`
	// CreatedByIntent and Epoch are the fin.case fields the pack's success predicate reads (SoT 15.3).
	CreatedByIntent string `json:"created_by_intent"`
	Epoch           int64  `json:"epoch"`

	Seq     int64 `json:"-"`
	Version int64 `json:"-"`
}

// Complaint is the fin.complaint_ref payload of PERD §34.4.
type Complaint struct {
	ComplaintRef   string    `json:"complaint_ref"`
	CaseID         string    `json:"case_id"`
	IdempotencyKey string    `json:"idempotency_key"`
	LoggedAt       time.Time `json:"logged_at"`
	ClockStartedAt time.Time `json:"clock_started_at"`
	TenantID       string    `json:"tenant_id"`

	Seq int64 `json:"-"`
}

// Attachment is the fin.evidence_attachment payload of PERD §34.4.
type Attachment struct {
	AttachmentID   string    `json:"attachment_id"`
	CaseID         string    `json:"case_id"`
	PayloadHash    string    `json:"payload_hash"`
	IdempotencyKey string    `json:"idempotency_key"`
	AttachedAt     time.Time `json:"attached_at"`
	TenantID       string    `json:"tenant_id"`
	Payload        string    `json:"payload"`

	Seq int64 `json:"-"`
}

// VersionToken renders a case version as the opaque token clients echo back. Compare, never parse.
func VersionToken(v int64) string { return "v" + strconv.FormatInt(v, 10) }

// ParseVersionToken is the inverse of VersionToken.
func ParseVersionToken(s string) (int64, error) {
	if !strings.HasPrefix(s, "v") {
		return 0, errors.New("not a version token")
	}
	n, err := strconv.ParseInt(s[1:], 10, 64)
	if err != nil || n < 1 || VersionToken(n) != s {
		return 0, errors.New("not a version token")
	}
	return n, nil
}

// Outcome says what a command did, in the terms the API needs to pick a status and the log needs to record.
type Outcome int

const (
	Created Outcome = iota
	Replayed
	Found
	Routed
	NotFound
	IdempotencyConflict
	VersionMismatch
)

func (o Outcome) String() string {
	return [...]string{"CREATED", "REPLAYED", "FOUND", "ROUTED", "NOT_FOUND", "IDEMPOTENCY_CONFLICT", "VERSION_MISMATCH"}[o]
}

// Status is the HTTP status the outcome maps to.
func (o Outcome) Status() int {
	switch o {
	case Created:
		return 201
	case Replayed, Found, Routed:
		return 200
	case NotFound:
		return 404
	}
	return 409
}

// ErrorCode is the API error code of a refused outcome ("" for a success).
func (o Outcome) ErrorCode() string {
	switch o {
	case NotFound:
		return "NOT_FOUND"
	case IdempotencyConflict:
		return "IDEMPOTENCY_CONFLICT"
	case VersionMismatch:
		return "VERSION_MISMATCH"
	}
	return ""
}

// Cmd is what the API knows about one call before the store runs it. The store writes it, with the result,
// to command_log in the same transaction as the effect, so a command and its effect are never apart.
type Cmd struct {
	At             time.Time
	Seed           int64
	ScenarioID     string
	FaultPlanID    string
	Operation      string
	CallNo         int64
	CorrelationID  string
	FaultIndex     *int
	FaultEffect    string
	FaultParams    json.RawMessage
	Suppressed     bool // the response will be lost, so the caller never sees the result
	StatusOverride int  // a 5xx that replaces the response after the command committed (0 = none)
}

// Micro is the timestamp precision of the database; every stored time is cut to it first.
func Micro(t time.Time) time.Time { return t.UTC().Truncate(time.Microsecond) }

func (c Cmd) validate() error {
	if c.Operation == "" || c.CallNo < 1 || c.ScenarioID == "" || c.FaultPlanID == "" {
		return fmt.Errorf("incomplete command: %+v", c)
	}
	return nil
}
