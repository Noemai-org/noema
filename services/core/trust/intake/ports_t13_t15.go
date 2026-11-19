package intake

import (
	"context"
	"errors"
	"time"

	"github.com/Noemai-org/noema/services/core/trust/decide"
)

// ErrPlanInvalid is what a Planner returns for a request it cannot plan, or for a plan that fails its own validation (the registry, the shape rules, the hash).
// T14 turns it into UNKNOWN PLAN_INVALID. Any other Planner error is a dependency failure: UNKNOWN INTERNAL_ERROR.
var ErrPlanInvalid = errors.New("intake: the plan is invalid")

// ConfirmationStatus is what the confirmation store says about one request (T13). The zero value is not a status: it ends the evaluation in UNKNOWN.
type ConfirmationStatus int

// The four statuses. The values 1 to 3 are fixed; ConfirmationDeclined was added after them.
const (
	// ConfirmationNotRequired: the trust profile asks for no approver confirmation for this request.
	ConfirmationNotRequired ConfirmationStatus = iota + 1
	// ConfirmationPending: the profile asks for one and there is no valid resolved confirmation: none yet, or the one there is has expired, is not eligible, or its binding
	// changed. A decline is not Pending: it is ConfirmationDeclined.
	ConfirmationPending
	// ConfirmationGiven: the profile asks for one and a valid ACCEPT resolved to a fresh evaluation (confirm.Proceed, INV-25), whose event the Confirmation names.
	ConfirmationGiven
	// ConfirmationDeclined: the profile asks for one and the approver declined it. T13 turns it into REJECT DECLINED_BY_APPROVER, recorded before it is returned (SoT 43.2a step 4;
	// a ruling relayed in a session and not yet committed: interim, and PERD 23.3 still lists CONFIRM only for T13). The answer must carry the declined request's RequestID, and no
	// EventID and no NewRequest; a PlanHash is not read.
	ConfirmationDeclined
)

// FreshBinding is the two values of the confirmation binding (confirm.BindingInput, SoT 43.2a) that only the fresh evaluation knows: the hash of the sealed evidence set it
// read at T9 and the policy version it was decided under at T11. The adapter recomputes the binding of a Given answer from the other six values of the stored request and these two,
// so an ACCEPT given for other evidence or another policy never resolves to this evaluation (INV-25). An adapter must refuse (return an error for) a Given answer when either value is
// empty, and T13 no longer relies on it: it refuses a Given answer itself (UNKNOWN INTERNAL_ERROR, nothing planned or recorded) when either value it passed is empty.
type FreshBinding struct {
	// EvidenceSetHash is the evidence_set_hash of the sealed snapshot T9 read, in the contract's Hash form; empty when the Evidence port did not give it.
	EvidenceSetHash string
	// PolicyVersion is the composite of EVERY policy version that took part in T11 (decide.CompositePolicyVersion, A341, A367): "sha256:" and the lower-case SHA-256 hex of the RFC 8785
	// form of the sorted, distinct JSON array of their "policy_id@version" strings, for one policy version as for many. Empty when none took part or one cannot be bound. It is the
	// policy_version of confirm.BindingInput, and the Writer binds the same value, so the two agree byte for byte.
	PolicyVersion string
}

// RequestSpec says how to create the confirmation request T13 asks for. Requirement is the approver requirement of the contract: SELF, MAKER_CHECKER or ROLE:<role> (the role in
// lower case, as enums.v1.json writes it); anything else is not a requirement. A TTL under one second is not a TTL (the expiry is in whole Unix seconds). TTL is the
// request's lifetime, taken from the pack or the configuration and never chosen in Trust (AGENT-16). The requester is not in the spec: Trust takes it from the admitted
// intent's PrincipalID, which comes only from the authenticated session (INV-12).
type RequestSpec struct {
	Requirement string
	TTL         time.Duration
}

