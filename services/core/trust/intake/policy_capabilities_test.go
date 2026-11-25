package intake

// A306 (the per-step policy of noema#572's follow-up; REQ-TRU-004, REQ-TRU-003, REQ-TRU-007, REQ-NFR-002; INV-03, INV-09, INV-16, INV-21): T11 evaluates the tenant's policy for
// every capability of the authorized set (the intent's own and the pack action graph's), not only the intent's own, and the per-capability results become the one T11 outcome.
// PERD 23.3 and 23.4 are silent about how they combine; the rule under test is the safer reading, ratified by nobody yet (see status/TRU-07-a306-per-step-policy.md):
// the most restrictive effect wins (REJECT, ESCALATE, CONFIRM, WAIT, PERMIT: the within-class order of PERD 23.4), PermitMatched only if every capability had a PERMIT, and
// the rules and versions of every capability are recorded once. Every value is SYNTHETIC.

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/Noemai-org/noema/services/core/trust/decide"
	"github.com/Noemai-org/noema/services/core/trust/policy"
)

const capLookup = "fin.cap.lookup_transaction" // a READ capability the pack's action graph may contain

// capPolicy answers the Policies, Capabilities (Authorized) and Dispatches ports per capability. A capability it holds no answer for gets the zero PolicyEvaluation, which is
// not an answer the step can use, so a query the test did not expect ends the evaluation in UNKNOWN.
type capPolicy struct {
	*fake
	by        map[string]PolicyEvaluation
	failFor   map[string]bool
	panicFor  map[string]bool
	zeroLimit map[string]bool // the ledger answers Limit 0 for these (a contract breach by the adapter)
	queries   []PolicyQuery
	authAsked int
}

func (p *capPolicy) Evaluate(_ context.Context, q PolicyQuery) (PolicyEvaluation, error) {
	p.queries = append(p.queries, q)
	if p.panicFor[q.CapabilityID] {
		panic("injected panic in Policies.Evaluate for " + q.CapabilityID)
	}
	if p.failFor[q.CapabilityID] {
		return PolicyEvaluation{}, errFault
	}
	return p.by[q.CapabilityID], nil
}

func (p *capPolicy) Authorized(ctx context.Context, a AdmittedIntent) ([]string, error) {
	p.authAsked++
	return p.fake.Authorized(ctx, a)
}

func (p *capPolicy) Count(ctx context.Context, k DispatchKey) (DispatchCount, error) {
	if p.zeroLimit[k.CapabilityID] {
		p.fake.gotDispatch = append(p.fake.gotDispatch, k)
		return DispatchCount{Used: 0, Limit: 0}, nil
	}
	return p.fake.Count(ctx, k)
}

func (p *capPolicy) queried() []string {
	out := []string{}
	for _, q := range p.queries {
		out = append(out, q.CapabilityID)
	}
	return out
}

// newWriteEv is an evaluation for a write: the intent names `own`, the authorized set is `authorized`, and the evidence T9 would have recorded is in the basis.
func newWriteEv(t *testing.T, p *capPolicy, own string, authorized ...string) *evaluation {
	t.Helper()
	if p.fake == nil {
		p.fake = &fake{}
	}
	p.fake.authorized = authorized
	ev := newEv(t, p.fake)
	ev.svc.Ports.Policies, ev.svc.Ports.Capabilities, ev.svc.Ports.Dispatches = p, p, p
	ev.withCapability(own)
	ev.late.needsWrite = true
	ev.intent.TargetRef, ev.intent.CorrelationID = "txn_synth0001", "corr_synth0001"
	recordEvidence(t, ev)
	return ev
}

func ruleOf(id, version string) decide.PolicyRuleVersion {
	return decide.PolicyRuleVersion{RuleID: id, PolicyID: "fin.pol", Version: version}
}

