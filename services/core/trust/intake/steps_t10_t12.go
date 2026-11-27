package intake

import (
	"context"
	"errors"
	"slices"
	"sort"
	"strings"

	"github.com/Noemai-org/noema/services/core/trust/clocks"
	"github.com/Noemai-org/noema/services/core/trust/decide"
	"github.com/Noemai-org/noema/services/core/trust/policy"
	"github.com/Noemai-org/noema/services/core/trust/reconcile"
)

// reasonNoRunnable is the core reason code of a first evaluation with no runnable capability (A531).
const reasonNoRunnable = "NO_RUNNABLE_CAPABILITY"

// Errors of T10 to T12. None is a decision: the evaluation turns each into UNKNOWN INTERNAL_ERROR at the step (INV-16).
var (
	errNotReady   = errors.New("intake: the step ran before the evaluation held what it needs")
	errNoSnapshot = errors.New("intake: there is no sealed evidence snapshot to reconcile")
	errNoMapping  = errors.New("intake: the target is frozen for this capability and the pack maps nothing to escalate with")
	errAnswer     = errors.New("intake: a port gave an answer the step cannot use")
	// the two refusals of a continuation (TRU-14): not decisions, so they end the evaluation in UNKNOWN INTERNAL_ERROR like any other step failure (INV-16)
	errStepNotWrite   = errors.New("intake: a continuation was asked for an intent that leads to no write")
	errStepNotAllowed = errors.New("intake: the continuation's capability is not authorized and runnable now")
	errNoHandoff      = errors.New("intake: no authorized capability is runnable and the pack routes no handoff for it")
)

// packReason makes the reason code `<vertical>.reason.<name>` a pack gave (T10 and T12 take nothing else from a pack, and core codes are never a pack's to give).
func packReason(code string) (decide.ReasonCode, error) {
	vertical, _, _ := strings.Cut(code, ".")
	return decide.Pack(vertical, code)
}

// ruleReason makes the reason code of a policy rule, which is a core code or a pack code.
func ruleReason(code string) (decide.ReasonCode, error) {
	if r, err := decide.Core(code); err == nil {
		return r, nil
	}
	return packReason(code)
}

// layers are the authority layers of a decision made after the technical layer (T6) and the business checks (T8, T10) passed.
func layers(policyResult decide.LayerResult) decide.Authority {
	return decide.Authority{Technical: decide.Pass, Policy: policyResult, Business: decide.Pass}
}

// t10: reconcile the sealed snapshot; escalate with the pack's reason code when the pack maps the classification to ESCALATE (a contradiction, an unknown outcome),
// and when the target is frozen for the requested capability (REQ-RES-002).
func (e *evaluation) t10(ctx context.Context) decide.StepResult {
	r, err := e.resolved()
	if err != nil {
		return decide.FailStep(err)
	}
	if e.snapshotID == "" {
		return decide.FailStep(errNoSnapshot)
	}
	rec, err := e.svc.Ports.Reconciler.Reconcile(ctx, e.req.Session.TenantID, e.snapshotID)
	if err != nil {
		return decide.FailStep(err)
	}
	if rec.Classification == "" {
		return decide.FailStep(errAnswer)
	}
	e.frozen, e.allowedUnderFreeze = rec.Frozen, append([]string(nil), rec.AllowedUnderFreeze...) // T14 checks the plan's capabilities against the freeze
	if rec.WaitUntil != 0 {
		// REQ-RES-005 (interim, relayed): every disagreement is still inside its convergence window, so the answer waits: no write and no verdict until the end of the window. A waiting answer is
		// neither a verdict nor a freeze (INV-20), so one that names a settled classification, a freeze or an end that is not in the future is a defect and fails closed (INV-16). PERD 23.3 lets
		// only T11 end an evaluation in WAIT, so T10 goes on with the end of the window and T11 ends in WAIT before it asks the policy anything.
		if rec.WaitUntil <= e.now || rec.Frozen || rec.Classification != reconcile.InsufficientEvidence {
			return decide.FailStep(errAnswer)
		}
		e.waitUntil = rec.WaitUntil
		return decide.Continue()
	}
	if rec.EscalateReason == "" {
		// Nothing maps this classification to an escalation. A frozen target: reads continue, and only a write the freeze blocks is refused, as a typed refusal and not an internal
		// error (A225, A254: TARGET_FROZEN); an intent that names no capability asks for no write (the plan's capabilities are checked against the freeze again at T14). Any other
		// target goes on only on a settled classification.
		if rec.Frozen {
			if e.capability != nil && freezeBlocks(rec, e.capability) {
				return escalateCore(r, "TARGET_FROZEN", decide.Authority{Technical: decide.Pass, Policy: decide.NotEvaluated, Business: decide.Pass})
			}
			return decide.Continue()
		}
		if !goesOnUnmapped(rec.Classification) {
			return decide.FailStep(errNoMapping)
		}
		return decide.Continue()
	}
	return escalate(r, rec.EscalateReason, decide.Authority{Technical: decide.Pass, Policy: decide.NotEvaluated, Business: decide.Pass})
}

