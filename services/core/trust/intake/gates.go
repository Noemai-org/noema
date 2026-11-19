package intake

import (
	"context"
	"errors"

	"github.com/Noemai-org/noema/services/core/trust/decide"
)

// evaluation holds what one Evaluate call learns as it goes: the admitted intent T1 read, the capability T4 saw, the evaluation's one clock reading (Unix
// seconds) and the basis builder. It is private to the call and never shared.
type evaluation struct {
	svc        *Service
	req        Request
	intentID   string
	version    int
	intent     *AdmittedIntent
	capability *Capability
	snapshotID string
	now        int64
	bb         *decide.BasisBuilder
	// policyVersions are the policy versions T11 used, kept for the Decision Input (REQ-TRU-007).
	policyVersions []decide.PolicyVersionRef
	// evidenceSetHash is the evidence_set_hash of the sealed snapshot T9 read, kept for the binding T13 passes to the Confirmations port (INV-25). Empty when the Evidence port gave none.
	evidenceSetHash string
	late            lateState // what T3 and T13 to T15 learn
	// frozen and allowedUnderFreeze are what T10 saw: the target carries a contradiction freeze, and the capabilities the pack allows on it. T14 checks the plan against them.
	frozen             bool
	allowedUnderFreeze []string
	// authorized is the capabilities the intent and the tenant's policy authorize (Capabilities.Authorized plus the intent's own capability), sorted, read once per evaluation
	// by the first step that needs it (T11 for a write): T11 asks the policy about each (A306), T12 asks the dispatch budget for each, T14 refuses a plan step outside it
	// (Codex P1 and P2 on noema#572). nil until first read.
	authorized []string
	// step is set only by a continuation evaluation (EvaluateStep): the one capability it decides. nil for Evaluate.
	step *stepScope
	// policyParams are the winning PERMIT rule's params T11 read, per capability (TRU-14); passed to the Planner.
	policyParams map[string]map[string]string
}

func (e *evaluation) steps() decide.Steps {
	return decide.Steps{
		decide.T1AdmittedIntent - 1:     e.t1,
		decide.T2Session - 1:            e.t2,
		decide.T3WritesEnabled - 1:      e.t3,
		decide.T4Capability - 1:         e.t4,
		decide.T5Scope - 1:              e.t5,
		decide.T6TechnicalAuthority - 1: e.t6,
		decide.T7TargetResolution - 1:   e.t7,
		decide.T8BusinessOwnership - 1:  e.t8,
		decide.T9Evidence - 1:           e.t9,
		decide.T10Reconciliation - 1:    e.t10,
		decide.T11Policy - 1:            e.t11,
		decide.T12Clocks - 1:            e.t12,
		decide.T13Confirmation - 1:      e.t13,
		decide.T14Plan - 1:              e.t14,
		decide.T15DecisionRecorded - 1:  e.t15,
	}
}

// refuse is the one decision a failed gate step produces (decide.FailAtGate fixes the outcome and the core reason code of each step, PERD 23.3).
func (e *evaluation) refuse(step decide.GateStep) decide.StepResult {
	d, err := decide.FailAtGate(step, e.version)
	if err != nil {
		return decide.FailStep(err)
	}
	return decide.Stop(d)
}

// t1: the admitted Intent Object exists for the session's tenant at the version asked for, that version is the latest, and it still verifies. An unknown intent and another
// tenant's are the same answer. Without a tenant and a principal in the session nothing is read: the step passes and T2 refuses.
func (e *evaluation) t1(ctx context.Context) decide.StepResult {
	s := e.req.Session
	if s.TenantID == "" || s.PrincipalID == "" {
		return decide.Continue()
	}
	a, err := e.svc.Ports.Intents.Get(ctx, s.TenantID, e.intentID, e.version)
	switch {
	case errors.Is(err, ErrNotFound):
		return e.refuse(decide.T1AdmittedIntent)
	case err != nil:
		return decide.FailStep(err)
	}
	if a.TenantID != s.TenantID || a.IntentID != e.intentID || a.Version != e.version {
		return e.refuse(decide.T1AdmittedIntent)
	}
	// The intent was admitted under one correlation; a request under another is not this intent's request (A226). An empty correlation never matches.
	if e.req.CorrelationID == "" || a.CorrelationID != e.req.CorrelationID {
		return e.refuse(decide.T1AdmittedIntent)
	}
	latest, err := e.svc.Ports.Intents.LatestVersion(ctx, s.TenantID, e.intentID)
	if err != nil {
		return decide.FailStep(err)
	}
	if latest != e.version {
		return e.refuse(decide.T1AdmittedIntent)
	}
	switch err := e.svc.Ports.Intents.Verify(ctx, a); {
	case errors.Is(err, ErrInvalid):
		return e.refuse(decide.T1AdmittedIntent)
	case err != nil:
		return decide.FailStep(err)
	}
	e.intent = &a
	return decide.Continue()
}

// t2: the session names a tenant and a principal, and the intent's principal is the session's (INV-12: identity comes from the session and from nothing else).
func (e *evaluation) t2(context.Context) decide.StepResult {
	s := e.req.Session
	if s.TenantID == "" || s.PrincipalID == "" || e.intent == nil || e.intent.PrincipalID != s.PrincipalID {
		return e.refuse(decide.T2Session)
	}
	return decide.Continue()
}

// t3: the tenant's kill switch and the pack: UNKNOWN WRITES_DISABLED for a request that leads to a write; reads continue.
func (e *evaluation) t3(ctx context.Context) decide.StepResult {
	needs, err := e.svc.Ports.Writes.NeedsWrite(ctx, *e.intent)
	if err != nil {
		return decide.FailStep(err)
	}
	e.late.needsWrite = needs
	if !needs {
		return decide.Continue()
	}
	enabled, err := e.svc.Ports.Writes.Enabled(ctx, e.req.Session.TenantID)
	if err != nil {
		return decide.FailStep(err)
	}
	if !enabled {
		return e.refuse(decide.T3WritesEnabled)
	}
	return decide.Continue()
}

// t4: a capability the intent asks for must be registered and not prohibited. A capability the registry does not know has no adapter and is refused the same way.
func (e *evaluation) t4(ctx context.Context) decide.StepResult {
	c, present, err := e.svc.Ports.Capabilities.Requested(ctx, *e.intent)
	if err != nil {
		return decide.FailStep(err)
	}
	if present && (!c.Registered || c.Prohibited) {
		return e.refuse(decide.T4Capability)
	}
	if present {
		e.capability = &c
	}
	return decide.Continue()
}

// t5: the intent node is in scope for the tenant.
func (e *evaluation) t5(ctx context.Context) decide.StepResult {
	ok, err := e.svc.Ports.Scope.InScope(ctx, e.req.Session.TenantID, e.intent.IntentNode)
	if err != nil {
		return decide.FailStep(err)
	}
	if !ok {
		return e.refuse(decide.T5Scope)
	}
	return decide.Continue()
}

// t6: the session's role is allowed by the node's trust profile. A session with no role is refused without asking.
func (e *evaluation) t6(ctx context.Context) decide.StepResult {
	role := e.req.Session.Role
	if role == "" {
		return e.refuse(decide.T6TechnicalAuthority)
	}
	ok, err := e.svc.Ports.Authority.RoleAllowed(ctx, e.intent.IntentNode, role)
	if err != nil {
		return decide.FailStep(err)
	}
	if !ok {
		return e.refuse(decide.T6TechnicalAuthority)
	}
	return decide.Continue()
}