// verdict is one capability's own policy result: the rule that decided is `rule_<name>`, the reason code `fin.reason.<name>_<effect>`.
func verdict(name string, e policy.Effect) PolicyEvaluation {
	p := PolicyEvaluation{Effect: e, Rules: []decide.PolicyRuleVersion{ruleOf("rule_"+name, "3.2")}, Versions: vers, PermitMatched: true}
	if e != policy.Permit {
		p.ReasonCode = fmt.Sprintf("fin.reason.%s_%s", name, lower(e))
	}
	return p
}

func lower(e policy.Effect) string {
	return map[policy.Effect]string{policy.Reject: "reject", policy.Escalate: "escalate", policy.Confirm: "confirm", policy.Wait: "wait", policy.Permit: "permit"}[e]
}

func layersOf(o decide.Outcome) decide.Authority {
	switch o {
	case decide.Confirm:
		return allPassLayers
	case decide.Wait:
		return waitLayers
	}
	return rejectLayers
}

// ---------------------------------------------------------------------------------------------------------------------------------------------------------------
// The defect: a graph step on another capability was never policy-evaluated.

// planFor is the plan of the two capabilities, both on the intent's target.
func planFor(f *f13) {
	f.authorized = []string{capCase, capRoute}
	f.plan = writePlan(stepOn(capCase, "txn_synth0001"), stepOn(capRoute, "txn_synth0001"))
	f.rec = RecordResult{DecisionID: "dec_synth0001", GrantIDs: []string{"grt_synth0001", "grt_synth0002"}}
}

// policyEval13 is an evaluation whose T11, T13, T14 and T15 are the real steps (T12 and the steps before T11 continue), over the ports of f and the policy of p.
func policyEval13(t *testing.T, f *f13, p *capPolicy) *evaluation {
	t.Helper()
	ev := newEval13(t, f)
	if p.fake == nil {
		p.fake = &fake{}
	}
	ev.svc.Ports.Policies = p
	ev.bb = freshBuilder(t, true, false)
	ev.capability = &Capability{ID: capCase, Registered: true, RoleAllowed: true}
	return ev
}

func (e *evaluation) policySteps() decide.Steps {
	s := e.lateSteps()
	s[decide.T11Policy-1] = e.t11
	return s
}

// The graph step's capability is denied for the session's role by the tenant policy; the root is permitted. The evaluation must not ALLOW: it ends at T11 with the outcome
// the rule's effect gives, the Planner is never asked, and nothing is recorded or granted.
func TestT11AGraphCapabilityDeniedByPolicyWhileTheRootIsPermittedIsNotAllowed(t *testing.T) {
	for _, c := range []struct {
		name    string
		graph   PolicyEvaluation
		outcome decide.Outcome
		reasons []string
	}{
		{"an explicit REJECT", verdict("graph", policy.Reject), decide.Reject, []string{"fin.reason.graph_reject"}},
		{"the policy's own default deny (no rule matched)", PolicyEvaluation{Effect: policy.Reject, ReasonCode: policy.NoPermittingRule, Versions: vers}, decide.Reject, []string{"NO_PERMITTING_RULE"}},
		{"an ESCALATE", verdict("graph", policy.Escalate), decide.Escalate, []string{"fin.reason.graph_escalate"}},
		{"a CONFIRM", verdict("graph", policy.Confirm), decide.Confirm, []string{"fin.reason.graph_confirm"}},
		{"a WAIT", func() PolicyEvaluation { p := verdict("graph", policy.Wait); p.ReevaluateAt = tNow + 300; return p }(), decide.Wait, []string{"fin.reason.graph_wait"}},
	} {
		f := newFake13()
		planFor(f)
		f.rec = RecordResult{DecisionID: "dec_synth0005"} // an ESCALATE is recorded, with no grant (A534); no other outcome here is
		p := &capPolicy{by: map[string]PolicyEvaluation{capCase: verdict("root", policy.Permit), capRoute: c.graph}}
		ev := policyEval13(t, f, p)
		res := ev.run(tctx(t), ev.policySteps())
		if res.Decision.Outcome() == decide.Allow || res.Decision.Outcome() != c.outcome || !reflect.DeepEqual(codes13(res.Decision), c.reasons) {
			t.Errorf("%s: %s %v, want %s %v", c.name, res.Decision.Outcome(), codes13(res.Decision), c.outcome, c.reasons)
		}
		if lastTrace(res).Step != decide.T11Policy || lastTrace(res).Result != decide.StepTerminal {
			t.Errorf("%s: trace %v, want it to end at T11 TERMINAL", c.name, res.Trace)
		}
		wantRecords, wantID := 0, ""
		if c.outcome == decide.Escalate {
			wantRecords, wantID = 1, "dec_synth0005"
		}
		if f.called("Planner.Plan") != 0 || f.called("Recorder.Record") != wantRecords || res.DecisionID != wantID || len(res.GrantIDs) != 0 {
			t.Errorf("%s: the plan was asked or something else was recorded: %v %q %v; want %d Record call(s) and ID %q", c.name, f.calls, res.DecisionID, res.GrantIDs, wantRecords, wantID)
		}
	}
}

