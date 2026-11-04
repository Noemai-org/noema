package decide

import "errors"

// Errors returned by WithBasis. Callers compare with errors.Is.
var (
	// ErrBasisInvalid is returned when a basis has an empty identifier or a missing time, when it is attached to a decision
	// no constructor built, or when a decision already has one.
	ErrBasisInvalid = errors.New("decide: the basis is invalid")
	// ErrBasisIncomplete is returned when an ALLOW or a CONFIRM would carry no policy rule, no evidence snapshot or no
	// evidence freshness: such a decision rests on nothing and is refused (fail closed).
	ErrBasisIncomplete = errors.New("decide: an ALLOW or CONFIRM needs the policy rules and the evidence it rests on")
)

// PolicyRuleVersion names the policy version of one rule that took part in the decision (INV-09: a decision can use rules of
// two versions, for example an AT_ADMISSION rule and an AT_DECISION rule, and records which was which).
type PolicyRuleVersion struct {
	RuleID   string
	PolicyID string
	Version  string
}

// Freshness records the effective time of one evidence item the decision rested on (REQ-TRU-001 evidence_freshness[]).
type Freshness struct {
	EvidenceID  string
	EffectiveAt int64
}

// Basis is what a decision rests on (REQ-TRU-001): the pack version, the policy version of each rule that decided, the
// sealed evidence snapshot, the freshness of the evidence, and the time of the evaluation in Unix seconds. A refusal made
// before any evidence is read (T1 to T6) has no snapshot and no policy: it needs only the pack version and the time.
type Basis struct {
	PackVersion        string
	PolicyRules        []PolicyRuleVersion
	EvidenceSnapshotID string
	EvidenceFreshness  []Freshness
	EvaluatedAt        int64
}

func (b Basis) copied() Basis {
	b.PolicyRules = append([]PolicyRuleVersion(nil), b.PolicyRules...)
	b.EvidenceFreshness = append([]Freshness(nil), b.EvidenceFreshness...)
	return b
}

// valid checks the parts every basis needs.
func (b Basis) valid() bool {
	if b.PackVersion == "" || b.EvaluatedAt <= 0 {
		return false
	}
	for _, r := range b.PolicyRules {
		if r.RuleID == "" || r.PolicyID == "" || r.Version == "" {
			return false
		}
	}
	for _, f := range b.EvidenceFreshness {
		if f.EvidenceID == "" || f.EffectiveAt <= 0 {
			return false
		}
	}
	return true
}

// complete reports whether the basis names the policy and the evidence a decision rests on.
func (b Basis) complete() bool {
	return len(b.PolicyRules) > 0 && b.EvidenceSnapshotID != "" && len(b.EvidenceFreshness) > 0
}

// Basis returns a copy of the decision's basis; the zero Basis when none was attached.
func (d Decision) Basis() Basis { return d.basis.copied() }

// HasCompleteBasis reports whether the decision carries a basis that names its policy and its evidence. An ALLOW needs it:
// the evaluator refuses an ALLOW or CONFIRM without it.
func (d Decision) HasCompleteBasis() bool { return d.basis.valid() && d.basis.complete() }

// WithBasis returns the decision with its basis attached. It can be done once, and only to a decision a constructor built.
// An ALLOW or a CONFIRM needs a complete basis (REQ-TRU-001, DEC-2026-10-18); another outcome needs only the pack version and
// the time.
func (d Decision) WithBasis(b Basis) (Decision, error) {
	if d.outcome == "" || d.basis.PackVersion != "" || !b.valid() {
		return Decision{}, ErrBasisInvalid
	}
	if (d.outcome == Allow || d.outcome == Confirm) && !b.complete() {
		return Decision{}, ErrBasisIncomplete
	}
	d.basis = b.copied()
	return d, nil
}
