package completeness_test

// REC-04: completeness per consequential decision. REQ-AUD-002 (the full event set of a governed write; missing events are
// counted in noema_audit_incomplete_total), SoT §38.2 (consequential decisions with a complete causal record: 100%), INV-10.
// Everything is SYNTHETIC: the rows are built in memory with the fields the check reads (correlation, event type, payload).

import (
	"fmt"
	"slices"
	"testing"

	"pgregory.net/rapid"

	"github.com/Noemai-org/noema/services/core/record/chain"
	"github.com/Noemai-org/noema/services/core/record/completeness"
)

// full is the event set of REQ-AUD-002 for a governed write, with the rendered explanation.
var full = []string{"DECISION_MADE", "PLAN_CREATED", "GRANT_ISSUED", "REVALIDATION_PASSED", "GRANT_SPENT", "DISPATCH_ATTEMPTED", "RECEIPT",
	"VERIFICATION_READ", "VERDICT", "EXPLANATION_RENDERED"}

func rows(corr string, types ...string) []chain.Row {
	out := make([]chain.Row, 0, len(types))
	for i, ty := range types {
		r := chain.Row{Seq: int64(i + 1), CorrelationID: corr, EventType: ty, EventID: fmt.Sprintf("recev_%s_%d", corr, i)}
		if ty == "DECISION_MADE" {
			p := `{"decision_outcome":"ALLOW"}`
			r.Payload = &p
		}
		out = append(out, r)
	}
	return out
}

func decision(corr, outcome string) chain.Row {
	p := fmt.Sprintf(`{"decision_outcome":%q}`, outcome)
	return chain.Row{Seq: 1, CorrelationID: corr, EventType: "DECISION_MADE", EventID: "recev_d_" + corr, Payload: &p}
}

type counter struct{ n int }

func (c *counter) AuditIncomplete() { c.n++ }

// REQ-AUD-002: a complete governed write is complete.
func TestACompleteGovernedWriteIsComplete(t *testing.T) {
	rep := completeness.Check(rows("corr_a", full...), nil)
	if !rep.Complete() || rep.Checked != 1 || rep.Governed != 1 {
		t.Fatalf("report = %+v, want 1 checked, 1 governed, complete", rep)
	}
}

// Done when: removing any required event in a test makes the check fail, naming the missing event.
func TestRemovingAnyRequiredEventMakesTheCheckFail(t *testing.T) {
	for i, gone := range full {
		without := slices.Concat(full[:i:i], full[i+1:])
		rep := completeness.Check(rows("corr_a", without...), nil)
		if rep.Complete() {
			t.Fatalf("without %s the check passed", gone)
		}
		want := gone
		if gone == "EXPLANATION_RENDERED" {
			want = "EXPLANATION_RENDERED|EXPLANATION_BLOCKED"
		}
		got := rep.Incomplete[0].Missing
		if len(got) != 1 || got[0] != want {
			t.Fatalf("without %s the missing list is %v, want [%s]", gone, got, want)
		}
	}
}

// A blocked explanation counts as the explanation (REQ-AUD-002: "EXPLANATION_RENDERED or EXPLANATION_BLOCKED"); neither does not.
func TestEitherExplanationEventSatisfiesTheRule(t *testing.T) {
	blocked := slices.Concat(full[:len(full)-1], []string{"EXPLANATION_BLOCKED"})
	if rep := completeness.Check(rows("corr_a", blocked...), nil); !rep.Complete() {
		t.Fatalf("a blocked explanation was not accepted: %+v", rep)
	}
	neither := full[:len(full)-1]
	if rep := completeness.Check(rows("corr_a", neither...), nil); rep.Complete() {
		t.Fatal("a write with no explanation event passed")
	}
}

// VERIFICATION_READ needs at least one; more are fine.
func TestSeveralVerificationReadsAreFine(t *testing.T) {
	more := slices.Concat(full, []string{"VERIFICATION_READ", "VERIFICATION_READ"})
	if rep := completeness.Check(rows("corr_a", more...), nil); !rep.Complete() {
		t.Fatalf("extra verification reads failed the check: %+v", rep)
	}
}

// A path that never reached a write is not a governed write: a clarification needs none of the write events.
func TestAClarificationIsNotAGovernedWrite(t *testing.T) {
	rs := []chain.Row{decision("corr_c", "CLARIFY"), {Seq: 2, CorrelationID: "corr_c", EventType: "EXPLANATION_RENDERED", EventID: "recev_x"}}
	rep := completeness.Check(rs, nil)
	if !rep.Complete() || rep.Governed != 0 || rep.Checked != 1 {
		t.Fatalf("report = %+v, want 1 checked, 0 governed, complete", rep)
	}
}

// Events removed down to the decision alone must not hide a write: an ALLOW decision is consequential by itself.
func TestAnAllowDecisionWithNothingAfterItIsIncomplete(t *testing.T) {
	rep := completeness.Check([]chain.Row{decision("corr_d", "ALLOW")}, nil)
	if rep.Complete() || rep.Governed != 1 {
		t.Fatalf("report = %+v, want a governed, incomplete correlation", rep)
	}
	if !slices.Contains(rep.Incomplete[0].Missing, "GRANT_ISSUED") || !slices.Contains(rep.Incomplete[0].Missing, "VERDICT") {
		t.Fatalf("missing = %v", rep.Incomplete[0].Missing)
	}
}

