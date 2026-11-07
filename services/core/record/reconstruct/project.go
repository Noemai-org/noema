package reconstruct

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// The twelve sections, in the order of REQ-AUD-003 (contracts/schemas/reconstruction.v3.json).
const (
	secWho        = "WHO"
	secRequest    = "REQUEST"
	secAmbiguity  = "AMBIGUITY"
	secResolution = "RESOLUTION"
	secObserved   = "OBSERVED_STATE"
	secPolicy     = "POLICY"
	secAuthority  = "AUTHORITY"
	secDecision   = "DECISION"
	secAction     = "ACTION"
	secDownstream = "DOWNSTREAM_RESULT"
	secVerify     = "VERIFICATION"
	secExplain    = "EXPLANATION"
)

var sectionOrder = []string{secWho, secRequest, secAmbiguity, secResolution, secObserved, secPolicy, secAuthority, secDecision, secAction,
	secDownstream, secVerify, secExplain}

// The three sources of PERD 32.2: an evidence item, a versioned rule, an authenticated principal's action. A fact with
// none of them is not served ("the model said so is never provenance").
const (
	basisEvidence  = "EVIDENCE"
	basisRule      = "RULE"
	basisPrincipal = "PRINCIPAL_ACTION"
)

// Event types with special projection. The rest are plain table rows.
const (
	evReceipt     = "RECEIPT"
	evDispatch    = "DISPATCH_ATTEMPTED"
	evVerdict     = "VERDICT"
	evExplanation = "EXPLANATION_RENDERED"
	evClarify     = "CLARIFICATION_REQUESTED"
	evGrantIssued = "GRANT_ISSUED"
	evGrantSpent  = "GRANT_SPENT"
	evGrantVoided = "GRANT_VOIDED"
)

// projection says where an event type goes and which payload keys it may contribute as facts. A key missing from the
// payload is left out; a key not listed is never served. Event types not in the table (INTENT_PROPOSED, which is model
// output, lifecycle, control-plane, security and provider events) appear in the timeline only.
// The payload shapes are fixed by REC-04; until then this table is the one place that names them.
type projection struct {
	section  string
	basis    string
	keys     []string
	required []string
}

var (
	observedKeys   = []string{"classification", "fact_status", "source"}
	authorityKeys  = []string{"grant_id", "plan_id", "confirmation_id", "capability_id", "attempt", "reason", "policy_version"}
	downstreamKeys = []string{"capability_id", "fact_status", "source", "watch_id", "recovery_request_id", "delivery_status"}
)

