package recovery

import (
	"encoding/json"
	"sort"

	"github.com/Noemai-org/noema/services/core/record/chain"
)

// Reason says which half of the retry rule a dispatch broke.
type Reason string

const (
	// NoNonAchievedVerdict: no VERDICT other than ACHIEVED since the previous dispatch of the correlation.
	NoNonAchievedVerdict Reason = "NO_NON_ACHIEVED_VERDICT"
	// NoNewAllow: no DECISION_MADE with the outcome ALLOW after that verdict.
	NoNewAllow Reason = "NO_NEW_ALLOW"
	// AttemptUnreadable: the dispatch event has no attempt that is a whole number of at least 1. Unknown is never a first attempt.
	AttemptUnreadable Reason = "ATTEMPT_UNREADABLE"
)

// Violation is a dispatch that broke the rule.
type Violation struct {
	CorrelationID string
	Seq           int64
	Attempt       int64 // 0 when unreadable
	Reason        Reason
}

// CheckRetryOrder checks REQ-RCV-001's acceptance criterion over rows the caller has read and verified: every DISPATCH_ATTEMPTED with
// attempt > 1 is preceded, since the previous dispatch of the same correlation, by a VERDICT other than ACHIEVED and then by a
// DECISION_MADE with the outcome ALLOW. Each retry needs its own pair. Correlations are judged alone, in order; within one, rows are
// taken in seq order. It reads nothing else and changes nothing.
func CheckRetryOrder(rows []chain.Row) []Violation {
	by := map[string][]chain.Row{}
	for _, r := range rows {
		by[r.CorrelationID] = append(by[r.CorrelationID], r)
	}
	corrs := make([]string, 0, len(by))
	for c := range by {
		corrs = append(corrs, c)
	}
	sort.Strings(corrs)

	var out []Violation
	for _, c := range corrs {
		rs := by[c]
		sort.SliceStable(rs, func(i, j int) bool { return rs[i].Seq < rs[j].Seq })
		var verdictSeen, allowAfter bool // since the previous dispatch
		for _, r := range rs {
			switch r.EventType {
			case "VERDICT":
				switch v := verdictOf(r); {
				case v == "ACHIEVED":
					verdictSeen, allowAfter = false, false // nothing to recover: it cancels an earlier failed verdict
				case v == "COMPENSATED":
					verdictSeen, allowAfter = false, false // the harm was undone (ruling A6, 8 Nov 2026): it authorises no retry and ends the authority of any earlier failed verdict
				case verdicts[v]:
					verdictSeen, allowAfter = true, false // a later verdict restarts the wait for an ALLOW
				default:
					verdictSeen, allowAfter = false, false // unreadable or unknown: never a non-ACHIEVED verdict, and it ends the authority of any earlier pair
				}
			case "DECISION_MADE":
				if verdictSeen && payloadString(r, "decision_outcome") == "ALLOW" {
					allowAfter = true
				}
			case "DISPATCH_ATTEMPTED":
				attempt, ok := payloadAttempt(r)
				switch {
				case !ok:
					out = append(out, Violation{CorrelationID: c, Seq: r.Seq, Reason: AttemptUnreadable})
				case attempt > 1 && !verdictSeen:
					out = append(out, Violation{CorrelationID: c, Seq: r.Seq, Attempt: attempt, Reason: NoNonAchievedVerdict})
				case attempt > 1 && !allowAfter:
					out = append(out, Violation{CorrelationID: c, Seq: r.Seq, Attempt: attempt, Reason: NoNewAllow})
				}
				verdictSeen, allowAfter = false, false
			}
		}
	}
	return out
}

// verdicts are the verdicts other than ACHIEVED (enums.v1 Verdict).
var verdicts = map[string]bool{"NOT_ACHIEVED": true, "PARTIALLY_ACHIEVED": true, "PENDING_EXTERNAL": true, "UNKNOWN": true}

// verdictOf reads the verdict enum of a VERDICT row. Outcome writes the verdict document nested under "verdict" (outcome/store), so the
// enum is at verdict.verdict; the flat form {"verdict": "<enum>"} that the reconstruction projection reads is accepted too. Anything
// else is unreadable and yields "".
func verdictOf(r chain.Row) string {
	if r.Payload == nil {
		return ""
	}
	var m map[string]json.RawMessage
	if json.Unmarshal([]byte(*r.Payload), &m) != nil {
		return ""
	}
	var flat string
	if json.Unmarshal(m["verdict"], &flat) == nil {
		return flat
	}
	var doc struct {
		Verdict string `json:"verdict"`
	}
	if json.Unmarshal(m["verdict"], &doc) == nil {
		return doc.Verdict
	}
	return ""
}

func payloadString(r chain.Row, key string) string {
	if r.Payload == nil {
		return ""
	}
	var m map[string]json.RawMessage
	if json.Unmarshal([]byte(*r.Payload), &m) != nil {
		return ""
	}
	var s string
	if json.Unmarshal(m[key], &s) != nil {
		return ""
	}
	return s
}

// payloadAttempt reads the attempt as a whole number of at least 1.
func payloadAttempt(r chain.Row) (int64, bool) {
	if r.Payload == nil {
		return 0, false
	}
	var m map[string]json.RawMessage
	if json.Unmarshal([]byte(*r.Payload), &m) != nil {
		return 0, false
	}
	var n int64
	if json.Unmarshal(m["attempt"], &n) != nil || n < 1 {
		return 0, false
	}
	return n, true
}
