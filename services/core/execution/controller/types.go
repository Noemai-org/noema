package controller

import "errors"

// Grant is the signed part of a Decision Grant that Execution reads (contracts/schemas/decision-grant.v1.json).
// Its canonical JSON, without the signature, is what the HMAC covers (REQ-TRU-008). Only trust/grants builds one.
type Grant struct {
	SchemaVersion   string          `json:"schema_version"`
	GrantID         string          `json:"grant_id"`
	DecisionID      string          `json:"decision_id"`
	PrincipalID     string          `json:"principal_id"`
	IntentHash      string          `json:"intent_hash"`
	CapabilityID    string          `json:"capability_id"`
	TargetRef       string          `json:"target_ref"`
	ParamsHash      string          `json:"params_hash"`
	EvidenceSetHash string          `json:"evidence_set_hash"`
	PlanHash        string          `json:"plan_hash"`
	PlanStep        int64           `json:"plan_step"`
	TargetVersions  []TargetVersion `json:"target_versions"`
	PolicyVersion   string          `json:"policy_version"`
	PackVersion     string          `json:"pack_version"`
	NotBefore       string          `json:"not_before"`
	ExpiresAt       string          `json:"expires_at"`
	Nonce           string          `json:"nonce"`
	SingleUse       bool            `json:"single_use"`
	TenantID        string          `json:"tenant_id,omitempty"`
	CorrelationID   string          `json:"correlation_id,omitempty"`
}

// TargetVersion is an expected version token of a target (INV-27).
type TargetVersion struct {
	ResourceRef  string `json:"resource_ref"`
	Source       string `json:"source"`
	VersionToken string `json:"version_token"`
}

// Sig is a key ID and the lower-case hex HMAC-SHA-256 (contracts common Sig).
type Sig struct {
	KeyID string `json:"key_id"`
	Value string `json:"value"`
}

// SignedGrant is a grant as stored.
type SignedGrant struct {
	Grant Grant
	Sig   Sig
}

// VoidReason is the reason a grant is void (PERD section 27.1). A void grant produces a RecoveryRequest
// addressed to Trust and is never retried locally (REQ-EXE-001). Eight reasons void a grant before its write; the
// ninth, REJECTED_STALE_VERSION, voids it after the one write, in the same step as the receipt (REQ-EXE-004).
type VoidReason string

// The reasons of PERD section 27.1 and REQ-EXE-003.
const (
	VoidSignatureInvalid   VoidReason = "SIGNATURE_INVALID"
	VoidExpired            VoidReason = "EXPIRED"
	VoidIntentSuperseded   VoidReason = "INTENT_SUPERSEDED"
	VoidPolicySuperseded   VoidReason = "POLICY_SUPERSEDED"
	VoidBindingMismatch    VoidReason = "BINDING_MISMATCH"
	VoidWritesDisabled     VoidReason = "WRITES_DISABLED"
	VoidStateChanged       VoidReason = "STATE_CHANGED"
	VoidRevalidationFailed VoidReason = "REVALIDATION_UNAVAILABLE"
	VoidTargetVersionStale VoidReason = "REJECTED_STALE_VERSION"
)

const (
	errPrefix                 = "controller: "
	transportSweeperNoReceipt = "SWEEPER_NO_RECEIPT"
	eventRevalidationPassed   = "REVALIDATION_PASSED"
	eventRevalidationFailed   = "REVALIDATION_FAILED"
	eventSecurity             = "SECURITY_EVENT"
	receiptSchemaVersion      = "1.0"
	kindIdempotencyCollision  = "IDEMPOTENCY_COLLISION"
	kindSignatureInvalid      = "SIGNATURE_INVALID"
)

// The cause carried by the SECURITY_EVENT SIGNATURE_INVALID of a receipt that did not verify (DEC-2026-10-28 section 7).
const (
	CauseSignatureNotVerified = "RECEIPT_SIGNATURE_NOT_VERIFIED"
	CauseDocumentNotCanonical = "RECEIPT_DOCUMENT_NOT_CANONICAL"
)

// ErrKeyCollision is what a Ledger returns from Spend when the idempotency key already exists with other
// parameters or another action (EC-E06). Nothing was spent; the controller voids the grant.
var ErrKeyCollision = errors.New(errPrefix + "idempotency key collision")

