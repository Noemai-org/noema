package controller

import (
	"context"
	"time"
)

// Everything below is a port: the controller owns the order of the protocol and the rules about what may
// happen between steps, and nothing else. Production implementations (Postgres, the Tushe and case adapters,
// the registry and evidence reads) are separate tasks; the simulation and the integration test supply the rest.

// GrantSource loads a stored grant. A grant that does not exist is an error, not a void.
type GrantSource interface {
	Load(ctx context.Context, tenantID, grantID string) (SignedGrant, error)
}

// KeyVerifier verifies a signature against any active key ID (E1). platform/crypto.KeyRing satisfies it
// through VerifyGrantSignature.
type KeyVerifier interface {
	VerifyWith(keyID string, msg []byte, sigHex string) bool
	SignWith(keyID string, msg []byte) (string, error)
}

// Binder reads the authoritative state that E3 to E6 compare with. It has no side effects.
type Binder interface {
	// LatestIntentHash is the hash of the latest admitted intent of the grant's investigation (E3).
	LatestIntentHash(ctx context.Context, g Grant) (string, error)
	// DecisionStillAllowed reports that the grant's decision outcome is ALLOW and its policy version is still ACTIVE (E4).
	DecisionStillAllowed(ctx context.Context, g Grant) (bool, error)
	// Plan returns the stored plan step the grant names, with the parameters to send (E5).
	Plan(ctx context.Context, g Grant) (PlanStep, error)
	// WritesEnabled is false when the tenant kill switch is on or the circuit is open (E6). An error means
	// "could not read", which the controller treats as disabled (REQ-EXE-008, fail closed).
	WritesEnabled(ctx context.Context, tenantID string) (bool, error)
}

// PlanStep is what E5 compares with the grant and what E9 sends.
type PlanStep struct {
	PlanHash      string
	Step          int64
	CapabilityID  string
	Registered    bool
	Prohibited    bool
	AdapterExists bool
	Params        map[string]any
	KeyFields     KeyFields
	TimeoutMS     int64 // from the capability descriptor
	Conditional   bool  // CONDITIONAL_WRITE: send the grant's target versions as expected versions
}

// KeyFields are the PERD 27.2 inputs that the plan and grant supply; the attempt is never one of them.
type KeyFields struct {
	TransactionID, LogicalAction, ReasonCode, PolicyScope, LogicalResolutionEpoch string
}

// Revalidation is the result of E7.
type Revalidation struct {
	Passed       bool
	ChangedFacts []string // the facts that differ from the evidence set, ownership or freeze
}

// Revalidator re-reads exactly the descriptor's revalidation reads, fresh, and compares them (E7).
// An error means a read could not be made.
type Revalidator interface {
	Revalidate(ctx context.Context, g Grant, step PlanStep) (Revalidation, error)
}

// SpendCommand is everything E8 commits in one transaction.
type SpendCommand struct {
	Grant          Grant
	IdempotencyKey string
	LogicalAction  string
}

// SpendResult is the answer of E8.
type SpendResult struct {
	Spent          bool
	Replay         bool  // the grant ID or its nonce was already spent
	WritesDisabled bool  // the kill switch was read inside the transaction and was on
	Attempt        int64 // the attempt number of this dispatch for the key
}

// Event is a Record event (PERD section 32.2). The Record never acts.
type Event struct {
	Type    string
	Kind    string // SECURITY_EVENT only
	GrantID string
	Detail  string
}

// Ledger is the one place that writes Postgres. Spend and Complete are single transactions; the Record
// events they carry are appended inside them, so a failed append fails the step and nothing is dispatched
// (PERD 32.4).
type Ledger interface {
	// Append writes Record events outside a spend, for example REVALIDATION_PASSED.
	Append(ctx context.Context, tenantID string, events ...Event) error
	// Spend is E8: insert grant_spend (unique grant ID and nonce), insert the idempotency record
	// (DISPATCHING), append GRANT_SPENT and DISPATCH_ATTEMPTED, all or nothing. It reads the kill switch
	// inside the same transaction.
	Spend(ctx context.Context, cmd SpendCommand) (SpendResult, error)
	// Void records GRANT_VOIDED and the RecoveryRequest addressed to Trust, once per grant.
	Void(ctx context.Context, g Grant, reason VoidReason) error
	// Complete is E11: insert the signed receipt (one per grant and attempt; the first writer wins), update
	// the idempotency record, append RECEIPT, and enqueue the Outcome verification. It returns false when a
	// receipt for this grant and attempt already exists.
	Complete(ctx context.Context, r Receipt) (written bool, err error)
	// Dispatching lists spent attempts with no receipt (the sweeper's input).
	Dispatching(ctx context.Context, tenantID string) ([]Pending, error)
}

// Pending is a spent attempt that has no receipt.
type Pending struct {
	GrantID        string
	TenantID       string
	CorrelationID  string
	IdempotencyKey string
	LogicalAction  string
	Attempt        int64
}

// DispatchRequest is the single write of E9.
type DispatchRequest struct {
	TenantID       string
	GrantID        string
	PrincipalID    string // from the grant, which took it from the authenticated session (INV-12)
	IdempotencyKey string
	Attempt        int64
	CapabilityID   string
	TargetRef      string
	Params         map[string]any
	ExpectedFrom   []TargetVersion // empty unless the step is conditional
	TimeoutMS      int64
}

// Dispatcher makes the write. It never retries: a second call is a second write attempt and needs a second
// grant (REQ-EXE-006). A timeout is reported as a class, never as an error that could be read as failure.
type Dispatcher interface {
	Dispatch(ctx context.Context, req DispatchRequest) TransportResult
}

// IDs makes receipt IDs. The real generator is ULID-based (PERD 32.3); the simulation is seeded.
type IDs interface {
	ReceiptID() string
}

// Clock is the platform clock interface.
type Clock interface {
	Now() time.Time
}
