package verifier

import (
	"regexp"
	"time"
)

// packID is the contract's PackId (common.v1.json): namespaced, lower-case.
var packID = regexp.MustCompile(`^[a-z][a-z0-9]*(\.[a-z][a-z0-9_]*)+$`)

// validTriggers holds when every trigger is a PackId and no two are the same, as outcome-verdict.v1 requires of watch.triggers.
func validTriggers(triggers []string) bool {
	seen := make(map[string]bool, len(triggers))
	for _, t := range triggers {
		if !packID.MatchString(t) || seen[t] {
			return false
		}
		seen[t] = true
	}
	return true
}

// pendingExternal accepts the watch only if it can be kept: a first check and a deadline, both ahead of now and in order
// (INV-28). A PENDING_EXTERNAL verdict without a usable watch is never stored.
func (r *run) pendingExternal(predicate *bool) decision {
	// Times are stored to the microsecond, so they are checked at that precision: a time less than a microsecond ahead would be
	// stored as now, and the watch would be due the moment it is created.
	p := *r.req.Definition.Pending
	now := r.v.cfg.Clock.Now().UTC().Truncate(time.Microsecond)
	p.NextCheckAt, p.DeadlineAt = p.NextCheckAt.UTC().Truncate(time.Microsecond), p.DeadlineAt.UTC().Truncate(time.Microsecond)
	if !validTriggers(p.Triggers) {
		return unknown(ReasonWatchUnusable)
	}
	if p.DeadlineAt.IsZero() || !p.DeadlineAt.After(now) || !p.NextCheckAt.After(now) || p.NextCheckAt.After(p.DeadlineAt) {
		return unknown(ReasonWatchUnusable)
	}
	return decision{verdict: PendingExternal, reason: ReasonPendingCondition, predicate: predicate, watch: &p}
}
