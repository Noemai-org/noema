package intake

import (
	"context"
	"errors"
	"regexp"
	"slices"

	"github.com/Noemai-org/noema/services/core/trust/decide"
)

// Errors that end an evaluation in UNKNOWN INTERNAL_ERROR (the step fails; there is no decision to give).
var (
	errNoIntent   = errors.New("intake: no admitted intent was read")
	errNoBasis    = errors.New("intake: there is no evaluated set to build the decision on")
	errUnresolved = errors.New("intake: the intent is not resolved, so no CONFIRM or ALLOW can be built")
)

var (
	hashRe       = regexp.MustCompile(`\Asha256:[0-9a-f]{64}\z`)
	requestIDRe  = regexp.MustCompile(`\Acfr_[0-9A-Za-z]+\z`)
	decisionIDRe = regexp.MustCompile(`\Adec_[0-9A-Za-z]{2,32}\z`)
	grantIDRe    = regexp.MustCompile(`\Agrt_[0-9A-Za-z]{2,32}\z`)
)

// lateState is what T3 and T13 to T15 learn as the call goes. It is private to one call.
type lateState struct {
	needsWrite           bool         // T3: the request leads to a write
	plan                 *Plan        // T14: the validated plan
	confirmationID       string       // T13: the event of the confirmation the ALLOW follows
	confirmationPlanHash string       // T13: the plan hash that confirmation was given for (A230)
	requestID            string       // T13: the open confirmation request a CONFIRM rests on (TRU-12)
	recorded             RecordResult // T15: what the Recorder wrote
}

// passed is the authority of a decision built at T13 to T15. Steps run in the fixed order and stop at the first terminal result, so the technical layer (T6), the business
// layer (T8) and the policy layer (T11) each ended in Continue before any of these steps ran: that is what PASS means (PERD 23.2). decide refuses a CONFIRM or an ALLOW
// without all three at PASS.
func passed() decide.Authority {
	return decide.Authority{Technical: decide.Pass, Policy: decide.Pass, Business: decide.Pass}
}

// withBasis gives d the evaluated set of this call as its basis: from the builder, never from a caller (TRU-13).
func (e *evaluation) withBasis(d decide.Decision) (decide.Decision, error) {
	if e.bb == nil {
		return decide.Decision{}, errNoBasis
	}
	return e.bb.Attach(d)
}

