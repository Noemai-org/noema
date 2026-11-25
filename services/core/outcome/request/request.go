package request

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/Noemai-org/noema/services/core/outcome/verifier"
)

// Typed refusals. Each is a fixed text that names no identifier, no hash, no key and no acknowledgement (AGENT-13). A caller tests
// with errors.Is.
var (
	// ErrReceiptUnsigned is returned for a receipt with no signed document, key id or signature value.
	ErrReceiptUnsigned = errors.New("request: the receipt is not signed")
	// ErrReceiptNotVerified is returned unless the caller's signature check said VALID (INV-04: a receipt is a pointer, never proof).
	ErrReceiptNotVerified = errors.New("request: the receipt signature was not verified")
	// ErrAckMissing is returned for an ACKNOWLEDGED receipt with no external acknowledgement: an HTTP 200 alone is not an outcome (AGENT-21).
	ErrAckMissing = errors.New("request: the receipt claims success with no external acknowledgement")
	// ErrMismatch is returned when the receipt, the plan and what the caller expects do not name one tenant, one intent and one plan.
	ErrMismatch = errors.New("request: the receipt, the plan and the expected values do not agree")
	// ErrState is returned for a receipt state no Execution produces.
	ErrState = errors.New("request: the receipt is in a state no execution produces")
	// ErrPlanStep is returned when the plan holds no step, or more than one, of the capability the receipt executed.
	ErrPlanStep = errors.New("request: the plan has not exactly one step of the executed capability")
	// ErrBinding is returned for a binding the inputs cannot supply, or a binding specification that is not valid.
	ErrBinding = errors.New("request: a predicate binding cannot be built from the inputs")
	// ErrIncomplete is returned for a missing identifier, a missing or invalid definition, or a control character in an identifier.
	ErrIncomplete = errors.New("request: the inputs are incomplete")
)

// SourceKind says where one predicate variable's value comes from. The set is closed.
type SourceKind string

// The sources.
const (
	FromIntentID    SourceKind = "INTENT_ID"    // the plan's intent id
	FromPrincipalID SourceKind = "PRINCIPAL_ID" // the principal the receipt's grant was issued to
	FromPlanParam   SourceKind = "PLAN_PARAM"   // one param of the plan step of the executed capability
	FromExternalAck SourceKind = "EXTERNAL_ACK" // the target's own acknowledgement; bound only for an ACKNOWLEDGED receipt
)

// Source is one binding specification: the pack names the variable, this says where its value is read.
type Source struct {
	Kind SourceKind
	Name string // the param name, for FromPlanParam
}

// Receipt holds the facts of the Execution receipt row the builder needs.
type Receipt struct {
	ReceiptID      string
	TenantID       string
	GrantID        string
	PrincipalID    string
	IdempotencyKey string
	State          string  // the execution state the receipt claims
	RawStatus      string  // the transport result: diagnostic only, never proof
	ExternalAck    *string // the target's acknowledgement, nil when it gave none
	Document       []byte  // the signed receipt as stored
	SigKeyID       string
	SigValue       string
}

// Step is one plan step: the capability it names and its params as canonical text.
type Step struct {
	CapabilityID string
	Params       map[string]string
}

// Plan holds the facts of the stored plan.
type Plan struct {
	TenantID   string
	IntentID   string
	IntentHash string
	PlanHash   string
	Steps      []Step
}

// Expect is what the caller trusts, from the decision and the grant it holds: the plan and the intent the receipt must belong to.
type Expect struct {
	TenantID   string
	IntentID   string
	IntentHash string
	PlanHash   string
}

// Capability is the registry's view of the executed capability: its id and its retry safety.
type Capability struct {
	ID          string
	RetrySafety string
}

// Definition is the part of the pack's outcome definition the verifier needs.
type Definition struct {
	OutcomeID           string
	SuccessPredicate    string
	SettleWindowSeconds int64
}

// Inputs is everything Build reads.
type Inputs struct {
	VerificationID   string // the id of the outcome.verify outbox event, or of a watch check
	Receipt          Receipt
	Authenticity     verifier.Authenticity // the caller's signature check on the receipt; only VALID passes
	Plan             Plan
	Expect           Expect
	StepCapabilityID string // the capability the receipt executed: it picks the plan step
	Capability       Capability
	Outcome          Definition
	SubjectVariable  string            // the predicate variable that names the subject whose cases are read
	Bindings         map[string]Source // predicate variable to source, from the pack
}

const acknowledged = "ACKNOWLEDGED"

var knownStates = map[string]bool{"DISPATCHED": true, "ACKNOWLEDGED": true, "OUTCOME_UNKNOWN": true, "REJECTED_BY_TARGET": true,
	"REJECTED_STALE_VERSION": true, "FAILED_TERMINAL": true}