// goesOnUnmapped reports whether a classification the pack maps to nothing lets the evaluation go on. Only the classifications that name a settled, understood state do;
// a contradiction, an unknown outcome, a possible duplicate, an uncertain event order, insufficient evidence and a value that is not a classification at all stop it, so
// the pack may add escalations and is never the only thing between a contradiction and an ALLOW (INV-16, REQ-RES-001; the Security Reviewer's blocking finding on
// noema#570). The split of the twelve into these two sets is accepted by ruling A218, refined by A225: a Partial classification is unsettled and fails closed (both relayed: not on `docs` main yet).
func goesOnUnmapped(c reconcile.Classification) bool {
	switch c {
	case reconcile.SuccessConfirmed, reconcile.RejectedConfirmed, reconcile.ReversedConfirmed, reconcile.DebitPostedCreditUnconfirmed,
		reconcile.SettlementPending, reconcile.ReversalPending:
		return true
	}
	return false
}

// freezeBlocks reports that the target is frozen and the pack does not allow the requested capability under the freeze. It needs a capability: the caller refuses a frozen
// target that names none.
func freezeBlocks(rec Reconciliation, c *Capability) bool {
	if !rec.Frozen || c == nil {
		return false
	}
	_, err := reconcile.NewFreezeGate(rec.AllowedUnderFreeze).Permit(rec.Frozen, c.ID)
	return err != nil
}

// escalateCore is the ESCALATE with one core reason code that Trust itself gives when the pack routes nothing (A225) or the dispatch budget is spent (A224).
func escalateCore(r decide.ResolvedIntent, code string, a decide.Authority) decide.StepResult {
	reason, err := decide.Core(code)
	if err != nil {
		return decide.FailStep(err)
	}
	d, err := r.Escalate(a, reason)
	if err != nil {
		return decide.FailStep(err)
	}
	return decide.Stop(d)
}

// escalate is the ESCALATE that a pack reason code asks for.
func escalate(r decide.ResolvedIntent, packCode string, a decide.Authority) decide.StepResult {
	code, err := packReason(packCode)
	if err != nil {
		return decide.FailStep(err)
	}
	d, err := r.Escalate(a, code)
	if err != nil {
		return decide.FailStep(err)
	}
	return decide.Stop(d)
}

