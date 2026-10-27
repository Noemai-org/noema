package chain

import "sort"

// The closed sets of contracts/schemas/record-event.v3.json. A test (types_test.go) reads the schema and requires these
// sets to equal its enums, so the writer, the verifier and the contract cannot drift apart. An unknown member is rejected.
var (
	// emitters are the service identities that may append to the Record (PERD v1.17 section 32.1): the ten identities of the
	// section 41.1 table, and the seven section 34.1 logical services whose adapters carry a per-adapter identity (REQ-ENV-001).
	// fault-controller and fixture-loader (34.1) are benchmark-only and never reachable from the core, so they are not here.
	//
	// The contract lists 17 emitters; this writer accepts 16. The 17th is the benchmark runner's identity (section 41.1,
	// "benchmark-runner"): the core never appends to the Record as the benchmark runner, and the architecture lint (rule
	// benchmark-branch, SoT section 51) forbids that term in core code. The schema keeps it as the Founder decided; the writer
	// and the verifier refuse it. A test requires exactly that one difference (CONFLICT block of the pull request).
	emitters = setOf(
		"core-edge", "core-trust", "core-execution", "core-outcome", "core-record", "core-evidence", "core-workflows", "model-service", "admin",
		"mock-ledger", "mock-switch", "mock-destination", "mock-customer", "mock-policy", "mock-case", "mock-clock",
	)
	// principalKinds are the kinds a person's or administrator's principal may have (PERD v1.17 section 32.1). service is not one.
	principalKinds = setOf("customer", "operator", "approver", "auditor", "regulator", "admin")
	// eventTypes are the Record event types of PERD 32.2.
	eventTypes = setOf(
		"INPUT_RECEIVED", "INPUT_DEDUPLICATED", "SESSION_VALIDATED", "INTENT_PROPOSED", "INTENT_ADMITTED", "INTENT_REJECTED_AT_GATE", "LEXICON_APPLIED", "AUDIO_RETAINED",
		"CANDIDATES_RESOLVED", "CLARIFICATION_REQUESTED", "CLARIFICATION_ANSWERED", "CLARIFICATION_EXPIRED",
		"EVIDENCE_READ", "EVIDENCE_REFRESHED", "CONTEXT_SNAPSHOT_SEALED", "STATE_RECONCILED", "CONTRADICTION_FREEZE_SET", "CONTRADICTION_FREEZE_CLEARED", "SLA_EVALUATED", "INBOUND_EVENT_RECEIVED",
		"POLICY_EVALUATED", "DECISION_MADE", "CONFIRMATION_REQUESTED", "CONFIRMATION_RECEIVED", "CONFIRMATION_EXPIRED", "PLAN_CREATED", "GRANT_ISSUED", "GRANT_VOIDED",
		"REVALIDATION_PASSED", "REVALIDATION_FAILED", "GRANT_SPENT", "DISPATCH_ATTEMPTED", "RECEIPT", "CIRCUIT_STATE_CHANGED",
		"VERIFICATION_READ", "VERDICT", "WATCH_CREATED", "WATCH_CHECKED", "WATCH_CLOSED", "RECOVERY_REQUESTED", "CALLBACK_DELIVERY_ATTEMPTED", "CALLBACK_DELIVERED", "CALLBACK_FAILED",
		"EXPLANATION_RENDERED", "EXPLANATION_BLOCKED",
		"INVESTIGATION_STATE_CHANGED", "INVESTIGATION_RESUMED", "OPS_ALERT_RAISED", "RESOLUTION_POSTED",
		"POLICY_PUBLISHED", "PACK_LOADED", "KILL_SWITCH_CHANGED", "KEY_ROTATED", "CONFIG_CHANGED",
		"SECURITY_EVENT", "PROVIDER_FAILOVER", "MEMORY_OBSERVATION_RECORDED",
	)
	securityKinds = setOf("INJECTION_DETECTED", "PRINCIPAL_OVERRIDE_ATTEMPT", "SIGNATURE_INVALID", "CROSS_SCOPE_ATTEMPT", "REQUEST_ID_REUSED", "IDEMPOTENCY_COLLISION", "CHAIN_BREAK")
	// grantVoidReasons are the nine reasons PERD 27.1 and REQ-EXE-003 name for a void grant, kept distinct.
	grantVoidReasons = setOf("INTENT_SUPERSEDED", "STATE_CHANGED", "POLICY_SUPERSEDED", "SIGNATURE_INVALID", "EXPIRED", "BINDING_MISMATCH", "WRITES_DISABLED",
		"REVALIDATION_UNAVAILABLE", "REJECTED_STALE_VERSION")
	providerFailoverReasons = setOf("PROVIDER_ERROR", "TIMEOUT", "REFUSED_REPLY")
)

func setOf(members ...string) map[string]bool {
	m := make(map[string]bool, len(members))
	for _, s := range members {
		m[s] = true
	}
	return m
}

func sorted(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// Emitters lists the accepted values of written_by, sorted.
func Emitters() []string { return sorted(emitters) }

// PrincipalKinds lists the accepted kinds of a person's principal, sorted.
func PrincipalKinds() []string { return sorted(principalKinds) }

// GrantVoidReasons lists the accepted GRANT_VOIDED reasons, sorted.
func GrantVoidReasons() []string { return sorted(grantVoidReasons) }

// EventTypes lists the accepted Record event types, sorted.
func EventTypes() []string { return sorted(eventTypes) }
