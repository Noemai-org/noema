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
	// ErrApproverNotEligible: the approver may not answer this request (confirm.NotEligible). The route returns APPROVER_NOT_ELIGIBLE; the intake turns it into UNKNOWN.
	ErrApproverNotEligible = errors.New("trustport: the approver is not eligible for this confirmation request")
	// ErrWrongRequest: the answer names another request, or is neither ACCEPT nor DECLINE (confirm.WrongRequest). Fail closed.
	ErrWrongRequest = errors.New("trustport: the answer does not belong to this confirmation request")
	// ErrDeclined: the approver declined. The intake has no Declined status yet (it needs one, trust/intake, C1's path), so Status fails closed with this error
	// and Evaluate carries the typed Declined result.
	ErrDeclined = errors.New("trustport: the approver declined the confirmation")
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

// ConfirmationPath implements intake.Confirmations over trust/confirm. It decides nothing the contract does not: Given only for confirm.Proceed (INV-25), and every doubt is
// Pending, Declined or an error, never Given (INV-17). It never opens a second request while one is open and unexpired.
type ConfirmationPath struct{ d ConfirmationDeps }

var _ intake.Confirmations = ConfirmationPath{}

// NewConfirmationPath builds the adapter. A missing port is ErrConfig.
func NewConfirmationPath(d ConfirmationDeps) (ConfirmationPath, error) {
	if d.Policy == nil || d.Planner == nil || d.Fresh == nil || d.Store == nil || d.Resolutions == nil || d.Clock == nil {
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

// Evaluate answers T13 with the typed result. Tenant, principal and intent are the admitted intent's (INV-12); the fresh binding is the same eight values recomputed: the
// principal and the intent hash from the admitted intent, the capability, the target and the parameters from the one step of the plan the Planner builds now, the plan hash
// from that plan, and the evidence-set hash and the policy version from FreshBinding.
func (c ConfirmationPath) Evaluate(ctx context.Context, a intake.AdmittedIntent) (ConfirmationResult, error) {
	d := c.d
	if d.Policy == nil || d.Planner == nil || d.Fresh == nil || d.Store == nil || d.Resolutions == nil || d.Clock == nil {
		return ConfirmationResult{}, ErrConfig
	}
	if a.TenantID == "" || a.PrincipalID == "" || a.IntentID == "" {
		return ConfirmationResult{}, ErrSession
	}
	terms, err := d.Policy.Terms(ctx, a)
	if err != nil {
		return ConfirmationResult{}, fmt.Errorf("trustport: read the confirmation policy: %w", err)
	}
	if !terms.Required {
		return ConfirmationResult{State: ConfirmationStateNotRequired}, nil
	}
	if !validRequirement(terms.Requirement) || terms.ExpiresIn <= 0 {
		return ConfirmationResult{}, fmt.Errorf("%w: the policy names an unusable approver requirement or expiry", ErrBadAnswer)
	}
	fresh, err := c.freshBinding(ctx, a)
	if err != nil {
		return ConfirmationResult{}, err
	}
	stored, found, err := d.Store.Latest(ctx, a.TenantID, a.PrincipalID, a.IntentID, a.Version)
	if err != nil {
		return ConfirmationResult{}, fmt.Errorf("trustport: read the confirmation store: %w", err)
	}
	if !found {
		return c.open(ctx, a, terms, fresh, "", "")
	}
	req := stored.Request
	b := req.Binding
	if req.ID == "" || b.PrincipalID != a.PrincipalID || req.RequesterID != a.PrincipalID || b.IntentHash != a.IntentHash {
		return c.open(ctx, a, terms, fresh, req.ID, "")
	}
	now := d.Clock.Now()
	expired := now.Unix() > req.ExpiresAt
	if stored.Answer == nil {
		if !expired {
			return ConfirmationResult{State: ConfirmationStatePending, RequestID: req.ID}, nil
		}
		return c.expireAndOpen(ctx, a, terms, fresh, req.ID)
	}
	ev := stored.Answer.Event
	res, err := confirm.Resolve(req, ev, fresh)
	if err != nil {
		return ConfirmationResult{}, fmt.Errorf("%w: resolve the confirmation: %v", ErrBadAnswer, err)
	}
	switch res {
	case confirm.Proceed:
		if expired {
			return c.expireAndOpen(ctx, a, terms, fresh, req.ID)
		}
		return c.given(ctx, a, stored, fresh)
	case confirm.Declined:
		return ConfirmationResult{State: ConfirmationStateDeclined}, nil
	case confirm.Expired:
		return c.expireAndOpen(ctx, a, terms, fresh, req.ID)
	case confirm.BindingChanged, confirm.PolicyChanged:
		return c.open(ctx, a, terms, fresh, req.ID, "")
	case confirm.NotEligible:
		return ConfirmationResult{}, ErrApproverNotEligible
	case confirm.WrongRequest:
		return ConfirmationResult{}, ErrWrongRequest
	}
	return ConfirmationResult{}, fmt.Errorf("%w: resolution %q", ErrBadAnswer, res)
}

func (c ConfirmationPath) freshBinding(ctx context.Context, a intake.AdmittedIntent) (confirm.BindingInput, error) {
	plan, err := c.d.Planner.Plan(ctx, intake.PlanRequest{TenantID: a.TenantID, CorrelationID: a.CorrelationID, IntentID: a.IntentID, IntentVersion: a.Version,
		IntentHash: a.IntentHash, IntentNode: a.IntentNode, WriteRequired: true})
	if err != nil {
		return confirm.BindingInput{}, fmt.Errorf("trustport: plan for the confirmation: %w", err)
	}
	if len(plan.Steps) != 1 {
		return confirm.BindingInput{}, fmt.Errorf("%w: a confirmation approves one action, the plan has %d steps", ErrBadAnswer, len(plan.Steps))
	}
	v, err := c.d.Fresh.Fresh(ctx, a, plan)
	if err != nil {
		return confirm.BindingInput{}, fmt.Errorf("trustport: read the fresh binding: %w", err)
	}
	s := plan.Steps[0]
	in := confirm.BindingInput{PrincipalID: a.PrincipalID, IntentHash: a.IntentHash, CapabilityID: s.CapabilityID, TargetRef: s.TargetRef, ParamsHash: s.ParamsHash,
		EvidenceSetHash: v.EvidenceSetHash, PlanHash: plan.Hash, PolicyVersion: v.PolicyVersion}
	if _, err := confirm.BindingHash(in); err != nil {
		return confirm.BindingInput{}, fmt.Errorf("%w: the binding: %v", ErrBadAnswer, err)
	}
	return in, nil
}

func (c ConfirmationPath) open(ctx context.Context, a intake.AdmittedIntent, t ConfirmationTerms, fresh confirm.BindingInput, supersedes, expired string) (ConfirmationResult, error) {
	id, err := c.d.Store.Open(ctx, NewConfirmationRequest{TenantID: a.TenantID, CorrelationID: a.CorrelationID, IntentID: a.IntentID, Version: a.Version, Binding: fresh,
		Requirement: t.Requirement, RequesterID: a.PrincipalID, ExpiresAt: c.d.Clock.Now().Add(t.ExpiresIn).Unix(), Supersedes: supersedes})
	if err != nil {
		return ConfirmationResult{}, fmt.Errorf("trustport: open the confirmation request: %w", err)
	}
	if id == "" {
		return ConfirmationResult{}, fmt.Errorf("%w: a request with no ID", ErrBadAnswer)
	}
	return ConfirmationResult{State: ConfirmationStatePending, RequestID: id, Superseded: supersedes, Expired: expired}, nil
}

func (c ConfirmationPath) expireAndOpen(ctx context.Context, a intake.AdmittedIntent, t ConfirmationTerms, fresh confirm.BindingInput, old string) (ConfirmationResult, error) {
	if err := c.d.Store.Expire(ctx, a.TenantID, old); err != nil {
		return ConfirmationResult{}, fmt.Errorf("trustport: record the expiry: %w", err)
	}
	return c.open(ctx, a, t, fresh, old, old)
}

func (c ConfirmationPath) given(ctx context.Context, a intake.AdmittedIntent, s StoredRequest, fresh confirm.BindingInput) (ConfirmationResult, error) {
	if s.Answer.EventID == "" {
		return ConfirmationResult{}, fmt.Errorf("%w: an answer with no event ID", ErrBadAnswer)
	}
	bh, err := confirm.BindingHash(s.Request.Binding)
	if err != nil {
		return ConfirmationResult{}, fmt.Errorf("%w: the binding: %v", ErrBadAnswer, err)
	}
	if err := c.d.Resolutions.Record(ctx, ResolvedConfirmation{TenantID: a.TenantID, EventID: s.Answer.EventID, RequestID: s.Request.ID, BindingHash: bh,
		AcceptedAt: time.Unix(s.Answer.Event.At, 0).UTC(), ExpiresAt: time.Unix(s.Request.ExpiresAt, 0).UTC()}); err != nil {
		return ConfirmationResult{}, fmt.Errorf("trustport: record the resolved confirmation: %w", err)
	}
	return ConfirmationResult{State: ConfirmationStateGiven, EventID: s.Answer.EventID, PlanHash: fresh.PlanHash}, nil
}

// Status answers intake.Confirmations. The intake has three statuses, so a Declined result and the two refusals fail closed as errors (the intake turns an error into
// UNKNOWN); a Pending answer loses its RequestID here, which the intake cannot carry (see Evaluate).
func (c ConfirmationPath) Status(ctx context.Context, a intake.AdmittedIntent) (intake.Confirmation, error) {
	r, err := c.Evaluate(ctx, a)
	if err != nil {
		return intake.Confirmation{}, err
	}
	switch r.State {
	case ConfirmationStateNotRequired:
		return intake.Confirmation{Status: intake.ConfirmationNotRequired}, nil
	case ConfirmationStatePending:
		return intake.Confirmation{Status: intake.ConfirmationPending}, nil
	case ConfirmationStateGiven:
		return intake.Confirmation{Status: intake.ConfirmationGiven, EventID: r.EventID, PlanHash: r.PlanHash}, nil
	case ConfirmationStateDeclined:
		return intake.Confirmation{}, ErrDeclined
	}
	return intake.Confirmation{}, fmt.Errorf("%w: state %d", ErrBadAnswer, r.State)
}
