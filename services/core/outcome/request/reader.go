package request

import (
	"context"
	"errors"

	"github.com/Noemai-org/noema/services/core/outcome/verifier"
)

// reader.go: the Record reader PORT and GrantRequests, which implements the write path's Requests over Build (LANE-5 slice 4; A675, A692; interim; REQ-VER-001, REQ-OUT-001;
// INV-04, INV-12, AGENT-13). Nothing here opens a database: Reader is implemented in outcome/store on the outcome login, and Outcomes by the composition from the pack.

// ErrNotFound is returned by a Reader for a grant that has no receipt, grant or decision of the tenant. A row of another tenant is never found (INV-12).
var ErrNotFound = errors.New("request: no receipt for the grant")

// Facts are what Execution wrote for one grant, as the reader finds them: the receipt row with its signed document, the grant's capability, plan hash and intent hash, the decision's
// intent and intent hash, and the receipt's step_params as canonical text (A692: only the params the outcome definition's PLAN_PARAM bindings name).
type Facts struct {
	Receipt            verifier.Receipt
	CapabilityID       string // the grant's capability: it names the plan step and picks the outcome definition
	IntentID           string // the Decision Record's intent
	DecisionIntentHash string // the Decision Record's intent hash
	GrantIntentHash    string // the grant's intent hash
	PlanHash           string // the grant's plan hash
	StepParams         map[string]string
}

// Reader reads the Facts of a grant as the tenant. An absent row is ErrNotFound.
type Reader interface {
	Load(ctx context.Context, tenantID, grantID string) (Facts, error)
}

// Signatures is the caller's check of the receipt signature (outcome/receiptsig.Checker).
type Signatures interface {
	Check(ctx context.Context, r verifier.Receipt) (verifier.Authenticity, error)
}

// Outcome is what the pack gives for one capability: the outcome definition, the predicate variable that names the subject, where each variable's value is read, and the retry safety.
type Outcome struct {
	Definition      Definition
	SubjectVariable string
	Bindings        map[string]Source
	Capability      Capability
}

// Outcomes gives the pack's Outcome for a capability of a tenant.
type Outcomes interface {
	For(ctx context.Context, tenantID, capabilityID string) (Outcome, error)
}

// IDs makes the id of one verification.
type IDs interface {
	ID(prefix string) (string, error)
}

// GrantRequests builds the verification request of a grant. It implements the Requests of orchestrator/portadapters/verifier.
type GrantRequests struct {
	Reader     Reader
	Signatures Signatures
	Outcomes   Outcomes
	IDs        IDs
}

// For reads the facts of the grant and builds the request, or refuses.
func (s GrantRequests) For(ctx context.Context, tenantID, grantID string) (verifier.Request, error) {
	return verifier.Request{}, errors.New("request: not built")
}
