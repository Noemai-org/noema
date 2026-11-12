package recovery_test

// REQ-RCV-001 acceptance: "Every dispatch with attempt > 1 SHALL be preceded by a non-ACHIEVED verdict and a new ALLOW decision."
// Test: "A property test over Record sequences." INV-15, AGENT-08. Rows are built in memory (SYNTHETIC).

import (
	"fmt"
	"testing"

	"pgregory.net/rapid"

	"github.com/Noemai-org/noema/services/core/execution/recovery"
	"github.com/Noemai-org/noema/services/core/record/chain"
)

type ev struct {
	ty      string
	payload string
}

func seq(corr string, evs ...ev) []chain.Row {
	out := make([]chain.Row, len(evs))
	for i, e := range evs {
		p := e.payload
		out[i] = chain.Row{Seq: int64(i + 1), CorrelationID: corr, EventType: e.ty, EventID: fmt.Sprintf("recev_%s_%d", corr, i)}
		if p != "" {
			out[i].Payload = &p
		}
	}
	return out
}

func dispatch(attempt int) ev {
	return ev{"DISPATCH_ATTEMPTED", fmt.Sprintf(`{"attempt":%d}`, attempt)}
}

// verdict is the payload Outcome writes (outcome/store): the verdict document nested under "verdict", with the enum at verdict.verdict.
func verdict(v string) ev {
	return ev{"VERDICT", fmt.Sprintf(`{"verdict":{"verdict":%q,"verdict_id":"ver_1"},"reason":"POSTCONDITION_UNVERIFIED"}`, v)}
}

// flatVerdict is the flat shape the reconstruction projection reads (a "verdict" string); both are accepted.
func flatVerdict(v string) ev { return ev{"VERDICT", fmt.Sprintf(`{"verdict":%q}`, v)} }
func decided(o string) ev     { return ev{"DECISION_MADE", fmt.Sprintf(`{"decision_outcome":%q}`, o)} }

func TestAFirstDispatchNeedsNoVerdict(t *testing.T) {
	if v := recovery.CheckRetryOrder(seq("c1", decided("ALLOW"), dispatch(1))); len(v) != 0 {
		t.Fatalf("violations = %+v", v)
	}
}

func TestARetryAfterAVerdictAndANewAllowIsOrdered(t *testing.T) {
	rs := seq("c1", decided("ALLOW"), dispatch(1), verdict("NOT_ACHIEVED"), decided("ALLOW"), dispatch(2))
	if v := recovery.CheckRetryOrder(rs); len(v) != 0 {
		t.Fatalf("violations = %+v", v)
	}
}

func TestARetryWithoutAVerdictIsAViolation(t *testing.T) {
	rs := seq("c1", decided("ALLOW"), dispatch(1), decided("ALLOW"), dispatch(2))
	v := recovery.CheckRetryOrder(rs)
	if len(v) != 1 || v[0].Reason != recovery.NoNonAchievedVerdict || v[0].Attempt != 2 || v[0].CorrelationID != "c1" {
		t.Fatalf("violations = %+v", v)
	}
}

func TestARetryAfterAnAchievedVerdictIsAViolation(t *testing.T) {
	rs := seq("c1", decided("ALLOW"), dispatch(1), verdict("ACHIEVED"), decided("ALLOW"), dispatch(2))
	if v := recovery.CheckRetryOrder(rs); len(v) != 1 || v[0].Reason != recovery.NoNonAchievedVerdict {
		t.Fatalf("violations = %+v", v)
	}
}

func TestARetryWithoutANewAllowIsAViolation(t *testing.T) {
	rs := seq("c1", decided("ALLOW"), dispatch(1), verdict("NOT_ACHIEVED"), dispatch(2))
	if v := recovery.CheckRetryOrder(rs); len(v) != 1 || v[0].Reason != recovery.NoNewAllow {
		t.Fatalf("violations = %+v", v)
	}
}

// The ALLOW must come after the verdict: the old ALLOW that authorised the first dispatch does not count as a new one.
func TestAnAllowBeforeTheVerdictIsNotANewAllow(t *testing.T) {
	rs := seq("c1", decided("ALLOW"), verdict("NOT_ACHIEVED"), dispatch(1), dispatch(2))
	if v := recovery.CheckRetryOrder(rs); len(v) == 0 {
		t.Fatal("a retry with no ALLOW after the verdict passed")
	}
	rs = seq("c1", decided("ALLOW"), dispatch(1), decided("ALLOW"), verdict("NOT_ACHIEVED"), dispatch(2))
	if v := recovery.CheckRetryOrder(rs); len(v) != 1 || v[0].Reason != recovery.NoNewAllow {
		t.Fatalf("an ALLOW before the verdict counted: %+v", v)
	}
}

// A decision that is not ALLOW does not authorise a retry.
func TestANonAllowDecisionDoesNotAuthoriseARetry(t *testing.T) {
	rs := seq("c1", decided("ALLOW"), dispatch(1), verdict("NOT_ACHIEVED"), decided("ESCALATE"), dispatch(2))
	if v := recovery.CheckRetryOrder(rs); len(v) != 1 || v[0].Reason != recovery.NoNewAllow {
		t.Fatalf("violations = %+v", v)
	}
}

