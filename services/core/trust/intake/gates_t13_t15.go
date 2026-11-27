package intake

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"sort"
	"time"

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
	decisionIDRe = regexp.MustCompile(`\Adec_[0-9A-Za-z]{2,32}\z`)
	grantIDRe    = regexp.MustCompile(`\Agrt_[0-9A-Za-z]{2,32}\z`)
	requestIDRe  = regexp.MustCompile(`\Acfr_[0-9A-Za-z]{2,32}\z`)
	turnIDRe     = regexp.MustCompile(`\A[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\z`) // the client's request ID of the turn (A581): a lower-case UUID
	// requirementRe is the contract's ApproverRequirement (enums.v1.json): SELF, MAKER_CHECKER or ROLE:<role>.
	requirementRe = regexp.MustCompile(`\A(?:SELF|MAKER_CHECKER|ROLE:[a-z][a-z0-9_]*)\z`)
	// bindingValueRe is the character set a bound value may hold (trust/confirm refuses any other when it hashes the binding); the intake checks it first so that a value the
	// Recorder would refuse ends the evaluation here, before anything is recorded. It is a copy of that rule, not a second rule: the Recorder still computes the hash.
	bindingValueRe = regexp.MustCompile(`\A[A-Za-z0-9_.:@-]+\z`)
)