// The same evaluation with the graph capability permitted is ALLOW with both grants: the refusal above is the policy of the graph step, not the setup.
func TestT11EveryCapabilityPermittedIsAllowedWithBothStepsGranted(t *testing.T) {
	f := newFake13()
	planFor(f)
	p := &capPolicy{by: map[string]PolicyEvaluation{capCase: verdict("root", policy.Permit), capRoute: verdict("graph", policy.Permit)}}
	ev := policyEval13(t, f, p)
	res := ev.run(tctx(t), ev.policySteps())
	if res.Decision.Outcome() != decide.Allow || f.called("Recorder.Record") != 1 || len(res.GrantIDs) != 2 {
		t.Fatalf("%s %v, Record called %d times, grants %v; want ALLOW, one Record, two grants", res.Decision.Outcome(), codes13(res.Decision), f.called("Recorder.Record"), res.GrantIDs)
	}
	if got := p.queried(); !reflect.DeepEqual(got, []string{capCase, capRoute}) {
		t.Errorf("the policy was asked about %v, want both capabilities, sorted", got)
	}
	base, err := ev.bb.Basis()
	if err != nil {
		t.Fatal(err)
	}
	if want := []decide.PolicyRuleVersion{ruleOf("rule_root", "3.2"), ruleOf("rule_graph", "3.2")}; !reflect.DeepEqual(base.PolicyRules, want) {
		t.Errorf("the ALLOW's basis names %v, want the rules of both capabilities %v", base.PolicyRules, want)
	}
}

// ---------------------------------------------------------------------------------------------------------------------------------------------------------------
// How the per-capability effects combine.

var effectsStrongestFirst = []policy.Effect{policy.Reject, policy.Escalate, policy.Confirm, policy.Wait, policy.Permit} // PERD 23.4, within the winning class

var outcomeOf = map[policy.Effect]decide.Outcome{policy.Reject: decide.Reject, policy.Escalate: decide.Escalate, policy.Confirm: decide.Confirm, policy.Wait: decide.Wait}

// waitAt is the time each capability's WAIT is re-evaluated: the intent's own capability later than the graph's.
var waitAt = map[string]int64{capCase: tNow + 600, capRoute: tNow + 300}

func waitingVerdict(name, capID string) PolicyEvaluation {
	p := verdict(name, policy.Wait)
	p.ReevaluateAt = waitAt[capID]
	return p
}

func answerFor(name, capID string, e policy.Effect) PolicyEvaluation {
	if e == policy.Wait {
		return waitingVerdict(name, capID)
	}
	return verdict(name, e)
}

