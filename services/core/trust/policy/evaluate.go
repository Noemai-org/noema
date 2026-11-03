package policy

import (
	"fmt"
	"sort"

	"github.com/Noemai-org/noema/services/core/internal/predicate"
)

// Set is the policy versions known to Trust, validated. It is immutable.
type Set struct{ versions []Version }

// NewSet validates the versions: each has a status, a policy's versions are unique, at most one is ACTIVE, and no two
// non-draft versions of one policy share an effective_from (the version in force at a time must be unambiguous).
func NewSet(vs ...Version) (Set, error) {
	seen := map[string]bool{}
	activeOf := map[string]bool{}
	from := map[string]bool{}
	for _, v := range vs {
		if v.status == "" {
			return Set{}, invalid(v.policyID + "@" + v.version + ": the version has no lifecycle (call WithMeta)")
		}
		id := v.policyID + "@" + v.version
		if seen[id] {
			return Set{}, invalid(id + " appears twice")
		}
		seen[id] = true
		if v.status == Active {
			if activeOf[v.policyID] {
				return Set{}, invalid(v.policyID + " has more than one ACTIVE version")
			}
			activeOf[v.policyID] = true
		}
		if v.status != Draft {
			key := v.policyID + "/" + fmt.Sprint(v.effectiveFrom)
			if from[key] {
				return Set{}, invalid(v.policyID + " has two versions with the same effective_from")
			}
			from[key] = true
		}
	}
	out := append([]Version(nil), vs...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].policyID != out[j].policyID {
			return out[i].policyID < out[j].policyID
		}
		return out[i].effectiveFrom < out[j].effectiveFrom
	})
	return Set{versions: out}, nil
}

// active is the ACTIVE version of a policy in force at now. A RETIRED version is never returned.
func (s Set) active(policyID string, now int64) (Version, bool) {
	for _, v := range s.versions {
		if v.policyID == policyID && v.status == Active && v.effectiveFrom <= now {
			return v, true
		}
	}
	return Version{}, false
}

// inForce is the non-draft version of a policy that was in force at the time at: the one with the latest effective_from
// not after it. It is how an AT_ADMISSION rule finds the version the intent was admitted under (SoT INV-09).
func (s Set) inForce(policyID string, at int64) (Version, bool) {
	var found Version
	ok := false
	for _, v := range s.versions {
		if v.policyID == policyID && v.status != Draft && v.effectiveFrom <= at {
			found, ok = v, true
		}
	}
	return found, ok
}

// Request is one evaluation. Now and AdmittedAt are Unix seconds passed by the caller (the engine reads no clock). Env holds
// the variables and evidence the conditions read; its Now is replaced by Request.Now.
type Request struct {
	Target     string
	Now        int64
	AdmittedAt int64
	Env        predicate.Env
}

// VersionRef names a policy version an evaluation used.
type VersionRef struct {
	PolicyID string
	Version  string
}

// Applied is one rule that matched, with the version it came from (INV-09).
type Applied struct {
	PolicyID       string
	Version        string
	RuleID         string
	Class          Class
	Effect         Effect
	EvaluationTime EvaluationTime
	ReasonCode     string
	Params         map[string]Param
}

// Result is the outcome of the policy for one target.
type Result struct {
	// Effect is the winning rule's effect, or Reject when no rule matched.
	Effect Effect
	// Reason is the winning rule's reason code, or NoPermittingRule.
	Reason string
	// Winner is the winning rule; nil on default deny.
	Winner *Applied
	// PermitMatched reports whether any PERMIT rule matched, whatever won.
	PermitMatched bool
	// Applied lists every matching rule, strongest first.
	Applied []Applied
	// Versions lists the policy versions used, oldest first: the ACTIVE one for AT_DECISION rules and the one in force at
	// admission for AT_ADMISSION rules.
	Versions []VersionRef
}

// Evaluate applies the precedence ladder of PERD §23.4 to the rules of one policy that apply to the target. AT_DECISION rules
// come from the version ACTIVE at Now, AT_ADMISSION rules from the version in force at AdmittedAt. Across classes the
// highest wins; within it REJECT beats ESCALATE beats CONFIRM beats WAIT beats PERMIT; a tie goes to the smallest rule_id
// (then version), so the result never depends on the order of the rules. No matching rule is REJECT NO_PERMITTING_RULE.
// A condition that cannot be evaluated returns ErrCondition and no result (INV-16).
func (s Set) Evaluate(policyID string, req Request) (Result, error) {
	env := req.Env
	env.Now = req.Now
	type candidate struct {
		v *Version
		r *rule
	}
	var cands []candidate
	var used []Version
	if v, ok := s.active(policyID, req.Now); ok {
		used = append(used, v)
		for i := range v.rules {
			if v.rules[i].evalTime == AtDecision {
				cands = append(cands, candidate{&v, &v.rules[i]})
			}
		}
	}
	if v, ok := s.inForce(policyID, req.AdmittedAt); ok {
		if len(used) == 0 || used[0].version != v.version {
			used = append(used, v)
		}
		for i := range v.rules {
			if v.rules[i].evalTime == AtAdmission {
				cands = append(cands, candidate{&v, &v.rules[i]})
			}
		}
	}
	var res Result
	for _, c := range cands {
		if !c.r.targets[req.Target] {
			continue
		}
		hit, err := c.r.cond.EvalBool(env)
		if err != nil {
			return Result{}, fmt.Errorf("%w: rule %s of %s@%s: %w", ErrCondition, c.r.id, c.v.policyID, c.v.version, err)
		}
		if !hit {
			continue
		}
		params := make(map[string]Param, len(c.r.params))
		for k, p := range c.r.params {
			params[k] = p
		}
		res.Applied = append(res.Applied, Applied{PolicyID: c.v.policyID, Version: c.v.version, RuleID: c.r.id, Class: c.r.class,
			Effect: c.r.effect, EvaluationTime: c.r.evalTime, ReasonCode: c.r.reason, Params: params})
		res.PermitMatched = res.PermitMatched || c.r.effect == Permit
	}
	sort.SliceStable(res.Applied, func(i, j int) bool { return stronger(res.Applied[i], res.Applied[j]) })
	sort.SliceStable(used, func(i, j int) bool { return used[i].effectiveFrom < used[j].effectiveFrom })
	for _, v := range used {
		res.Versions = append(res.Versions, VersionRef{PolicyID: v.policyID, Version: v.version})
	}
	if len(res.Applied) == 0 {
		res.Effect, res.Reason = Reject, NoPermittingRule
		return res, nil
	}
	w := res.Applied[0]
	res.Winner, res.Effect, res.Reason = &w, w.Effect, w.ReasonCode
	return res, nil
}

// stronger orders two matching rules: class first, then effect, then rule_id, then version.
func stronger(a, b Applied) bool {
	if ca, cb := rank(classOrder(), a.Class), rank(classOrder(), b.Class); ca != cb {
		return ca < cb
	}
	if ea, eb := rank(effectOrder(), a.Effect), rank(effectOrder(), b.Effect); ea != eb {
		return ea < eb
	}
	if a.RuleID != b.RuleID {
		return a.RuleID < b.RuleID
	}
	return a.Version < b.Version
}