// Confirmation is the answer of Confirmations.Status. EventID names the confirmation event a Given answer rests on; the grant of the ALLOW follows it.
type Confirmation struct {
	Status   ConfirmationStatus
	EventID  string
	PlanHash string // a Given answer: the plan hash the confirmation was given for (A230); the intake compares it with the plan T14 builds and the writer with the steps
	// RequestID is the confirmation request the answer rests on (a cfr_ ID). Pending: the open, unexpired request, if one exists. Given and Declined: the request that was
	// answered; a Given answer without one is not an answer. NotRequired carries none.
	RequestID string
	// NewRequest is set only on Pending. Non-nil means no open request exists and Trust must create one from the spec; nil with a RequestID means one is already open and no
	// second request is made. A Pending answer with both, or with neither, is not an answer (UNKNOWN).
	NewRequest *RequestSpec
}

// Confirmations answers T13. The adapter hides the confirmation store and applies trust/confirm (Resolve, BindingHash): the intake asks one question and takes one answer.
// f is what the fresh evaluation knows about the binding; the adapter adds the other six values from the stored request.
type Confirmations interface {
	Status(ctx context.Context, a AdmittedIntent, f FreshBinding) (Confirmation, error)
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
	// ConfirmationPlanHash is the plan hash that confirmation was given for (A230), empty with no confirmation; the writer refuses an ALLOW whose steps carry another.
	ConfirmationPlanHash string
	// ConfirmationRequest is set only on a CONFIRM decision that creates the confirmation request (T13, Pending with a NewRequest): the eight bound values, the approver
	// requirement, the requester and the expiry. The Recorder mints the request ID and computes the binding hash itself (SoT 43.2a); a CONFIRM that names an already open
	// request carries none, and so does every other decision.
	ConfirmationRequest *NewConfirmationRequest
}

// NewConfirmationRequest is a confirmation request to create, as plain values. The first eight are the binding of SoT 43.2a (confirm.BindingInput); the intake does not import
// trust/confirm, so it names them here and the Recorder checks them and computes the hash. Requirement is the contract's approver requirement (SELF, MAKER_CHECKER or
// ROLE:<role>), RequesterID the admitted intent's principal (INV-12) and ExpiresAt the expiry in Unix seconds, from the evaluation's one clock reading and the spec's TTL.
type NewConfirmationRequest struct {
	PrincipalID     string
	IntentHash      string
	CapabilityID    string
	TargetRef       string
	ParamsHash      string
	EvidenceSetHash string
	PlanHash        string
	PolicyVersion   string
	Requirement     string
	RequesterID     string
	ExpiresAt       int64
}

// RecordResult is what was written: the decision ID, the IDs of the grants issued, one per step in step order, and, when the request created a confirmation request, its ID.
type RecordResult struct {
	DecisionID string
	GrantIDs   []string
	// ConfirmationRequestID is the cfr_ ID the Recorder minted for the ConfirmationRequest of the RecordRequest; empty when none was asked for or none was created (a stored
	// decision of the same input was returned instead).
	ConfirmationRequestID string
}

// Recorder answers T15. The composition implements Record with recorder.Writer inside one transaction it owns and commits, so the intake never touches the database
// and the Decision Record, the grants and their events appear together or not at all (PERD 32.4, EC-E16). A returned error means nothing was written.
type Recorder interface {
	Record(ctx context.Context, r RecordRequest) (RecordResult, error)
}

// DispatchKey names one logical write for the dispatch budget: the same four values the ledger counts a key's dispatches by (ruling A153).
type DispatchKey struct {
	TenantID      string
	CorrelationID string
	CapabilityID  string
	TargetRef     string
}

// DispatchCount is how many times the key has been dispatched and how many it may be. The limit belongs to the ledger (ruling A153); the intake holds no number.
type DispatchCount struct {
	Used  int
	Limit int
}

// Dispatches answers the question after T14: has this plan step used its dispatch budget. The adapter reads the Record's dispatch events through the ledger; the intake
// compares the two numbers and nothing else (A224: Trust itself decides ESCALATE, the runner's refusal is only the backstop).
type Dispatches interface {
	Count(ctx context.Context, k DispatchKey) (DispatchCount, error)
}
