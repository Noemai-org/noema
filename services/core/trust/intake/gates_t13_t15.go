package intake

import (
	"context"
	"errors"
	"regexp"

	"github.com/Noemai-org/noema/services/core/trust/decide"
)

// Errors that end an evaluation in UNKNOWN INTERNAL_ERROR (the step fails; there is no decision to give).
var (
	errNoIntent   = errors.New("intake: no admitted intent was read")
	errNoBasis    = errors.New("intake: there is no evaluated set to build the decision on")
	errUnresolved = errors.New("intake: the intent is not resolved, so no CONFIRM or ALLOW can be built")
	errAnswer     = errors.New("intake: a port answered with something that is not an answer")
)

var (
	hashRe       = regexp.MustCompile(`\Asha256:[0-9a-f]{64}\z`)
	decisionIDRe = regexp.MustCompile(`\Adec_[0-9A-Za-z]{2,32}\z`)
	grantIDRe    = regexp.MustCompile(`\Agrt_[0-9A-Za-z]{2,32}\z`)
)

// lateState is what T3 and T13 to T15 learn as the call goes. It is private to one call.
type lateState struct {
	needsWrite     bool         // T3: the request leads to a write
	plan           *Plan        // T14: the validated plan
	confirmationID string       // T13: the event of the confirmation the ALLOW follows
	recorded       RecordResult // T15: what the Recorder wrote
}

// passed is the authority of a decision built at T13 to T15. Steps run in the fixed order and stop at the first terminal result, so the technical layer (T6), the business
// layer (T8) and the policy layer (T11) each ended in Continue before any of these steps ran: that is what PASS means (PERD 23.2). decide refuses a CONFIRM or an ALLOW
// without all three at PASS.
func passed() decide.Authority {
	return decide.Authority{Technical: decide.Pass, Policy: decide.Pass, Business: decide.Pass}
}

func (e *evaluation) resolved() (decide.ResolvedIntent, error) {
	if e.intent == nil {
		return decide.ResolvedIntent{}, errNoIntent
	}
	r, ok := decide.Admit(e.intent.View).Resolved()
	if !ok {
		return decide.ResolvedIntent{}, errUnresolved
	}
	return r, nil
}

// withBasis gives d the evaluated set of this call as its basis: from the builder, never from a caller (TRU-13).
func (e *evaluation) withBasis(d decide.Decision) (decide.Decision, error) {
	if e.basis == nil {
		return decide.Decision{}, errNoBasis
	}
	return e.basis.Attach(d)
}

// t13: the trust profile's approver confirmation. Required and not resolved: CONFIRM, which rests on the evaluated set. Required and resolved (a valid ACCEPT that the
// confirmation store turned into this fresh evaluation, INV-25): continue, and the grant will follow its event. Not required: continue. Any other answer is not an answer.
func (e *evaluation) t13(ctx context.Context) decide.StepResult {
	if e.intent == nil {
		return decide.FailStep(errNoIntent)
	}
	c, err := e.svc.Ports.Confirmations.Status(ctx, *e.intent)
	if err != nil {
		return decide.FailStep(err)
	}
	switch c.Status {
	case ConfirmationNotRequired:
		return decide.Continue()
	case ConfirmationGiven:
		if c.EventID == "" {
			return decide.FailStep(errAnswer)
		}
		e.late.confirmationID = c.EventID
		return decide.Continue()
	case ConfirmationPending:
		r, err := e.resolved()
		if err != nil {
			return decide.FailStep(err)
		}
		d, err := r.Confirm(passed())
		if err != nil {
			return decide.FailStep(err)
		}
		if d, err = e.withBasis(d); err != nil {
			return decide.FailStep(err)
		}
		return decide.Stop(d)
	}
	return decide.FailStep(errAnswer)
}

