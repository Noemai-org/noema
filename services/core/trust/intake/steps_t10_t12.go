package intake

import (
	"context"
	"errors"
	"strings"

	"github.com/Noemai-org/noema/services/core/trust/clocks"
	"github.com/Noemai-org/noema/services/core/trust/decide"
	"github.com/Noemai-org/noema/services/core/trust/policy"
	"github.com/Noemai-org/noema/services/core/trust/reconcile"
)

// Errors of T10 to T12. None is a decision: the evaluation turns each into UNKNOWN INTERNAL_ERROR at the step (INV-16).
var (
	errNotReady   = errors.New("intake: the step ran before the evaluation held what it needs")
	errUnresolved = errors.New("intake: the intent is not resolved, and this step has no decision for an unresolved intent")
	errNoSnapshot = errors.New("intake: there is no sealed evidence snapshot to reconcile")
	errNoMapping  = errors.New("intake: the target is frozen for this capability and the pack maps nothing to escalate with")
	errAnswer     = errors.New("intake: a port gave an answer the step cannot use")
)

// resolved is the intent as a resolved intent. T10 to T12 come after T7, which clarifies or escalates an intent that is not resolved; one that reaches them
// anyway is a defect, and these steps have no decision for it.
func (e *evaluation) resolved() (decide.ResolvedIntent, error) {
	if e.intent == nil {
		return decide.ResolvedIntent{}, errNotReady
	}
	r, ok := decide.Admit(e.intent.View).Resolved()
	if !ok {
		return decide.ResolvedIntent{}, errUnresolved
	}
	return r, nil
}

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
	if rec.EscalateReason == "" {
		// Nothing maps this classification to an escalation. A frozen target goes on only with a capability the pack allows under the freeze; an intent that names none has
		// nothing that says what it may do (its plan's capabilities are checked against the freeze again at T14). Any other target goes on only on a settled classification.
		if rec.Frozen && (e.capability == nil || freezeBlocks(rec, e.capability)) {
			return decide.FailStep(errNoMapping)
		}
		if !rec.Frozen && !goesOnUnmapped(rec.Classification) {
			return decide.FailStep(errNoMapping)
		}
		return decide.Continue()
	}
	return escalate(r, rec.EscalateReason, decide.Authority{Technical: decide.Pass, Policy: decide.NotEvaluated, Business: decide.Pass})
}

// goesOnUnmapped reports whether a classification the pack maps to nothing lets the evaluation go on. Only the classifications that name a settled, understood state do;
// a contradiction, an unknown outcome, a possible duplicate, an uncertain event order, insufficient evidence and a value that is not a classification at all stop it, so
// the pack may add escalations and is never the only thing between a contradiction and an ALLOW (INV-16, REQ-RES-001; the Security Reviewer's blocking finding on
// noema#570). This is the provisional safer reading of PERD 22.3 and 23.3 (AGENT-20) until the Architect confirms or changes the two sets.
func goesOnUnmapped(c reconcile.Classification) bool {
	switch c {
	case reconcile.SuccessConfirmed, reconcile.RejectedConfirmed, reconcile.ReversedConfirmed, reconcile.DebitPostedCreditUnconfirmed,
		reconcile.SettlementPending, reconcile.ReversalPending, reconcile.Partial:
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

// t11: evaluate the tenant's active policy set. The rules that decided are recorded in the basis once, whatever the effect; each effect is its own outcome; no
// permitting rule is REJECT NO_PERMITTING_RULE (default deny, PERD 23.4); a CONFIRM stands only with a PERMIT matched (INV-03).
func (e *evaluation) t11(ctx context.Context) decide.StepResult {
	r, err := e.resolved()
	if err != nil {
		return decide.FailStep(err)
	}
	if e.bb == nil {
		return decide.FailStep(errNotReady)
	}
	q := PolicyQuery{TenantID: e.req.Session.TenantID, IntentID: e.intentID, IntentVersion: e.version, IntentNode: e.intent.IntentNode, SnapshotID: e.snapshotID, Now: e.now}
	if e.capability != nil {
		q.CapabilityID = e.capability.ID
	}
	res, err := e.svc.Ports.Policies.Evaluate(ctx, q)
	if err != nil {
		return decide.FailStep(err)
	}
	for _, rule := range res.Rules {
		if rule.RuleID == "" || rule.PolicyID == "" || rule.Version == "" {
			return decide.FailStep(errAnswer)
		}
	}
	d, goOn, err := e.policyOutcome(r, res)
	if err != nil {
		return decide.FailStep(err)
	}
	if err := e.bb.RecordPolicy(res.Rules); err != nil {
		return decide.FailStep(err)
	}
	e.policyVersions = append([]decide.PolicyVersionRef(nil), res.Versions...)
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
		return decide.Continue()
	}
	routing, err := e.svc.Ports.Clocks.Route(ctx, q, statuses)
	if err != nil {
		return decide.FailStep(err)
	}
	if !routing.Escalate {
		return decide.Continue()
	}
	return escalate(r, routing.ReasonCode, decide.Authority{Technical: decide.Pass, Policy: decide.Pass, Business: decide.Pass})
}