// ExecutionState is the Execution Receipt state (contracts enums ExecutionState). Execution never writes
// SUCCEEDED: that word belongs to Outcome.
type ExecutionState string

// The six states of the contract.
const (
	StateDispatched           ExecutionState = "DISPATCHED"
	StateAcknowledged         ExecutionState = "ACKNOWLEDGED"
	StateOutcomeUnknown       ExecutionState = "OUTCOME_UNKNOWN"
	StateRejectedByTarget     ExecutionState = "REJECTED_BY_TARGET"
	StateRejectedStaleVersion ExecutionState = "REJECTED_STALE_VERSION"
	StateFailedTerminal       ExecutionState = "FAILED_TERMINAL"
)

// TransportClass is what the adapter saw on the wire, before classification (PERD section 27.1, E10).
type TransportClass string

// Every transport class the adapter can report.
const (
	TransportAck2xx          TransportClass = "ACK_2XX"
	TransportVersionConflict TransportClass = "VERSION_CONFLICT_409_412"
	TransportDuplicate409    TransportClass = "DUPLICATE_409_SAME_KEY"
	// TransportDuplicateCreate: a create conditional on absence was refused because the target already holds a case for
	// the subject and epoch under another key. The string is the error code DUPLICATE_CREATE of contracts/schemas/errors.v2.json.
	TransportDuplicateCreate TransportClass = "DUPLICATE_CREATE"
	TransportRejected4xx     TransportClass = "REJECTED_4XX"
	TransportTimeout         TransportClass = "TIMEOUT"
	TransportConnectionReset TransportClass = "CONNECTION_RESET"
	TransportServer5xx       TransportClass = "SERVER_5XX"
	TransportNoResponse      TransportClass = "NO_RESPONSE"
	TransportDefect          TransportClass = "ADAPTER_NON_RETRYABLE_DEFECT"
)

// TransportResult is the adapter's report. ExternalAck is the target's own reference (an existing one on a
// duplicate). RawCode keeps the raw status next to the semantic mapping (REQ-EXE-007).
type TransportResult struct {
	Class       TransportClass
	ExternalAck string
	RawCode     string
	// KeyCollision is true when the target refused the same key with another payload (EC-E06).
	KeyCollision bool
}

// Receipt is the Execution Receipt (contracts/schemas/execution-receipt.v1.json).
type Receipt struct {
	ReceiptID       string
	GrantID         string
	TenantID        string
	CorrelationID   string
	IdempotencyKey  string
	LogicalAction   string
	Attempt         int64
	DispatchedAt    string
	TransportResult string // the transport class only (contract: ^[A-Z][A-Z0-9_]*$)
	RawCode         string // the raw status next to the class (REQ-EXE-007); stored and put on the Record, not signed
	ExternalAck     *string
	State           ExecutionState
	Sig             Sig
}

// Outcome of one Execute call. Exactly one field group is set.
type Outcome struct {
	Void     VoidReason // the grant was voided at a step; no write was made
	Replayed bool       // the grant was already spent; nothing was done (E8 conflict)
	Receipt  *Receipt   // E11 ran: the receipt that was written
	Raced    bool       // another writer already wrote this attempt's receipt (first writer wins)
}

// Point is a step boundary of the protocol. A crash can be injected at every one (D7).
type Point int

// The boundaries, in order. A crash "in" a transaction happens before it commits, so the transaction leaves nothing.
const (
	PointBeforeE1 Point = iota
	PointAfterE1
	PointAfterE2
	PointAfterE3
	PointAfterE4
	PointAfterE5
	PointAfterE6
	PointAfterE7
	PointInSpendBeforeCommit
	PointAfterSpend
	PointAfterDispatch
	PointAfterClassify
	PointInCompleteBeforeCommit
	PointAfterComplete
	pointCount
)

// Points lists every boundary.
func Points() []Point {
	out := make([]Point, pointCount)
	for i := range out {
		out[i] = Point(i)
	}
	return out
}

// ErrCrash is returned by a Boundary hook to stand for the process dying at that point: Execute returns at
// once, without cleanup, and only what was committed survives.
var ErrCrash = errors.New(errPrefix + "crash injected")