// t13: the trust profile's approver confirmation. Required and not resolved: CONFIRM, which rests on the evaluated set. Declined: REJECT DECLINED_BY_APPROVER (SoT 43.2a). Required and resolved (a valid ACCEPT that the
// confirmation store turned into this fresh evaluation, INV-25): continue, and the grant will follow its event. Not required: continue. Any other answer is not an answer.
func (e *evaluation) t13(ctx context.Context) decide.StepResult {
	if e.intent == nil {
		return decide.FailStep(errNoIntent)
	}
	c, err := e.svc.Ports.Confirmations.Status(ctx, *e.intent, e.freshBinding())
	if err != nil {
		return decide.FailStep(err)
	}
	switch c.Status {
	case ConfirmationNotRequired:
		return decide.Continue()
	case ConfirmationGiven:
		if c.EventID == "" || !hashRe.MatchString(c.PlanHash) {
			return decide.FailStep(errAnswer)
		}
		e.late.confirmationID, e.late.confirmationPlanHash = c.EventID, c.PlanHash
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
		id, err := e.requestID(ctx, *e.intent, c)
		if err != nil {
			return decide.FailStep(err)
		}
		e.late.requestID = id
		return decide.Stop(d)
	case ConfirmationDeclined:
		// SoT 43.2a: on DECLINE the decision closes as REJECT with reason DECLINED_BY_APPROVER. No new request is opened and no request id leaves the evaluation.
		reason, err := decide.Core("DECLINED_BY_APPROVER")
		if err != nil {
			return decide.FailStep(err)
		}
		r, err := e.resolved()
		if err != nil {
			return decide.FailStep(err)
		}
		d, err := r.Reject(passed(), reason)
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

// requestID gives the id of the confirmation request a CONFIRM rests on. An open request (RequestID, no NewRequest) is named as it is and no second one is made. With a NewRequest
// the Recorder creates the request (Status is pure) for the session's principal (INV-12), from the spec and the fresh binding, and its id is the one the caller gets; no id, or one
// that is not a cfr_ id, means no request exists and the evaluation fails closed. An answer that is both open and new, or a spec without a requirement or a positive TTL, is not an
// answer (INV-16). A bare Pending, which names no request, is passed on as before.
func (e *evaluation) requestID(ctx context.Context, a AdmittedIntent, c Confirmation) (string, error) {
	if c.NewRequest == nil {
		return c.RequestID, nil
	}
	if c.RequestID != "" || c.NewRequest.Requirement == "" || c.NewRequest.TTL <= 0 {
		return "", errAnswer
	}
	id, err := e.svc.Ports.Recorder.RequestConfirmation(ctx, ConfirmationRequest{TenantID: a.TenantID, CorrelationID: a.CorrelationID, RequesterID: a.PrincipalID, IntentID: a.IntentID,
		IntentVersion: a.Version, IntentHash: a.IntentHash, Requirement: c.NewRequest.Requirement, TTL: c.NewRequest.TTL, Binding: e.freshBinding()})
	if err != nil {
		return "", err
	}
	if !requestIDRe.MatchString(id) {
		return "", errAnswer
	}
	return id, nil
}

// freshBinding is the two values of the confirmation binding that only this evaluation knows (INV-25): the evidence set hash T9 read and the policy T11 used. The policy is
// "policy_id@version" when exactly one policy version took part, and empty otherwise: Trust does not choose between policies. (trust/grants.BoundPolicy applies the same rule
// to the basis, but this package may not import it.) Empty values are passed on as empty.
func (e *evaluation) freshBinding() FreshBinding {
	b := FreshBinding{EvidenceSetHash: e.evidenceSetHash}
	if len(e.policyVersions) > 0 {
		first := e.policyVersions[0]
		if slices.IndexFunc(e.policyVersions, func(v decide.PolicyVersionRef) bool { return v != first }) < 0 {
			b.PolicyVersion = first.PolicyID + "@" + first.Version
		}
	}
	return b
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
	agrees, err := e.planAgrees(ctx, p)
	switch {
	case err != nil:
		return decide.FailStep(err)
	case !agrees:
		return e.planInvalid()
	}
	if e.late.confirmationID != "" && e.late.confirmationPlanHash != p.Hash {
		// A230 (interim): a confirmation was given for ONE plan. The plan built now is another, so the confirmation does not authorise it. T14's stop set is empty (PERD 23.3), so
		// this is a plan that disagrees with what was approved: UNKNOWN PLAN_INVALID, nothing recorded. The writer refuses the same mismatch as the last line of defence.
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

// planAgrees checks every step of the plan against what the earlier steps found, so that a plan the Planner built can never grant more than T4, T8 and T10 let through (the Security
// Reviewer's blocking finding 2 on noema#572): the step's capability is registered and not prohibited (T4, asked of the registry for each step, not only for the capability the
// intent names); its target is the one the stored intent names, which T8 checked the principal owns (a write whose intent names none has nothing owned to act on); and on a
// target frozen for a contradiction (T10) its capability is one the pack allows under the freeze. A plan that disagrees is PLAN_INVALID and nothing is recorded; a registry
// that cannot answer is an error, never a pass.
func (e *evaluation) planAgrees(ctx context.Context, p Plan) (bool, error) {
	if len(p.Steps) == 0 {
		return true, nil
	}
	authorized, err := e.authorizedCapabilities(ctx)
	if err != nil {
		return false, err
	}
	for _, s := range p.Steps {
		c, err := e.svc.Ports.Capabilities.Check(ctx, s.CapabilityID, e.req.Session.Role)
		if err != nil {
			return false, err
		}
		if !c.Registered || c.Prohibited || !c.RoleAllowed { // A255: the role of the session is never assumed to be allowed
			return false, nil
		}
		if !slices.Contains(authorized, s.CapabilityID) { // Codex P1 on noema#572: the intent and the tenant's policy do not authorize this capability, whatever the registry says
			return false, nil
		}
		if e.intent.TargetRef == "" || s.TargetRef != e.intent.TargetRef {
			return false, nil
		}
		if e.frozen && !slices.Contains(e.allowedUnderFreeze, s.CapabilityID) {
			return false, nil
		}
	}
	return true, nil
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
	digest, err := e.bb.Digest()
	if err != nil {
		return decide.FailStep(err)
	}
	a, plan := e.intent, e.late.plan
	steps := make([]RecordStep, 0, len(plan.Steps))
	for i, s := range plan.Steps {
		steps = append(steps, RecordStep{PlanStep: int64(i), CapabilityID: s.CapabilityID, TargetRef: s.TargetRef, ParamsHash: s.ParamsHash})
	}
	res, err := e.svc.Ports.Recorder.Record(ctx, RecordRequest{TenantID: a.TenantID, CorrelationID: a.CorrelationID, PrincipalID: a.PrincipalID, IntentID: a.IntentID,
		IntentHash: a.IntentHash, IntentVersion: a.Version, Decision: d, Evaluated: e.bb.Sealed(), BasisDigest: digest, PlanHash: plan.Hash, Steps: steps,
		ConfirmationEventID: e.late.confirmationID, ConfirmationPlanHash: e.late.confirmationPlanHash})
	if err != nil {
		return decide.FailStep(err)
	}
	if !decisionIDRe.MatchString(res.DecisionID) || len(res.GrantIDs) != len(steps) {
		return decide.FailStep(errAnswer)
	}
	distinct := map[string]bool{}
	for _, g := range res.GrantIDs {
		if !grantIDRe.MatchString(g) || distinct[g] { // C2's note on noema#572: one grant for each write step, each named once
			return decide.FailStep(errAnswer)
		}
		distinct[g] = true
	}
	e.late.recorded = RecordResult{DecisionID: res.DecisionID, GrantIDs: append([]string(nil), res.GrantIDs...)}
	return decide.Stop(d)
}
