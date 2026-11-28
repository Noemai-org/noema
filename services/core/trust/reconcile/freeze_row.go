package reconcile

import (
	"context"
	"errors"
	"sort"
	"strings"
	"unicode"
)

// The freeze row (REQ-RES-002, INV-20): a contradiction is Recorded as CONTRADICTION_FREEZE_SET, and the freeze is lifted only by a Trust-recorded resolution or escalation, recorded as
// CONTRADICTION_FREEZE_CLEARED. This file holds the facts of those two events and the effective answer. It appends nothing: the Record is written by the Recorder (a consumer-side port below);
// this package stays pure.

// ErrNotContradictory is returned when a freeze row is asked for a result that is not CONTRADICTORY_STATE.
var ErrNotContradictory = errors.New("reconcile: only a contradiction makes a freeze row")

// ErrNotCleared is returned when a clearing event is asked for without a recorded resolution or escalation decision.
var ErrNotCleared = errors.New("reconcile: a freeze is cleared only by a recorded resolution or escalation decision")

// ErrFreezeFacts is returned when a freeze row or its clearing lacks the tenant, the target or the rule set it must name.
var ErrFreezeFacts = errors.New("reconcile: a freeze row names its tenant, target and rule set")

// FreezeSet is the facts of CONTRADICTION_FREEZE_SET: which target, which rule set and rule, which evidence and which matrix pairs disagreed. Lists are sorted, so the same result gives the same
// row.
type FreezeSet struct {
	TenantID string
	// CorrelationID is the investigation the event belongs to, set by the Recorder's caller (the Record's events carry one); NewFreezeSet leaves it empty.
	CorrelationID string
	Target        string
	RulesetID     string
	RuleID        string
	Evidence      []string
	Labels        []string
}

// NewFreezeSet builds the row for a contradictory result. Anything else is ErrNotContradictory: a waiting result, an unknown outcome or a converged state never freezes. The row is built from
// the freezing set alone (Result.Contradictions): a pair still inside its window (Result.InWindow) is in neither Labels nor Evidence. Evidence is the union of the items that disagreed (both
// ends of each freezing pair, whatever the matched rule uses) and the evidence the matched rule used.
func NewFreezeSet(tenantID, target, rulesetID string, res Result) (FreezeSet, error) {
	if res.Classification != ContradictoryState {
		return FreezeSet{}, ErrNotContradictory
	}
	if tenantID == "" || target == "" || rulesetID == "" {
		return FreezeSet{}, ErrFreezeFacts
	}
	set := FreezeSet{TenantID: tenantID, Target: target, RulesetID: rulesetID, RuleID: res.RuleID}
	labels, evidence := map[string]bool{}, map[string]bool{}
	for _, c := range res.Contradictions {
		labels[c.Label] = true
		evidence[c.A], evidence[c.B] = true, true
	}
	for _, id := range res.Contributing {
		evidence[id] = true
	}
	delete(evidence, "")
	for l := range labels {
		set.Labels = append(set.Labels, l)
	}
	for id := range evidence {
		set.Evidence = append(set.Evidence, id)
	}
	sort.Strings(set.Evidence)
	sort.Strings(set.Labels)
	return set, nil
}

// ClearedBy says which recorded Trust decision lifted a freeze.
type ClearedBy string

// The two decisions that lift a freeze (REQ-RES-002).
const (
	ClearedByResolution ClearedBy = "RESOLUTION"
	ClearedByEscalation ClearedBy = "ESCALATION"
)

// FreezeLift is the facts of CONTRADICTION_FREEZE_CLEARED: the target, who lifted it and the Decision Record that did.
type FreezeLift struct {
	TenantID   string
	Target     string
	By         ClearedBy
	DecisionID string
}

// decisionPrefix is the prefix of a decision id (`<prefix>_<ULID>`, PERD 32.3).
const decisionPrefix = "dec_"

// decisionIDShape is the cheap check on a decision id: the dec_ prefix, something after it and no control character or space. That the decision exists, is the tenant's and names the target
// is the Recorder's to check against the Record.
func decisionIDShape(id string) bool {
	if !strings.HasPrefix(id, decisionPrefix) || len(id) == len(decisionPrefix) {
		return false
	}
	return strings.IndexFunc(id, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }) < 0
}

// NewFreezeCleared builds the clearing event. It needs a decision that Trust recorded: a changed classification is not one, and neither is a missing or malformed decision id.
func NewFreezeCleared(tenantID, target string, by ClearedBy, decisionID string) (FreezeLift, error) {
	if by != ClearedByResolution && by != ClearedByEscalation {
		return FreezeLift{}, ErrNotCleared
	}
	if !decisionIDShape(decisionID) {
		return FreezeLift{}, ErrNotCleared
	}
	if tenantID == "" || target == "" {
		return FreezeLift{}, ErrFreezeFacts
	}
	return FreezeLift{TenantID: tenantID, Target: target, By: by, DecisionID: decisionID}, nil
}

// RecordedFreeze is what the Record says about a target's freeze. The zero value is FreezeUnknown: a reader that could not answer must not read as "no freeze".
type RecordedFreeze int

// The recorded states. FreezeNone: no freeze row ever; FreezeRecorded: a SET with no later CLEARED; FreezeCleared: the latest row is a CLEARED.
const (
	FreezeUnknown RecordedFreeze = iota
	FreezeNone
	FreezeRecorded
	FreezeCleared
)

// EffectiveFrozen is whether the target is frozen now: the derived freeze (the classification is CONTRADICTORY_STATE) or a recorded freeze never cleared. A changed classification does not lift a
// recorded freeze (the safer reading, A642); an unknown or out-of-range recorded state is frozen (fail closed). A cleared freeze does not mask a new contradiction.
func EffectiveFrozen(derived bool, rec RecordedFreeze) bool {
	switch rec {
	case FreezeNone, FreezeCleared:
		return derived
	default: // FreezeRecorded, FreezeUnknown and any value out of range
		return true
	}
}

// FreezeRecorder is the Recorder's side of the freeze row, a consumer-side port: it appends CONTRADICTION_FREEZE_SET and CONTRADICTION_FREEZE_CLEARED, Record first, in the same transaction as the
// decision that caused them. Implementing it is the Recorder's (trust/recorder), not this package's.
type FreezeRecorder interface {
	RecordFreezeSet(ctx context.Context, s FreezeSet) error
	RecordFreezeCleared(ctx context.Context, c FreezeLift) error
}

// FreezeReader is the Record's answer for a target's freeze. Callers use ReadFreeze, never the port directly, so that an error cannot be read as "no freeze".
type FreezeReader interface {
	RecordedFreeze(ctx context.Context, tenantID, target string) (RecordedFreeze, error)
}

// ReadFreeze asks the reader and returns FreezeUnknown on any error (even if the reader returned a value with it), on a missing reader and on a value out of range. An adapter calls only this
// (INV-20: doubt stays frozen).
func ReadFreeze(ctx context.Context, r FreezeReader, tenantID, target string) RecordedFreeze {
	if r == nil {
		return FreezeUnknown
	}
	rec, err := r.RecordedFreeze(ctx, tenantID, target)
	if err != nil {
		return FreezeUnknown
	}
	switch rec {
	case FreezeNone, FreezeRecorded, FreezeCleared:
		return rec
	}
	return FreezeUnknown
}
