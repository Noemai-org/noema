package recovery

import (
	"errors"
	"sort"

	"github.com/Noemai-org/noema/services/core/record/chain"
)

// Key is the logical key of the dispatch budget (Architect ruling A3): tenant_id, correlation_id, capability, target_ref.
type Key struct {
	TenantID      string
	CorrelationID string
	Capability    string
	TargetRef     string
}

// BudgetDecision is the gate's answer. Only DispatchAllowed lets the caller go on to ask Trust for a new ALLOW and a new grant.
type BudgetDecision string

const (
	// DispatchAllowed: the key has dispatched fewer times than the budget.
	DispatchAllowed BudgetDecision = "DISPATCH_ALLOWED"
	// BudgetSpent: the budget is used up. The case escalates; no new dispatch is authorised (REQ-RCV-003).
	BudgetSpent BudgetDecision = "BUDGET_SPENT"
)

// Gate is the recovery budget. The number comes from pack data (execution-limits.v1, via execution/limits); there is no default.
type Gate struct{ max int }

// NewGate refuses a budget below 1.
func NewGate(max int) (*Gate, error) {
	if max < 1 {
		return nil, errors.New("recovery: the dispatch budget must be at least 1 and has no default")
	}
	return &Gate{max: max}, nil
}

// Decide counts the DISPATCH_ATTEMPTED events already in rows for k and answers BudgetSpent when they reach the budget. rows are the
// verified rows of one tenant's chain, the tenant tenantID; rows of other correlations do not count. A dispatch of k's correlation whose
// capability or target cannot be read might belong to k, so it counts against every key of that correlation: unknown is never "not counted".
//
// Tenant isolation is the caller's: chain.Row carries no tenant, so the comparison of tenantID with k.TenantID shows only that two strings the caller
// supplies agree. The isolation holds when the rows were read through chain.ReadChain, whose tenant comes from the database login (DEBT-04), and only then.
func (g *Gate) Decide(tenantID string, rows []chain.Row, k Key) BudgetDecision {
	if k.TenantID != tenantID || k.CorrelationID == "" || k.Capability == "" || k.TargetRef == "" {
		return BudgetSpent // not of this chain's tenant, or a part is missing, so it could match no recorded dispatch; refuse
	}
	n := 0
	for _, r := range rows {
		if r.EventType != "DISPATCH_ATTEMPTED" || r.CorrelationID != k.CorrelationID {
			continue
		}
		c, t, ok := dispatchKey(r)
		if !ok || (c == k.Capability && t == k.TargetRef) {
			n++
		}
	}
	if n >= g.max {
		return BudgetSpent
	}
	return DispatchAllowed
}

// BudgetReason says why a dispatch is a violation.
type BudgetReason string

const (
	// BudgetExceeded: this was the (max+1)th or later DISPATCH_ATTEMPTED of its key.
	BudgetExceeded BudgetReason = "BUDGET_EXCEEDED"
	// KeyUnreadable: the dispatch carries no readable capability and target_ref, so it cannot be counted against a key.
	KeyUnreadable BudgetReason = "KEY_UNREADABLE"
)

// BudgetViolation is a dispatch that broke the budget. Count is the number of dispatches of the key up to and including this one (0 when
// the key is unreadable).
type BudgetViolation struct {
	Key    Key
	Seq    int64
	Count  int
	Reason BudgetReason
}

// CheckBudget checks, over verified rows of one tenant's chain, that no key dispatched more than max times. It reads nothing else and
// changes nothing. Output order is seq order.
func CheckBudget(tenantID string, rows []chain.Row, max int) []BudgetViolation {
	sorted := append([]chain.Row(nil), rows...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Seq < sorted[j].Seq })
	counts := map[Key]int{}
	var out []BudgetViolation
	for _, r := range sorted {
		if r.EventType != "DISPATCH_ATTEMPTED" {
			continue
		}
		c, t, ok := dispatchKey(r)
		if !ok {
			out = append(out, BudgetViolation{Key: Key{TenantID: tenantID, CorrelationID: r.CorrelationID}, Seq: r.Seq, Reason: KeyUnreadable})
			continue
		}
		k := Key{TenantID: tenantID, CorrelationID: r.CorrelationID, Capability: c, TargetRef: t}
		counts[k]++
		if counts[k] > max {
			out = append(out, BudgetViolation{Key: k, Seq: r.Seq, Count: counts[k], Reason: BudgetExceeded})
		}
	}
	return out
}

// dispatchKey reads capability and target_ref from a DISPATCH_ATTEMPTED payload. Both must be non-empty strings.
func dispatchKey(r chain.Row) (capability, target string, ok bool) {
	capability, target = payloadString(r, "capability"), payloadString(r, "target_ref")
	return capability, target, capability != "" && target != ""
}