var projections = map[string]projection{
	"SESSION_VALIDATED":            {secWho, basisPrincipal, []string{"channel", "locale"}, nil},
	"INPUT_RECEIVED":               {secRequest, basisPrincipal, []string{"channel", "locale", "request_id"}, nil},
	"INTENT_ADMITTED":              {secRequest, basisPrincipal, []string{"intent_node", "intent_id"}, nil},
	"INTENT_REJECTED_AT_GATE":      {secRequest, basisRule, []string{"reason_codes"}, nil},
	evClarify:                      {secAmbiguity, basisRule, []string{"clarification_id", "reason_codes"}, nil},
	"CLARIFICATION_ANSWERED":       {secAmbiguity, basisPrincipal, []string{"clarification_id"}, nil},
	"CLARIFICATION_EXPIRED":        {secAmbiguity, basisRule, []string{"clarification_id"}, nil},
	"CANDIDATES_RESOLVED":          {secResolution, basisRule, []string{"resolved_target", "candidate_count", "margin"}, nil},
	"EVIDENCE_READ":                {secObserved, basisEvidence, observedKeys, nil},
	"EVIDENCE_REFRESHED":           {secObserved, basisEvidence, observedKeys, nil},
	"CONTEXT_SNAPSHOT_SEALED":      {secObserved, basisEvidence, observedKeys, nil},
	"STATE_RECONCILED":             {secObserved, basisEvidence, observedKeys, nil},
	"CONTRADICTION_FREEZE_SET":     {secObserved, basisEvidence, observedKeys, nil},
	"CONTRADICTION_FREEZE_CLEARED": {secObserved, basisEvidence, observedKeys, nil},
	"SLA_EVALUATED":                {secObserved, basisEvidence, observedKeys, nil},
	"INBOUND_EVENT_RECEIVED":       {secObserved, basisEvidence, observedKeys, nil},
	"POLICY_EVALUATED":             {secPolicy, basisRule, []string{"policy_id", "policy_version", "reason_codes"}, nil},
	"CONFIRMATION_REQUESTED":       {secAuthority, basisRule, authorityKeys, nil},
	"CONFIRMATION_RECEIVED":        {secAuthority, basisPrincipal, []string{"confirmation_id", "accepted"}, nil},
	"CONFIRMATION_EXPIRED":         {secAuthority, basisRule, authorityKeys, nil},
	"PLAN_CREATED":                 {secAuthority, basisRule, authorityKeys, nil},
	evGrantIssued:                  {secAuthority, basisRule, authorityKeys, nil},
	evGrantSpent:                   {secAuthority, basisRule, authorityKeys, nil},
	evGrantVoided:                  {secAuthority, basisRule, authorityKeys, nil},
	"REVALIDATION_PASSED":          {secAuthority, basisRule, authorityKeys, nil},
	"REVALIDATION_FAILED":          {secAuthority, basisRule, authorityKeys, nil},
	"DECISION_MADE":                {secDecision, basisRule, []string{"decision_outcome", "reason_codes", "decision_id"}, []string{"decision_outcome"}},
	evDispatch:                     {secAction, basisRule, []string{"attempt"}, []string{"capability_id", "execution_state", "grant_state"}},
	evReceipt:                      {secAction, basisRule, []string{"execution_state", "attempt", "receipt_id"}, []string{"capability_id", "execution_state", "grant_state"}},
	"VERIFICATION_READ":            {secDownstream, basisEvidence, downstreamKeys, nil},
	"WATCH_CREATED":                {secDownstream, basisRule, downstreamKeys, nil},
	"WATCH_CHECKED":                {secDownstream, basisEvidence, downstreamKeys, nil},
	"WATCH_CLOSED":                 {secDownstream, basisRule, downstreamKeys, nil},
	"RECOVERY_REQUESTED":           {secDownstream, basisRule, downstreamKeys, nil},
	"CALLBACK_DELIVERY_ATTEMPTED":  {secDownstream, basisRule, downstreamKeys, nil},
	"CALLBACK_DELIVERED":           {secDownstream, basisRule, downstreamKeys, nil},
	"CALLBACK_FAILED":              {secDownstream, basisRule, downstreamKeys, nil},
	"RESOLUTION_POSTED":            {secDownstream, basisRule, downstreamKeys, nil},
	"VERDICT":                      {secVerify, basisEvidence, []string{"capability_id", "verdict", "verdict_id"}, []string{"capability_id", "verdict"}},
	evExplanation:                  {secExplain, basisRule, []string{"fact_id", "text", "bound_fact_status", "disclosed_refs"}, []string{"text", "bound_fact_status"}},
}

// grant_state values (reconstruction.v3.json, ACTION facts).
const (
	grantValid     = "VALID"
	grantMissing   = "MISSING"
	grantExpired   = "EXPIRED"
	grantSpent     = "SPENT"
	grantUnmatched = "UNMATCHED"
)

// Limits and patterns of reconstruction.v3.json and common.v1.json. A fact outside them is an error, never served.
const (
	maxSafeInt      = 1<<53 - 1
	maxFactString   = 200
	maxFactList     = 50
	maxClaimText    = 300
	maxCandidates   = 20
	maxTimelineText = 300
)

var (
	factKeyRe   = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	packIDRe    = regexp.MustCompile(`^[a-z][a-z0-9]*(\.[a-z][a-z0-9_]*)+$`)
	resourceRe  = regexp.MustCompile(`^[a-z][a-z0-9]*\.[a-z][a-z0-9_]*:[A-Za-z0-9_.-]{1,64}$`)
	upperCodeRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
	objectRefRe = regexp.MustCompile(`^[a-z]{2,6}_[0-9A-Za-z_]{2,64}$`)
	longDigits  = regexp.MustCompile(`[0-9]{5,}`)
)

// keepDigits is how many digits of a long run a served fact keeps (AGENTS.md section 7: mask to the last four).
const keepDigits = 4

