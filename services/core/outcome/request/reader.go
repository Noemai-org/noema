package request

import (
	"context"
	"errors"
	"fmt"

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

// For reads the facts of the grant and builds the request, or refuses. The reader is a port that may be wrong, so what it returns is checked against the call: the tenant and the grant
// are the caller's, the pack is asked for the GRANT's capability, and the receipt must have run that capability. The decision and the grant must share one intent hash. The signature is
// checked on the receipt as stored and only VALID passes (Build). A fresh verification id is made for each call (it is the id of the verdict that follows, INV-04).
func (s GrantRequests) For(ctx context.Context, tenantID, grantID string) (verifier.Request, error) {
	if s.Reader == nil || s.Signatures == nil || s.Outcomes == nil || s.IDs == nil || !clean(tenantID) || !clean(grantID) {
		return verifier.Request{}, ErrIncomplete
	}
	f, err := s.Reader.Load(ctx, tenantID, grantID)
	if err != nil {
		return verifier.Request{}, fmt.Errorf("request: read the grant: %w", err)
	}
	if f.Receipt.TenantID != tenantID || f.Receipt.GrantID != grantID {
		return verifier.Request{}, ErrMismatch
	}
	if f.Receipt.LogicalAction != f.CapabilityID || f.GrantIntentHash != f.DecisionIntentHash {
		return verifier.Request{}, ErrMismatch
	}
	auth, err := s.Signatures.Check(ctx, f.Receipt)
	if err != nil {
		return verifier.Request{}, fmt.Errorf("request: check the receipt signature: %w", err)
	}
	out, err := s.Outcomes.For(ctx, tenantID, f.CapabilityID)
	if err != nil {
		return verifier.Request{}, fmt.Errorf("request: the pack's outcome: %w", err)
	}
	id, err := s.IDs.ID("vrf")
	if err != nil {
		return verifier.Request{}, fmt.Errorf("request: make the verification id: %w", err)
	}
	r := f.Receipt
	return Build(Inputs{
		VerificationID: id,
		Receipt: Receipt{ReceiptID: r.ReceiptID, TenantID: r.TenantID, GrantID: r.GrantID, PrincipalID: r.PrincipalID, IdempotencyKey: r.IdempotencyKey, State: r.State,
			RawStatus: r.RawStatus, ExternalAck: r.ExternalAck, Document: r.Document, SigKeyID: r.SigKeyID, SigValue: r.SigValue},
		Authenticity: auth,
		Plan: Plan{TenantID: tenantID, IntentID: f.IntentID, IntentHash: f.GrantIntentHash, PlanHash: f.PlanHash,
			Steps: []Step{{CapabilityID: f.CapabilityID, Params: f.StepParams}}},
		Expect:           Expect{TenantID: tenantID, IntentID: f.IntentID, IntentHash: f.DecisionIntentHash, PlanHash: f.PlanHash},
		StepCapabilityID: f.CapabilityID,
		Capability:       out.Capability,
		Outcome:          out.Definition,
		SubjectVariable:  out.SubjectVariable,
		Bindings:         out.Bindings,
	})
}