// t11: evaluate the tenant's active policy set for every capability a plan may use, and combine the answers into the one outcome (A306; policy_combine.go). The rules that
// decided are recorded in the basis once, whatever the effect; each effect is its own outcome; no permitting rule is REJECT NO_PERMITTING_RULE (default deny, PERD 23.4); a CONFIRM
// stands only with a PERMIT matched for every capability (INV-03). The Policies port stays the one place policy is evaluated: the intake asks it once for each capability.
func (e *evaluation) t11(ctx context.Context) decide.StepResult {
	r, err := e.resolved()
	if err != nil {
		return decide.FailStep(err)
	}
	if e.bb == nil {
		return decide.FailStep(errNotReady)
	}
	if e.waitUntil != 0 {
		// The reconciliation answer waits inside a convergence window (T10): nothing is asked of the policy and nothing is permitted. The decision is WAIT until the end of the window (REQ-RES-005).
		reason, err := decide.Core("INSUFFICIENT_EVIDENCE")
		if err != nil {
			return decide.FailStep(err)
		}
		d, err := r.Wait(layers(decide.NotEvaluated), e.waitUntil, reason)
		if err != nil {
			return decide.FailStep(err)
		}
		return decide.Stop(d)
	}
	ids, err := e.policyCapabilities(ctx)
	if err != nil {
		return decide.FailStep(err)
	}
	if d, stop, err := e.rateLimited(ctx, r, ids); err != nil {
		return decide.FailStep(err)
	} else if stop {
		return decide.Stop(d)
	}
	answers := make([]PolicyEvaluation, 0, len(ids))
	params := map[string]map[string]string{}
	for _, id := range ids {
		res, err := e.svc.Ports.Policies.Evaluate(ctx, PolicyQuery{TenantID: e.req.Session.TenantID, IntentID: e.intentID, IntentVersion: e.version, IntentNode: e.intent.IntentNode,
			CapabilityID: id, SnapshotID: e.snapshotID, Now: e.now})
		if err != nil {
			return decide.FailStep(err)
		}
		for _, rule := range res.Rules {
			if rule.RuleID == "" || rule.PolicyID == "" || rule.Version == "" {
				return decide.FailStep(errAnswer)
			}
		}
		answers = append(answers, res)
		if res.PermitMatched && len(res.PermitParams) > 0 {
			params[id] = cloneStrings(res.PermitParams) // the winning PERMIT rule's params, for the plan request (TRU-14)
		}
	}
	res, err := combinePolicies(answers)
	if err != nil {
		return decide.FailStep(err)
	}
	d, goOn, err := e.policyOutcome(r, res)
	if err != nil {
		return decide.FailStep(err)
	}
	if err := e.bb.RecordPolicy(res.Rules); err != nil {
		return decide.FailStep(err)
	}
	e.policyVersions = append([]decide.PolicyVersionRef(nil), res.Versions...)
	if len(params) > 0 {
		e.policyParams = params
	}
	if goOn {
		return decide.Continue()
	}
	if d.Outcome() == decide.Confirm {
		if d, err = e.bb.Attach(d); err != nil {
			return decide.FailStep(err)
		}
	}
	return decide.Stop(d)
}

// policyCapabilities is the capability IDs T11 asks the policy about. A write may use any capability of the authorized set (T14 refuses the rest), so the policy is asked about each
// of them, sorted; the set is read here once for the evaluation and T12 and T14 reuse it. A read has an empty plan, so only the intent's own capability is asked about, as the
// intent names it (empty when it names none); so does a write whose set is empty, which has nothing but the intent node to be judged by.
func (e *evaluation) policyCapabilities(ctx context.Context) ([]string, error) {
	own := ""
	if e.capability != nil {
		own = e.capability.ID
	}
	if !e.late.needsWrite {
		return []string{own}, nil
	}
	set, err := e.authorizedCapabilities(ctx)
	if err != nil {
		return nil, err
	}
	if len(set) == 0 {
		return []string{own}, nil
	}
	return set, nil
}

// policyOutcome turns the winning effect into a decision, or reports that the evaluation goes on (a PERMIT that names the rule that permits).
func (e *evaluation) policyOutcome(r decide.ResolvedIntent, res PolicyEvaluation) (d decide.Decision, goOn bool, err error) {
	denied := func() (decide.Decision, bool, error) {
		code, err := decide.Core(policy.NoPermittingRule)
		if err != nil {
			return decide.Decision{}, false, err
		}
		d, err := r.Reject(layers(decide.Fail), code)
		return d, false, err
	}
	var reasons []decide.ReasonCode
	if res.ReasonCode != "" {
		code, err := ruleReason(res.ReasonCode)
		if err != nil {
			return decide.Decision{}, false, err
		}
		reasons = append(reasons, code)
	}
	switch res.Effect {
	case policy.Permit:
		if len(res.Rules) == 0 {
			return denied()
		}
		return decide.Decision{}, true, nil
	case policy.Reject:
		if len(reasons) == 0 {
			return denied()
		}
		d, err := r.Reject(layers(decide.Fail), reasons...)
		return d, false, err
	case policy.Escalate:
		d, err := r.Escalate(layers(decide.Fail), reasons...)
		return d, false, err
	case policy.Confirm:
		if !res.PermitMatched {
			return denied()
		}
		d, err := r.Confirm(layers(decide.Pass), reasons...)
		return d, false, err
	case policy.Wait:
		if res.ReevaluateAt <= e.now {
			return decide.Decision{}, false, errAnswer
		}
		d, err := r.Wait(layers(decide.NotEvaluated), res.ReevaluateAt, reasons...)
		return d, false, err
	}
	return decide.Decision{}, false, errAnswer
}