// Unknown is unknown: a verdict that cannot be read, or that is not one of the six, is not a non-ACHIEVED verdict.
func TestAVerdictThatCannotBeReadDoesNotAuthoriseARetry(t *testing.T) {
	for name, vd := range map[string]ev{"empty payload": {"VERDICT", ""}, "not json": {"VERDICT", "nope"}, "no verdict": {"VERDICT", `{}`}, "an unknown value": verdict("MAYBE"), "lower case": verdict("not_achieved")} {
		rs := seq("c1", decided("ALLOW"), dispatch(1), vd, decided("ALLOW"), dispatch(2))
		if v := recovery.CheckRetryOrder(rs); len(v) != 1 || v[0].Reason != recovery.NoNonAchievedVerdict {
			t.Fatalf("%s: violations = %+v", name, v)
		}
	}
}

// The flat shape of the reconstruction projection is read too.
func TestAFlatVerdictPayloadIsReadToo(t *testing.T) {
	rs := seq("c1", decided("ALLOW"), dispatch(1), flatVerdict("NOT_ACHIEVED"), decided("ALLOW"), dispatch(2))
	if v := recovery.CheckRetryOrder(rs); len(v) != 0 {
		t.Fatalf("violations = %+v", v)
	}
}

// A nested document whose inner verdict is missing or not a string is unreadable.
func TestANestedVerdictWithNoEnumIsUnreadable(t *testing.T) {
	for name, p := range map[string]string{"empty object": `{"verdict":{}}`, "number": `{"verdict":{"verdict":3}}`, "null": `{"verdict":null}`, "array": `{"verdict":[]}`} {
		rs := seq("c1", decided("ALLOW"), dispatch(1), ev{"VERDICT", p}, decided("ALLOW"), dispatch(2))
		if v := recovery.CheckRetryOrder(rs); len(v) != 1 || v[0].Reason != recovery.NoNonAchievedVerdict {
			t.Fatalf("%s: violations = %+v", name, v)
		}
	}
}

// An ACHIEVED verdict after a failed one means there is nothing to recover: it cancels the earlier verdict.
func TestAnAchievedVerdictCancelsAnEarlierFailedOne(t *testing.T) {
	rs := seq("c1", decided("ALLOW"), dispatch(1), verdict("NOT_ACHIEVED"), verdict("ACHIEVED"), decided("ALLOW"), dispatch(2))
	if v := recovery.CheckRetryOrder(rs); len(v) != 1 || v[0].Reason != recovery.NoNonAchievedVerdict {
		t.Fatalf("violations = %+v", v)
	}
}

// Each retry needs its own verdict and ALLOW: a third dispatch cannot reuse the pair that authorised the second.
func TestEachRetryNeedsItsOwnVerdictAndAllow(t *testing.T) {
	rs := seq("c1", decided("ALLOW"), dispatch(1), verdict("NOT_ACHIEVED"), decided("ALLOW"), dispatch(2), dispatch(3))
	v := recovery.CheckRetryOrder(rs)
	if len(v) != 1 || v[0].Attempt != 3 {
		t.Fatalf("violations = %+v", v)
	}
}

// Unknown is unknown: a dispatch whose attempt cannot be read is a violation, never taken as a first attempt.
func TestADispatchWithAnUnreadableAttemptIsAViolation(t *testing.T) {
	for name, p := range map[string]string{"": "", "not json": "nope", "no attempt": `{}`, "a string": `{"attempt":"2"}`, "zero": `{"attempt":0}`, "a float": `{"attempt":1.5}`} {
		rs := seq("c1", ev{"DISPATCH_ATTEMPTED", p})
		if v := recovery.CheckRetryOrder(rs); len(v) != 1 || v[0].Reason != recovery.AttemptUnreadable {
			t.Fatalf("%q: violations = %+v", name, v)
		}
	}
}

// Correlations are judged alone.
func TestCorrelationsAreJudgedAlone(t *testing.T) {
	good := seq("c_good", decided("ALLOW"), dispatch(1), verdict("NOT_ACHIEVED"), decided("ALLOW"), dispatch(2))
	bad := seq("c_bad", decided("ALLOW"), dispatch(1), dispatch(2))
	v := recovery.CheckRetryOrder(append(good, bad...))
	if len(v) != 1 || v[0].CorrelationID != "c_bad" {
		t.Fatalf("violations = %+v", v)
	}
}

