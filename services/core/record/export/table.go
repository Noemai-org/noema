package export

import "strings"

// The step table of the neutral audit_record (the scoring side's table, agreed on noema#355 item 2). It is many-to-one: no Noema
// vocabulary leaves in `step` (INV-22). An event type absent from both lists is a refusal (ErrUnmapped), never a silent drop.
var stepOfType = map[string]string{
	"INPUT_RECEIVED": "INTAKE", "INPUT_DEDUPLICATED": "INTAKE", "SESSION_VALIDATED": "INTAKE",
	"INTENT_PROPOSED": "UNDERSTAND", "INTENT_ADMITTED": "UNDERSTAND", "INTENT_REJECTED_AT_GATE": "UNDERSTAND", "LEXICON_APPLIED": "UNDERSTAND",
	"CANDIDATES_RESOLVED":     "RESOLVE_TARGET",
	"CLARIFICATION_REQUESTED": "CLARIFY", "CLARIFICATION_ANSWERED": "CLARIFY", "CLARIFICATION_EXPIRED": "CLARIFY",
	"EVIDENCE_READ": "READ_EVIDENCE", "EVIDENCE_REFRESHED": "READ_EVIDENCE", "CONTEXT_SNAPSHOT_SEALED": "READ_EVIDENCE", "INBOUND_EVENT_RECEIVED": "READ_EVIDENCE",
	"STATE_RECONCILED": "RECONCILE", "CONTRADICTION_FREEZE_SET": "RECONCILE", "CONTRADICTION_FREEZE_CLEARED": "RECONCILE", "SLA_EVALUATED": "RECONCILE",
	"POLICY_EVALUATED": "DECIDE", "DECISION_MADE": "DECIDE",
	"CONFIRMATION_REQUESTED": "CONFIRM", "CONFIRMATION_RECEIVED": "CONFIRM", "CONFIRMATION_EXPIRED": "CONFIRM",
	"PLAN_CREATED": "PLAN",
	"GRANT_ISSUED": "AUTHORISE", "GRANT_VOIDED": "AUTHORISE", "REVALIDATION_PASSED": "AUTHORISE", "REVALIDATION_FAILED": "AUTHORISE", "GRANT_SPENT": "AUTHORISE",
	"DISPATCH_ATTEMPTED": "EXECUTE", "RECEIPT": "EXECUTE",
	"VERIFICATION_READ": "VERIFY", "VERDICT": "VERIFY", "WATCH_CREATED": "VERIFY", "WATCH_CHECKED": "VERIFY", "WATCH_CLOSED": "VERIFY", "RECOVERY_REQUESTED": "VERIFY",
	"EXPLANATION_RENDERED": "EXPLAIN", "EXPLANATION_BLOCKED": "EXPLAIN",
	"INVESTIGATION_STATE_CHANGED": "CASE", "INVESTIGATION_RESUMED": "CASE", "RESOLUTION_POSTED": "CASE",
	"SECURITY_EVENT": "SECURITY",
}

// leftOut are the system-level types: no case, so no entry (noema#355 item 2).
var leftOut = map[string]bool{
	"POLICY_PUBLISHED": true, "PACK_LOADED": true, "KILL_SWITCH_CHANGED": true, "KEY_ROTATED": true, "CONFIG_CHANGED": true,
	"CIRCUIT_STATE_CHANGED": true, "OPS_ALERT_RAISED": true, "PROVIDER_FAILOVER": true, "MEMORY_OBSERVATION_RECORDED": true,
	"AUDIO_RETAINED": true, "CALLBACK_DELIVERY_ATTEMPTED": true, "CALLBACK_DELIVERED": true, "CALLBACK_FAILED": true,
}

// StepOf returns the neutral step of a Record event type. inCase is false for the system-level types the export leaves out;
// known is false for a type that has no row at all.
func StepOf(eventType string) (step string, inCase, known bool) {
	if s, ok := stepOfType[eventType]; ok {
		return s, true, true
	}
	if leftOut[eventType] {
		return "", false, true
	}
	return "", false, false
}

// personActs are the events that are a person's own act: their neutral actor is the principal's kind.
var personActs = map[string]bool{"INPUT_RECEIVED": true, "CLARIFICATION_ANSWERED": true, "CONFIRMATION_RECEIVED": true, "RESOLUTION_POSTED": true}

// actorOfEmitter maps the four powers and the model service to their neutral roles; every other emitter is the system.
var actorOfEmitter = map[string]string{"core-trust": "policy", "core-execution": "executor", "core-outcome": "verifier", "model-service": "model"}

// neutralActor is a pure function of the event type, the emitter and the principal (noema#355 item 4).
func neutralActor(eventType, writtenBy, principal string) string {
	if eventType == "INTENT_PROPOSED" {
		return "model"
	}
	if personActs[eventType] {
		kind, _, _ := strings.Cut(principal, ":")
		switch kind {
		case "customer", "operator":
			return kind
		case "approver":
			return "operator"
		}
		return "system"
	}
	if a, ok := actorOfEmitter[writtenBy]; ok {
		return a
	}
	return "system"
}

// dispositions are the decision set of the SoT. A DECISION_MADE with any other value is refused.
var dispositions = map[string]bool{"ALLOW": true, "CLARIFY": true, "CONFIRM": true, "WAIT": true, "ESCALATE": true, "REJECT": true, "UNKNOWN": true}
