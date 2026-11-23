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

// EvaluateStep runs T1 to T15 for the one next step of an action graph (A335: the step is its own action decision with its own grant). Everything of Evaluate holds: the same
// order, the same refusals, the Decision Record written before an ALLOW, one grant for each write step of the plan, which here is exactly the one step. In addition the
// authorized set is that one capability, and only if the intent and the tenant's policy authorize it and the Runnable port says its preconditions are met now; a capability
// outside either set is never decided (UNKNOWN INTERNAL_ERROR at the first step that reads the set, before the policy, the planner and the recorder). A continuation is for
// a write: an intent that leads to none is UNKNOWN at T3. A request without a capability or without the Runnable port is an error, never a decision.
func (s *Service) EvaluateStep(ctx context.Context, r ContinuationRequest) (Result, error) {
	if err := s.configured(); err != nil {
		return Result{}, err
	}
	if _, ok := ctx.Deadline(); !ok {
		return Result{}, ErrNoDeadline
	}
	if r.CapabilityID == "" || r.Runnable == nil {
		return Result{}, ErrStepInvalid
	}
	return s.evaluate(ctx, r.Request, scopeOf(r))
}

// scopeOf is the step a continuation request names; the facts are copied, so the caller cannot change them during the evaluation.
func scopeOf(r ContinuationRequest) *stepScope {
	return &stepScope{capabilityID: r.CapabilityID, facts: cloneStrings(r.Facts), packet: r.EvidencePacket, runnable: r.Runnable}
}

func cloneStrings(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
