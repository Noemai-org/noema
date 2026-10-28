package chain

import (
	"bytes"
	_ "embed" // actors.json
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
)

// actorsJSON is the one machine-readable source of the emitter and principal-kind sets (PERD v1.17 section 32.1). The writer and the
// verifier derive their sets from it and keep no list of their own; types_test.go and tools/contracts/test_record_event_v3.py fail
// when contracts/schemas/record-event.v3.json lists anything else.
//
//go:embed actors.json
var actorsJSON []byte

// actorTable is actors.json. An emitter with the scope RUNTIME is one the core may append as; the contract lists every emitter, and
// the core refuses the ones with any other scope (the benchmark runner's identity: the core never writes the Record as it).
type actorTable struct {
	Emitters []struct {
		Name    string `json:"name"`
		Section string `json:"section"`
		Scope   string `json:"scope"`
		Note    string `json:"note"`
	} `json:"emitters"`
	NotEmitters []struct {
		Name    string `json:"name"`
		Section string `json:"section"`
		Reason  string `json:"reason"`
	} `json:"not_emitters"`
	PrincipalKinds []struct {
		Name    string `json:"name"`
		Section string `json:"section"`
		Note    string `json:"note"`
	} `json:"principal_kinds"`
	Description string `json:"description"`
}

const scopeRuntime = "RUNTIME"

var nameRe = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// The closed sets of contracts/schemas/record-event.v3.json. A test (types_test.go) reads the schema and requires these
// sets to equal its enums, so the writer, the verifier and the contract cannot drift apart. An unknown member is rejected.
var (
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

// The sets are derived from actors.json when the package loads. A table that is malformed, repeats a name, names an emitter that is also
// a non-emitter, or leaves a set empty stops the program at start-up: the sets are static data and a wrong one must never run.
var (
	emitters       map[string]bool // identities the core may append as: scope RUNTIME
	contractSet    map[string]bool // every identity the contract lists, whatever the scope
	principalKinds map[string]bool
)

func init() {
	var t actorTable
	dec := json.NewDecoder(bytes.NewReader(actorsJSON))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&t); err != nil {
		panic(fmt.Sprintf("%sactors.json: %v", errPrefix, err))
	}
	emitters, contractSet, principalKinds = map[string]bool{}, map[string]bool{}, map[string]bool{}
	seen := map[string]bool{}
	for _, e := range t.Emitters {
		if !nameRe.MatchString(e.Name) || e.Section == "" || e.Scope == "" || seen[e.Name] {
			panic(fmt.Sprintf("%sactors.json: emitter %q is malformed or repeated", errPrefix, e.Name))
		}
		seen[e.Name] = true
		contractSet[e.Name] = true
		if e.Scope == scopeRuntime {
			emitters[e.Name] = true
		}
	}
	for _, n := range t.NotEmitters {
		if !nameRe.MatchString(n.Name) || seen[n.Name] {
			panic(fmt.Sprintf("%sactors.json: non-emitter %q is malformed or also an emitter", errPrefix, n.Name))
		}
		seen[n.Name] = true
	}
	kinds := map[string]bool{}
	for _, k := range t.PrincipalKinds {
		if !nameRe.MatchString(k.Name) || kinds[k.Name] {
			panic(fmt.Sprintf("%sactors.json: principal kind %q is malformed or repeated", errPrefix, k.Name))
		}
		kinds[k.Name] = true
		principalKinds[k.Name] = true
	}
	if len(emitters) == 0 || len(principalKinds) == 0 {
		panic(errPrefix + "actors.json: an empty set")
	}
}

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

// Emitters lists the identities the core accepts as written_by, sorted: the table's emitters with the scope RUNTIME.
func Emitters() []string { return sorted(emitters) }

// ContractEmitters lists every identity the contract (record-event.v3.json) lists as written_by, sorted. The core accepts the
// ones in Emitters; the difference is what the table marks as not written by the core.
func ContractEmitters() []string { return sorted(contractSet) }

// PrincipalKinds lists the accepted kinds of a person's principal, sorted.
func PrincipalKinds() []string { return sorted(principalKinds) }

// GrantVoidReasons lists the accepted GRANT_VOIDED reasons, sorted.
func GrantVoidReasons() []string { return sorted(grantVoidReasons) }

// EventTypes lists the accepted Record event types, sorted.
func EventTypes() []string { return sorted(eventTypes) }
