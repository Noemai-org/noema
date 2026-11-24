package reconcile

import (
	"context"
	"errors"
	"sort"
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
	TenantID  string
	Target    string
	RulesetID string
	RuleID    string
	Evidence  []string
	Labels    []string
}

// NewFreezeSet builds the row for a contradictory result. Anything else is ErrNotContradictory: a waiting result, an unknown outcome or a converged state never freezes.
func NewFreezeSet(tenantID, target, rulesetID string, res Result) (FreezeSet, error) {
	if res.Classification != ContradictoryState {
		return FreezeSet{}, ErrNotContradictory
	}
	if tenantID == "" || target == "" || rulesetID == "" {
		return FreezeSet{}, ErrFreezeFacts
	}
	set := FreezeSet{TenantID: tenantID, Target: target, RulesetID: rulesetID, RuleID: res.RuleID,
		Evidence: append([]string(nil), res.Contributing...)}
	labels := map[string]bool{}
	for _, c := range res.Contradictions {
		labels[c.Label] = true
	}
	for l := range labels {
		set.Labels = append(set.Labels, l)
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

// NewFreezeCleared builds the clearing event. It needs a decision that Trust recorded: a changed classification is not one, and neither is a missing decision id.
func NewFreezeCleared(tenantID, target string, by ClearedBy, decisionID string) (FreezeLift, error) {
	if by != ClearedByResolution && by != ClearedByEscalation {
		return FreezeLift{}, ErrNotCleared
	}
	if decisionID == "" {
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

// FreezeReader is the Record's answer for a target's freeze; an error is read as FreezeUnknown by the caller.
type FreezeReader interface {
	RecordedFreeze(ctx context.Context, tenantID, target string) (RecordedFreeze, error)
}
