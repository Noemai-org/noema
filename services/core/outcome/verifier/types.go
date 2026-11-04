package verifier

import (
	"context"
	"errors"
	"fmt"
	"time"

	ip "github.com/Noemai-org/noema/services/core/internal/predicate"
)

// Verdict is the outcome-verdict.v1 value this verifier produces. PARTIALLY_ACHIEVED and COMPENSATED are not produced
// in OUT-01 (one predicate, no compensation; REQ-RCV-005 is a later task).
type Verdict string

// The verdicts OUT-01 produces.
const (
	Achieved        Verdict = "ACHIEVED"
	NotAchieved     Verdict = "NOT_ACHIEVED"
	PendingExternal Verdict = "PENDING_EXTERNAL"
	Unknown         Verdict = "UNKNOWN"
)

// Reason says why a verdict is what it is. It is stored beside the verdict and is the reason of the RecoveryRequest.
type Reason string

// The closed set of reasons.
const (
	ReasonPredicateTrue          Reason = "PREDICATE_TRUE"
	ReasonVerifiedAbsent         Reason = "VERIFIED_ABSENT"
	ReasonPredicateFalse         Reason = "PREDICATE_FALSE"
	ReasonPostconditionUnchecked Reason = "POSTCONDITION_UNVERIFIED" // the receipt said ACKNOWLEDGED and the target shows nothing (PERD 29.1)
	ReasonPendingCondition       Reason = "PENDING_CONDITION"
	ReasonReadsExhausted         Reason = "READS_EXHAUSTED"
	ReasonIncompleteRead         Reason = "INCOMPLETE_READ"
	ReasonMalformedRead          Reason = "MALFORMED_READ"
	ReasonReadRefused            Reason = "READ_REFUSED"
	ReasonInconsistent           Reason = "INCONSISTENT_EVIDENCE"
	ReasonPredicateUnknown       Reason = "PREDICATE_UNKNOWN"
	ReasonNotQueryableByKey      Reason = "NOT_QUERYABLE_BY_KEY"
	ReasonReceiptUnverifiable    Reason = "RECEIPT_UNVERIFIABLE"
	ReasonWatchUnusable          Reason = "WATCH_UNUSABLE" // the pending condition holds but its watch has no usable deadline (INV-28)
)

// Receipt is the pointer a verification starts from: which key, tenant and attempt were dispatched, and when. It is never
// proof that the effect happened (INV-04, AGENT-21).
type Receipt struct {
	ReceiptID      string
	TenantID       string
	GrantID        string
	PrincipalID    string // the principal the grant was issued to: the scope of the transaction read (INV-12)
	Attempt        int64
	IdempotencyKey string
	LogicalAction  string
	State          string // the execution state the receipt claims
	CorrelationID  string
	DispatchedAt   time.Time
	RawStatus      string  // the row's raw_status: the transport result, then ":" and a raw code when there is one
	ExternalAck    *string // the row's external_ack, nil when the target gave none
	Document       []byte  // the signed receipt as stored, for ReceiptAuthenticity
	SigKeyID       string
	SigValue       string
}

// Definition is the part of an outcome definition the verifier needs (SoT 15.3).
type Definition struct {
	OutcomeID           string
	SuccessPredicate    string
	SettleWindowSeconds int64
	Pending             *Pending // nil when the definition has no pending condition
}

// Pending is a definition's pending condition and the timing of the watch it creates (REQ-VER-004). The caller computes the
// absolute times from the SLA clock of the definition; the verifier checks them and stores them.
type Pending struct {
	Predicate   string
	Triggers    []string
	NextCheckAt time.Time
	DeadlineAt  time.Time
}

// Request is one verification.
type Request struct {
	TenantID  string
	ReceiptID string
	// VerificationID identifies this verification across redeliveries: the id of the outcome.verify outbox event, or of a watch
	// check. A second Verify with the same id returns the stored verdict (the outbox is at least once).
	VerificationID string
	Definition     Definition
	Bindings       map[string]string // reference variables of the predicate (subject_resource, intent_id, ...), from the plan and the intent
	TransactionRef string            // the subject whose cases are read
	QueryableByKey bool              // the capability's retry safety is QUERYABLE_BY_KEY
}

// Result is what a verification decided and stored.
type Result struct {
	VerdictID string
	Verdict   Verdict
	Reason    Reason
	Replayed  bool // the verification had already been decided: this is the stored verdict, and nothing was read or written
}

// Found is a verdict already stored for a verification, with the receipt it was made for.
type Found struct {
	Result
	ReceiptID string
}

// ErrAlreadyVerified is returned by Store.RecordVerdict when another writer stored a verdict for the same verification between
// the lookup and the insert. The verifier then returns that verdict.
var ErrAlreadyVerified = errors.New("verifier: the verification already has a verdict")

// TimestampLayout is the RFC 3339 layout of the contract's Timestamp in UTC; the signed document and the Record events use it.
const TimestampLayout = "2006-01-02T15:04:05.999999999Z07:00"

// ErrSettleWindowOpen is returned, with nothing read and nothing written, while the settle window has not passed.
type ErrSettleWindowOpen struct{ ReadyAt time.Time }

func (e *ErrSettleWindowOpen) Error() string {
	return fmt.Sprintf("verifier: the settle window is open until %s", e.ReadyAt.Format(time.RFC3339))
}

// ErrDefinition is a definition the verifier cannot compile or bind. It is a pack defect, not an evidence verdict.
var ErrDefinition = errors.New("verifier: the outcome definition does not compile")

