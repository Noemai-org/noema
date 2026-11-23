package intake

import (
	"context"
	"errors"
	"fmt"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/trust/decide"
)

// Service evaluates requests. Its zero value cannot run.
type Service struct {
	Ports       Ports
	Clock       clock.Clock
	PackVersion string
	// Runnable is optional (A511): which capabilities of the intent node's action graph may run now. When set, the first evaluation of a write asks it once, after T11, and the plan
	// request names the authorized capabilities that are runnable; an empty intersection is PLAN_INVALID. Nil keeps the earlier request, which names none. It is a field of the
	// service and not of Ports, as a continuation names its own (ContinuationRequest.Runnable): the composition sets it where it builds the plan contexts.
	Runnable Runnable
}

// Result is the outcome of one evaluation: the decision and the trace of the steps it reached.
type Result struct {
	Decision decide.Decision
	Trace    []decide.TraceEntry
	// DecisionID and GrantIDs are set only for an ALLOW or a CONFIRM that T13 recorded (and DecisionID alone for a REJECT of a declined approval, A435): the Decision Record that was written before the decision was returned, and the
	// grants issued with it (one per write plan step for an ALLOW; none for a read, and none for a CONFIRM).
	DecisionID string
	GrantIDs   []string
	// ConfirmationRequestID is set only for a CONFIRM that T13 recorded: the confirmation request the approver answers, created by this evaluation or already open.
	ConfirmationRequestID string
}

// ErrNotBuilt is what a step that does not exist yet returns: the evaluation ends in UNKNOWN INTERNAL_ERROR at that step (fail closed, INV-16).
var ErrNotBuilt = errors.New("intake: this step is not built")

// ErrNoDeadline is returned when the context has no deadline (AGENTS section 7: a deadline on every call).
var ErrNoDeadline = errors.New("intake: the context needs a deadline")

// ErrConfig is returned when the service is missing a port, the clock or the pack version: an error, never a decision.
var ErrConfig = errors.New("intake: the service is not configured")

func (s *Service) configured() error {
	p := s.Ports
	if p.Intents == nil || p.Writes == nil || p.Capabilities == nil || p.Scope == nil || p.Authority == nil || p.Candidates == nil || p.Ownership == nil || p.Evidence == nil || p.Reconciler == nil || p.Policies == nil || p.Clocks == nil || p.Confirmations == nil || p.Planner == nil || p.Recorder == nil || p.Dispatches == nil || s.Clock == nil || s.PackVersion == "" {
		return ErrConfig
	}
	return nil
}

// Evaluate runs T1 to T15 for r in the fixed order of PERD 23.3 and returns the one decision with its trace (REQ-TRU-002). A malformed intent reference is REJECT
// INTENT_INVALID at T1 and reads nothing. Every other failure of a step is UNKNOWN INTERNAL_ERROR (trust/decide), never ALLOW. Steps T1 to T15 are built; T15 is the only step that can end in ALLOW.
func (s *Service) Evaluate(ctx context.Context, r Request) (Result, error) {
	return s.evaluate(ctx, r, nil)
}

// evaluate is Evaluate and EvaluateStep: step is nil for the first evaluation of an intent and names the one step of a continuation otherwise.
func (s *Service) evaluate(ctx context.Context, r Request, step *stepScope) (Result, error) {
	if err := s.configured(); err != nil {
		return Result{}, err
	}
	if _, ok := ctx.Deadline(); !ok {
		return Result{}, ErrNoDeadline
	}
	intentID, version, perr := contracts.ParseIntentRef(r.IntentRef)
	if perr != nil {
		d, err := decide.FailAtGate(decide.T1AdmittedIntent, 0)
		if err != nil {
			return Result{}, fmt.Errorf("intake: %w", err)
		}
		return Result{Decision: d, Trace: []decide.TraceEntry{{Step: decide.T1AdmittedIntent, Result: decide.StepTerminal}}}, nil
	}
	ev, err := s.newEvaluation(r, intentID, version)
	if err != nil {
		return Result{}, err
	}
	ev.step = step
	return ev.run(ctx, ev.steps()), nil
}

// run evaluates steps in the fixed order against the evaluated set of this call. An ALLOW carries the decision ID and the grant IDs that T15 wrote before it was returned; a
// CONFIRM that T13 recorded carries its decision ID and the confirmation request it names; a REJECT of a declined approval (A435) carries its decision ID alone.
func (e *evaluation) run(ctx context.Context, steps decide.Steps) Result {
	d, trace := decide.EvaluateBound(ctx, e.version, steps, e.bb)
	res := Result{Decision: d, Trace: trace}
	switch d.Outcome() {
	case decide.Allow, decide.Confirm:
		res.DecisionID, res.GrantIDs, res.ConfirmationRequestID = e.late.recorded.DecisionID, append([]string(nil), e.late.recorded.GrantIDs...), e.late.requestID
	case decide.Reject:
		// A435 (interim, relayed, not yet committed): only a decline that T13 recorded is a recorded REJECT here; it carries its decision ID, no grant and no request.
		res.DecisionID = e.late.recorded.DecisionID
	}
	return res
}

// newEvaluation starts one evaluation: one clock reading, taken once, is the evaluation's time for every step (INV-21), and one basis builder holds what the
// steps read and evaluate (TRU-13).
func (s *Service) newEvaluation(r Request, intentID string, version int) (*evaluation, error) {
	now := s.Clock.Now().Unix()
	bb, err := decide.NewBasisBuilder(s.PackVersion, now)
	if err != nil {
		return nil, fmt.Errorf("intake: %w", err)
	}
	return &evaluation{svc: s, req: r, intentID: intentID, version: version, now: now, bb: bb, snapshotID: r.EvidenceSnapshotID}, nil
}
