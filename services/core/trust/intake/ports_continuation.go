package intake

import (
	"context"
	"errors"
)

// The continuation evaluation (TRU-14; A335, A363: interim, relayed, not yet committed). After the first step of an escalation is verified, Trust is asked again for the next
// runnable step, as its own action decision with its own grant. It runs the same T1 to T15 as Evaluate (PERD 23.3) for that one step.

// ErrStepInvalid is returned by EvaluateStep for a request that does not name what a continuation needs (the capability, the runnable port). It is an error, never a decision.
var ErrStepInvalid = errors.New("intake: the continuation request is incomplete")

// RunnableQuery asks which capabilities of the intent's action graph may run now.
type RunnableQuery struct {
	TenantID      string
	IntentID      string
	IntentVersion int
	IntentNode    string
	SnapshotID    string
	// Facts are the caller's facts for the continuation (for example case_id), the same ones the plan request carries.
	Facts map[string]string
}

// Runnable answers which steps of the intent node's action graph have their preconditions met on the current facts. The adapter calls the `runnable` package; the intake imports
// nothing of it. The answer is the capability IDs only; an error is never "none" and never "all".
type Runnable interface {
	Runnable(ctx context.Context, q RunnableQuery) ([]string, error)
}

// ContinuationRequest is one next step to decide. Request is the same typed request Evaluate takes. CapabilityID is the step's capability. Facts and EvidencePacket are what the
// caller learned from verified results; they reach the planner unchanged and nothing else.
type ContinuationRequest struct {
	Request        Request
	CapabilityID   string
	Facts          map[string]string
	EvidencePacket string
	Runnable       Runnable
}

// stepScope is what a continuation evaluation holds: the one capability it decides and the inputs of its plan request.
type stepScope struct {
	capabilityID string
	facts        map[string]string
	packet       string
	runnable     Runnable
}

// EvaluateStep runs T1 to T15 for the one next step of an action graph (not built: RED stub).
func (s *Service) EvaluateStep(ctx context.Context, r ContinuationRequest) (Result, error) {
	return Result{}, ErrNotBuilt
}