// mask keeps only the last four digits of every run of five or more (REQ-AUD-003 "Masked"). It applies to free text
// and plain scalar facts; references, IDs and enum values are checked by their contract pattern instead and served
// as written, because the reader matches on them. Masking is idempotent, so text that Explanation already masked
// is unchanged.
func mask(s string) string {
	return longDigits.ReplaceAllStringFunc(s, func(run string) string {
		return strings.Repeat("*", len(run)-keepDigits) + run[len(run)-keepDigits:]
	})
}

// errProjection marks a Record object that cannot be served under the contract. The endpoint refuses rather than serve
// a partial or invalid narrative.
type errProjection struct{ detail string }

func (e *errProjection) Error() string { return "reconstruct: cannot project the Record: " + e.detail }

func projErr(format string, a ...any) error { return &errProjection{fmt.Sprintf(format, a...)} }

// fact checks one value against the contract and returns it in the form it is served.
func fact(key string, v any) (any, error) {
	if !factKeyRe.MatchString(key) {
		return nil, projErr("fact key %q is not snake_case", key)
	}
	switch key {
	case "decision_outcome":
		return enumFact(key, v, decisionOutcomes)
	case "execution_state":
		return enumFact(key, v, executionStates)
	case "verdict":
		return enumFact(key, v, verdicts)
	case "bound_fact_status":
		if v == nil {
			return nil, nil
		}
		return enumFact(key, v, factStatuses)
	case "capability_id":
		return patternFact(key, v, packIDRe)
	case "resolved_target":
		if v == nil {
			return nil, nil
		}
		return patternFact(key, v, resourceRe)
	case "classification":
		return patternFact(key, v, upperCodeRe)
	case "candidate_count":
		return intFact(key, v, 0, maxCandidates)
	case "attempt":
		return intFact(key, v, 1, maxSafeInt)
	case "text":
		s, ok := v.(string)
		if !ok || s == "" || len([]rune(s)) > maxClaimText {
			return nil, projErr("%s is not a string of 1 to %d characters", key, maxClaimText)
		}
		return mask(s), nil
	case "disclosed_refs":
		list, err := listFact(key, v)
		if err != nil {
			return nil, err
		}
		for _, r := range list {
			if !resourceRe.MatchString(r) {
				return nil, projErr("%s holds %q, which is not a resource reference", key, r)
			}
		}
		return list, nil
	}
	switch x := v.(type) {
	case nil, bool:
		return x, nil
	case string:
		if len([]rune(x)) > maxFactString {
			return nil, projErr("%s is longer than %d characters", key, maxFactString)
		}
		if strings.HasSuffix(key, "_id") {
			return x, nil // an ID cited by other facts; never a personal number (PERD 32.3 prefixes)
		}
		return mask(x), nil
	case json.Number:
		return intFact(key, x, 0, maxSafeInt)
	case []any:
		return listFact(key, x)
	}
	return nil, projErr("%s is not a scalar or a list of strings", key)
}

func enumFact(key string, v any, set map[string]bool) (any, error) {
	s, ok := v.(string)
	if !ok || !set[s] {
		return nil, projErr("%s is not a value of its contract enum", key)
	}
	return s, nil
}

func patternFact(key string, v any, re *regexp.Regexp) (any, error) {
	s, ok := v.(string)
	if !ok || !re.MatchString(s) {
		return nil, projErr("%s does not match its contract pattern", key)
	}
	return s, nil
}

func intFact(key string, v any, lo, hi int64) (any, error) {
	n, ok := v.(json.Number)
	if !ok {
		return nil, projErr("%s is not an integer", key)
	}
	i, err := n.Int64()
	if err != nil || i < lo || i > hi {
		return nil, projErr("%s is not an integer from %d to %d", key, lo, hi)
	}
	return i, nil
}

func listFact(key string, v any) ([]string, error) {
	raw, ok := v.([]any)
	if !ok || len(raw) > maxFactList {
		return nil, projErr("%s is not a list of at most %d strings", key, maxFactList)
	}
	out := make([]string, 0, len(raw))
	for _, x := range raw {
		s, ok := x.(string)
		if !ok || len([]rune(s)) > maxFactString {
			return nil, projErr("%s holds an item that is not a string of at most %d characters", key, maxFactString)
		}
		out = append(out, s)
	}
	return out, nil
}

