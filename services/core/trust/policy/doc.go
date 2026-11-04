// Package policy: the versioned, effective-dated policy model, its loader, its evaluator, the precedence ladder and default
// deny (PERD §23.4, REQ-TRU-004). Policy is data, never code: a rule is a class, an effect, the targets it applies to and a
// predicate in the SoT §43.7 language, type-checked when the policy loads.
//
// Implements: REQ-TRU-004 (REQ-TRU-005 and REQ-OPS-003, the publication transaction and the control plane, are TRU-08).
//
// May import: platform/{clock,crypto,config,telemetry,redaction}, internal/contracts, internal/predicate.
//
// Must never: hold a threshold, TTL or margin as a literal (it comes from platform/config); evaluate a prose rule; treat a
// retired policy version as active; read the clock (the caller passes now and admitted_at); call a model.
//
// What the evaluator decides, and what it leaves to the caller:
//   - It returns the winning rule's effect (REJECT, ESCALATE, CONFIRM, WAIT or PERMIT) and every rule that matched, with the
//     policy version each came from (INV-09). With no matching rule the effect is REJECT NO_PERMITTING_RULE.
//   - A CONFIRM never grants authority by itself: a winning CONFIRM with no PERMIT matched is REJECT NO_PERMITTING_RULE with
//     Result.ConfirmWithoutPermit set (PERD §23.4, INV-03). With a PERMIT matched, the CONFIRM stands and the action needs
//     the approver.
//   - It does not turn an effect into a Trust decision. Whether a winning CONFIRM or WAIT can later lead to an ALLOW (the
//     confirmation flow, TRU-07) is the caller's.
//   - A condition that cannot be evaluated is an error (ErrCondition), never a result: the caller turns it into UNKNOWN
//     (INV-16). It never skips the rule.
package policy