// lateState is what T3 and T13 to T15 learn as the call goes. It is private to one call.
type lateState struct {
	needsWrite           bool         // T3: the request leads to a write
	plan                 *Plan        // T14: the validated plan
	confirmationID       string       // T13: the event of the confirmation the ALLOW follows
	confirmationPlanHash string       // T13: the plan hash that confirmation was given for (A230)
	requestID            string       // T13: the confirmation request a recorded CONFIRM names (created by this evaluation, or already open)
	recorded             RecordResult // T13 (a CONFIRM) and T15 (an ALLOW): what the Recorder wrote
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

// t13: the trust profile's approver confirmation. Not required: continue. Required and resolved (a valid ACCEPT that the confirmation store turned into this fresh evaluation,
// INV-25): continue, and the grant will follow its event. Required and not resolved (Pending): the CONFIRM, recorded before it is returned (EC-E16), with the confirmation request
// created in the same Record call when none is open. Declined is the REJECT DECLINED_BY_APPROVER (t13Declined: recorded first, with no
// steps and no grant, naming the request it rests on, A542); any other answer is not an answer and ends in UNKNOWN like a status Trust does not know.
func (e *evaluation) t13(ctx context.Context) decide.StepResult {
	if e.intent == nil {
		return decide.FailStep(errNoIntent)
	}
	fresh := e.freshBinding()
	c, err := e.svc.Ports.Confirmations.Status(ctx, *e.intent, fresh)
	if err != nil {
		return decide.FailStep(err)
	}
	switch c.Status {
	case ConfirmationNotRequired:
		if c.RequestID != "" || c.NewRequest != nil {
			return decide.FailStep(errAnswer)
		}
		return decide.Continue()
	case ConfirmationGiven:
		if fresh.ConfirmationBindingEvidenceHash == "" || fresh.PolicyVersion == "" { // fail closed: the adapter must refuse it, and Trust does not rely on that (INV-25)
			return decide.FailStep(errAnswer)
		}
		if c.EventID == "" || !hashRe.MatchString(c.PlanHash) || !requestIDRe.MatchString(c.RequestID) || c.NewRequest != nil {
			return decide.FailStep(errAnswer)
		}
		e.late.confirmationID, e.late.confirmationPlanHash = c.EventID, c.PlanHash
		return decide.Continue()
	case ConfirmationPending:
		return e.t13Confirm(ctx, c)
	case ConfirmationDeclined:
		return e.t13Declined(ctx, c)
	}
	return decide.FailStep(errAnswer)
}

// t13Declined is the Declined answer (SoT 43.2a, A435: interim, relayed, not yet committed): the decision closes as REJECT DECLINED_BY_APPROVER, and the REJECT is a decision, so it
// is recorded, and first (PERD 32.4, A456: the Recorder port serves every outcome; a non-ALLOW has no steps and no plan hash). No request is opened (the answer rests on the one that
// was answered; it never carries a new one), nothing is planned, and no grant exists for a refusal (INV-03). If the Recorder fails, nothing else happens and the evaluation ends in
// UNKNOWN, never in a REJECT the caller could take as recorded.
func (e *evaluation) t13Declined(ctx context.Context, c Confirmation) decide.StepResult {
	if c.NewRequest != nil || !requestIDRe.MatchString(c.RequestID) { // A542: a decline names the request it rests on, as the Given answer does
		return decide.FailStep(errAnswer)
	}
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
	digest, err := e.bb.Digest()
	if err != nil {
		return decide.FailStep(err)
	}
	a := e.intent
	res, err := e.svc.Ports.Recorder.Record(ctx, RecordRequest{TenantID: a.TenantID, CorrelationID: a.CorrelationID, PrincipalID: a.PrincipalID, IntentID: a.IntentID,
		IntentHash: a.IntentHash, IntentVersion: a.Version, Decision: d, Evaluated: e.bb.Sealed(), BasisDigest: digest, DeclinedRequestID: c.RequestID})
	if err != nil {
		return decide.FailStep(err)
	}
	if !decisionIDRe.MatchString(res.DecisionID) || len(res.GrantIDs) != 0 || res.ConfirmationRequestID != "" {
		return decide.FailStep(errAnswer)
	}
	e.late.recorded = RecordResult{DecisionID: res.DecisionID}
	return decide.Stop(d)
}

// t13Confirm is the Pending answer: a request is open (RequestID) or Trust must create one (NewRequest), never both and never neither. The CONFIRM is built on the evaluated set,
// then recorded with the Recorder, and returned only if the Recorder wrote it. A request to make is bound to the eight values of SoT 43.2a (INV-25) and is created by the same
// Record call, in the decision's own transaction; an open request is named and no second one is made.
func (e *evaluation) t13Confirm(ctx context.Context, c Confirmation) decide.StepResult {
	if (c.RequestID == "") == (c.NewRequest == nil) || c.RequestID != "" && !requestIDRe.MatchString(c.RequestID) {
		return decide.FailStep(errAnswer)
	}
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
	digest, err := e.bb.Digest()
	if err != nil {
		return decide.FailStep(err)
	}
	var request *NewConfirmationRequest
	if c.NewRequest != nil {
		req, stop, err := e.newRequest(ctx, *c.NewRequest)
		if stop != nil {
			return *stop
		}
		if err != nil {
			return decide.FailStep(err)
		}
		request = &req
	}
	a := e.intent
	res, err := e.svc.Ports.Recorder.Record(ctx, RecordRequest{TenantID: a.TenantID, CorrelationID: a.CorrelationID, PrincipalID: a.PrincipalID, IntentID: a.IntentID,
		IntentHash: a.IntentHash, IntentVersion: a.Version, Decision: d, Evaluated: e.bb.Sealed(), BasisDigest: digest, ConfirmationRequest: request})
	if err != nil {
		return decide.FailStep(err)
	}
	requestID := c.RequestID
	switch {
	case !decisionIDRe.MatchString(res.DecisionID), len(res.GrantIDs) != 0: // no grant can come from a CONFIRM
		return decide.FailStep(errAnswer)
	case request != nil && !requestIDRe.MatchString(res.ConfirmationRequestID):
		return decide.FailStep(errAnswer)
	case request == nil && res.ConfirmationRequestID != "":
		return decide.FailStep(errAnswer)
	case request != nil:
		requestID = res.ConfirmationRequestID
	}
	e.late.recorded, e.late.requestID = RecordResult{DecisionID: res.DecisionID}, requestID
	return decide.Stop(d)
}

// newRequest builds the confirmation request T13 is asked to create (SoT 43.2a). The eight bound values: the principal and the intent hash of the admitted intent, the evidence content
// digest (A435) and the policy version of the fresh evaluation (both must be present and well formed), and the capability, target, parameters hash and plan hash of the one write step of
// the plan, which T13 asks the Planner for because T14 comes later. The expiry is the evaluation's clock reading plus the spec's TTL, the requester the intent's principal (INV-12);
// the requirement and the TTL come from the adapter and Trust picks neither (AGENT-16). A value that is missing, malformed or outside the binding's character set is not a value:
// the evaluation ends in UNKNOWN. A plan that is invalid, that does not agree with the earlier steps, or that does not have exactly one write step ends in UNKNOWN PLAN_INVALID,
// as T14 gives for a plan that is not one (a confirmation approves one action). A non-nil stop is that result; an error is a dependency failure.
func (e *evaluation) newRequest(ctx context.Context, spec RequestSpec) (NewConfirmationRequest, *decide.StepResult, error) {
	a, fresh := e.intent, e.freshBinding()
	switch {
	case spec.TTL < time.Second, !requirementRe.MatchString(spec.Requirement):
		return NewConfirmationRequest{}, nil, errAnswer
	case !hashRe.MatchString(fresh.ConfirmationBindingEvidenceHash), !bindingValueRe.MatchString(fresh.PolicyVersion):
		return NewConfirmationRequest{}, nil, errAnswer
	case !bindingValueRe.MatchString(a.PrincipalID), !hashRe.MatchString(a.IntentHash):
		return NewConfirmationRequest{}, nil, errAnswer
	}
	p, ok, err := e.validPlan(ctx)
	if err != nil {
		return NewConfirmationRequest{}, nil, err
	}
	if !ok || len(p.Steps) != 1 {
		stop := e.planInvalid()
		return NewConfirmationRequest{}, &stop, nil
	}
	s := p.Steps[0]
	if !bindingValueRe.MatchString(s.CapabilityID) || !bindingValueRe.MatchString(s.TargetRef) {
		return NewConfirmationRequest{}, nil, errAnswer
	}
	return NewConfirmationRequest{PrincipalID: a.PrincipalID, IntentHash: a.IntentHash, CapabilityID: s.CapabilityID, TargetRef: s.TargetRef, ParamsHash: s.ParamsHash,
		ConfirmationBindingEvidenceHash: fresh.ConfirmationBindingEvidenceHash, PlanHash: p.Hash, PolicyVersion: fresh.PolicyVersion, Requirement: spec.Requirement, RequesterID: a.PrincipalID,
		ExpiresAt: e.now + int64(spec.TTL/time.Second)}, nil, nil
}

// freshBinding is the two values of the confirmation binding that only this evaluation knows (INV-25): the evidence content digest T9 kept (A435) and the policy version T11 used. The policy
// version is the composite of EVERY policy version that took part in T11 (decide.CompositePolicyVersion, A341, A367): "sha256:" and a hash, whether one policy took part or several. It
// is empty when none did, or when one cannot be bound; empty values are passed on as empty and T13 refuses a Given answer for them.
func (e *evaluation) freshBinding() FreshBinding {
	b := FreshBinding{ConfirmationBindingEvidenceHash: e.confirmationBindingEvidenceHash}
	if composite, err := decide.CompositePolicyVersion(e.policyVersions); err == nil {
		b.PolicyVersion = composite
	}
	return b
}

// t14: ask the Planner for the plan and validate what comes back. A plan the Planner calls invalid, or one that is not well formed, is UNKNOWN PLAN_INVALID. For a read
// the plan is empty and has its hash (ruling A205, Q3).
func (e *evaluation) t14(ctx context.Context) decide.StepResult {
	if e.intent == nil {
		return decide.FailStep(errNoIntent)
	}
	p, ok, err := e.validPlan(ctx)
	switch {
	case err != nil:
		return decide.FailStep(err)
	case !ok:
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

// planRequest is what Trust asks the Planner for: the intent, what T3 learned and, for a continuation, the one capability to plan with the facts and the evidence packet the
// caller gave. The policy params are T11's, copied, for every capability it asked the policy about.
func (e *evaluation) planRequest() PlanRequest {
	a := e.intent
	r := PlanRequest{TenantID: a.TenantID, CorrelationID: a.CorrelationID, IntentID: a.IntentID, IntentVersion: a.Version, IntentHash: a.IntentHash, IntentNode: a.IntentNode,
		WriteRequired: e.late.needsWrite}
	if len(e.policyParams) > 0 {
		r.PolicyParams = make(map[string]map[string]string, len(e.policyParams))
		for id, p := range e.policyParams {
			r.PolicyParams[id] = cloneStrings(p)
		}
	}
	switch {
	case e.step != nil:
		r.Capabilities, r.Facts, r.EvidencePacket = []string{e.step.capabilityID}, cloneStrings(e.step.facts), e.step.packet
	case e.firstRead:
		// A511: a first evaluation of a write names the capabilities Trust itself says may be planned now. firstCapabilities has already refused an empty set, so a list that is
		// empty here never means "plan everything"; Facts and EvidencePacket stay empty, a first evaluation has no verified result to carry.
		r.Capabilities = append([]string(nil), e.first...)
	}
	return r
}

// firstCapabilities reads, once per evaluation, the capabilities a first evaluation of a write may plan (A511): the authorized set (the intent's and the tenant's policy, T4 and T11)
// intersected with what the Runnable port says may run now on the intent node's graph, sorted, without repeats. ok is false for an empty intersection: nothing authorized is runnable,
// which is a plan that is not one (PLAN_INVALID), and the Planner is never asked with an empty list, which would mean "the first steps as before". An error from either port is
// an error, never an empty set and never all. T12 asks it first (noRunnable, A531), so the empty set ends there as ESCALATE or UNKNOWN; the PLAN_INVALID below is the backstop for a
// request that reaches T14 with an empty set. No port, a continuation (which has its own set) and a read (no write step) ask nothing and leave the request as it was.
func (e *evaluation) firstCapabilities(ctx context.Context) (ok bool, err error) {
	if e.step != nil || !e.late.needsWrite || e.svc.Runnable == nil {
		return true, nil
	}
	if e.firstRead {
		return len(e.first) > 0, nil
	}
	authorized, err := e.authorizedCapabilities(ctx)
	if err != nil {
		return false, err
	}
	a := e.intent
	// A first evaluation has no verified result to carry, so the query's EvidencePacket stays empty here (A694); it is never defaulted.
	ids, err := e.svc.Runnable.Runnable(ctx, RunnableQuery{TenantID: a.TenantID, IntentID: a.IntentID, IntentVersion: a.Version, IntentNode: a.IntentNode, SnapshotID: e.snapshotID})
	if err != nil {
		return false, err
	}
	var both []string
	for _, id := range ids {
		if slices.Contains(authorized, id) && !slices.Contains(both, id) {
			both = append(both, id)
		}
	}
	sort.Strings(both)
	e.first, e.firstRead = both, true
	return len(both) > 0, nil
}

// validPlan asks the Planner for the plan of the admitted intent and validates it: the Planner must not call it invalid, it must be well formed, and every step must agree with what
// the earlier steps found (planAgrees). T14 and T13 (which needs the plan to bind a new confirmation request) use this one function. ok is false for a plan that is not valid
// (PLAN_INVALID); an error is a dependency that failed, never a pass.
func (e *evaluation) validPlan(ctx context.Context) (p Plan, ok bool, err error) {
	switch asked, err := e.firstCapabilities(ctx); {
	case err != nil:
		return Plan{}, false, err
	case !asked:
		return Plan{}, false, nil
	}
	p, err = e.svc.Ports.Planner.Plan(ctx, e.planRequest())
	switch {
	case errors.Is(err, ErrPlanInvalid):
		return Plan{}, false, nil
	case err != nil:
		return Plan{}, false, err
	case !e.wellFormed(p):
		return Plan{}, false, nil
	}
	agrees, err := e.planAgrees(ctx, p)
	if err != nil || !agrees {
		return Plan{}, false, err
	}
	return p, true, nil
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
	if e.step != nil && (len(p.Steps) != 1 || p.Steps[0].CapabilityID != e.step.capabilityID) {
		return false, nil // a continuation is one step with one grant (A335)
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
	var bindingEvidence string // the evidence component of the binding the confirmation was given for (A435): only an ALLOW that follows one names it
	if e.late.confirmationID != "" {
		bindingEvidence = e.confirmationBindingEvidenceHash
	}
	res, err := e.svc.Ports.Recorder.Record(ctx, RecordRequest{TenantID: a.TenantID, CorrelationID: a.CorrelationID, PrincipalID: a.PrincipalID, IntentID: a.IntentID,
		IntentHash: a.IntentHash, IntentVersion: a.Version, Decision: d, Evaluated: e.bb.Sealed(), BasisDigest: digest, PlanHash: plan.Hash, Steps: steps,
		ConfirmationEventID: e.late.confirmationID, ConfirmationPlanHash: e.late.confirmationPlanHash, ConfirmationBindingEvidenceHash: bindingEvidence,
		DispositionDecisionID: e.recordedDisposition(steps)})
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

// recordedDisposition is the disposition the Decision Record names (A562, REQ-TRU-010): the one the continuation follows, and only when a step of the decision is in the pack's escalation plan.
// A read, a step outside the set, or a service with no escalation set names none (fail closed).
func (e *evaluation) recordedDisposition(steps []RecordStep) string {
	d := e.disposition()
	if d == "" || e.svc.Escalation == nil {
		return ""
	}
	for _, s := range steps {
		if e.svc.Escalation.Contains(s.CapabilityID) {
			return d
		}
	}
	return ""
}

// disposition is the ESCALATE decision a continuation follows, as the caller named it (A534); empty for a first evaluation and for a continuation that names none.
func (e *evaluation) disposition() string {
	if e.step == nil {
		return ""
	}
	return e.step.disposition
}

// recordEscalate records the ESCALATE that the evaluation has ACCEPTED, through the Recorder, before it is returned (A534, interim, relayed; EC-E16, INV-25): the decision with the evaluated
// basis, no step, no plan, no confirmation request, no grant. It runs in run, after EvaluateBound, so the decision is the one that is returned: an ESCALATE that Evaluate turned into UNKNOWN
// (the step may not end in ESCALATE, the context ended, the intent version differs) never reaches it, and the Record holds nothing for it (Codex and C2 on noema#903). When the Record fails,
// when the context has ended since, or when the Recorder answers what a decision with no step cannot be (no or malformed decision ID, a grant, a request), the result is UNKNOWN
// INTERNAL_ERROR, never an ESCALATE nobody recorded. The decision ID of the record is the result's.
func (e *evaluation) recordEscalate(ctx context.Context, d decide.Decision, trace []decide.TraceEntry) (decide.Decision, []decide.TraceEntry) {
	failed := func() (decide.Decision, []decide.TraceEntry) {
		if n := len(trace); n > 0 {
			trace[n-1].Result = decide.StepFailed
		}
		return decide.InternalError(e.version), trace
	}
	if e.intent == nil || ctx.Err() != nil {
		return failed()
	}
	d, err := e.withBasis(d)
	if err != nil {
		return failed()
	}
	digest, err := e.bb.Digest()
	if err != nil {
		return failed()
	}
	a := e.intent
	req := RecordRequest{TenantID: a.TenantID, CorrelationID: a.CorrelationID, PrincipalID: a.PrincipalID, IntentID: a.IntentID,
		IntentHash: a.IntentHash, IntentVersion: a.Version, Decision: d, Evaluated: e.bb.Sealed(), BasisDigest: digest}
	if !d.HasCompleteBasis() {
		// A663, A581, A590 (interim, relayed): an ESCALATE reached before the evidence and the policy were evaluated is recorded with its basis null, keyed by the turn. A request that names no
		// well-formed turn cannot be keyed, so nothing is recorded and the evaluation ends in UNKNOWN INTERNAL_ERROR (A578), never an ESCALATE nobody recorded.
		if !turnIDRe.MatchString(e.req.TurnID) {
			return failed()
		}
		req.TurnID = e.req.TurnID
	}
	rec, err := e.svc.Ports.Recorder.Record(ctx, req)
	if err != nil || !decisionIDRe.MatchString(rec.DecisionID) || len(rec.GrantIDs) != 0 || rec.ConfirmationRequestID != "" {
		return failed()
	}
	e.late.recorded = RecordResult{DecisionID: rec.DecisionID}
	return d, trace
}

// recordRefusal records a refusal at T1 to T9 (A680, interim, relayed; A574, A581, A590; EC-E16, INV-25): a REJECT whose basis is incomplete, because the evidence and the policy were not
// evaluated, is recorded through the Recorder before it is returned, in the null-basis form keyed by the turn of the request and the verified principal, with no step and no grant. It runs in
// run, after EvaluateBound, so the decision is the one that is returned. A refusal that needs no record is returned as it is: one with a complete basis (T11; its basis is the policy's), one the
// intake cannot key (no admitted intent, or a request that names no well-formed turn: the evaluation refuses as it always did and records nothing), and the declined approval of T13, which
// recorded itself. When the Record fails, when the context has ended since, or when the Recorder answers what a refusal cannot be (no or malformed decision ID, a grant, a request), the
// result is UNKNOWN INTERNAL_ERROR, never a REJECT the Record does not hold.
func (e *evaluation) recordRefusal(ctx context.Context, d decide.Decision, trace []decide.TraceEntry) (decide.Decision, []decide.TraceEntry) {
	if e.intent == nil || d.HasCompleteBasis() || !turnIDRe.MatchString(e.req.TurnID) {
		return d, trace
	}
	failed := func() (decide.Decision, []decide.TraceEntry) {
		if n := len(trace); n > 0 {
			trace[n-1].Result = decide.StepFailed
		}
		return decide.InternalError(e.version), trace
	}
	if ctx.Err() != nil {
		return failed()
	}
	d, err := e.withBasis(d)
	if err != nil {
		return failed()
	}
	digest, err := e.bb.Digest()
	if err != nil {
		return failed()
	}
	a := e.intent
	rec, err := e.svc.Ports.Recorder.Record(ctx, RecordRequest{TenantID: a.TenantID, CorrelationID: a.CorrelationID, PrincipalID: a.PrincipalID, IntentID: a.IntentID,
		IntentHash: a.IntentHash, IntentVersion: a.Version, Decision: d, Evaluated: e.bb.Sealed(), BasisDigest: digest, TurnID: e.req.TurnID})
	if err != nil || !decisionIDRe.MatchString(rec.DecisionID) || len(rec.GrantIDs) != 0 || rec.ConfirmationRequestID != "" {
		return failed()
	}
	e.late.recorded = RecordResult{DecisionID: rec.DecisionID}
	return d, trace
}