// For every pair of effects, in both orders and whichever capability is the intent's own, the more restrictive effect is the outcome, with the reason code of the capability
// that gave it (the first in sorted order when both give the same effect; the earliest re-evaluation for two WAITs); a PERMIT on every capability goes on.
func TestT11TheMostRestrictiveEffectOfTheCapabilitiesIsTheOutcome(t *testing.T) {
	for x, caseEffect := range effectsStrongestFirst {
		for y, routeEffect := range effectsStrongestFirst {
			for _, own := range []string{capCase, capRoute} {
				name := fmt.Sprintf("%s for %s and %s for %s, own %s", caseEffect, "case", routeEffect, "route", own)
				p := &capPolicy{by: map[string]PolicyEvaluation{capCase: answerFor("case", capCase, caseEffect), capRoute: answerFor("route", capRoute, routeEffect)}}
				ev := newWriteEv(t, p, own, capCase, capRoute)
				d, trace := run(t, ev, decide.T11Policy)
				winner, win := "case", caseEffect
				if y < x || (x == y && win == policy.Wait) {
					winner, win = "route", routeEffect
				}
				if win == policy.Permit {
					wentOn(t, name, trace, decide.T11Policy)
					continue
				}
				stopped(t, name, d, trace, decide.T11Policy, outcomeOf[win], []string{fmt.Sprintf("fin.reason.%s_%s", winner, lower(win))}, layersOf(outcomeOf[win]))
				wantAt := int64(0)
				if win == policy.Wait {
					wantAt = waitAt[map[string]string{"case": capCase, "route": capRoute}[winner]]
				}
				if d.ReevaluateAt() != wantAt {
					t.Errorf("%s: reevaluate_at %d, want %d", name, d.ReevaluateAt(), wantAt)
				}
			}
		}
	}
}

// The order itself, written out (PERD 23.4: REJECT > ESCALATE > CONFIRM > WAIT > PERMIT), so that the table above cannot be right by repeating one mistake.
func TestT11TheOrderOfTheEffectsIsTheOneOfPERD234(t *testing.T) {
	for _, c := range []struct {
		a, b policy.Effect
		want decide.Outcome
	}{
		{policy.Permit, policy.Reject, decide.Reject},
		{policy.Escalate, policy.Reject, decide.Reject},
		{policy.Confirm, policy.Escalate, decide.Escalate},
		{policy.Wait, policy.Confirm, decide.Confirm},
		{policy.Permit, policy.Wait, decide.Wait},
	} {
		p := &capPolicy{by: map[string]PolicyEvaluation{capCase: verdict("case", c.a), capRoute: verdict("route", c.b)}}
		if c.a == policy.Wait {
			p.by[capCase] = waitingVerdict("case", capCase)
		}
		if c.b == policy.Wait {
			p.by[capRoute] = waitingVerdict("route", capRoute)
		}
		d, _ := run(t, newWriteEv(t, p, capCase, capCase, capRoute), decide.T11Policy)
		if d.Outcome() != c.want {
			t.Errorf("%s with %s: %s, want %s", c.a, c.b, d.Outcome(), c.want)
		}
	}
}

// Three capabilities: the strongest of all three, and the reason of the first of the capabilities that gave that effect.
func TestT11ThreeCapabilitiesTheStrongestWinsAndTheFirstOfTiesGivesTheReason(t *testing.T) {
	p := &capPolicy{by: map[string]PolicyEvaluation{tWriteCap: verdict("adjust", policy.Permit), capCase: verdict("case", policy.Escalate), capRoute: verdict("route", policy.Escalate)}}
	d, trace := run(t, newWriteEv(t, p, capRoute, tWriteCap, capCase, capRoute), decide.T11Policy)
	stopped(t, "ESCALATE from two of three", d, trace, decide.T11Policy, decide.Escalate, []string{"fin.reason.case_escalate"}, rejectLayers)
	if got := p.queried(); !reflect.DeepEqual(got, []string{tWriteCap, capCase, capRoute}) {
		t.Errorf("queried %v, want all three in sorted order", got)
	}
}

// Two WAITs: the evaluation is re-evaluated at the earliest of the times, with the reason of that capability.
func TestT11TwoWaitsAreReevaluatedAtTheEarliestTime(t *testing.T) {
	p := &capPolicy{by: map[string]PolicyEvaluation{capCase: waitingVerdict("case", capCase), capRoute: waitingVerdict("route", capRoute)}}
	d, trace := run(t, newWriteEv(t, p, capCase, capCase, capRoute), decide.T11Policy)
	stopped(t, "two WAITs", d, trace, decide.T11Policy, decide.Wait, []string{"fin.reason.route_wait"}, waitLayers)
	if d.ReevaluateAt() != tNow+300 {
		t.Errorf("reevaluate_at %d, want %d", d.ReevaluateAt(), tNow+300)
	}
}

