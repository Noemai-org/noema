package intake

import (
	"context"
	"errors"

	"github.com/Noemai-org/noema/services/core/trust/decide"
)

// ErrPlanInvalid is what a Planner returns for a request it cannot plan, or for a plan that fails its own validation (the registry, the shape rules, the hash).
// T14 turns it into UNKNOWN PLAN_INVALID. Any other Planner error is a dependency failure: UNKNOWN INTERNAL_ERROR.
var ErrPlanInvalid = errors.New("intake: the plan is invalid")

// ConfirmationStatus is what the confirmation store says about one request (T13). The zero value is not a status: it ends the evaluation in UNKNOWN.
type ConfirmationStatus int

// The three statuses.
const (
	// ConfirmationNotRequired: the trust profile asks for no approver confirmation for this request.
	ConfirmationNotRequired ConfirmationStatus = iota + 1
	// ConfirmationPending: the profile asks for one and there is no valid resolved confirmation (none yet, declined, expired, not eligible, or its binding changed).
	ConfirmationPending
	// ConfirmationGiven: the profile asks for one and a valid ACCEPT resolved to a fresh evaluation (confirm.Proceed, INV-25), whose event the Confirmation names.
	ConfirmationGiven
)

// Confirmation is the answer of Confirmations.Status. EventID names the confirmation event a Given answer rests on; the grant of the ALLOW follows it.
type Confirmation struct {
	Status  ConfirmationStatus
	EventID string
}

// Confirmations answers T13. The adapter hides the confirmation store and applies trust/confirm (Resolve, BindingHash): the intake asks one question and takes one answer.
type Confirmations interface {
	Status(ctx context.Context, a AdmittedIntent) (Confirmation, error)
}

// PlanRequest is what Trust asks the Planner for. WriteRequired is what T3 learned: a read has no write step.
type PlanRequest struct {
	TenantID      string
	CorrelationID string
	IntentID      string
	IntentVersion int
	IntentHash    string
	IntentNode    string
	WriteRequired bool
}

// PlanStep is one step of a plan as plain values: the capability, the target and the hash of the parameters. Its index in Plan.Steps is its plan step number.
type PlanStep struct {
	CapabilityID string
	TargetRef    string
	ParamsHash   string
}

// Plan is a plan as plain values. Hash is the plan hash ("sha256:" and 64 lower-case hex). A plan for a read has no step and still has its hash (ruling A205, Q3), so
// that the Decision Record always names a plan.
type Plan struct {
	Hash  string
	Steps []PlanStep
}

// Planner answers T14. The adapter calls execution/planner and returns ErrPlanInvalid when the plan cannot be built or hashed; Trust imports no execution package.
type Planner interface {
	Plan(ctx context.Context, r PlanRequest) (Plan, error)
}

// RecordStep is one write plan step to issue a grant for.
type RecordStep struct {
	PlanStep     int64
	CapabilityID string
	TargetRef    string
	ParamsHash   string
}

// RecordRequest is what T15 hands to the Recorder, in the intake's own types. Decision is the ALLOW candidate with the basis of the builder in Evaluated, which the
// adapter passes to recorder.Request unchanged (TRU-13); BasisDigest is that builder's digest, for the adapter to compare.
type RecordRequest struct {
	TenantID            string
	CorrelationID       string
	PrincipalID         string
	IntentID            string
	IntentHash          string
	IntentVersion       int
	Decision            decide.Decision
	Evaluated           *decide.BasisBuilder
	BasisDigest         string
	PlanHash            string
	Steps               []RecordStep
	ConfirmationEventID string // empty when no confirmation was required
}

// RecordResult is what was written: the decision ID and the IDs of the grants issued, one per step in step order.
type RecordResult struct {
	DecisionID string
	GrantIDs   []string
}

// Recorder answers T15. The composition implements Record with recorder.Writer inside one transaction it owns and commits, so the intake never touches the database
// and the Decision Record, the grants and their events appear together or not at all (PERD 32.4, EC-E16). A returned error means nothing was written.
type Recorder interface {
	Record(ctx context.Context, r RecordRequest) (RecordResult, error)
}
