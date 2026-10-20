package decide_test

import (
	"errors"
	"math/rand"
	"reflect"
	"sort"
	"strconv"
	"testing"

	"github.com/Noemai-org/noema/services/core/trust/decide"
)

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func core(t *testing.T, code string) decide.ReasonCode {
	t.Helper()
	r, err := decide.Core(code)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

var allPass = decide.Authority{Technical: decide.Pass, Policy: decide.Pass, Business: decide.Pass}

func TestAdmitSortsIntents(t *testing.T) {
	cases := []struct {
		name     string
		view     decide.IntentView
		resolved bool
	}{
		{"all slots resolved", decide.IntentView{Version: 1, RequiredSlots: map[string]decide.SlotStatus{"subject_resource": decide.SlotResolved}}, true},
		{"no required slots", decide.IntentView{Version: 1}, true},
		{"an ambiguous slot", decide.IntentView{Version: 1, RequiredSlots: map[string]decide.SlotStatus{"subject_resource": decide.SlotAmbiguous}}, false},
		{"an unresolved slot", decide.IntentView{Version: 1, RequiredSlots: map[string]decide.SlotStatus{"subject_resource": decide.SlotUnresolved}}, false},
		{"an absent slot", decide.IntentView{Version: 1, RequiredSlots: map[string]decide.SlotStatus{"subject_resource": decide.SlotAbsent}}, false},
		{"a material flag on resolved slots", decide.IntentView{Version: 1, MaterialFlags: []string{"COUNTERPARTY_MULTIPLE_MATCH"}, RequiredSlots: map[string]decide.SlotStatus{"subject_resource": decide.SlotResolved}}, false},
		{"one resolved, one not", decide.IntentView{Version: 1, RequiredSlots: map[string]decide.SlotStatus{"a": decide.SlotResolved, "b": decide.SlotAmbiguous}}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			adm := decide.Admit(c.view)
			_, isResolved := adm.Resolved()
			_, isUnresolved := adm.Unresolved()
			if isResolved != c.resolved || isUnresolved == c.resolved {
				t.Fatalf("resolved=%v unresolved=%v, want resolved=%v", isResolved, isUnresolved, c.resolved)
			}
		})
	}
}

// REQ-AMB-001: the type of an unresolved intent has exactly two decision constructors.
func TestUnresolvedIntentExposesOnlyClarifyAndEscalate(t *testing.T) {
	typ := reflect.TypeOf(decide.UnresolvedIntent{})
	decisionType := reflect.TypeOf(decide.Decision{})
	var builders []string
	for i := 0; i < typ.NumMethod(); i++ {
		m := typ.Method(i)
		if m.Type.NumOut() > 0 && m.Type.Out(0) == decisionType {
			builders = append(builders, m.Name)
		}
	}
	sort.Strings(builders)
	if !reflect.DeepEqual(builders, []string{"Clarify", "Escalate"}) {
		t.Fatalf("an unresolved intent can build %v", builders)
	}
}

func randomView(r *rand.Rand) decide.IntentView {
	v := decide.IntentView{Version: 1 + r.Intn(5), RequiredSlots: map[string]decide.SlotStatus{}}
	statuses := []decide.SlotStatus{decide.SlotResolved, decide.SlotResolved, decide.SlotUnresolved, decide.SlotAmbiguous, decide.SlotAbsent}
	for i := r.Intn(4); i > 0; i-- {
		v.RequiredSlots["slot"+strconv.Itoa(i)] = statuses[r.Intn(len(statuses))]
	}
	for i := r.Intn(3); i > 0 && r.Intn(2) == 0; i-- {
		v.MaterialFlags = append(v.MaterialFlags, "FLAG_"+strconv.Itoa(i))
	}
	return v
}