// t14: ask the Planner for the plan and validate what comes back. A plan the Planner calls invalid, or one that is not well formed, is UNKNOWN PLAN_INVALID. For a read
// the plan is empty and has its hash (ruling A205, Q3).
func (e *evaluation) t14(ctx context.Context) decide.StepResult {
	if e.intent == nil {
		return decide.FailStep(errNoIntent)
	}
	a := e.intent
	p, err := e.svc.Ports.Planner.Plan(ctx, PlanRequest{TenantID: a.TenantID, CorrelationID: a.CorrelationID, IntentID: a.IntentID, IntentVersion: a.Version, IntentHash: a.IntentHash,
		IntentNode: a.IntentNode, WriteRequired: e.late.needsWrite})
	switch {
	case errors.Is(err, ErrPlanInvalid):
		return e.planInvalid()
	case err != nil:
		return decide.FailStep(err)
	case !e.wellFormed(p):
		return e.planInvalid()
	}
	p.Steps = append([]PlanStep(nil), p.Steps...)
	e.late.plan = &p
	return decide.Continue()
}

// wellFormed: a hash in the contract's form; no step for a read and at least one for a write; every step names a capability, a target and a params hash in the contract's form.
func (e *evaluation) wellFormed(p Plan) bool {
	if !hashRe.MatchString(p.Hash) || e.late.needsWrite == (len(p.Steps) == 0) {
		return false
	}
	for _, s := range p.Steps {
		if s.CapabilityID == "" || s.TargetRef == "" || !hashRe.MatchString(s.ParamsHash) {
			return false
		}
	}
	return true
}

func (e *evaluation) planInvalid() decide.StepResult {
	r, err := e.resolved()
	if err != nil {
		return decide.FailStep(err)
	}
	reason, err := decide.Core("PLAN_INVALID")
	if err != nil {
		return decide.FailStep(err)
	}
	d, err := r.Unknown(passed(), reason)
	if err != nil {
		return decide.FailStep(err)
	}
	return decide.Stop(d)
}

// t15: the Decision Record first, then the ALLOW. The candidate is built here, with all three layers at PASS and the basis of the builder, and handed to the Recorder, which
// writes the record and issues the grants in one transaction. Only if it returns without error, and with what a write returns (a decision ID and one grant for each write
// step), does the ALLOW stand (EC-E16); any other result ends in UNKNOWN. T15 runs only when T1 to T14 all continued.
func (e *evaluation) t15(ctx context.Context) decide.StepResult {
	if e.late.plan == nil {
		return decide.FailStep(errAnswer)
	}
	r, err := e.resolved()
	if err != nil {
		return decide.FailStep(err)
	}
	d, err := r.Allow(passed())
	if err != nil {
		return decide.FailStep(err)
	}
	if d, err = e.withBasis(d); err != nil {
		return decide.FailStep(err)
	}
	digest, err := e.basis.Digest()
	if err != nil {
		return decide.FailStep(err)
	}
	a, plan := e.intent, e.late.plan
	steps := make([]RecordStep, 0, len(plan.Steps))
	for i, s := range plan.Steps {
		steps = append(steps, RecordStep{PlanStep: int64(i), CapabilityID: s.CapabilityID, TargetRef: s.TargetRef, ParamsHash: s.ParamsHash})
	}
	res, err := e.svc.Ports.Recorder.Record(ctx, RecordRequest{TenantID: a.TenantID, CorrelationID: a.CorrelationID, PrincipalID: a.PrincipalID, IntentID: a.IntentID,
		IntentHash: a.IntentHash, IntentVersion: a.Version, Decision: d, Evaluated: e.basis, BasisDigest: digest, PlanHash: plan.Hash, Steps: steps,
		ConfirmationEventID: e.late.confirmationID})
	if err != nil {
		return decide.FailStep(err)
	}
	if !decisionIDRe.MatchString(res.DecisionID) || len(res.GrantIDs) != len(steps) {
		return decide.FailStep(errAnswer)
	}
	for _, g := range res.GrantIDs {
		if !grantIDRe.MatchString(g) {
			return decide.FailStep(errAnswer)
		}
	}
	e.late.recorded = RecordResult{DecisionID: res.DecisionID, GrantIDs: append([]string(nil), res.GrantIDs...)}
	return decide.Stop(d)
}