// facts projects the listed payload keys and checks the required ones are present.
func facts(p projection, payload map[string]any, extra map[string]any) (map[string]any, error) {
	out := map[string]any{}
	for _, k := range p.keys {
		v, ok := payload[k]
		if !ok {
			continue
		}
		f, err := fact(k, v)
		if err != nil {
			return nil, err
		}
		out[k] = f
	}
	for k, v := range extra {
		f, err := fact(k, v)
		if err != nil {
			return nil, err
		}
		out[k] = f
	}
	for _, k := range p.required {
		if _, ok := out[k]; !ok {
			return nil, projErr("the required fact %s is missing", k)
		}
	}
	return out, nil
}

// stored is one verified event of the correlation, decoded.
type stored struct {
	id, eventType, objectRef, writtenBy, kind string
	at                                        string
	body                                      map[string]any
	payload                                   map[string]any
}

// item is one reconstruction item (reconstruction.v3.json $defs/Item).
type item struct {
	section string
	value   map[string]any
}

// project turns the correlation's events, in chain order, into section items.
func project(events []stored) ([]item, error) {
	var out []item
	for i, ev := range events {
		p, ok := projections[ev.eventType]
		if !ok {
			continue
		}
		if !objectRefRe.MatchString(ev.objectRef) {
			return nil, projErr("event %s has an object_ref outside the contract pattern", ev.id)
		}
		mk := func(f map[string]any) item {
			return item{p.section, map[string]any{"record_event_id": ev.id, "object_ref": ev.objectRef, "basis": p.basis, "facts": f}}
		}
		switch ev.eventType {
		case evExplanation:
			claims, ok := ev.payload["claims"].([]any)
			if !ok {
				return nil, projErr("event %s has no list of claims", ev.id)
			}
			for _, c := range claims {
				claim, ok := c.(map[string]any)
				if !ok {
					return nil, projErr("event %s holds a claim that is not an object", ev.id)
				}
				f, err := facts(p, claim, nil)
				if err != nil {
					return nil, err
				}
				out = append(out, mk(f))
			}
			continue
		case evDispatch:
			if receiptFollows(events, i) {
				continue // the receipt of this attempt is its ACTION item
			}
			capability := capabilityOf(events, i)
			extra := map[string]any{"capability_id": capability, "execution_state": "DISPATCHED", "grant_state": grantState(events, i)}
			f, err := facts(p, ev.payload, extra)
			if err != nil {
				return nil, fmt.Errorf("dispatch %s: %w", ev.id, err)
			}
			out = append(out, mk(f))
			continue
		case evReceipt:
			capability := capabilityOf(events, i)
			extra := map[string]any{"capability_id": capability, "grant_state": grantState(events, i)}
			f, err := facts(p, ev.payload, extra)
			if err != nil {
				return nil, fmt.Errorf("receipt %s: %w", ev.id, err)
			}
			out = append(out, mk(f))
			continue
		case evVerdict:
			payload := ev.payload
			if doc, ok := ev.payload["verdict"].(map[string]any); ok {
				// Outcome's event carries the signed verdict document; its verdict and id are the facts. The capability is the one of the receipt it verified.
				payload = map[string]any{"verdict": doc["verdict"], "verdict_id": doc["verdict_id"], "capability_id": capabilityOfReceipt(events, doc["receipt_id"])}
			}
			f, err := facts(p, payload, nil)
			if err != nil {
				return nil, fmt.Errorf("verdict %s: %w", ev.id, err)
			}
			out = append(out, mk(f))
			continue
		case evClarify:
			f, err := facts(p, ev.payload, map[string]any{"clarification_asked": true})
			if err != nil {
				return nil, err
			}
			out = append(out, mk(f))
			continue
		}
		f, err := facts(p, ev.payload, nil)
		if err != nil {
			return nil, fmt.Errorf("event %s: %w", ev.id, err)
		}
		out = append(out, mk(f))
	}
	return out, nil
}

