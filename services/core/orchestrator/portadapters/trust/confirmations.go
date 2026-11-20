package trustport

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
	// Planner is asked for the plan hash (A369); the adapter never takes one from a stored or supplied value.
	Planner intake.Planner
	Clock   clock.Clock
}

// ConfirmationPath implements intake.Confirmations over trust/confirm. Status is pure.
type ConfirmationPath struct{ d ConfirmationDeps }

var _ intake.Confirmations = ConfirmationPath{}

// NewConfirmationPath builds the adapter. A missing port is ErrConfig.
func NewConfirmationPath(d ConfirmationDeps) (ConfirmationPath, error) {
	if d.Policy == nil || d.Store == nil || d.Planner == nil || d.Clock == nil {
		return ConfirmationPath{}, ErrConfig
	}
	return ConfirmationPath{d: d}, nil
}

func validRequirement(r string) bool {
	switch {
	case r == "SELF", r == "MAKER_CHECKER":
		return true
	case strings.HasPrefix(r, "ROLE:"):
		return len(r) > len("ROLE:")
	}
	return false
}

// Status answers T13 and writes nothing: no request, no resolution row, no event. A Pending answer with a NewRequest tells the Recorder to persist a request; a Given answer
// tells whoever spends the confirmation to insert the immutable resolution row (trust/recorder.RecordConfirmation). Tenant, principal and intent are the admitted intent's
// (INV-12). Given only for confirm.Proceed against the binding rebuilt from the stored request's six values and f (INV-25); every doubt is Pending, Declined or an error.
func (c ConfirmationPath) Status(ctx context.Context, a intake.AdmittedIntent, f intake.FreshBinding) (intake.Confirmation, error) {
	d := c.d
	if d.Policy == nil || d.Store == nil || d.Planner == nil || d.Clock == nil {
		return intake.Confirmation{}, ErrConfig
	}
	if a.TenantID == "" || a.PrincipalID == "" || a.IntentID == "" {
		return intake.Confirmation{}, ErrSession
	}
	terms, err := d.Policy.Terms(ctx, a)
	if err != nil {
		return intake.Confirmation{}, fmt.Errorf("trustport: read the confirmation policy: %w", err)
	}
	if !terms.Required {
		return intake.Confirmation{Status: intake.ConfirmationNotRequired}, nil
	}
	if !validRequirement(terms.Requirement) || terms.ExpiresIn <= 0 {
		return intake.Confirmation{}, fmt.Errorf("%w: the policy names an unusable approver requirement or expiry", ErrBadAnswer)
	}
	planHash, err := c.planHash(ctx, a)
	if err != nil {
		return intake.Confirmation{}, err
	}
	fresh := intake.Confirmation{Status: intake.ConfirmationPending, NewRequest: &intake.RequestSpec{Requirement: terms.Requirement, TTL: terms.ExpiresIn}}
	stored, found, err := d.Store.Latest(ctx, a.TenantID, a.PrincipalID, a.IntentID, a.Version)
	if err != nil {
		return intake.Confirmation{}, fmt.Errorf("trustport: read the confirmation store: %w", err)
	}
	if !found {
		return fresh, nil
	}
	req := stored.Request
	b := req.Binding
	if req.ID == "" || b.PrincipalID != a.PrincipalID || req.RequesterID != a.PrincipalID || b.IntentHash != a.IntentHash {
		return fresh, nil
	}
	expired := d.Clock.Now().Unix() > req.ExpiresAt
	if stored.Answer == nil {
		if expired || b.PlanHash != planHash || (f.ConfirmationBindingEvidenceHash != "" && b.EvidenceSetHash != f.ConfirmationBindingEvidenceHash) || (f.PolicyVersion != "" && b.PolicyVersion != f.PolicyVersion) {
			return fresh, nil
		}
		return intake.Confirmation{Status: intake.ConfirmationPending, RequestID: req.ID}, nil
	}
	b.EvidenceSetHash, b.PolicyVersion, b.PlanHash = f.ConfirmationBindingEvidenceHash, f.PolicyVersion, planHash
	res, err := confirm.Resolve(req, stored.Answer.Event, b)
	if err != nil {
		return intake.Confirmation{}, fmt.Errorf("%w: resolve the confirmation: %v", ErrBadAnswer, err)
	}
	switch res {
	case confirm.Proceed:
		if expired {
			return fresh, nil
		}
		if stored.Answer.EventID == "" {
			return intake.Confirmation{}, fmt.Errorf("%w: an answer with no event ID", ErrBadAnswer)
		}
		return intake.Confirmation{Status: intake.ConfirmationGiven, EventID: stored.Answer.EventID, PlanHash: planHash, RequestID: req.ID}, nil
	case confirm.Declined:
		return intake.Confirmation{Status: intake.ConfirmationDeclined, RequestID: req.ID}, nil
	case confirm.Expired, confirm.BindingChanged, confirm.PolicyChanged:
		return fresh, nil
	case confirm.NotEligible:
		return intake.Confirmation{}, ErrApproverNotEligible
	case confirm.WrongRequest:
		return intake.Confirmation{}, ErrWrongRequest
	}
	return intake.Confirmation{}, fmt.Errorf("%w: resolution %q", ErrBadAnswer, res)
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
	PlanHash      string // the plan hash the Planner gives now (A369)
}

// planHash asks the Planner for the plan of the admitted intent now (A369), built from the intent exactly as T14 builds its request: a confirmation is for one write. A refusal
// stays recognisable as intake.ErrPlanInvalid; an empty hash is never usable.
func (c ConfirmationPath) planHash(ctx context.Context, a intake.AdmittedIntent) (string, error) {
	p, err := c.d.Planner.Plan(ctx, intake.PlanRequest{TenantID: a.TenantID, CorrelationID: a.CorrelationID, IntentID: a.IntentID, IntentVersion: a.Version, IntentHash: a.IntentHash,
		IntentNode: a.IntentNode, WriteRequired: true})
	if err != nil {
		return "", fmt.Errorf("trustport: ask the planner for the plan hash: %w", err)
	}
	if p.Hash == "" {
		return "", fmt.Errorf("%w: the planner gave no plan hash", ErrBadAnswer)
	}
	return p.Hash, nil
}

// NewRequestFor builds the hand-over from the admitted intent and the spec Status returned, with the plan hash the Planner gives now (A369).
func (c ConfirmationPath) NewRequestFor(ctx context.Context, a intake.AdmittedIntent, s *intake.RequestSpec) (NewConfirmationRequest, error) {
	if c.d.Planner == nil {
		return NewConfirmationRequest{}, ErrConfig
	}
	h, err := c.planHash(ctx, a)
	if err != nil {
		return NewConfirmationRequest{}, err
	}
	return NewConfirmationRequest{TenantID: a.TenantID, CorrelationID: a.CorrelationID, IntentID: a.IntentID, IntentVersion: a.Version, IntentHash: a.IntentHash,
		RequesterID: a.PrincipalID, Requirement: s.Requirement, TTL: s.TTL, PlanHash: h}, nil
}