// t12: evaluate every clock that applies at the evaluation's clock reading, with the at-risk fraction the port gave, and ask the pack's routing whether to escalate.
// An input a clock cannot be evaluated from (clocks.ErrInvalid) is never guessed. Nothing about a clock is recorded in the basis.
func (e *evaluation) t12(ctx context.Context) decide.StepResult {
	r, err := e.resolved()
	if err != nil {
		return decide.FailStep(err)
	}
	q := ClockQuery{TenantID: e.req.Session.TenantID, IntentNode: e.intent.IntentNode, SnapshotID: e.snapshotID}
	set, err := e.svc.Ports.Clocks.Applicable(ctx, q)
	if err != nil {
		return decide.FailStep(err)
	}
	statuses := make([]clocks.Status, 0, len(set.Clocks))
	for _, c := range set.Clocks {
		st, err := clocks.Evaluate(c.Clock, c.Anchor, e.now, set.AtRisk)
		if err != nil {
			return decide.FailStep(err)
		}
		statuses = append(statuses, st)
	}
	if len(statuses) == 0 {
		return e.afterClocks(ctx, r)
	}
	routing, err := e.svc.Ports.Clocks.Route(ctx, q, statuses)
	if err != nil {
		return decide.FailStep(err)
	}
	if !routing.Escalate {
		// A225: a breached clock the pack's routing does not route is not a clock to ignore; T12 escalates by itself, with the core code.
		for _, st := range statuses {
			if st.Phase == clocks.Breached {
				return escalateCore(r, "CLOCK_BREACHED_UNROUTED", passed())
			}
		}
		return e.afterClocks(ctx, r)
	}
	return escalate(r, routing.ReasonCode, decide.Authority{Technical: decide.Pass, Policy: decide.Pass, Business: decide.Pass})
}

// afterClocks is what T12 asks once the clocks are done, in this order: is anything runnable (A531), then the dispatch budget (A224). Both may end the evaluation in ESCALATE, which T14
// may not (PERD 23.3).
func (e *evaluation) afterClocks(ctx context.Context, r decide.ResolvedIntent) decide.StepResult {
	if res, stop := e.noRunnable(ctx, r); stop {
		return res
	}
	return e.dispatchBudget(ctx, r)
}

// noRunnable is the end of a first evaluation of a write when no authorized capability is runnable now (A531, interim, relayed, not yet committed): neither PLAN_INVALID nor a WAIT but
// the explicit result NO_RUNNABLE_CAPABILITY, and never a grant. The disposition stays ESCALATE when the pack routes that reason to a handoff (the orchestrator raises the ops alert on
// the reason; Trust has no alert port); with no handoff routed, no port, or a port that fails, it is UNKNOWN. stop is false when something is runnable, and for a read, a continuation
// and a service with no Runnable port, which ask nothing here.
func (e *evaluation) noRunnable(ctx context.Context, r decide.ResolvedIntent) (res decide.StepResult, stop bool) {
	ok, err := e.firstCapabilities(ctx)
	switch {
	case err != nil:
		return decide.FailStep(err), true
	case ok:
		return decide.Continue(), false
	}
	if e.svc.Handoffs == nil {
		return decide.FailStep(errNoHandoff), true
	}
	routed, err := e.svc.Handoffs.Routed(ctx, HandoffQuery{TenantID: e.intent.TenantID, IntentNode: e.intent.IntentNode, ReasonCode: reasonNoRunnable})
	switch {
	case err != nil:
		return decide.FailStep(err), true
	case !routed:
		return decide.FailStep(errNoHandoff), true
	}
	return escalateCore(r, reasonNoRunnable, passed()), true
}