// Property over Record sequences: build a story of dispatches, each retry optionally missing its verdict, missing its ALLOW or having
// them in the wrong order; the checker must flag exactly the retries that break the rule, with the right reason.
func TestPropertyTheCheckerFlagsExactlyTheBrokenRetries(t *testing.T) {
	// kinds: 0 ordered; 1 no verdict; 2 verdict but no ALLOW; 3 ALLOW before the verdict; 4 an ACHIEVED verdict
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(1, 5).Draw(t, "dispatches")
		kinds := rapid.SliceOfN(rapid.IntRange(0, 4), n-1, n-1).Draw(t, "kinds")
		evs := []ev{decided("ALLOW"), dispatch(1)}
		var want []recovery.Reason
		for i, k := range kinds {
			switch k {
			case 0:
				evs = append(evs, verdict("NOT_ACHIEVED"), decided("ALLOW"))
			case 1:
				evs = append(evs, decided("ALLOW"))
			case 2:
				evs = append(evs, verdict("PENDING_EXTERNAL"))
			case 3:
				evs = append(evs, decided("ALLOW"), verdict("NOT_ACHIEVED"))
			case 4:
				evs = append(evs, verdict("ACHIEVED"), decided("ALLOW"))
			}
			evs = append(evs, dispatch(i+2))
			switch k {
			case 0:
				want = append(want, "")
			case 1, 4:
				want = append(want, recovery.NoNonAchievedVerdict)
			case 2, 3:
				want = append(want, recovery.NoNewAllow)
			}
		}
		got := recovery.CheckRetryOrder(seq("c_p", evs...))
		var wantBroken []recovery.Reason
		for _, w := range want {
			if w != "" {
				wantBroken = append(wantBroken, w)
			}
		}
		if len(got) != len(wantBroken) {
			t.Fatalf("kinds %v: got %+v, want reasons %v", kinds, got, wantBroken)
		}
		for i := range got {
			if got[i].Reason != wantBroken[i] {
				t.Fatalf("kinds %v: violation %d is %s, want %s", kinds, i, got[i].Reason, wantBroken[i])
			}
		}
	})
}

// A verdict that cannot be read, or is not one of the enum, is later evidence than an earlier failed verdict and ALLOW. It must not leave
// that earlier pair standing as authority for a retry: unknown is never safe (Codex review of noema#196, REQ-RCV-001, INV-15, AGENT-08).
func TestAnUnreadableLaterVerdictRevokesTheEarlierPairAsAuthority(t *testing.T) {
	for name, bad := range map[string]ev{
		"no verdict member":   {"VERDICT", `{"reason":"X"}`},
		"unknown enum":        verdict("MAYBE"),
		"not json":            {"VERDICT", `{`},
		"verdict is a number": {"VERDICT", `{"verdict":7}`},
		"no payload at all":   {"VERDICT", ""},
	} {
		rows := seq("c1", dispatch(1), verdict("NOT_ACHIEVED"), decided("ALLOW"), bad, dispatch(2))
		v := recovery.CheckRetryOrder(rows)
		if len(v) != 1 || v[0].Reason != recovery.NoNonAchievedVerdict {
			t.Errorf("%s: violations = %+v, want one %s", name, v, recovery.NoNonAchievedVerdict)
		}
	}
	// A readable failed verdict and a new ALLOW after the unreadable one authorise the retry again.
	ok := seq("c1", dispatch(1), verdict("NOT_ACHIEVED"), decided("ALLOW"), ev{"VERDICT", `{`}, verdict("NOT_ACHIEVED"), decided("ALLOW"), dispatch(2))
	if v := recovery.CheckRetryOrder(ok); len(v) != 0 {
		t.Errorf("a fresh pair after the unreadable verdict: %+v", v)
	}
}

// Ruling A68 of DEC-2026-11-08-rulings-a41-onward (merged; called A6 in the 8 Nov answers to C1): a COMPENSATED verdict does not authorise a retry. The
// harm was already undone; another dispatch needs a verdict that says the target is not in the wanted state. REQ-RCV-001, REQ-RCV-002, AGENT-08, INV-07.
func TestACompensatedVerdictDoesNotAuthoriseARetry(t *testing.T) {
	rs := seq("c1", dispatch(1), verdict("COMPENSATED"), decided("ALLOW"), dispatch(2))
	v := recovery.CheckRetryOrder(rs)
	if len(v) != 1 || v[0].Reason != recovery.NoNonAchievedVerdict || v[0].Attempt != 2 {
		t.Fatalf("a retry after COMPENSATED and a new ALLOW: %+v, want one NoNonAchievedVerdict violation at attempt 2", v)
	}
	// A failed verdict before it still counts once a later failed verdict restarts the wait: COMPENSATED ends the authority of the earlier pair.
	rs = seq("c2", dispatch(1), verdict("NOT_ACHIEVED"), verdict("COMPENSATED"), decided("ALLOW"), dispatch(2))
	if v := recovery.CheckRetryOrder(rs); len(v) != 1 || v[0].Reason != recovery.NoNonAchievedVerdict {
		t.Fatalf("COMPENSATED after NOT_ACHIEVED then ALLOW: %+v, want a violation (it ends the authority of the earlier verdict)", v)
	}
	// Control: the same sequence with NOT_ACHIEVED is ordered.
	if v := recovery.CheckRetryOrder(seq("c3", dispatch(1), verdict("NOT_ACHIEVED"), decided("ALLOW"), dispatch(2))); len(v) != 0 {
		t.Fatalf("control: %+v", v)
	}
}