// INV-03: a CONFIRM stands only with a PERMIT matched, and with several capabilities that means every one of them: a WAIT rule that matched for the graph step with no PERMIT
// for it leaves the CONFIRM of the root without authority. The result is the default deny, whichever capability is the intent's own.
func TestT11AConfirmStandsOnlyWhenEveryCapabilityHadAPermit(t *testing.T) {
	for _, own := range []string{capCase, capRoute} {
		noPermit := waitingVerdict("route", capRoute)
		noPermit.PermitMatched = false
		p := &capPolicy{by: map[string]PolicyEvaluation{capCase: verdict("case", policy.Confirm), capRoute: noPermit}}
		d, trace := run(t, newWriteEv(t, p, own, capCase, capRoute), decide.T11Policy)
		stopped(t, "CONFIRM and a WAIT with no PERMIT, own "+own, d, trace, decide.T11Policy, decide.Reject, []string{"NO_PERMITTING_RULE"}, rejectLayers)

		p = &capPolicy{by: map[string]PolicyEvaluation{capCase: verdict("case", policy.Confirm), capRoute: verdict("route", policy.Wait)}}
		p.by[capRoute] = waitingVerdict("route", capRoute)
		d, trace = run(t, newWriteEv(t, p, own, capCase, capRoute), decide.T11Policy)
		stopped(t, "CONFIRM and a WAIT, each with a PERMIT, own "+own, d, trace, decide.T11Policy, decide.Confirm, []string{"fin.reason.case_confirm"}, allPassLayers)
	}
}

// Each capability's own default deny is applied before the results are combined: a PERMIT that names no rule, a REJECT that says nothing and a CONFIRM with no PERMIT for one
// capability are REJECT NO_PERMITTING_RULE, however well the other capabilities permit (the rules of the others must not make up for it).
func TestT11AnEmptyPermitOrAnUnauthorizedConfirmForOneCapabilityIsTheDefaultDeny(t *testing.T) {
	for name, bad := range map[string]PolicyEvaluation{
		"a PERMIT that names no deciding rule": {Effect: policy.Permit, Versions: vers, PermitMatched: true},
		"a REJECT with no reason code":         func() PolicyEvaluation { p := verdict("route", policy.Reject); p.ReasonCode = ""; return p }(),
		"a CONFIRM with no PERMIT matched":     func() PolicyEvaluation { p := verdict("route", policy.Confirm); p.PermitMatched = false; return p }(),
	} {
		p := &capPolicy{by: map[string]PolicyEvaluation{capCase: verdict("case", policy.Permit), capRoute: bad}}
		d, trace := run(t, newWriteEv(t, p, capCase, capCase, capRoute), decide.T11Policy)
		stopped(t, name, d, trace, decide.T11Policy, decide.Reject, []string{"NO_PERMITTING_RULE"}, rejectLayers)
	}
}

// ---------------------------------------------------------------------------------------------------------------------------------------------------------------
// The basis (INV-09, REQ-TRU-007)

