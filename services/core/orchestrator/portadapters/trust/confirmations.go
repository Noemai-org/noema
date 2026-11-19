package trustport

import (
	"context"
	"errors"
	"time"

	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/trust/confirm"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

// Errors of the confirmation path. Callers compare with errors.Is.
var (
	// ErrApproverNotEligible: the approver may not answer this request (confirm.NotEligible). The route returns APPROVER_NOT_ELIGIBLE; the intake turns it into UNKNOWN.
	ErrApproverNotEligible = errors.New("trustport: the approver is not eligible for this confirmation request")
	// ErrWrongRequest: the answer names another request, or is neither ACCEPT nor DECLINE (confirm.WrongRequest). Fail closed.
	ErrWrongRequest = errors.New("trustport: the answer does not belong to this confirmation request")
	// ErrDeclined: the approver declined. The intake has no Declined status yet (it needs one, trust/intake, C1's path), so Status fails closed with this error
	// and Evaluate carries the typed Declined result.
	ErrDeclined = errors.New("trustport: the approver declined the confirmation")
	// ErrNotBuilt is the red-commit stub's answer.
	ErrNotBuilt = errors.New("trustport: not built")
)

// ConfirmationState is the typed result of the full path.
type ConfirmationState int

// The states of the path. The zero value is not a state.
const (
	ConfirmationStateNotRequired ConfirmationState = iota + 1
	ConfirmationStatePending
	ConfirmationStateGiven
	ConfirmationStateDeclined
)

// ConfirmationResult is the answer of Evaluate. RequestID names the open request of a Pending answer; EventID and PlanHash a Given one; Superseded the request a fresh one
// replaced; Expired the request whose expiry was recorded.
type ConfirmationResult struct {
	State      ConfirmationState
	RequestID  string
	EventID    string
	PlanHash   string
	Superseded string
	Expired    string
}

// ConfirmationTerms is what the pack's policy says about one request.
type ConfirmationTerms struct {
	Required    bool
	Requirement string // SELF, MAKER_CHECKER or ROLE:<role>
	ExpiresIn   time.Duration
}

// ConfirmationPolicy reads the terms from the pack's policy.
type ConfirmationPolicy interface {
	Terms(ctx context.Context, a intake.AdmittedIntent) (ConfirmationTerms, error)
}

// FreshValues are the two values of the binding that only the evaluation knows.
type FreshValues struct {
	EvidenceSetHash string
	PolicyVersion   string
}

// FreshBinding gives the evidence-set hash and the policy version of the evaluation that is running now.
type FreshBinding interface {
	Fresh(ctx context.Context, a intake.AdmittedIntent, p intake.Plan) (FreshValues, error)
}

// StoredAnswer is the approver's answer to a request, as the route recorded it.
type StoredAnswer struct {
	EventID string // cfe_...
	Event   confirm.Event
}

// StoredRequest is a request with its answer, if any.
type StoredRequest struct {
	Request confirm.Request
	Answer  *StoredAnswer
}

// NewConfirmationRequest is what the store records (CONFIRMATION_REQUESTED) before it returns the new ID.
type NewConfirmationRequest struct {
	TenantID      string
	CorrelationID string
	IntentID      string
	Version       int
	Binding       confirm.BindingInput
	Requirement   string
	RequesterID   string
	ExpiresAt     int64
	Supersedes    string
}

// ConfirmationStore keeps requests and answers.
type ConfirmationStore interface {
	// Latest is the newest request of the intent of the principal, with its answer.
	Latest(ctx context.Context, tenantID, principalID, intentID string, version int) (StoredRequest, bool, error)
	// Open records a new request and returns its ID.
	Open(ctx context.Context, r NewConfirmationRequest) (string, error)
	// Expire records CONFIRMATION_EXPIRED for the request, once.
	Expire(ctx context.Context, tenantID, requestID string) error
}

// ResolvedConfirmation is an accepted confirmation that resolved to Proceed (migration 0028).
type ResolvedConfirmation struct {
	TenantID    string
	EventID     string
	RequestID   string
	BindingHash string
	AcceptedAt  time.Time
	ExpiresAt   time.Time
}

// ConfirmationResolutions inserts the immutable resolution row, once.
type ConfirmationResolutions interface {
	Record(ctx context.Context, c ResolvedConfirmation) error
}

// ConfirmationDeps are the ports of the path.
type ConfirmationDeps struct {
	Policy      ConfirmationPolicy
	Planner     intake.Planner
	Fresh       FreshBinding
	Store       ConfirmationStore
	Resolutions ConfirmationResolutions
	Clock       clock.Clock
}

// ConfirmationPath implements intake.Confirmations over trust/confirm.
type ConfirmationPath struct{ d ConfirmationDeps }

var _ intake.Confirmations = ConfirmationPath{}

// NewConfirmationPath builds the adapter. A missing port is ErrConfig.
func NewConfirmationPath(d ConfirmationDeps) (ConfirmationPath, error) {
	return ConfirmationPath{d: d}, ErrNotBuilt
}

// Evaluate answers T13 with the typed result.
func (c ConfirmationPath) Evaluate(ctx context.Context, a intake.AdmittedIntent) (ConfirmationResult, error) {
	return ConfirmationResult{}, ErrNotBuilt
}

// Status answers intake.Confirmations.
func (c ConfirmationPath) Status(ctx context.Context, a intake.AdmittedIntent) (intake.Confirmation, error) {
	return intake.Confirmation{}, ErrNotBuilt
}