// REQ-AMB-001 acceptance: over generated ambiguous intents, 0 decisions outside {CLARIFY, ESCALATE}.
func TestAmbiguousIntentsNeverYieldAnotherOutcome(t *testing.T) {
	r := rand.New(rand.NewSource(20261020))
	q := decide.Clarification{Mode: decide.ModeOptions, QuestionKey: "fin.q.which", AllowNone: true}
	reason := core(t, "AMBIGUOUS_COUNTERPARTY")
	ambiguous := 0
	for i := 0; i < 5000; i++ {
		v := randomView(r)
		adm := decide.Admit(v)
		u, isUnresolved := adm.Unresolved()
		_, isResolved := adm.Resolved()
		clean := len(v.MaterialFlags) == 0
		for _, s := range v.RequiredSlots {
			clean = clean && s == decide.SlotResolved
		}
		if isResolved != clean || isUnresolved == clean {
			t.Fatalf("view %+v sorted wrongly", v)
		}
		if !isUnresolved {
			continue
		}
		ambiguous++
		for _, d := range []decide.Decision{must(u.Clarify(q, reason)), must(u.Escalate(reason))} {
			if d.Outcome() != decide.Clarify && d.Outcome() != decide.Escalate {
				t.Fatalf("an ambiguous intent produced %s", d.Outcome())
			}
		}
	}
	if ambiguous < 1000 {
		t.Fatalf("only %d ambiguous intents generated", ambiguous)
	}
}

func TestZeroIntentsCannotDecide(t *testing.T) {
	var r decide.ResolvedIntent
	var u decide.UnresolvedIntent
	reason := core(t, "NOT_AUTHORIZED")
	if _, err := r.Allow(allPass); !errors.Is(err, decide.ErrZeroIntent) {
		t.Fatalf("zero ResolvedIntent allowed: %v", err)
	}
	if _, err := r.Reject(allPass, reason); !errors.Is(err, decide.ErrZeroIntent) {
		t.Fatalf("zero ResolvedIntent rejected: %v", err)
	}
	if _, err := u.Escalate(reason); !errors.Is(err, decide.ErrZeroIntent) {
		t.Fatalf("zero UnresolvedIntent escalated: %v", err)
	}
}

// REQ-TRU-001: an ALLOW needs all three layers at PASS; FAIL or NOT_EVALUATED anywhere makes it invalid (all 27 combinations).
func TestAllowAndConfirmNeedEveryLayerToPass(t *testing.T) {
	resolved, _ := decide.Admit(decide.IntentView{Version: 3}).Resolved()
	layers := []decide.LayerResult{decide.Pass, decide.Fail, decide.NotEvaluated}
	n := 0
	for _, a := range layers {
		for _, b := range layers {
			for _, c := range layers {
				auth := decide.Authority{Technical: a, Policy: b, Business: c}
				for name, build := range map[string]func(decide.Authority, ...decide.ReasonCode) (decide.Decision, error){"Allow": resolved.Allow, "Confirm": resolved.Confirm} {
					d, err := build(auth)
					if auth.AllPass() && (err != nil || d.IntentVersion() != 3) {
						t.Fatalf("%s with all PASS failed: %v", name, err)
					}
					if !auth.AllPass() && !errors.Is(err, decide.ErrAuthorityNotPass) {
						t.Fatalf("%s accepted %+v", name, auth)
					}
				}
				n++
			}
		}
	}
	if n != 27 {
		t.Fatalf("covered %d combinations", n)
	}
	if _, err := resolved.Allow(decide.Authority{Technical: "MAYBE", Policy: decide.Pass, Business: decide.Pass}); !errors.Is(err, decide.ErrAuthorityInvalid) {
		t.Fatalf("an invalid layer value was accepted: %v", err)
	}
}