// The rules and the policy versions of every capability are recorded in the basis once each, in the order they first appear (capabilities sorted), whatever the outcome.
func TestT11EveryCapabilitysRulesAndVersionsAreRecordedOnceEach(t *testing.T) {
	ruleC := ruleOf("rule_c", "3.3")
	for _, effects := range [][2]policy.Effect{{policy.Permit, policy.Permit}, {policy.Permit, policy.Reject}, {policy.Escalate, policy.Escalate}} {
		root, graph := verdict("case", effects[0]), verdict("route", effects[1])
		root.Rules = []decide.PolicyRuleVersion{ruleA, ruleB}
		graph.Rules = []decide.PolicyRuleVersion{ruleB, ruleC} // rule_b decided for both capabilities
		graph.Versions = []decide.PolicyVersionRef{{PolicyID: "fin.pol", Version: "3.2"}, {PolicyID: "fin.pol", Version: "3.3"}}
		p := &capPolicy{by: map[string]PolicyEvaluation{capCase: root, capRoute: graph}}
		ev := newWriteEv(t, p, capCase, capCase, capRoute)
		run(t, ev, decide.T11Policy)
		base, err := ev.bb.Basis()
		if err != nil {
			t.Fatal(err)
		}
		if want := []decide.PolicyRuleVersion{ruleA, ruleB, ruleC}; !reflect.DeepEqual(base.PolicyRules, want) {
			t.Errorf("%v and %v: the basis names %v, want each rule once: %v", effects[0], effects[1], base.PolicyRules, want)
		}
		wantVersions := []decide.PolicyVersionRef{{PolicyID: "fin.pol", Version: "3.1"}, {PolicyID: "fin.pol", Version: "3.2"}, {PolicyID: "fin.pol", Version: "3.3"}}
		if !reflect.DeepEqual(ev.policyVersions, wantVersions) {
			t.Errorf("%v and %v: policy versions %v, want each once: %v", effects[0], effects[1], ev.policyVersions, wantVersions)
		}
		if err := ev.bb.RecordPolicy(nil); err == nil {
			t.Errorf("the policy was not recorded exactly once")
		}
	}
}

// A CONFIRM from the graph step carries the evaluated basis with the rules of both capabilities, and it checks out.
func TestT11AConfirmFromAGraphStepCarriesTheBasisOfEveryCapability(t *testing.T) {
	p := &capPolicy{by: map[string]PolicyEvaluation{capCase: verdict("case", policy.Permit), capRoute: verdict("route", policy.Confirm)}}
	ev := newWriteEv(t, p, capCase, capCase, capRoute)
	d, _ := run(t, ev, decide.T11Policy)
	if d.Outcome() != decide.Confirm {
		t.Fatalf("%s", d.Outcome())
	}
	if err := ev.bb.Check(d); err != nil {
		t.Fatalf("the basis does not match the evaluated set: %v", err)
	}
	if want := []decide.PolicyRuleVersion{ruleOf("rule_case", "3.2"), ruleOf("rule_route", "3.2")}; !reflect.DeepEqual(d.Basis().PolicyRules, want) {
		t.Errorf("basis rules %v, want %v", d.Basis().PolicyRules, want)
	}
}

// ---------------------------------------------------------------------------------------------------------------------------------------------------------------
// The queries, and the one read of the authorized set

// One query for each capability of the set, with the capability ID set and everything else the request's; the set is the intent's own capability and the graph's, sorted.
func TestT11AsksThePolicyOnceForEachCapabilityOfTheAuthorizedSet(t *testing.T) {
	p := &capPolicy{by: map[string]PolicyEvaluation{tWriteCap: verdict("adjust", policy.Permit), capCase: verdict("case", policy.Permit), capRoute: verdict("route", policy.Permit)}}
	ev := newWriteEv(t, p, capRoute, capCase, tWriteCap, capCase) // unsorted, a repeat, and the intent's own capability is also in the set
	run(t, ev, decide.T11Policy)
	var want []PolicyQuery
	for _, id := range []string{tWriteCap, capCase, capRoute} {
		want = append(want, PolicyQuery{TenantID: tTenant, IntentID: tIntentID, IntentVersion: 2, IntentNode: tNode, CapabilityID: id, SnapshotID: tSnapshot, Now: tNow})
	}
	if !reflect.DeepEqual(p.queries, want) {
		t.Errorf("queries %+v, want %+v", p.queries, want)
	}
}

