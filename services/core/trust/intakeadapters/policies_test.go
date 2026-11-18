package intakeadapters

// The Policies adapter of the intake (T11; REQ-TRU-004, REQ-TRU-005, REQ-TRU-007; A306; INV-03, INV-09, INV-16, INV-19, INV-21). The shipped fintech pack, the real policy engine and a real
// PostgreSQL publication store, read as each tenant's own Trust login. The two sources with no production implementation yet (the admission time and the facts) are the test's own.

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/Noemai-org/noema/services/core/internal/predicate"
	"github.com/Noemai-org/noema/services/core/trust/decide"
	"github.com/Noemai-org/noema/services/core/trust/intake"
	"github.com/Noemai-org/noema/services/core/trust/policy"
	"github.com/Noemai-org/noema/services/core/trust/policystore"
)

const (
	lookup    = "fin.cap.lookup_transaction"
	complaint = "fin.cap.log_complaint_reference"
	caseWrite = "fin.cap.create_escalation_case"
	routing   = "fin.cap.route_to_internal_owner"
	transfer  = "fin.cap.transfer_funds"
)

func ref(id, version string) decide.PolicyRuleVersion {
	return decide.PolicyRuleVersion{RuleID: id, PolicyID: policyID, Version: version}
}

func evaluate(t *testing.T, p *Policies, q intake.PolicyQuery) intake.PolicyEvaluation {
	t.Helper()
	got, err := p.Evaluate(ctx(t), q)
	if err != nil {
		t.Fatalf("Evaluate(%s): %v", q.CapabilityID, err)
	}
	return got
}

