package intake

import (
	"slices"

	"github.com/Noemai-org/noema/services/core/trust/policy"
)

// A306: T11 asks the policy about every capability of the authorized set, and the answers become the one T11 outcome. PERD 23.3 and 23.4 define the effect of one capability's
// rules (the class ladder, then REJECT > ESCALATE > CONFIRM > WAIT > PERMIT, and no matching PERMIT is REJECT NO_PERMITTING_RULE) and say nothing about how several
// capabilities combine. The reading below is the safer one (AGENT-20) and is not ratified: the most restrictive effect of the capabilities wins, in the order PERD 23.4 gives
// for the effects of one class; a capability that is not permitted by a rule is the default deny of that capability; a PERMIT stands only if every capability had one; the
// rules and the policy versions of every capability are recorded, each once.

// restrictionOrder is the order of PERD 23.4 within a winning class, used here across capabilities: the most restrictive effect first.
var restrictionOrder = []policy.Effect{policy.Reject, policy.Escalate, policy.Confirm, policy.Wait, policy.Permit}

// withDefaultDeny is one capability's answer after its own default deny (PERD 23.4, 23.3): a PERMIT that names no deciding rule, a REJECT that gives no reason code and a CONFIRM
// with no PERMIT matched are REJECT NO_PERMITTING_RULE, so that the rules of another capability can never make up for them. Rules, versions and PermitMatched stay as given.
func withDefaultDeny(r PolicyEvaluation) PolicyEvaluation {
	deny := (r.Effect == policy.Permit && len(r.Rules) == 0) || (r.Effect == policy.Reject && r.ReasonCode == "") || (r.Effect == policy.Confirm && !r.PermitMatched)
	if deny {
		r.Effect, r.ReasonCode, r.ReevaluateAt = policy.Reject, policy.NoPermittingRule, 0
	}
	return r
}

// combinePolicies makes the one PolicyEvaluation of the answers for the capabilities, given in the order of the sorted authorized set. An answer whose effect is none of the five
// is not an answer (errAnswer). The winning answer is the most restrictive one; of equal effects the first gives the reason code (the earliest re-evaluation for WAIT), so the
// result never depends on anything but the order of the sorted set (INV-21). The rules and the versions of all answers are kept, each once, in the order they first appear.
func combinePolicies(answers []PolicyEvaluation) (PolicyEvaluation, error) {
	if len(answers) == 0 {
		return PolicyEvaluation{}, errAnswer
	}
	var out PolicyEvaluation
	winner, winnerRank := -1, len(restrictionOrder)
	out.PermitMatched = true
	for i, raw := range answers {
		a := withDefaultDeny(raw)
		rank := slices.Index(restrictionOrder, a.Effect)
		if rank < 0 {
			return PolicyEvaluation{}, errAnswer
		}
		if rank < winnerRank || (rank == winnerRank && a.Effect == policy.Wait && a.ReevaluateAt < withDefaultDeny(answers[winner]).ReevaluateAt) {
			winner, winnerRank = i, rank
		}
		out.PermitMatched = out.PermitMatched && a.PermitMatched
		for _, rule := range a.Rules {
			if !slices.Contains(out.Rules, rule) {
				out.Rules = append(out.Rules, rule)
			}
		}
		for _, v := range a.Versions {
			if !slices.Contains(out.Versions, v) {
				out.Versions = append(out.Versions, v)
			}
		}
	}
	w := withDefaultDeny(answers[winner])
	out.Effect, out.ReasonCode, out.ReevaluateAt = w.Effect, w.ReasonCode, w.ReevaluateAt
	return out, nil
}