// T11 comes before T12: the set is read once for the evaluation, by whichever step needs it first, and T12 and T14 see the same set.
func TestT11AndT12ShareOneReadOfTheAuthorizedSet(t *testing.T) {
	p := &capPolicy{by: map[string]PolicyEvaluation{capCase: verdict("case", policy.Permit), capRoute: verdict("route", policy.Permit)}, fake: &fake{dispatch: DispatchCount{Used: 0, Limit: 1}}}
	ev := newWriteEv(t, p, capCase, capRoute)
	if r := ev.t11(tctx(t)); !reflect.DeepEqual(r, decide.Continue()) {
		t.Fatalf("T11 did not go on: %+v", r)
	}
	p.fake.authorized = []string{capLookup} // the registry changes between the two questions
	ev.t12(tctx(t))
	if p.authAsked != 1 {
		t.Errorf("the authorized set was read %d times, want once for the evaluation", p.authAsked)
	}
	if got := len(p.fake.gotDispatch); got != 2 {
		t.Errorf("the ledger was asked %d times, want 2 (the set T11 read: %+v)", got, p.fake.gotDispatch)
	}
}

// A read has an empty plan, so no graph step can use a capability: only the intent's own capability is evaluated, as before, and the authorized set is not read.
func TestT11AReadEvaluatesOnlyTheIntentsOwnCapability(t *testing.T) {
	// a graph capability the tenant policy would deny does not matter to a read
	p := &capPolicy{by: map[string]PolicyEvaluation{capCase: verdict("case", policy.Permit), capRoute: verdict("route", policy.Reject)}}
	ev := newWriteEv(t, p, capCase, capCase, capRoute)
	ev.late.needsWrite = false
	_, trace := run(t, ev, decide.T11Policy)
	wentOn(t, "a read", trace, decide.T11Policy)
	if got := p.queried(); !reflect.DeepEqual(got, []string{capCase}) {
		t.Errorf("a read asked the policy about %v, want only the intent's own capability", got)
	}
	if p.authAsked != 0 {
		t.Errorf("the authorized set was read %d times for a read, want none", p.authAsked)
	}

	// an intent that names no capability: one query with no capability ID, as before
	p = &capPolicy{by: map[string]PolicyEvaluation{"": verdict("node", policy.Permit)}}
	ev = newWriteEv(t, p, "")
	ev.capability, ev.late.needsWrite = nil, false
	_, trace = run(t, ev, decide.T11Policy)
	wentOn(t, "a read with no capability", trace, decide.T11Policy)
	if got := p.queried(); !reflect.DeepEqual(got, []string{""}) {
		t.Errorf("queried %v, want one query with no capability ID", got)
	}
}

// A write that names no capability and whose authorized set is empty has nothing to ask about but the intent node: one query with no capability ID, as before.
func TestT11AWriteWithNoCapabilityAndNoAuthorizedSetAsksOnceAboutTheNode(t *testing.T) {
	p := &capPolicy{by: map[string]PolicyEvaluation{"": verdict("node", policy.Permit)}}
	ev := newWriteEv(t, p, "")
	ev.capability = nil
	_, trace := run(t, ev, decide.T11Policy)
	wentOn(t, "a write with no capability", trace, decide.T11Policy)
	if got := p.queried(); !reflect.DeepEqual(got, []string{""}) {
		t.Errorf("queried %v, want one query with no capability ID", got)
	}
}

// ---------------------------------------------------------------------------------------------------------------------------------------------------------------
// Fail closed (INV-16)