// Case is one case the target reports. Its fields are the evidence descriptor of the case entity.
type Case struct {
	CaseID          string
	TenantID        string
	PrincipalID     string
	IdempotencyKey  string
	TransactionRef  string
	Status          string
	OwnerQueue      string
	CreatedByIntent string
	Epoch           int64
}

// CaseQuery asks for cases of one tenant by idempotency key, or by transaction within one principal's cases.
type CaseQuery struct {
	TenantID       string
	PrincipalID    string // set on a read by transaction, never together with an idempotency key
	IdempotencyKey string
	TransactionRef string
	Cursor         string
}

// CasePage is a page with its completeness marker: an empty page with Complete means "no such case"; an incomplete page
// means "do not conclude absence" (INV-19).
type CasePage struct {
	Items      []Case
	Complete   bool
	NextCursor string
}

// ReadKind classifies a read that gave no usable page.
type ReadKind string

// The read failures.
const (
	ReadUnavailable ReadKind = "UNAVAILABLE" // timeout, reset, 5xx, 429, no response: retried within the budget
	ReadMalformed   ReadKind = "MALFORMED"   // a 2xx the reader cannot read: schema drift is UNKNOWN, not retried
	ReadRefused     ReadKind = "REFUSED"     // 400, 401, 403, 404: the query or the credential is wrong, not retried
)

// ReadError is a failed read.
type ReadError struct{ Kind ReadKind }

func (e *ReadError) Error() string { return "verifier: read failed: " + string(e.Kind) }

// CaseReader is Outcome's read-only view of the case system: no write method exists on it. An adapter implements it with
// Outcome's own identity (REQ-VER-001 Security); it is never Trust's snapshot.
type CaseReader interface {
	FindCases(ctx context.Context, q CaseQuery) (CasePage, error)
}

// Waiter sleeps between read retries on the injected clock's time (simulated in tests and benchmarks).
type Waiter interface {
	Wait(ctx context.Context, d time.Duration) error
}

// Clock is the injected clock.
type Clock interface{ Now() time.Time }

// IDSource makes the prefixed ULIDs of PERD 32.3.
type IDSource interface {
	ID(prefix string) (string, error)
}

// Sig is a key ID and signature (SoT 43).
type Sig struct{ KeyID, Value string }

// Signer signs the canonical bytes of a verdict. The key is Outcome's own, never Execution's or Trust's (BD-05, Q4 of the
// OUT-01 card).
type Signer interface {
	Sign(canonical []byte) (Sig, error)
}

// Authenticity is the result of checking a receipt's signature.
type Authenticity string

// The results.
const (
	AuthNotChecked Authenticity = "NOT_CHECKED" // no check is installed yet (W1, GOV-28)
	AuthValid      Authenticity = "VALID"
	AuthInvalid    Authenticity = "INVALID"
)

// ReceiptAuthenticity checks a receipt's signature before any read (SoT 13.1). OUT-01 ships no implementation that holds
// a key; the W1 design (GOV-28) supplies Execution's public key.
type ReceiptAuthenticity interface {
	Check(ctx context.Context, r Receipt) (Authenticity, error)
}

// ReadBudget bounds the reads. Backoffs are the waits before each retry; MaxPages bounds the pages of one query. The
// values are configuration (AGENT-16, DEC-2026-10-17): the verifier never supplies a default.
type ReadBudget struct {
	Backoffs []time.Duration
	MaxPages int
}

// ReadRecord is one read attempt, kept as a VERIFICATION_READ Record event.
type ReadRecord struct {
	EvidenceID string
	Query      string // BY_KEY or BY_TRANSACTION
	Attempt    int
	Page       int
	Status     string // OK, UNAVAILABLE, MALFORMED, REFUSED or INCOMPLETE
	Items      int
	At         time.Time
}

// WatchRow is the durable watch of a PENDING_EXTERNAL verdict.
type WatchRow struct {
	WatchID     string
	NextCheckAt time.Time
	DeadlineAt  time.Time
	Triggers    []string
}

// VerdictDoc is the outcome-verdict.v1 object and the fields stored beside it.
type VerdictDoc struct {
	VerdictID        string
	VerificationID   string
	TenantID         string
	CorrelationID    string
	ReceiptID        string
	OutcomeID        string
	Verdict          Verdict
	Reason           Reason
	PredicateResult  *bool
	EvidenceReads    []string
	VerifiedAt       time.Time
	Sig              Sig
	Document         map[string]any
	ReceiptSignature Authenticity
}

// Write is everything one verification stores, in one transaction (REQ-VER-003).
type Write struct {
	Verdict           VerdictDoc
	Reads             []ReadRecord
	RecoveryRequestID string // set exactly when the verdict is not ACHIEVED
	Watch             *WatchRow
	SignatureInvalid  bool // raise SECURITY_EVENT SIGNATURE_INVALID for the receipt
}

// Store loads the receipt a verification starts from and stores its result.
type Store interface {
	// FindVerdict returns the verdict already stored for a verification, if any.
	FindVerdict(ctx context.Context, tenantID, verificationID string) (Found, bool, error)
	LoadReceipt(ctx context.Context, tenantID, receiptID string) (Receipt, error)
	RecordVerdict(ctx context.Context, w Write) error
}

// Config wires a Verifier.
type Config struct {
	Clock        Clock
	Waiter       Waiter
	IDs          IDSource
	Cases        CaseReader
	Store        Store
	Signer       Signer
	Authenticity ReceiptAuthenticity
	Scope        ip.Scope
	Budget       ReadBudget
}