func clean(s string) bool {
	if strings.TrimSpace(s) == "" {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// Build builds the verification request, or refuses. It is pure: the same inputs give the same request, and a refusal returns the
// zero Request, never a partly built one.
func Build(in Inputs) (verifier.Request, error) {
	if err := check(in); err != nil {
		return verifier.Request{}, err
	}
	step, err := stepOf(in)
	if err != nil {
		return verifier.Request{}, err
	}
	bound, err := bind(in, step)
	if err != nil {
		return verifier.Request{}, err
	}
	subject := bound[in.SubjectVariable]
	return verifier.Request{
		TenantID:       in.Receipt.TenantID,
		ReceiptID:      in.Receipt.ReceiptID,
		VerificationID: in.VerificationID,
		Definition: verifier.Definition{
			OutcomeID: in.Outcome.OutcomeID, SuccessPredicate: in.Outcome.SuccessPredicate, SettleWindowSeconds: in.Outcome.SettleWindowSeconds,
		},
		Bindings:       bound,
		TransactionRef: subject,
		QueryableByKey: in.Capability.RetrySafety == "QUERYABLE_BY_KEY",
	}, nil
}

func check(in Inputs) error {
	r := in.Receipt
	if !clean(in.VerificationID) || !clean(r.ReceiptID) || !clean(in.Outcome.OutcomeID) || strings.TrimSpace(in.Outcome.SuccessPredicate) == "" ||
		in.Outcome.SettleWindowSeconds < 0 {
		return ErrIncomplete
	}
	if r.SigKeyID == "" || r.SigValue == "" || len(r.Document) == 0 {
		return ErrReceiptUnsigned
	}
	if in.Authenticity != verifier.AuthValid {
		return ErrReceiptNotVerified
	}
	if !knownStates[r.State] {
		return ErrState
	}
	if r.State == acknowledged && (r.ExternalAck == nil || strings.TrimSpace(*r.ExternalAck) == "") {
		return ErrAckMissing
	}
	p, e := in.Plan, in.Expect
	if r.TenantID == "" || r.TenantID != p.TenantID || r.TenantID != e.TenantID ||
		p.IntentID == "" || p.IntentID != e.IntentID ||
		p.IntentHash == "" || p.IntentHash != e.IntentHash ||
		p.PlanHash == "" || p.PlanHash != e.PlanHash {
		return ErrMismatch
	}
	return nil
}

func stepOf(in Inputs) (Step, error) {
	if in.StepCapabilityID == "" || in.Capability.ID != in.StepCapabilityID {
		return Step{}, ErrPlanStep
	}
	var found *Step
	for i := range in.Plan.Steps {
		if in.Plan.Steps[i].CapabilityID == in.StepCapabilityID {
			if found != nil {
				return Step{}, ErrPlanStep
			}
			found = &in.Plan.Steps[i]
		}
	}
	if found == nil {
		return Step{}, ErrPlanStep
	}
	return *found, nil
}

func bind(in Inputs, step Step) (map[string]string, error) {
	if in.SubjectVariable == "" || in.Receipt.PrincipalID == "" {
		return nil, ErrBinding
	}
	names := make([]string, 0, len(in.Bindings))
	for n := range in.Bindings {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make(map[string]string, len(names))
	for _, n := range names {
		if n == "" {
			return nil, ErrBinding
		}
		v, ok, err := value(in, step, in.Bindings[n])
		if err != nil {
			return nil, fmt.Errorf("%w: variable %q", err, n)
		}
		if ok {
			out[n] = v
		}
	}
	if out[in.SubjectVariable] == "" {
		return nil, ErrBinding
	}
	return out, nil
}

// value reads one binding. ok is false only for an acknowledgement-sourced binding on a receipt that is not ACKNOWLEDGED: nothing
// is bound, so the verifier cannot reach a success verdict on a receipt that is not one (INV-07).
func value(in Inputs, step Step, s Source) (string, bool, error) {
	var v string
	switch s.Kind {
	case FromIntentID:
		v = in.Plan.IntentID
	case FromPrincipalID:
		v = in.Receipt.PrincipalID
	case FromPlanParam:
		if s.Name == "" {
			return "", false, ErrBinding
		}
		v = step.Params[s.Name]
	case FromExternalAck:
		if in.Receipt.State != acknowledged {
			return "", false, nil
		}
		if in.Receipt.ExternalAck != nil {
			v = *in.Receipt.ExternalAck
		}
	default:
		return "", false, ErrBinding
	}
	if !clean(v) {
		return "", false, ErrBinding
	}
	return v, true, nil
}