// REQ-TRU-004, REQ-TRU-007, INV-09: the tenant's active version is the version it published; the answer names the rule that permitted and the version it came from.
func TestAReadIsPermittedUnderTheVersionTheTenantPublishedAndTheAnswerNamesTheRuleAndTheVersion(t *testing.T) {
	w := newWorld(t)
	w.publish(t, tenantA, "3.1", t1)
	p := w.policies(t, book(t, realPack(t)), &adm{at: t1 + 60}, &facts{master: resolvedMaster()})
	got := evaluate(t, p, query(lookup, late))
	want := intake.PolicyEvaluation{Effect: policy.Permit, ReasonCode: "fin.reason.permitted", PermitMatched: true,
		Rules: []decide.PolicyRuleVersion{ref("fin.pol.default.permit_reads", "3.1")}, Versions: []decide.PolicyVersionRef{{PolicyID: policyID, Version: "3.1"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

// REQ-TRU-005, D8: the version in force at Now is the newest one published at or before Now. A version published after the reading is not in force at it, and a version that a newer
// one replaced does not apply to a decision made after the replacement. The version in force when the intent was admitted is named as well (INV-09).
func TestTheVersionInForceAtTheReadingIsTheNewestPublishedByThen(t *testing.T) {
	w := newWorld(t)
	w.publish(t, tenantA, "3.1", t1)
	w.publish(t, tenantA, "3.2", t2)
	denyReads := variantPack(t, "3.2", func(rules []any) []any {
		out := []any{}
		for _, r := range rules {
			if r.(map[string]any)["rule_id"] != "fin.pol.default.permit_reads" {
				out = append(out, r)
			}
		}
		return out
	})
	p := w.policies(t, book(t, realPack(t), denyReads), &adm{at: t1 + 60}, &facts{master: resolvedMaster()})
	got := evaluate(t, p, query(lookup, late))
	if got.Effect != policy.Reject || got.ReasonCode != policy.NoPermittingRule || got.PermitMatched || len(got.Rules) != 0 ||
		!reflect.DeepEqual(got.Versions, []decide.PolicyVersionRef{{PolicyID: policyID, Version: "3.1"}, {PolicyID: policyID, Version: "3.2"}}) {
		t.Fatalf("after 3.2 was published, a read is denied under 3.2 (3.1 is the version in force at admission): %+v", got)
	}
	// the same intent, read while 3.1 was the newest published: permitted under 3.1, and 3.2 is not in force yet
	got = evaluate(t, p, query(lookup, t2-1))
	if got.Effect != policy.Permit || !reflect.DeepEqual(got.Rules, []decide.PolicyRuleVersion{ref("fin.pol.default.permit_reads", "3.1")}) ||
		!reflect.DeepEqual(got.Versions, []decide.PolicyVersionRef{{PolicyID: policyID, Version: "3.1"}}) {
		t.Fatalf("before 3.2 was published, a read is permitted under 3.1: %+v", got)
	}
}

// D8, REQ-TRU-007, INV-09: a policy published later does not apply to an input admitted earlier for AT_ADMISSION rules, and one published before it does. Both versions
// are named when an AT_DECISION rule of the newest and an AT_ADMISSION rule of the one in force at admission both took part.
func TestAnAdmissionRuleOfALaterVersionDoesNotApplyToAnEarlierAdmissionAndOneOfAnEarlierVersionDoes(t *testing.T) {
	w := newWorld(t)
	w.publish(t, tenantA, "3.1", t1)
	w.publish(t, tenantA, "3.2", t2)
	hold := rule("fin.pol.tenant.admission_hold", "TENANT_SECURITY", "AT_ADMISSION", "fin.cap.lookup_account", "true", "REJECT", "fin.reason.admission_hold", nil)
	b := book(t, realPack(t), variantPack(t, "3.2", withRules(hold)))
	facts := &facts{master: resolvedMaster()}
	// admitted between the two publications: 3.2 was not in force, so its hold does not apply; the AT_DECISION rules are 3.2's
	got := evaluate(t, w.policies(t, b, &adm{at: t2 - 1}, facts), query("fin.cap.lookup_account", late))
	if got.Effect != policy.Permit || !reflect.DeepEqual(got.Rules, []decide.PolicyRuleVersion{ref("fin.pol.default.permit_reads", "3.2")}) ||
		!reflect.DeepEqual(got.Versions, []decide.PolicyVersionRef{{PolicyID: policyID, Version: "3.1"}, {PolicyID: policyID, Version: "3.2"}}) {
		t.Fatalf("admitted before 3.2: %+v", got)
	}
	// admitted after the second publication: its hold applies
	got = evaluate(t, w.policies(t, b, &adm{at: t2 + 1}, facts), query("fin.cap.lookup_account", late))
	if got.Effect != policy.Reject || got.ReasonCode != "fin.reason.admission_hold" || len(got.Rules) < 1 || got.Rules[0] != ref("fin.pol.tenant.admission_hold", "3.2") ||
		!reflect.DeepEqual(got.Versions, []decide.PolicyVersionRef{{PolicyID: policyID, Version: "3.2"}}) {
		t.Fatalf("admitted after 3.2: %+v", got)
	}
	// admitted exactly when 3.2 was published: it was in force at that second
	if got = evaluate(t, w.policies(t, b, &adm{at: t2}, facts), query("fin.cap.lookup_account", late)); got.Effect != policy.Reject {
		t.Fatalf("admitted at the second of the publication: %+v", got)
	}
}

// An AT_ADMISSION rule of the older version still holds for an intent admitted under it, and its rule is recorded with the older version beside the newest's AT_DECISION rules (INV-09).
func TestAnAdmissionRuleOfTheVersionInForceAtAdmissionHoldsAfterANewerVersionIsPublished(t *testing.T) {
	w := newWorld(t)
	w.publish(t, tenantA, "3.1", t1)
	w.publish(t, tenantA, "3.2", t2)
	hold := rule("fin.pol.tenant.admission_hold", "TENANT_SECURITY", "AT_ADMISSION", "fin.cap.lookup_account", "true", "REJECT", "fin.reason.admission_hold", nil)
	b := book(t, variantPack(t, "3.1", withRules(hold)), variantPack(t, "3.2", nil))
	got := evaluate(t, w.policies(t, b, &adm{at: t1 + 5}, &facts{master: resolvedMaster()}), query("fin.cap.lookup_account", late))
	if got.Effect != policy.Reject || got.ReasonCode != "fin.reason.admission_hold" ||
		!reflect.DeepEqual(got.Rules, []decide.PolicyRuleVersion{ref("fin.pol.tenant.admission_hold", "3.1"), ref("fin.pol.default.permit_reads", "3.2")}) ||
		!reflect.DeepEqual(got.Versions, []decide.PolicyVersionRef{{PolicyID: policyID, Version: "3.1"}, {PolicyID: policyID, Version: "3.2"}}) {
		t.Fatalf("an intent admitted under 3.1 keeps 3.1's admission rule: %+v", got)
	}
}

// A version the tenant published with no document in the pack cannot be evaluated: it is an error and never a pass, a default or an older version. The version in force at
// admission needs its document too; an older version nobody needs does not.
func TestAPublishedVersionWithNoDocumentIsAnErrorWhereverItIsNeeded(t *testing.T) {
	w := newWorld(t)
	w.publish(t, tenantA, "3.0", t1)
	w.publish(t, tenantA, "3.1", t2)
	w.publish(t, tenantA, "3.3", t3)
	facts := &facts{master: resolvedMaster()}
	onlyOld := w.policies(t, book(t, realPack(t)), &adm{at: t2 + 1}, facts)
	if got, err := onlyOld.Evaluate(ctx(t), query(lookup, late)); !errors.Is(err, ErrNoDocument) || !reflect.DeepEqual(got, intake.PolicyEvaluation{}) {
		t.Fatalf("the newest version 3.3 has no document: %+v, %v", got, err)
	}
	// 3.3 has a document, 3.1 (in force at admission) has none
	b := book(t, variantPack(t, "3.3", nil))
	if got, err := w.policies(t, b, &adm{at: t2 + 1}, facts).Evaluate(ctx(t), query(lookup, late)); !errors.Is(err, ErrNoDocument) || !reflect.DeepEqual(got, intake.PolicyEvaluation{}) {
		t.Fatalf("the version in force at admission (3.1) has no document: %+v, %v", got, err)
	}
	// admitted after 3.3: only 3.3 is needed; 3.0 and 3.1 have no document and are not asked for
	if got := evaluate(t, w.policies(t, b, &adm{at: t3 + 1}, facts), query(lookup, late)); got.Effect != policy.Permit {
		t.Fatalf("only the newest is needed: %+v", got)
	}
}

// REQ-TRU-004, PERD 23.4: a tenant with no publication has no rule that permits: REJECT NO_PERMITTING_RULE, with no rule and no version to name.
func TestATenantWithNoPublicationIsDeniedByDefaultAndTheSameForAReadingBeforeTheFirstPublication(t *testing.T) {
	w := newWorld(t)
	p := w.policies(t, book(t, realPack(t)), &adm{at: t1}, &facts{master: resolvedMaster()})
	want := intake.PolicyEvaluation{Effect: policy.Reject, ReasonCode: policy.NoPermittingRule}
	if got := evaluate(t, p, query(lookup, late)); !reflect.DeepEqual(got, want) {
		t.Fatalf("no publication: %+v", got)
	}
	w.publish(t, tenantA, "3.1", t2)
	// a reading before the publication (and an admission before it): nothing is in force
	if got := evaluate(t, p, query(lookup, t2-1)); !reflect.DeepEqual(got, want) {
		t.Fatalf("a reading before the publication: %+v", got)
	}
	// the clock reading is the intake's and the publication time is the database's: a reading that is not after the publication is not in force, whatever the clocks mean
	if got := evaluate(t, p, query(lookup, t2)); got.Effect != policy.Permit {
		t.Fatalf("a reading at the second of the publication: %+v", got)
	}
}

// REQ-TRU-004, INV-03: no matching PERMIT is REJECT NO_PERMITTING_RULE; a prohibited capability is rejected with the pack's rule; a CONFIRM alone is the default deny and says so.
func TestNoPermittingRuleAProhibitedCapabilityAndAConfirmWithNoPermitAreAllRejected(t *testing.T) {
	w := newWorld(t)
	w.publish(t, tenantA, "3.1", t1)
	confirmOnly := variantPack(t, "3.1", withRules(rule("fin.pol.regulatory.confirm_lookup", "REGULATORY", "AT_DECISION", "fin.cap.query_switch_status", "true", "CONFIRM", "fin.reason.complaint_needs_confirmation", nil)))
	// the confirm-only rule is for a capability whose pack PERMIT is removed
	confirmOnly = func() []byte {
		var m map[string]any
		_ = json.Unmarshal(confirmOnly, &m)
		for _, r := range m["policy"].(map[string]any)["rules"].([]any) {
			rm := r.(map[string]any)
			if rm["rule_id"] == "fin.pol.default.permit_reads" {
				rm["applies_to"] = []any{lookup}
			}
		}
		out, _ := json.Marshal(m)
		return out
	}()
	p := w.policies(t, book(t, confirmOnly), &adm{at: t1}, &facts{master: resolvedMaster()})
	if got := evaluate(t, p, query("fin.cap.query_settlement_status", late)); got.Effect != policy.Reject || got.ReasonCode != policy.NoPermittingRule || got.PermitMatched || len(got.Rules) != 0 {
		t.Fatalf("no rule names the capability: %+v", got)
	}
	got := evaluate(t, p, query(transfer, late))
	if got.Effect != policy.Reject || got.ReasonCode != "PROHIBITED_CAPABILITY" || got.PermitMatched || !reflect.DeepEqual(got.Rules, []decide.PolicyRuleVersion{ref("fin.pol.prohibition.remedy", "3.1")}) {
		t.Fatalf("a prohibited capability: %+v", got)
	}
	got = evaluate(t, p, query("fin.cap.query_switch_status", late))
	if got.Effect != policy.Reject || got.ReasonCode != policy.NoPermittingRule || got.PermitMatched || !reflect.DeepEqual(got.Rules, []decide.PolicyRuleVersion{ref("fin.pol.regulatory.confirm_lookup", "3.1")}) {
		t.Fatalf("a CONFIRM with no PERMIT is the default deny, and the rule that asked is still on the record: %+v", got)
	}
}

// A306: the adapter answers for the one capability of the query, with the rules that apply to it and no other; the query's own capability is what is evaluated.
func TestEachCapabilityIsAnsweredForItselfWithItsOwnRules(t *testing.T) {
	w := newWorld(t)
	w.publish(t, tenantA, "3.1", t1)
	p := w.policies(t, book(t, realPack(t)), &adm{at: t1}, &facts{master: resolvedMaster()})
	got := evaluate(t, p, query(complaint, late))
	if got.Effect != policy.Confirm || got.ReasonCode != "fin.reason.complaint_needs_confirmation" || !got.PermitMatched ||
		!reflect.DeepEqual(got.Rules, []decide.PolicyRuleVersion{ref("fin.pol.regulatory.complaint_confirm", "3.1"), ref("fin.pol.default.permit_complaint", "3.1")}) {
		t.Fatalf("a complaint needs a confirmation, with a PERMIT matched: %+v", got)
	}
	if got = evaluate(t, p, query(caseWrite, late)); got.Effect != policy.Permit || !reflect.DeepEqual(got.Rules, []decide.PolicyRuleVersion{ref("fin.pol.default.permit_case_writes", "3.1")}) {
		t.Fatalf("a case write on a resolved subject with no open case: %+v", got)
	}
	if got = evaluate(t, p, query(routing, late)); got.Effect != policy.Permit || !reflect.DeepEqual(got.Rules, []decide.PolicyRuleVersion{ref("fin.pol.default.permit_routing", "3.1")}) {
		t.Fatalf("routing with a case: %+v", got)
	}
	// a query that names no capability is judged by its intent node alone, which no rule of the pack names: default deny, never the rules of some other capability
	if got = evaluate(t, p, query("", late)); got.Effect != policy.Reject || got.ReasonCode != policy.NoPermittingRule || len(got.Rules) != 0 {
		t.Fatalf("no capability: %+v", got)
	}
	// the asks of the sources are for that capability alone
	f := &facts{master: resolvedMaster()}
	p = w.policies(t, book(t, realPack(t)), &adm{at: t1}, f)
	evaluate(t, p, query(routing, late))
	evaluate(t, p, query(lookup, late))
	if len(f.calls) != 2 || f.calls[0].q.CapabilityID != routing || !reflect.DeepEqual(f.calls[0].need, FactNeed{Variables: []string{"case_id"}}) ||
		f.calls[1].q.CapabilityID != lookup || len(f.calls[1].need.Variables)+len(f.calls[1].need.Entities) != 0 {
		t.Fatalf("fact calls %+v", f.calls)
	}
}

// The facts decide: an open case for the subject and the epoch is the pack's own REJECT, and the PERMIT that also matched is on the record; an unresolved subject is no PERMIT.
func TestTheFactsDecideAnOpenCaseRejectsAndAnUnresolvedSubjectIsNotPermitted(t *testing.T) {
	w := newWorld(t)
	w.publish(t, tenantA, "3.1", t1)
	open := resolvedMaster()
	open.Entities["case"] = []predicate.Row{{"subject": predicate.Ref("txn_1"), "epoch": predicate.Int(1), "status": predicate.Enum("OPEN")}}
	got := evaluate(t, w.policies(t, book(t, realPack(t)), &adm{at: t1}, &facts{master: open}), query(caseWrite, late))
	if got.Effect != policy.Reject || got.ReasonCode != "fin.reason.open_case_exists" || !got.PermitMatched ||
		!reflect.DeepEqual(got.Rules, []decide.PolicyRuleVersion{ref("fin.pol.workflow.case_dedupe", "3.1"), ref("fin.pol.default.permit_case_writes", "3.1")}) {
		t.Fatalf("a second open case: %+v", got)
	}
	unresolved := resolvedMaster()
	unresolved.Variables["subject_resource.status"] = predicate.Enum("AMBIGUOUS")
	got = evaluate(t, w.policies(t, book(t, realPack(t)), &adm{at: t1}, &facts{master: unresolved}), query(caseWrite, late))
	if got.Effect != policy.Reject || got.ReasonCode != policy.NoPermittingRule || got.PermitMatched {
		t.Fatalf("an unresolved subject: %+v", got)
	}
}

// INV-16: a rule whose condition cannot be evaluated is an error and no answer, never a pass and never a skipped rule.
func TestARuleThatCannotBeEvaluatedIsAnErrorAndNeverAnAnswer(t *testing.T) {
	w := newWorld(t)
	w.publish(t, tenantA, "3.1", t1)
	bad := rule("fin.pol.tenant.epoch_floor", "TENANT_SECURITY", "AT_DECISION", lookup, "logical_resolution_epoch > 0", "REJECT", "fin.reason.permitted", nil)
	b := book(t, variantPack(t, "3.1", withRules(bad)))
	m := resolvedMaster()
	m.Variables["logical_resolution_epoch"] = predicate.Value{} // read, and null: an ordering over a null is an error of the predicate language
	got, err := w.policies(t, b, &adm{at: t1}, &facts{master: m}).Evaluate(ctx(t), query(lookup, late))
	if !errors.Is(err, policy.ErrCondition) || !reflect.DeepEqual(got, intake.PolicyEvaluation{}) {
		t.Fatalf("got %+v, %v; want policy.ErrCondition and no answer", got, err)
	}
}

// PERD 23.4 (params: wait_seconds), plan T11: a WAIT is re-evaluated at the reading plus the rule's wait; a WAIT whose rule gives no usable wait is an error, never a guess.
func TestAWaitIsReevaluatedAtTheReadingPlusTheRulesWaitAndAnUnusableWaitIsAnError(t *testing.T) {
	w := newWorld(t)
	w.publish(t, tenantA, "3.1", t1)
	wait := func(params map[string]any) *PolicyBook {
		return book(t, variantPack(t, "3.1", withRules(rule("fin.pol.workflow.hold", "WORKFLOW", "AT_DECISION", lookup, "true", "WAIT", "fin.reason.sla_at_risk", params))))
	}
	got := evaluate(t, w.policies(t, wait(map[string]any{"wait_seconds": json.Number("600")}), &adm{at: t1}, &facts{master: resolvedMaster()}), query(lookup, late))
	if got.Effect != policy.Wait || got.ReasonCode != "fin.reason.sla_at_risk" || got.ReevaluateAt != late+600 || !got.PermitMatched {
		t.Fatalf("a WAIT of 600 seconds: %+v", got)
	}
	for name, params := range map[string]map[string]any{
		"no wait_seconds": nil, "a zero wait": {"wait_seconds": json.Number("0")}, "a negative wait": {"wait_seconds": json.Number("-5")},
		"a wait given as text": {"wait_seconds": "600"}, "a wait that overflows the clock": {"wait_seconds": json.Number("9223372036854775807")},
		"a boolean wait": {"wait_seconds": true},
	} {
		got, err := w.policies(t, wait(params), &adm{at: t1}, &facts{master: resolvedMaster()}).Evaluate(ctx(t), query(lookup, late))
		if !errors.Is(err, ErrRule) || !reflect.DeepEqual(got, intake.PolicyEvaluation{}) {
			t.Errorf("%s: %+v, %v; want ErrRule and no answer", name, got, err)
		}
	}
	// another effect carries no re-evaluation time
	p := w.policies(t, book(t, realPack(t)), &adm{at: t1}, &facts{master: resolvedMaster()})
	if got := evaluate(t, p, query(complaint, late)); got.ReevaluateAt != 0 {
		t.Fatalf("a CONFIRM has no re-evaluation time: %+v", got)
	}
}

// INV-19: a fact the rules read that the source did not give, gave of another type, or gave twice under an undeclared name is ErrFacts, never "no such case".
func TestFactsTheRulesReadMustBeGivenDeclaredAndOfTheirType(t *testing.T) {
	w := newWorld(t)
	w.publish(t, tenantA, "3.1", t1)
	b := book(t, realPack(t))
	for name, c := range map[string]struct {
		capability string
		edit       func(FactSet) FactSet
	}{
		"the case entity was not read": {caseWrite, func(s FactSet) FactSet { delete(s.Entities, "case"); return s }},
		"a variable the rules read was not given": {caseWrite, func(s FactSet) FactSet {
			delete(s.Variables, "logical_resolution_epoch")
			return s
		}},
		"a variable of another kind": {caseWrite, func(s FactSet) FactSet { s.Variables["logical_resolution_epoch"] = predicate.Str("1"); return s }},
		"an enum value the pack does not declare": {caseWrite, func(s FactSet) FactSet {
			s.Variables["subject_resource.status"] = predicate.Enum("DONE")
			return s
		}},
		"a variable the pack does not declare": {lookup, func(s FactSet) FactSet { s.Variables["invented"] = predicate.Int(1); return s }},
		"an entity the pack does not declare":  {lookup, func(s FactSet) FactSet { s.Entities["invented"] = nil; return s }},
		"a row field the entity does not declare": {caseWrite, func(s FactSet) FactSet {
			s.Entities["case"] = []predicate.Row{{"owner": predicate.Ref("x")}}
			return s
		}},
		"a row field of another kind": {caseWrite, func(s FactSet) FactSet {
			s.Entities["case"] = []predicate.Row{{"epoch": predicate.Str("1")}}
			return s
		}},
		"a row enum value the pack does not declare": {caseWrite, func(s FactSet) FactSet {
			s.Entities["case"] = []predicate.Row{{"status": predicate.Enum("DONE")}}
			return s
		}},
	} {
		f := &facts{master: resolvedMaster(), extra: c.edit}
		got, err := w.policies(t, b, &adm{at: t1}, f).Evaluate(ctx(t), query(c.capability, late))
		if !errors.Is(err, ErrFacts) || !reflect.DeepEqual(got, intake.PolicyEvaluation{}) {
			t.Errorf("%s: %+v, %v; want ErrFacts and no answer", name, got, err)
		}
	}
	// a null that was read is a value, and the rules decide on it: no epoch, so no case is the same epoch, so the write is permitted by its own PERMIT only
	m := resolvedMaster()
	m.Variables["logical_resolution_epoch"] = predicate.Value{}
	if got := evaluate(t, w.policies(t, b, &adm{at: t1}, &facts{master: m}), query(caseWrite, late)); got.Effect != policy.Permit {
		t.Fatalf("a variable read as null: %+v", got)
	}
	// the source's own failure is the answer
	boom := errors.New("the case system could not be read")
	if _, err := w.policies(t, b, &adm{at: t1}, &facts{master: resolvedMaster(), err: boom}).Evaluate(ctx(t), query(caseWrite, late)); !errors.Is(err, boom) {
		t.Fatalf("a failing source: %v", err)
	}
}

// The adapter refuses a query it cannot answer before it asks any port; a source or a store that fails is the error, never a default.
func TestAQueryItCannotAnswerIsRefusedBeforeAnyPortIsAsked(t *testing.T) {
	w := newWorld(t)
	w.publish(t, tenantA, "3.1", t1)
	a, f := &adm{at: t1}, &facts{master: resolvedMaster()}
	p := w.policies(t, book(t, realPack(t)), a, f)
	for name, mut := range map[string]func(*intake.PolicyQuery){
		"no tenant": func(q *intake.PolicyQuery) { q.TenantID = "" }, "no intent": func(q *intake.PolicyQuery) { q.IntentID = "" },
		"intent version 0": func(q *intake.PolicyQuery) { q.IntentVersion = 0 }, "no reading": func(q *intake.PolicyQuery) { q.Now = 0 },
		"a negative reading": func(q *intake.PolicyQuery) { q.Now = -1 }, "no capability and no node": func(q *intake.PolicyQuery) { q.CapabilityID, q.IntentNode = "", "" },
	} {
		q := query(lookup, late)
		mut(&q)
		if got, err := p.Evaluate(ctx(t), q); !errors.Is(err, ErrQuery) || !reflect.DeepEqual(got, intake.PolicyEvaluation{}) {
			t.Errorf("%s: %+v, %v; want ErrQuery", name, got, err)
		}
	}
	if a.calls != 0 || len(f.calls) != 0 {
		t.Fatalf("a refused query asked the admissions %d times and the facts %d times", a.calls, len(f.calls))
	}
}

// The admission time is the intent store's, asked for the version evaluated; an error, a time that is not a time, and an admission after the reading are errors.
func TestTheAdmissionTimeIsAskedForTheEvaluatedIntentAndAnUnusableOneIsAnError(t *testing.T) {
	w := newWorld(t)
	w.publish(t, tenantA, "3.1", t1)
	var asked struct {
		tenant, intent string
		version        int
	}
	spy := admissionsFunc(func(_ context.Context, tenant, intent string, version int) (int64, error) {
		asked.tenant, asked.intent, asked.version = tenant, intent, version
		return t1 + 1, nil
	})
	p := w.policies(t, book(t, realPack(t)), spy, &facts{master: resolvedMaster()})
	q := query(lookup, late)
	q.IntentVersion = 3
	evaluate(t, p, q)
	if asked.tenant != tenantA || asked.intent != "int_aa1" || asked.version != 3 {
		t.Fatalf("asked %+v", asked)
	}
	boom := errors.New("the intent store is unreadable")
	for name, a := range map[string]*adm{"an error": {err: boom}, "zero": {at: 0}, "a negative time": {at: -4}, "an admission after the reading": {at: late + 1}} {
		got, err := w.policies(t, book(t, realPack(t)), a, &facts{master: resolvedMaster()}).Evaluate(ctx(t), query(lookup, late))
		if err == nil || !reflect.DeepEqual(got, intake.PolicyEvaluation{}) {
			t.Errorf("%s: %+v, %v; want an error and no answer", name, got, err)
		}
	}
}

type admissionsFunc func(ctx context.Context, tenant, intent string, version int) (int64, error)

func (f admissionsFunc) AdmittedAt(ctx context.Context, tenant, intent string, version int) (int64, error) {
	return f(ctx, tenant, intent, version)
}

// INV-21: the history is put in order in time by the database's order of versions; two versions at one second, or a newer version with an earlier time, cannot be placed and are an error.
func TestAHistoryThatCannotBePutInOrderInTimeIsAnError(t *testing.T) {
	for name, times := range map[string][2]int64{"one second for two versions": {t1, t1}, "a newer version published earlier": {t2, t1}} {
		w := newWorld(t)
		w.publish(t, tenantA, "3.1", times[0])
		w.publish(t, tenantA, "3.2", times[1])
		p := w.policies(t, book(t, realPack(t), variantPack(t, "3.2", nil)), &adm{at: t2}, &facts{master: resolvedMaster()})
		if got, err := p.Evaluate(ctx(t), query(lookup, late)); !errors.Is(err, ErrPublications) || !reflect.DeepEqual(got, intake.PolicyEvaluation{}) {
			t.Errorf("%s: %+v, %v; want ErrPublications", name, got, err)
		}
	}
}

// REQ-SEC-004: each tenant is answered from its own publications. A tenant that published nothing is not given another tenant's version, even through a pool that is the other's.
func TestTenantsAreAnsweredFromTheirOwnPublicationsOnly(t *testing.T) {
	w := newWorld(t)
	w.publish(t, tenantA, "3.1", t1)
	p := w.policies(t, book(t, realPack(t)), &adm{at: t1}, &facts{master: resolvedMaster()})
	qb := query(lookup, late)
	qb.TenantID = tenantB
	if got := evaluate(t, p, qb); got.Effect != policy.Reject || len(got.Versions) != 0 {
		t.Fatalf("tenant B published nothing: %+v", got)
	}
	if got := evaluate(t, p, query(lookup, late)); got.Effect != policy.Permit || len(got.Versions) != 1 {
		t.Fatalf("tenant A: %+v", got)
	}
	// a composition that hands tenant B tenant A's login still shows B nothing: the database binds the tenant, not the adapter
	crossed := &Policies{c: PoliciesConfig{Book: book(t, realPack(t)), Publications: StorePublications{DBs: crossLogins{w}}, Admissions: &adm{at: t1}, Facts: &facts{master: resolvedMaster()}}}
	if got := evaluate(t, crossed, qb); got.Effect != policy.Reject || len(got.Versions) != 0 {
		t.Fatalf("tenant B through tenant A's login: %+v", got)
	}
	// a tenant with no login is an error and not a default
	qc := query(lookup, late)
	qc.TenantID = "ten_cc"
	if _, err := p.Evaluate(ctx(t), qc); err == nil {
		t.Fatal("a tenant with no login was answered")
	}
}

type crossLogins struct{ w *world }

func (c crossLogins) For(string) (policystore.Querier, error) { return c.w.For(tenantA) }

// A publication store that cannot be read is an error, and never "nothing published" (which would read as a default deny that looks like a decision).
func TestAnUnreadablePublicationStoreIsAnErrorAndNeverADefaultDeny(t *testing.T) {
	w := newWorld(t)
	w.publish(t, tenantA, "3.1", t1)
	p := w.policies(t, book(t, realPack(t)), &adm{at: t1}, &facts{master: resolvedMaster()})
	w.logins[tenantA].Close()
	if got, err := p.Evaluate(ctx(t), query(lookup, late)); err == nil || !reflect.DeepEqual(got, intake.PolicyEvaluation{}) {
		t.Fatalf("a closed login: %+v, %v", got, err)
	}
}

// INV-21: the same inputs give the same answer, and the answer holds no pointer into the adapter's data.
func TestTheSameQueryGivesTheSameAnswerAndTheAnswerIsTheCallersOwn(t *testing.T) {
	w := newWorld(t)
	w.publish(t, tenantA, "3.1", t1)
	p := w.policies(t, book(t, realPack(t)), &adm{at: t1}, &facts{master: resolvedMaster()})
	first, second := evaluate(t, p, query(complaint, late)), evaluate(t, p, query(complaint, late))
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("two answers differ: %+v %+v", first, second)
	}
	first.Rules[0].RuleID, first.Versions[0].Version = "changed", "changed"
	if third := evaluate(t, p, query(complaint, late)); !reflect.DeepEqual(second, third) {
		t.Fatalf("a caller's change reached the adapter: %+v", third)
	}
}

// The adapter holds no part it cannot run without.
func TestTheAdapterRefusesAConfigurationWithAMissingPart(t *testing.T) {
	w := newWorld(t)
	good := PoliciesConfig{Book: book(t, realPack(t)), Publications: StorePublications{DBs: w}, Admissions: &adm{at: 1}, Facts: &facts{}}
	for name, c := range map[string]PoliciesConfig{
		"no book": {Publications: good.Publications, Admissions: good.Admissions, Facts: good.Facts}, "no publications": {Book: good.Book, Admissions: good.Admissions, Facts: good.Facts},
		"no admissions": {Book: good.Book, Publications: good.Publications, Facts: good.Facts}, "no facts": {Book: good.Book, Publications: good.Publications, Admissions: good.Admissions},
		"a store with no logins": {Book: good.Book, Publications: StorePublications{}, Admissions: good.Admissions, Facts: good.Facts},
	} {
		if p, err := NewPolicies(c); !errors.Is(err, ErrConfig) || p != nil {
			t.Errorf("%s: %v, %v; want ErrConfig and no adapter", name, p, err)
		}
	}
	if _, err := NewPolicies(good); err != nil {
		t.Fatal(err)
	}
	if _, err := (StorePublications{}).Published(ctx(t), tenantA, policyID); !errors.Is(err, ErrConfig) {
		t.Fatalf("a store with no logins read: %v", err)
	}
}