// An authorized set that cannot be read, a policy error or panic for any one capability, and an answer the step cannot use for any one capability end in UNKNOWN
// INTERNAL_ERROR at T11: never ALLOW, never "the others permit", and nothing is recorded in the basis.
func TestT11FailsClosedWhenAnyCapabilityCannotBeEvaluated(t *testing.T) {
	good := func() map[string]PolicyEvaluation {
		return map[string]PolicyEvaluation{capCase: verdict("case", policy.Permit), capRoute: verdict("route", policy.Permit)}
	}
	for _, c := range []struct {
		name string
		set  func(p *capPolicy)
	}{
		{"the authorized set cannot be read", func(p *capPolicy) { p.fake.authErr = errFault }},
		{"a policy error for the root", func(p *capPolicy) { p.failFor = map[string]bool{capCase: true} }},
		{"a policy error for the graph capability", func(p *capPolicy) { p.failFor = map[string]bool{capRoute: true} }},
		{"a policy panic for the graph capability", func(p *capPolicy) { p.panicFor = map[string]bool{capRoute: true} }},
		{"no answer for the graph capability (an empty effect)", func(p *capPolicy) { p.by[capRoute] = PolicyEvaluation{} }},
		{"an effect nobody defined for the graph capability", func(p *capPolicy) {
			p.by[capRoute] = PolicyEvaluation{Effect: "ALLOW", Rules: []decide.PolicyRuleVersion{ruleA}}
		}},
		{"a deciding rule with no ID for the graph capability", func(p *capPolicy) {
			g := verdict("route", policy.Permit)
			g.Rules = []decide.PolicyRuleVersion{{PolicyID: "fin.pol", Version: "1"}}
			p.by[capRoute] = g
		}},
		{"a deciding rule with no version for the graph capability", func(p *capPolicy) {
			g := verdict("route", policy.Reject)
			g.Rules = []decide.PolicyRuleVersion{{RuleID: "r", PolicyID: "fin.pol"}}
			p.by[capRoute] = g
		}},
		{"a WAIT of the graph capability with no time in the future", func(p *capPolicy) {
			g := verdict("route", policy.Wait)
			g.ReevaluateAt = tNow
			p.by[capRoute] = g
		}},
		{"an ESCALATE of the graph capability with a reason that is no code", func(p *capPolicy) {
			p.by[capRoute] = verdict("route", policy.Escalate)
			g := p.by[capRoute]
			g.ReasonCode = "because"
			p.by[capRoute] = g
		}},
	} {
		p := &capPolicy{by: good(), fake: &fake{}}
		ev := newWriteEv(t, p, capCase, capCase, capRoute)
		c.set(p)
		d, trace := run(t, ev, decide.T11Policy)
		failedClosed(t, c.name, d, trace, decide.T11Policy)
		if err := ev.bb.RecordPolicy(nil); err != nil {
			t.Errorf("%s: the policy was recorded although the evaluation failed: %v", c.name, err)
		}
	}
}

// ---------------------------------------------------------------------------------------------------------------------------------------------------------------
// The contract of the Dispatches adapter for a READ capability in the authorized set (the Security Reviewer's and Codex's non-blocking note on noema#572). T12 asks the ledger
// for every capability of the set, reads included; the adapter must answer Limit >= 1 for a read. An answer of Limit 0 is not "no budget needed": it is an answer the step
// cannot use, and the evaluation ends in UNKNOWN INTERNAL_ERROR (fail closed). These two tests pass on the code of noema#572; they pin the contract.

func TestT12AReadCapabilityInTheAuthorizedSetWithALimitOfAtLeastOneGoesOn(t *testing.T) {
	p := &capPolicy{fake: &fake{dispatch: DispatchCount{Used: 0, Limit: 1}}}
	ev := newWriteEv(t, p, tWriteCap, capLookup)
	_, trace := run(t, ev, decide.T12Clocks)
	wentOn(t, "a read capability with Limit 1", trace, decide.T12Clocks)
	asked := map[string]bool{}
	for _, k := range p.fake.gotDispatch {
		asked[k.CapabilityID] = true
	}
	if !asked[capLookup] || !asked[tWriteCap] {
		t.Errorf("the ledger was asked about %v, want the read capability and the intent's own", p.fake.gotDispatch)
	}
}

func TestT12AReadCapabilityWhoseDispatchesAnswerIsLimitZeroFailsClosed(t *testing.T) {
	p := &capPolicy{fake: &fake{dispatch: DispatchCount{Used: 0, Limit: 1}}, zeroLimit: map[string]bool{capLookup: true}}
	ev := newWriteEv(t, p, tWriteCap, capLookup)
	d, trace := run(t, ev, decide.T12Clocks)
	failedClosed(t, "a read capability with Limit 0", d, trace, decide.T12Clocks)
}