// capabilityOf is the pack capability an action was dispatched under. The dispatch records it (capability_id); logical_action is the grant's
// capability, a pack ID, since the fix of G1 (noema#243), and is only the fallback for a Record written before the dispatch carried the capability. The event at i is a
// dispatch or a receipt of one attempt of one grant.
func capabilityOf(events []stored, i int) any {
	ev := events[i]
	grant, _ := ev.payload["grant_id"].(string)
	if grant == "" {
		grant = ev.objectRef
	}
	attempt, _ := ev.payload["attempt"].(json.Number)
	for j := range events {
		d := events[j]
		if d.eventType != evDispatch || d.objectRef != grant {
			continue
		}
		if a, _ := d.payload["attempt"].(json.Number); a == attempt {
			if c, ok := d.payload["capability_id"]; ok {
				return c
			}
		}
	}
	return ev.payload["logical_action"]
}

// capabilityOfReceipt is the capability of the receipt with the given receipt_id, or nil when the Record holds no such receipt.
func capabilityOfReceipt(events []stored, receiptID any) any {
	for i, ev := range events {
		if ev.eventType == evReceipt && ev.payload["receipt_id"] == receiptID {
			return capabilityOf(events, i)
		}
	}
	return nil
}

// receiptFollows reports whether the dispatch at i has a RECEIPT of the same grant and attempt after it.
func receiptFollows(events []stored, i int) bool {
	d := events[i]
	attempt, _ := d.payload["attempt"].(json.Number)
	for j := i + 1; j < len(events); j++ {
		r := events[j]
		if r.eventType != evReceipt || r.objectRef != d.objectRef {
			continue
		}
		if a, _ := r.payload["attempt"].(json.Number); a == attempt {
			return true
		}
	}
	return false
}

// grantState derives the grant's state at the moment of the spend behind the receipt at index i, from the Record alone
// (reconstruction.v3.json, ACTION grant_state): VALID only when the grant was issued in this correlation, spent once,
// for this attempt, before it expired, for this capability, and not voided first. Any other value on a dispatch is a
// breach of INV-03 that the reconstruction shows rather than hides.
func grantState(events []stored, i int) string {
	rc := events[i]
	grantID, _ := rc.payload["grant_id"].(string)
	if grantID == "" {
		grantID = rc.objectRef // a dispatch names its grant by its object_ref
	}
	attempt, _ := rc.payload["attempt"].(json.Number)
	var issued *stored
	spends, spendIdx := 0, -1
	voidedBeforeSpend := false
	for j := 0; j < i; j++ {
		ev := events[j]
		if ev.objectRef != grantID {
			continue
		}
		switch ev.eventType {
		case evGrantIssued:
			if issued == nil {
				issued = &events[j]
			}
		case evGrantSpent:
			spends++
			if a, _ := ev.payload["attempt"].(json.Number); spendIdx < 0 && a == attempt {
				spendIdx = j
			}
		case evGrantVoided:
			if spendIdx < 0 {
				voidedBeforeSpend = true
			}
		}
	}
	switch {
	case grantID == "" || issued == nil:
		return grantMissing
	case spendIdx < 0 || voidedBeforeSpend:
		return grantUnmatched
	case spends > 1:
		return grantSpent
	}
	if exp, ok := issued.payload["expires_at"].(string); ok {
		e, err1 := time.Parse(time.RFC3339Nano, exp)
		s, err2 := time.Parse(time.RFC3339Nano, events[spendIdx].at)
		if err1 != nil || err2 != nil || !s.Before(e) {
			return grantExpired
		}
	}
	if c, ok := issued.payload["capability_id"].(string); ok && c != capabilityOf(events, i) {
		return grantUnmatched
	}
	return grantValid
}

// timelineText is the rendered line for one event: its type in words and its emitter. It is built from the Record only
// and names IDs, never personal data.
func timelineText(ev stored) string {
	words := strings.ToLower(strings.ReplaceAll(ev.eventType, "_", " "))
	text := strings.ToUpper(words[:1]) + words[1:] + " (" + ev.writtenBy + ")"
	if ev.kind != "" {
		text += ": " + ev.kind
	}
	if r := []rune(text); len(r) > maxTimelineText {
		text = string(r[:maxTimelineText])
	}
	return text
}
