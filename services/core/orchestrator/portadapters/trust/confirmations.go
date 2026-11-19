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
	// ErrApproverNotEligible: the approver may not answer this request (confirm.NotEligible). The intake turns it into UNKNOWN.
	ErrApproverNotEligible = errors.New("trustport: the approver is not eligible for this confirmation request")
	// ErrWrongRequest: the answer names another request, or is neither ACCEPT nor DECLINE (confirm.WrongRequest). Fail closed.
	ErrWrongRequest = errors.New("trustport: the answer does not belong to this confirmation request")
)

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

// StoredAnswer is the approver's answer to a request, as the store holds it.
type StoredAnswer struct {
	EventID string // cfe_...
	Event   confirm.Event
}

// StoredRequest is a request with its answer, if any.
type StoredRequest struct {
	Request confirm.Request
	Answer  *StoredAnswer
}

// ConfirmationStore is the read side of the confirmation store. The adapter never writes: the Recorder persists a new request and the immutable resolution row.
type ConfirmationStore interface {
	// Latest is the newest request of the intent of the principal, with its answer.
	Latest(ctx context.Context, tenantID, principalID, intentID string, version int) (StoredRequest, bool, error)
}

// ConfirmationDeps are the ports of the path.
type ConfirmationDeps struct {
	Policy ConfirmationPolicy
	Store  ConfirmationStore
	Clock  clock.Clock
}

// ConfirmationPath implements intake.Confirmations over trust/confirm. Status is pure.
type ConfirmationPath struct{ d ConfirmationDeps }

var _ intake.Confirmations = ConfirmationPath{}

// NewConfirmationPath builds the adapter. A missing port is ErrConfig.
func NewConfirmationPath(d ConfirmationDeps) (ConfirmationPath, error) {
	if d.Policy == nil || d.Store == nil || d.Clock == nil {
		return ConfirmationPath{}, ErrConfig
	}
	return ConfirmationPath{d: d}, nil
}

// Status answers T13.
func (c ConfirmationPath) Status(context.Context, intake.AdmittedIntent, intake.FreshBinding) (intake.Confirmation, error) {
	return intake.Confirmation{}, nil
}

// NewConfirmationRequest is what the Recorder persists, in one transaction with the CONFIRM Decision Record, when Status answers Pending with a NewRequest: a new
// confirmation request (and its CONFIRMATION_REQUESTED event) that supersedes the intent's latest request, if any. The adapter hands over these values and persists nothing.
// The six binding values the request is bound to (capability, target, params, plan, and the evidence-set hash and policy version of the evaluation) are the Recorder's to supply.
type NewConfirmationRequest struct {
	TenantID      string
	CorrelationID string
	IntentID      string
	IntentVersion int
	IntentHash    string
	RequesterID   string // the admitted intent's PrincipalID (INV-12)
	Requirement   string // SELF, MAKER_CHECKER or ROLE:<role>
	TTL           time.Duration
}

// NewRequestFor builds the hand-over from the admitted intent and the spec Status returned.
func NewRequestFor(intake.AdmittedIntent, *intake.RequestSpec) NewConfirmationRequest {
	return NewConfirmationRequest{}
}