func TestOutcomesThatNeedAReasonOrATime(t *testing.T) {
	resolved, _ := decide.Admit(decide.IntentView{Version: 1}).Resolved()
	if _, err := resolved.Reject(allPass); !errors.Is(err, decide.ErrNoReason) {
		t.Fatalf("REJECT without a reason: %v", err)
	}
	if _, err := resolved.Unknown(allPass); !errors.Is(err, decide.ErrNoReason) {
		t.Fatalf("UNKNOWN without a reason: %v", err)
	}
	if _, err := resolved.Wait(allPass, 0, core(t, "STALE_EVIDENCE")); !errors.Is(err, decide.ErrNoReevaluateAt) {
		t.Fatalf("WAIT without a time: %v", err)
	}
	w := must(resolved.Wait(allPass, 1792500600, core(t, "STALE_EVIDENCE")))
	if w.Outcome() != decide.Wait || w.ReevaluateAt() != 1792500600 {
		t.Fatalf("%+v", w)
	}
	var zero decide.ReasonCode
	if _, err := resolved.Reject(allPass, zero); err == nil {
		t.Fatal("a zero reason code was accepted")
	}
}

func TestReasonCodes(t *testing.T) {
	core17 := []string{"PROHIBITED_CAPABILITY", "AMBIGUOUS_COUNTERPARTY", "DECLINED_BY_APPROVER", "POLICY_VERSION_MISMATCH", "INSUFFICIENT_EVIDENCE",
		"STALE_EVIDENCE", "INTENT_INVALID", "UNAUTHENTICATED", "WRITES_DISABLED", "OUT_OF_SCOPE", "NOT_AUTHORIZED", "NOT_FOUND_OR_NOT_AUTHORIZED",
		"PLAN_INVALID", "NO_PERMITTING_RULE", "RATE_LIMITED", "INTERNAL_ERROR", "MODE_NOT_ENABLED"}
	for _, c := range core17 {
		if r, err := decide.Core(c); err != nil || r.String() != c {
			t.Fatalf("core code %s rejected", c)
		}
	}
	for _, bad := range []string{"", "NONE", "POLICY_SUPERSEDED", "CONTRADICTORY_STATE", "prohibited_capability", "fin.reason.x"} {
		if _, err := decide.Core(bad); err == nil {
			t.Fatalf("%q accepted as a core code", bad)
		}
	}
	for _, ok := range []string{"fin.reason.open_case_exists", "fin.reason.a"} {
		if _, err := decide.Pack("fin", ok); err != nil {
			t.Fatalf("%s rejected: %v", ok, err)
		}
	}
	for _, bad := range []string{"tel.reason.x", "fin.reason.", "fin.reason.X", "NO_PERMITTING_RULE", "fin.x.y", "fin.reason.a b"} {
		if _, err := decide.Pack("fin", bad); err == nil {
			t.Fatalf("%q accepted as a fin code", bad)
		}
	}
}

// PERD §23.3: the outcome and the core reason of a failed gate are fixed by the step.
func TestFailAtGate(t *testing.T) {
	want := map[decide.GateStep]struct {
		outcome decide.Outcome
		reason  string
	}{
		decide.T1AdmittedIntent: {decide.Reject, "INTENT_INVALID"}, decide.T2Session: {decide.Reject, "UNAUTHENTICATED"},
		decide.T3WritesEnabled: {decide.Unknown, "WRITES_DISABLED"}, decide.T4Capability: {decide.Reject, "PROHIBITED_CAPABILITY"},
		decide.T5Scope: {decide.Reject, "OUT_OF_SCOPE"}, decide.T6TechnicalAuthority: {decide.Reject, "NOT_AUTHORIZED"},
	}
	for step, w := range want {
		d, err := decide.FailAtGate(step, 2)
		if err != nil || d.Outcome() != w.outcome || len(d.Reasons()) != 1 || d.Reasons()[0].String() != w.reason {
			t.Fatalf("step %d: %+v %v", step, d, err)
		}
		if d.Authority().AllPass() {
			t.Fatalf("step %d left every layer at PASS", step)
		}
	}
	for _, bad := range []decide.GateStep{0, 7, -1} {
		if _, err := decide.FailAtGate(bad, 1); !errors.Is(err, decide.ErrBadGateStep) {
			t.Fatalf("step %d accepted", bad)
		}
	}
}