// dispatchBudget is the last question of T12 for a write: has any capability the intent authorizes used the ledger's dispatch budget on the target (A224, A254; Codex P2 on
// noema#572). Trust itself decides ESCALATE DISPATCH_BUDGET_EXHAUSTED; the runner's refusal is only the backstop. It runs here, where an ESCALATE may end the evaluation (the stop
// sets of PERD 23.3 give T14 none), and asks for every capability of the authorized set, the only ones a plan may use, so no step can receive an ALLOW grant for a capability whose
// budget is spent. It is fail closed and may escalate for a capability the plan would not use. A read asks nothing. The intake holds no limit: the ledger answers both numbers.
func (e *evaluation) dispatchBudget(ctx context.Context, r decide.ResolvedIntent) decide.StepResult {
	if e.intent == nil || e.intent.TargetRef == "" || (e.capability == nil && !e.late.needsWrite) {
		return decide.Continue()
	}
	set, err := e.authorizedCapabilities(ctx)
	if err != nil {
		return decide.FailStep(err)
	}
	for _, id := range set {
		c, err := e.svc.Ports.Dispatches.Count(ctx, DispatchKey{TenantID: e.intent.TenantID, CorrelationID: e.intent.CorrelationID, CapabilityID: id, TargetRef: e.intent.TargetRef})
		if err != nil {
			return decide.FailStep(err)
		}
		if c.Limit < 1 || c.Used < 0 {
			return decide.FailStep(errAnswer)
		}
		if c.Used >= c.Limit {
			return escalateCore(r, "DISPATCH_BUDGET_EXHAUSTED", passed())
		}
	}
	return decide.Continue()
}

// authorizedCapabilities reads the authorized set once per evaluation, whichever step asks first (T11, T12 or T14): the port's answer and the intent's own capability, without
// repeats, sorted. An error from the port is an error, never an empty set.  T11 reads it only for a write (a request T3 says leads to one); T12 and T14 as they did.
func (e *evaluation) authorizedCapabilities(ctx context.Context) ([]string, error) {
	if e.authorized != nil {
		return e.authorized, nil
	}
	if e.intent == nil {
		return nil, errNoIntent
	}
	ids, err := e.svc.Ports.Capabilities.Authorized(ctx, *e.intent)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	set := make([]string, 0, len(ids)+1)
	if e.capability != nil && e.capability.ID != "" {
		ids = append(ids, e.capability.ID)
	}
	for _, id := range ids {
		if id != "" && !seen[id] {
			seen[id] = true
			set = append(set, id)
		}
	}
	sort.Strings(set)
	if e.step != nil {
		if set, err = e.stepSet(ctx, set); err != nil {
			return nil, err
		}
	}
	e.authorized = set
	return set, nil
}

// stepSet limits the authorized set of a continuation to its one capability (TRU-14): the capability must be in the set the intent and the tenant's policy authorize and in the
// set the Runnable port says may run now. The port is asked once, with the facts the caller gave; an error, or a capability outside either set, is never a pass.
func (e *evaluation) stepSet(ctx context.Context, authorized []string) ([]string, error) {
	st := e.step
	if st.runnable == nil || st.capabilityID == "" || !slices.Contains(authorized, st.capabilityID) {
		return nil, errStepNotAllowed
	}
	a := e.intent
	ids, err := st.runnable.Runnable(ctx, RunnableQuery{TenantID: a.TenantID, IntentID: a.IntentID, IntentVersion: a.Version, IntentNode: a.IntentNode, SnapshotID: e.snapshotID, Facts: cloneStrings(st.facts), EvidencePacket: st.packet})
	if err != nil {
		return nil, err
	}
	if !slices.Contains(ids, st.capabilityID) {
		return nil, errStepNotAllowed
	}
	return []string{st.capabilityID}, nil
}

// rateLimited is the per-principal rate check of T11 (REQ-TRU-012): a write whose principal has used up the hourly rate of any capability the plan may use is REJECT RATE_LIMITED,
// before the policy is asked and before any grant. The tenant and the principal are the authenticated session's (INV-12). It asks only for a request that leads to a write (T3), and
// only when the Rates port is composed: a nil port limits nothing. An error from the port is an error, never "not over the rate".
func (e *evaluation) rateLimited(ctx context.Context, r decide.ResolvedIntent, ids []string) (d decide.Decision, stop bool, err error) {
	if e.svc.Rates == nil || !e.late.needsWrite {
		return decide.Decision{}, false, nil
	}
	for _, id := range ids {
		over, err := e.svc.Rates.Exceeded(ctx, RateQuery{TenantID: e.req.Session.TenantID, PrincipalID: e.req.Session.PrincipalID, CapabilityID: id})
		if err != nil {
			return decide.Decision{}, false, err
		}
		if over {
			code, err := decide.Core("RATE_LIMITED")
			if err != nil {
				return decide.Decision{}, false, err
			}
			d, err := r.Reject(layers(decide.Fail), code)
			return d, true, err
		}
	}
	return decide.Decision{}, false, nil
}
