package completeness

import (
	"encoding/json"
	"slices"
	"sort"

	"github.com/Noemai-org/noema/services/core/record/chain"
)

// writeEvents are the event types that only a governed write produces. A correlation that holds any of them is a governed write.
var writeEvents = map[string]bool{"GRANT_ISSUED": true, "REVALIDATION_PASSED": true, "GRANT_SPENT": true, "DISPATCH_ATTEMPTED": true, "RECEIPT": true}

// governedSet is the event set REQ-AUD-002 requires of every governed write, in a fixed order. VERIFICATION_READ needs at least one.
// The explanation rule (rendered or blocked) is explanationEither below.
var governedSet = []string{"DECISION_MADE", "PLAN_CREATED", "GRANT_ISSUED", "REVALIDATION_PASSED", "GRANT_SPENT", "DISPATCH_ATTEMPTED", "RECEIPT",
	"VERIFICATION_READ", "VERDICT"}

// ExplanationEither is the name under which the "rendered or blocked" rule is reported as missing.
const ExplanationEither = "EXPLANATION_RENDERED|EXPLANATION_BLOCKED"

// Metrics receives noema_audit_incomplete_total (REQ-AUD-002), once for each incomplete correlation. The metric has no label.
type Metrics interface{ AuditIncomplete() }

// Gap is one correlation whose Record lacks events it must have.
type Gap struct {
	CorrelationID string
	Governed      bool     // the correlation is a governed write; otherwise only its case-specific events were required
	Missing       []string // the missing event types, in the order of the required set; the explanation rule is ExplanationEither
}

// Report is the result of one check. It is complete only when no correlation has a gap.
type Report struct {
	Checked    int // correlations judged
	Governed   int // of them, governed writes
	Incomplete []Gap
}

// Complete reports whether every correlation had every event it must have.
func (r Report) Complete() bool { return len(r.Incomplete) == 0 }

// Check judges every correlation in rows. extra maps a correlation ID to event types that correlation must also hold (a benchmark
// case's required_audit_events); it may be nil. Correlations are judged alone and reported in correlation order.
func Check(rows []chain.Row, extra map[string][]string) Report { return CheckWith(rows, extra, nil) }

// CheckWith is Check plus the metric hook: m.AuditIncomplete is called once for each incomplete correlation. m may be nil.
func CheckWith(rows []chain.Row, extra map[string][]string, m Metrics) Report {
	type seen struct {
		types      map[string]int
		allowed    bool
		hasWrite   bool
		hasExplain bool
	}
	by := map[string]*seen{}
	for _, r := range rows {
		s := by[r.CorrelationID]
		if s == nil {
			s = &seen{types: map[string]int{}}
			by[r.CorrelationID] = s
		}
		s.types[r.EventType]++
		switch {
		case writeEvents[r.EventType]:
			s.hasWrite = true
		case r.EventType == "DECISION_MADE" && decidedAllow(r):
			s.allowed = true
		}
	}
	corrs := make([]string, 0, len(by))
	for c := range by {
		corrs = append(corrs, c)
	}
	for c := range extra { // a case may require events of a correlation that has no row at all
		if _, ok := by[c]; !ok {
			by[c] = &seen{types: map[string]int{}}
			corrs = append(corrs, c)
		}
	}
	sort.Strings(corrs)

	var rep Report
	for _, c := range corrs {
		s := by[c]
		rep.Checked++
		governed := s.hasWrite || s.allowed
		if governed {
			rep.Governed++
		}
		var missing []string
		if governed {
			for _, ty := range governedSet {
				if s.types[ty] == 0 {
					missing = append(missing, ty)
				}
			}
			if s.types["EXPLANATION_RENDERED"] == 0 && s.types["EXPLANATION_BLOCKED"] == 0 {
				missing = append(missing, ExplanationEither)
			}
		}
		for _, ty := range extra[c] {
			if s.types[ty] == 0 && !slices.Contains(missing, ty) {
				missing = append(missing, ty)
			}
		}
		if len(missing) > 0 {
			rep.Incomplete = append(rep.Incomplete, Gap{CorrelationID: c, Governed: governed, Missing: missing})
			if m != nil {
				m.AuditIncomplete()
			}
		}
	}
	return rep
}

// notConsequential are the readable outcomes of REQ-TRU-001 that lead to no write: a clarification, a confirmation request, a wait, a
// rejection, an escalation, and UNKNOWN. ALLOW is the one outcome that leads to a governed write.
var notConsequential = map[string]bool{"CLARIFY": true, "CONFIRM": true, "WAIT": true, "REJECT": true, "ESCALATE": true, "UNKNOWN": true}

// decidedAllow reports whether a DECISION_MADE row must be treated as consequential: its outcome is ALLOW, or it is not one of the
// seven outcomes, or it cannot be read. Unknown is unknown: a row with no payload, a payload that is not JSON, no decision_outcome, or
// a value outside the closed set is taken as consequential, so a damaged row cannot hide a write (Codex review of noema#173).
func decidedAllow(r chain.Row) bool {
	if r.Payload == nil {
		return true
	}
	var p struct {
		Outcome *string `json:"decision_outcome"`
	}
	if json.Unmarshal([]byte(*r.Payload), &p) != nil || p.Outcome == nil {
		return true
	}
	return !notConsequential[*p.Outcome]
}