// Unknown is unknown: a decision whose outcome cannot be read is treated as consequential, so a damaged row cannot hide a write.
func TestADecisionWhoseOutcomeCannotBeReadIsTreatedAsConsequential(t *testing.T) {
	bad := "not json"
	empty := `{}`
	for name, payload := range map[string]*string{"no payload": nil, "not JSON": &bad, "no outcome": &empty} {
		rs := []chain.Row{{Seq: 1, CorrelationID: "corr_u", EventType: "DECISION_MADE", EventID: "recev_u", Payload: payload}}
		rep := completeness.Check(rs, nil)
		if rep.Complete() || rep.Governed != 1 {
			t.Fatalf("%s: report = %+v, want a governed, incomplete correlation", name, rep)
		}
	}
}

// A write event with the decision removed is still a governed write, and DECISION_MADE is reported missing.
func TestAWriteWithoutItsDecisionIsIncomplete(t *testing.T) {
	rep := completeness.Check(rows("corr_e", "GRANT_SPENT", "DISPATCH_ATTEMPTED", "RECEIPT"), nil)
	if rep.Complete() || !slices.Contains(rep.Incomplete[0].Missing, "DECISION_MADE") {
		t.Fatalf("report = %+v", rep)
	}
}

// Each correlation is judged alone, in a stable order, and the metric counts incomplete correlations once each.
func TestCorrelationsAreJudgedAloneAndCounted(t *testing.T) {
	m := &counter{}
	rs := slices.Concat(rows("corr_b", full[:5]...), rows("corr_a", full...), rows("corr_z", full[:2]...))
	rep := completeness.CheckWith(rs, nil, m)
	if rep.Checked != 3 || len(rep.Incomplete) != 2 || m.n != 2 {
		t.Fatalf("report = %+v, metric = %d; want 3 checked, 2 incomplete, 2 counted", rep, m.n)
	}
	if rep.Incomplete[0].CorrelationID != "corr_b" || rep.Incomplete[1].CorrelationID != "corr_z" {
		t.Fatalf("order = %+v", rep.Incomplete)
	}
}

// REQ-AUD-002 test: "a benchmark audit checker using each case's required_audit_events". A clarification case lists its own events.
func TestACasesOwnRequiredEventsAreChecked(t *testing.T) {
	rs := []chain.Row{decision("corr_c", "CLARIFY"), {Seq: 2, CorrelationID: "corr_c", EventType: "EXPLANATION_RENDERED", EventID: "recev_x"}}
	required := []string{"CANDIDATES_RESOLVED", "DECISION_MADE", "EXPLANATION_RENDERED"}
	rep := completeness.Check(rs, map[string][]string{"corr_c": required})
	if rep.Complete() || rep.Incomplete[0].Missing[0] != "CANDIDATES_RESOLVED" {
		t.Fatalf("report = %+v, want CANDIDATES_RESOLVED missing", rep)
	}
	rs = append(rs, chain.Row{Seq: 3, CorrelationID: "corr_c", EventType: "CANDIDATES_RESOLVED", EventID: "recev_y"})
	if rep := completeness.Check(rs, map[string][]string{"corr_c": required}); !rep.Complete() {
		t.Fatalf("a case with all its required events failed: %+v", rep)
	}
}

// An empty Record is complete and checks nothing; unknown is not counted as clean evidence of anything.
func TestAnEmptyRecordChecksNothing(t *testing.T) {
	rep := completeness.Check(nil, nil)
	if !rep.Complete() || rep.Checked != 0 {
		t.Fatalf("report = %+v", rep)
	}
}

// Property (REQ-AUD-002, INV-10): for any extra non-required events in any order, the complete set passes, and removing any one
// required event (keeping all copies of the others) fails with exactly that event missing.
func TestPropertyRemovalIsAlwaysDetectedAndExtrasNeverHideIt(t *testing.T) {
	extras := []string{"INPUT_RECEIVED", "SESSION_VALIDATED", "INTENT_PROPOSED", "CANDIDATES_RESOLVED", "WATCH_CHECKED", "CALLBACK_DELIVERED"}
	rapid.Check(t, func(t *rapid.T) {
		drop := rapid.IntRange(-1, len(full)-1).Draw(t, "drop")
		var types []string
		for i, ty := range full {
			if i != drop {
				types = append(types, ty)
			}
		}
		types = append(types, rapid.SliceOfN(rapid.SampledFrom(extras), 0, 8).Draw(t, "extras")...)
		perm := rapid.Permutation(types).Draw(t, "order")
		rep := completeness.Check(rows("corr_p", perm...), nil)
		if drop < 0 {
			if !rep.Complete() {
				t.Fatalf("a complete set with extras failed: %v", rep.Incomplete)
			}
			return
		}
		if rep.Complete() || len(rep.Incomplete) != 1 || len(rep.Incomplete[0].Missing) != 1 {
			t.Fatalf("dropping %s: report %+v", full[drop], rep)
		}
	})
}
