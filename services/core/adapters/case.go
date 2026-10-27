package adapters

import (
	"context"
	"time"
)

// This file is the interface of the logical service mock-case (PERD 34.1, 34.4; REQ-ENV-001, REQ-EXE-007,
// REQ-ENV-008). mock-case is the Noema-run case system, not part of hosted Tushe; the core addresses it like
// the other eight logical services. The core powers depend on this interface only (REQ-NFR-006): replacing
// the implementation with a stub that satisfies it changes nothing in Understanding, Trust, Execution or
// Outcome.
//
// Nothing here can move money, close or resolve a case, change a priority, an SLA or a clock, or reach the
// truth or control endpoints: there is no method for any of them (BD-08, REQ-ENV-002).

// TransportClass is what an adapter saw on the wire for one write, before Execution classifies it (PERD 27.1,
// E10). The string values are the ones of execution/controller.TransportClass; a test in that package keeps
// the two lists identical, because adapters must not import execution.
type TransportClass string

// Every class a write adapter can report.
const (
	ClassAck2xx          TransportClass = "ACK_2XX"
	ClassVersionConflict TransportClass = "VERSION_CONFLICT_409_412"
	ClassDuplicate409    TransportClass = "DUPLICATE_409_SAME_KEY"
	// ClassDuplicateCreate: the target refused a create conditional on absence because it already holds a case for the
	// subject and epoch under another key (mock-case: 412 PRECONDITION_FAILED). Same string as the error code DUPLICATE_CREATE.
	ClassDuplicateCreate TransportClass = "DUPLICATE_CREATE"
	ClassRejected4xx     TransportClass = "REJECTED_4XX"
	ClassTimeout         TransportClass = "TIMEOUT"
	ClassConnectionReset TransportClass = "CONNECTION_RESET"
	ClassServer5xx       TransportClass = "SERVER_5XX"
	ClassNoResponse      TransportClass = "NO_RESPONSE"
	ClassDefect          TransportClass = "ADAPTER_NON_RETRYABLE_DEFECT"
)

// WriteResult is the adapter's report of one write. It never decides what the write meant: Execution
// classifies it and Outcome verifies it (INV-04, AGENT-21). A write is never retried inside the adapter
// (REQ-EXE-006).
type WriteResult struct {
	Class TransportClass
	// ExternalAck is the target's own reference: the case reference, the existing one on a replay (REQ-EXE-007).
	// It is empty when the target answered 200 with a semantic PENDING and created nothing.
	ExternalAck string
	// RawCode keeps the raw status next to its mapping (REQ-EXE-007): "201", "200:REPLAY", "200:PENDING",
	// "409:VERSION_MISMATCH", "409:IDEMPOTENCY_CONFLICT", "503", and so on. It is never a free-text message.
	RawCode string
	// KeyCollision is true when the target refused the same idempotency key with a different payload
	// (EC-E06). The caller raises the security event; the adapter only reports it.
	KeyCollision bool
	// CurrentVersionToken is set on a version mismatch (the token the target holds now).
	CurrentVersionToken string
	// RetryAfter is the target's Retry-After on a 429, if it sent one.
	RetryAfter time.Duration
	// Case is the case the target returned, when it returned one.
	Case *CaseRecord
}

// CaseRecord is the normalised fin.case payload (PERD 34.4, [REC]) plus the fields the pack reads
// (created_by_intent, epoch; SoT 15.3). Every read carries a version token; compare it, never parse it.
type CaseRecord struct {
	CaseID          string
	Reference       string
	Status          string // OPEN or ROUTED
	OwnerQueue      string
	Priority        string
	IdempotencyKey  string
	VersionToken    string
	CreatedAt       string // the target's clock, RFC 3339 (SYNTHETIC in the POC)
	TenantID        string
	PrincipalID     string
	TransactionRef  string
	ReasonCode      string // the case reason (PERD 27.2), not a Trust reason code
	CreatedByIntent string
	Epoch           int64
}

// CreateCaseInput is one keyed create. Every field except OwnerQueue and Priority is required.
type CreateCaseInput struct {
	IdempotencyKey  string
	TenantID        string
	PrincipalID     string
	TransactionRef  string
	ReasonCode      string
	CreatedByIntent string
	Epoch           int64
	OwnerQueue      string // optional; the target defaults it
	Priority        string // optional; the target defaults it
	Timeout         time.Duration
}

// RouteCaseInput moves a case to a queue only if ExpectedVersionToken is still current.
type RouteCaseInput struct {
	TenantID             string
	CaseID               string
	OwnerQueue           string
	ExpectedVersionToken string
	Timeout              time.Duration
}

// CaseQuery is a scoped find. Exactly one form is allowed: by IdempotencyKey (with TenantID), or by
// PrincipalID (with TenantID and, optionally, TransactionRef). There is no global search.
type CaseQuery struct {
	TenantID       string
	IdempotencyKey string
	PrincipalID    string
	TransactionRef string
	Limit          int
	Cursor         string
}

// CasePage is a page of cases. Complete is the explicit completeness marker: an empty page with
// Complete=true means "no such case", an incomplete page means "do not conclude absence".
type CasePage struct {
	Items      []CaseRecord
	Complete   bool
	NextCursor string
}

// ReadFailure is why a read produced no usable answer. UNKNOWN and UNAVAILABLE are real answers
// (PERD 34.4): the caller maps them to an evidence status and never to "does not exist".
type ReadFailure string

// Every read failure.
const (
	ReadUnavailable  ReadFailure = "UNAVAILABLE"  // timeout, reset, 5xx, 429, no response
	ReadNotFound     ReadFailure = "NOT_FOUND"    // 404: the id is not in this tenant's scope
	ReadInvalid      ReadFailure = "INVALID"      // 400: the adapter or its caller built a bad query
	ReadUnauthorized ReadFailure = "UNAUTHORIZED" // 401 or 403: the adapter's credential or scope is wrong
	ReadMalformed    ReadFailure = "MALFORMED"    // a 2xx body the adapter cannot read: schema drift is UNKNOWN
)

// ReadError is returned by the read methods. It carries no response body and no credential.
type ReadError struct {
	Failure    ReadFailure
	RawCode    string
	RetryAfter time.Duration
}

func (e *ReadError) Error() string {
	return "case read failed: " + string(e.Failure) + " (" + e.RawCode + ")"
}

// Case is the logical service mock-case as the core sees it.
type Case interface {
	// CreateCase sends one keyed create (Idempotency-Key). A replay with the same payload is an
	// acknowledgement with the existing reference; the same key with another payload is a collision.
	CreateCase(ctx context.Context, in CreateCaseInput) WriteResult
	// RouteCase is a conditional write: a stale token applies nothing and is reported as a version conflict.
	RouteCase(ctx context.Context, in RouteCaseInput) WriteResult
	// GetCase reads one case with its version token.
	GetCase(ctx context.Context, tenantID, caseID string) (CaseRecord, error)
	// FindCases reads a scoped page with its completeness marker.
	FindCases(ctx context.Context, q CaseQuery) (CasePage, error)
}
