package intake

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"

	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/trust/decide"
)

// Service evaluates requests. Its zero value cannot run.
type Service struct {
	Ports       Ports
	Clock       clock.Clock
	PackVersion string
}

// Result is the outcome of one evaluation: the decision and the trace of the steps it reached.
type Result struct {
	Decision decide.Decision
	Trace    []decide.TraceEntry
}

// ErrNotBuilt is what a step that does not exist yet returns: the evaluation ends in UNKNOWN INTERNAL_ERROR at that step (fail closed, INV-16).
var ErrNotBuilt = errors.New("intake: this step is not built")

// ErrNoDeadline is returned when the context has no deadline (AGENTS section 7: a deadline on every call).
var ErrNoDeadline = errors.New("intake: the context needs a deadline")

// ErrConfig is returned when the service is missing a port, the clock or the pack version: an error, never a decision.
var ErrConfig = errors.New("intake: the service is not configured")

var intentRefRe = regexp.MustCompile(`\Aint_(?P<id>[0-9A-Za-z]{2,32})@(?P<version>[1-9][0-9]{0,8})\z`)

func (s *Service) configured() error {
	p := s.Ports
	if p.Intents == nil || p.Writes == nil || p.Capabilities == nil || p.Scope == nil || p.Authority == nil || p.Reconciler == nil || p.Policies == nil || p.Clocks == nil || s.Clock == nil || s.PackVersion == "" {
		return ErrConfig
	}
	return nil
}

// Evaluate runs T1 to T15 for r in the fixed order of PERD 23.3 and returns the one decision with its trace (REQ-TRU-002). A malformed intent reference is REJECT
// INTENT_INVALID at T1 and reads nothing. Every other failure of a step is UNKNOWN INTERNAL_ERROR (trust/decide), never ALLOW. Steps T7 to T15 are not built yet.
func (s *Service) Evaluate(ctx context.Context, r Request) (Result, error) {
	if err := s.configured(); err != nil {
		return Result{}, err
	}
	if _, ok := ctx.Deadline(); !ok {
		return Result{}, ErrNoDeadline
	}
	m := intentRefRe.FindStringSubmatch(r.IntentRef)
	if m == nil {
		d, err := decide.FailAtGate(decide.T1AdmittedIntent, 0)
		if err != nil {
			return Result{}, fmt.Errorf("intake: %w", err)
		}
		return Result{Decision: d, Trace: []decide.TraceEntry{{Step: decide.T1AdmittedIntent, Result: decide.StepTerminal}}}, nil
	}
	version, err := strconv.Atoi(m[intentRefRe.SubexpIndex("version")])
	if err != nil {
		return Result{}, fmt.Errorf("intake: the intent version: %w", err)
	}
	ev, err := s.newEvaluation(r, "int_"+m[intentRefRe.SubexpIndex("id")], version)
	if err != nil {
		return Result{}, err
	}
	d, trace := decide.EvaluateBound(ctx, version, ev.steps(), ev.bb)
	return Result{Decision: d, Trace: trace}, nil
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
