package policy_test

// TRU-03: the policy model, loader, evaluator, precedence ladder, evaluation_time and default deny (PERD §23.4,
// REQ-TRU-004, INV-09). The reference in TestRandomRuleSetsMatchAnIndependentReference is written from the PERD text alone and
// shares no code with the package.

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/internal/predicate"
	"github.com/Noemai-org/noema/services/core/trust/policy"
)

func scope() predicate.Scope {
	return predicate.Scope{
		Entities:  map[string]map[string]predicate.Type{"case": {"subject": predicate.Reference, "status": predicate.EnumOf("OPEN", "ROUTED", "CLOSED")}},
		Variables: map[string]predicate.Type{"x": predicate.Integer, "subject_resource": predicate.Reference},
	}
}

func vocab(targets ...string) policy.Vocabulary {
	if len(targets) == 0 {
		targets = []string{"a", "b"}
	}
	return policy.Vocabulary{Scope: scope(), Targets: targets}
}

// rule renders one PolicyRule as JSON. evalTime "" omits the field.
func rule(id, class, effect, target, cond, reason, evalTime string) string {
	et := ""
	if evalTime != "" {
		et = fmt.Sprintf(`"evaluation_time":%q,`, evalTime)
	}
	return fmt.Sprintf(`{"rule_id":%q,"class":%q,%s"applies_to":[%q],"condition":%q,"effect":%q,"reason_code":%q}`, id, class, et, target, cond, effect, reason)
}

func doc(policyID, version string, rules ...string) []byte {
	return []byte(fmt.Sprintf(`{"policy_id":%q,"version":%q,"rules":[%s]}`, policyID, version, strings.Join(rules, ",")))
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func active(policyID, version string, from int64, rules ...string) policy.Version {
	return must(must(policy.Parse(doc(policyID, version, rules...), vocab())).WithMeta(policy.Meta{Status: policy.Active, EffectiveFrom: from, PublishedAt: from}))
}

func retired(policyID, version string, from int64, rules ...string) policy.Version {
	return must(must(policy.Parse(doc(policyID, version, rules...), vocab())).WithMeta(policy.Meta{Status: policy.Retired, EffectiveFrom: from, PublishedAt: from}))
}

func eval(t *testing.T, s policy.Set, target string, now int64, vars map[string]predicate.Value) policy.Result {
	t.Helper()
	r, err := s.Evaluate("p", policy.Request{Target: target, Now: now, AdmittedAt: now, Env: predicate.Env{Variables: vars}})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	return r
}

// REQ-TRU-004, §23.4: precedence across classes, then within the winning class; default deny.
func TestPrecedenceLadder(t *testing.T) {
	classes := []string{"SAFETY", "PROHIBITION", "TENANT_SECURITY", "REGULATORY", "WORKFLOW", "DEFAULT"}
	effects := []string{"REJECT", "ESCALATE", "CONFIRM", "WAIT", "PERMIT"}
	for i, hi := range classes {
		for _, lo := range classes[i+1:] {
			for _, eh := range effects {
				for _, el := range effects {
					s := must(policy.NewSet(active("p", "1", 100,
						rule("lo", lo, el, "a", "true", "R_LO", "AT_DECISION"),
						rule("hi", hi, eh, "a", "true", "R_HI", "AT_DECISION"))))
					got := eval(t, s, "a", 200, nil)
					if eh == "CONFIRM" && el != "PERMIT" { // a CONFIRM with no PERMIT matched is default deny, but it still ranks first
						if got.Effect != policy.Reject || !got.ConfirmWithoutPermit || got.Applied[0].RuleID != "hi" {
							t.Fatalf("%s/CONFIRM over %s/%s: %+v", hi, lo, el, got)
						}
						continue
					}
					if got.Winner == nil || got.Winner.RuleID != "hi" || string(got.Effect) != eh {
						t.Fatalf("%s/%s over %s/%s: winner %+v effect %s", hi, eh, lo, el, got.Winner, got.Effect)
					}
				}
			}
		}
	}
	for i, strong := range effects {
		for _, weak := range effects[i+1:] {
			s := must(policy.NewSet(active("p", "1", 100,
				rule("w", "WORKFLOW", weak, "a", "true", "R_W", "AT_DECISION"),
				rule("s", "WORKFLOW", strong, "a", "true", "R_S", "AT_DECISION"))))
			if got := eval(t, s, "a", 200, nil); got.Applied[0].RuleID != "s" {
				t.Fatalf("%s should beat %s within a class, got %s", strong, weak, got.Applied[0].RuleID)
			}
		}
	}
}

// §23.4: no matching rule is REJECT NO_PERMITTING_RULE.
func TestDefaultDeny(t *testing.T) {
	s := must(policy.NewSet(active("p", "1", 100,
		rule("only_a", "DEFAULT", "PERMIT", "a", "true", "R", "AT_DECISION"),
		rule("never", "DEFAULT", "PERMIT", "b", "false", "R", "AT_DECISION"))))
	for _, target := range []string{"b", "c", ""} {
		got := eval(t, s, target, 200, nil)
		if got.Effect != policy.Reject || got.Reason != "NO_PERMITTING_RULE" || got.Winner != nil || got.PermitMatched {
			t.Fatalf("%q: %+v", target, got)
		}
	}
	if got := eval(t, s, "a", 200, nil); got.Effect != policy.Permit || !got.PermitMatched {
		t.Fatalf("a: %+v", got)
	}
}

// REQ-TRU-004: "If none exists, all writes are refused." With no ACTIVE version nothing is permitted.
func TestNoActiveVersionDeniesEverything(t *testing.T) {
	r := rule("p1", "DEFAULT", "PERMIT", "a", "true", "R", "AT_DECISION")
	for name, s := range map[string]policy.Set{
		"empty":        must(policy.NewSet()),
		"retired only": must(policy.NewSet(retired("p", "1", 100, r))),
		"draft only":   must(policy.NewSet(must(must(policy.Parse(doc("p", "1", r), vocab())).WithMeta(policy.Meta{Status: policy.Draft})))),
		"future":       must(policy.NewSet(active("p", "1", 5000, r))),
	} {
		if got := eval(t, s, "a", 200, nil); got.Effect != policy.Reject || got.Reason != "NO_PERMITTING_RULE" {
			t.Fatalf("%s: %+v", name, got)
		}
	}
}

// §23.4, INV-09: AT_DECISION rules use the ACTIVE version now; a RETIRED version is never treated as active.
func TestAtDecisionNeverUsesARetiredVersion(t *testing.T) {
	s := must(policy.NewSet(
		retired("p", "1", 100, rule("old", "WORKFLOW", "REJECT", "a", "true", "R_OLD", "AT_DECISION")),
		active("p", "2", 300, rule("new", "DEFAULT", "PERMIT", "a", "true", "R_NEW", "AT_DECISION"))))
	got := eval(t, s, "a", 400, nil)
	if got.Effect != policy.Permit || len(got.Applied) != 1 || got.Applied[0].Version != "2" {
		t.Fatalf("%+v", got)
	}
}

// §23.4 / INV-09 / US-08 / EC-T03: AT_ADMISSION rules use the version in force at admitted_at; both versions and which
// rule used which are recorded.
func TestAtAdmissionUsesTheVersionInForceAtAdmission(t *testing.T) {
	s := must(policy.NewSet(
		retired("p", "1", 100,
			rule("adm_old", "WORKFLOW", "REJECT", "a", "true", "R_ADM_OLD", "AT_ADMISSION"),
			rule("dec_old", "DEFAULT", "PERMIT", "a", "true", "R_DEC_OLD", "AT_DECISION")),
		active("p", "2", 300,
			rule("adm_new", "WORKFLOW", "ESCALATE", "a", "true", "R_ADM_NEW", "AT_ADMISSION"),
			rule("dec_new", "DEFAULT", "PERMIT", "a", "true", "R_DEC_NEW", "AT_DECISION"))))
	got, err := s.Evaluate("p", policy.Request{Target: "a", Now: 400, AdmittedAt: 200})
	if err != nil {
		t.Fatal(err)
	}
	if got.Effect != policy.Reject || got.Winner.RuleID != "adm_old" || got.Winner.Version != "1" {
		t.Fatalf("admitted under v1, decided under v2: %+v", got)
	}
	used := map[string]string{}
	for _, a := range got.Applied {
		used[a.RuleID] = a.Version
	}
	if !reflect.DeepEqual(used, map[string]string{"adm_old": "1", "dec_new": "2"}) {
		t.Fatalf("rule to version: %v", used)
	}
	if !reflect.DeepEqual(got.Versions, []policy.VersionRef{{PolicyID: "p", Version: "1"}, {PolicyID: "p", Version: "2"}}) {
		t.Fatalf("versions recorded: %+v", got.Versions)
	}
	got, _ = s.Evaluate("p", policy.Request{Target: "a", Now: 400, AdmittedAt: 350})
	if got.Winner.RuleID != "adm_new" || got.Winner.Version != "2" {
		t.Fatalf("admitted under v2: %+v", got.Winner)
	}
	if !reflect.DeepEqual(got.Versions, []policy.VersionRef{{PolicyID: "p", Version: "2"}}) {
		t.Fatalf("one version used, one recorded: %+v", got.Versions)
	}
}

// A policy that did not exist at admission contributes no AT_ADMISSION rule (fail closed: nothing permits).
func TestAdmissionBeforeAnyVersionHasNoAdmissionRules(t *testing.T) {
	s := must(policy.NewSet(active("p", "1", 300, rule("adm", "WORKFLOW", "REJECT", "a", "true", "R", "AT_ADMISSION"))))
	got, _ := s.Evaluate("p", policy.Request{Target: "a", Now: 400, AdmittedAt: 100})
	if got.Winner != nil || got.Reason != "NO_PERMITTING_RULE" {
		t.Fatalf("%+v", got)
	}
}

// A condition that cannot be evaluated is an error, never a result: the caller turns it into UNKNOWN (INV-16). A null operand
// to an ordering is such a case (a null in an equality is just false).
func TestUnevaluableConditionIsAnError(t *testing.T) {
	s := must(policy.NewSet(active("p", "1", 100,
		rule("deny", "WORKFLOW", "REJECT", "a", "x > 0", "R", "AT_DECISION"),
		rule("permit", "DEFAULT", "PERMIT", "a", "true", "R", "AT_DECISION"))))
	_, err := s.Evaluate("p", policy.Request{Target: "a", Now: 200, AdmittedAt: 200, Env: predicate.Env{Variables: map[string]predicate.Value{}}})
	var pe *predicate.Error
	if !errors.Is(err, policy.ErrCondition) || !errors.As(err, &pe) {
		t.Fatalf("want ErrCondition wrapping a predicate error, got %v", err)
	}
}

// Rule-skipping on error would let a PERMIT through: an erroring rule for ANOTHER target must not matter.
func TestOnlyRulesForTheTargetAreEvaluated(t *testing.T) {
	s := must(policy.NewSet(active("p", "1", 100,
		rule("other", "WORKFLOW", "REJECT", "b", "x > 0", "R", "AT_DECISION"),
		rule("mine", "DEFAULT", "PERMIT", "a", "true", "R", "AT_DECISION"))))
	if got := eval(t, s, "a", 200, map[string]predicate.Value{}); got.Effect != policy.Permit {
		t.Fatalf("%+v", got)
	}
}

func TestLoaderRejects(t *testing.T) {
	good := rule("r", "DEFAULT", "PERMIT", "a", "true", "R", "AT_DECISION")
	cases := map[string][]byte{
		"unknown top-level field": []byte(`{"policy_id":"p","version":"1","rules":[` + good + `],"extra":1}`),
		"unknown rule field":      []byte(`{"policy_id":"p","version":"1","rules":[` + strings.Replace(good, `"effect"`, `"surprise":true,"effect"`, 1) + `]}`),
		"missing evaluation_time": doc("p", "1", rule("r", "DEFAULT", "PERMIT", "a", "true", "R", "")),
		"unknown evaluation_time": doc("p", "1", rule("r", "DEFAULT", "PERMIT", "a", "true", "R", "SOMETIME")),
		"unknown capability":      doc("p", "1", rule("r", "DEFAULT", "PERMIT", "fin.cap.nope", "true", "R", "AT_DECISION")),
		"unknown class":           doc("p", "1", rule("r", "URGENT", "PERMIT", "a", "true", "R", "AT_DECISION")),
		"unknown effect":          doc("p", "1", rule("r", "DEFAULT", "ALLOW", "a", "true", "R", "AT_DECISION")),
		"duplicate rule id":       doc("p", "1", good, good),
		"empty applies_to":        []byte(`{"policy_id":"p","version":"1","rules":[` + strings.Replace(good, `["a"]`, `[]`, 1) + `]}`),
		"syntax error":            doc("p", "1", rule("r", "DEFAULT", "PERMIT", "a", "x ==", "R", "AT_DECISION")),
		"unknown name":            doc("p", "1", rule("r", "DEFAULT", "PERMIT", "a", "nope == 1", "R", "AT_DECISION")),
		"not boolean":             doc("p", "1", rule("r", "DEFAULT", "PERMIT", "a", "x", "R", "AT_DECISION")),
		"type mismatch":           doc("p", "1", rule("r", "DEFAULT", "PERMIT", "a", "x == 'one'", "R", "AT_DECISION")),
		"empty reason":            doc("p", "1", rule("r", "DEFAULT", "PERMIT", "a", "true", "", "AT_DECISION")),
		"empty rule id":           doc("p", "1", rule("", "DEFAULT", "PERMIT", "a", "true", "R", "AT_DECISION")),
		"float param":             []byte(`{"policy_id":"p","version":"1","rules":[` + strings.Replace(good, `}`, `,"params":{"wait_seconds":1.5}}`, 1) + `]}`),
		"nested param":            []byte(`{"policy_id":"p","version":"1","rules":[` + strings.Replace(good, `}`, `,"params":{"k":{"a":1}}}`, 1) + `]}`),
		"missing policy id":       []byte(`{"version":"1","rules":[` + good + `]}`),
		"trailing data":           append(doc("p", "1", good), []byte(` {}`)...),
		"not json":                []byte(`policy`),
		"duplicate member (JSON)": []byte(`{"policy_id":"p","policy_id":"q","version":"1","rules":[` + good + `]}`),
	}
	names := make([]string, 0, len(cases))
	for n := range cases {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if _, err := policy.Parse(cases[n], vocab()); !errors.Is(err, policy.ErrInvalidPolicy) {
			t.Errorf("%s: want ErrInvalidPolicy, got %v", n, err)
		}
	}
	if _, err := policy.Parse(doc("p", "1", good), vocab()); err != nil {
		t.Fatalf("the control document must load: %v", err)
	}
}

func TestSetRejectsAmbiguousVersions(t *testing.T) {
	r := rule("r", "DEFAULT", "PERMIT", "a", "true", "R", "AT_DECISION")
	if _, err := policy.NewSet(active("p", "1", 100, r), active("p", "2", 200, r)); !errors.Is(err, policy.ErrInvalidPolicy) {
		t.Errorf("two ACTIVE versions of one policy: %v", err)
	}
	if _, err := policy.NewSet(active("p", "1", 100, r), retired("p", "1", 50, r)); !errors.Is(err, policy.ErrInvalidPolicy) {
		t.Errorf("duplicate version: %v", err)
	}
	if _, err := must(policy.Parse(doc("p", "1", r), vocab())).WithMeta(policy.Meta{Status: policy.Active}); !errors.Is(err, policy.ErrInvalidPolicy) {
		t.Errorf("ACTIVE without effective_from: %v", err)
	}
	if _, err := must(policy.Parse(doc("p", "1", r), vocab())).WithMeta(policy.Meta{}); !errors.Is(err, policy.ErrInvalidPolicy) {
		t.Errorf("missing status: %v", err)
	}
}

// INV-21: same inputs, same result.
func TestEvaluationIsDeterministic(t *testing.T) {
	s := must(policy.NewSet(active("p", "1", 100,
		rule("b", "WORKFLOW", "CONFIRM", "a", "true", "R_B", "AT_DECISION"),
		rule("a", "WORKFLOW", "CONFIRM", "a", "true", "R_A", "AT_DECISION"),
		rule("p", "DEFAULT", "PERMIT", "a", "true", "R_P", "AT_DECISION"))))
	first := eval(t, s, "a", 200, nil)
	if first.Winner.RuleID != "a" {
		t.Fatalf("a tie on class and effect goes to the smallest rule_id, got %q", first.Winner.RuleID)
	}
	for i := 0; i < 1000; i++ {
		if got := eval(t, s, "a", 200, nil); !reflect.DeepEqual(got, first) {
			t.Fatalf("run %d differs:\n%+v\n%+v", i, got, first)
		}
	}
}

// Independent reference, from PERD §23.4 only.
var (
	referenceClasses = []string{"SAFETY", "PROHIBITION", "TENANT_SECURITY", "REGULATORY", "WORKFLOW", "DEFAULT"}
	referenceEffects = []string{"REJECT", "ESCALATE", "CONFIRM", "WAIT", "PERMIT"}
)

func indexOf(xs []string, x string) int {
	for i, v := range xs {
		if v == x {
			return i
		}
	}
	return -1
}

type genRule struct{ id, class, effect, target, cond string }

func reference(rules []genRule, target string, x int64) (effect, ruleID string) {
	best := -1
	permit := false
	for i, r := range rules {
		matched := r.target == target && (r.cond == "true" || (r.cond == "x == 1" && x == 1))
		if !matched {
			continue
		}
		permit = permit || r.effect == "PERMIT"
		if best < 0 {
			best = i
			continue
		}
		b := rules[best]
		ci, cb := indexOf(referenceClasses, r.class), indexOf(referenceClasses, b.class)
		ei, eb := indexOf(referenceEffects, r.effect), indexOf(referenceEffects, b.effect)
		if ci < cb || (ci == cb && ei < eb) || (ci == cb && ei == eb && r.id < b.id) {
			best = i
		}
	}
	if best < 0 || (rules[best].effect == "CONFIRM" && !permit) { // a CONFIRM alone is default deny (PERD §23.4, INV-03)
		return "REJECT", ""
	}
	return rules[best].effect, rules[best].id
}

func TestRandomRuleSetsMatchAnIndependentReference(t *testing.T) {
	rng := rand.New(rand.NewSource(20261103))
	conds := []string{"true", "false", "x == 1"}
	sawDeny, sawWinner := 0, 0
	for n := 0; n < 3000; n++ {
		count := 1 + rng.Intn(9)
		rules := make([]genRule, count)
		js := make([]string, count)
		for i := range rules {
			rules[i] = genRule{id: fmt.Sprintf("r%02d", i), class: referenceClasses[rng.Intn(6)], effect: referenceEffects[rng.Intn(5)],
				target: []string{"a", "b"}[rng.Intn(2)], cond: conds[rng.Intn(3)]}
			js[i] = rule(rules[i].id, rules[i].class, rules[i].effect, rules[i].target, rules[i].cond, "R", "AT_DECISION")
		}
		x := int64(rng.Intn(3))
		wantEffect, wantRule := reference(rules, "a", x)
		// the order of the rules in the document must not matter
		for round := 0; round < 2; round++ {
			rng.Shuffle(len(js), func(i, j int) { js[i], js[j] = js[j], js[i] })
			s := must(policy.NewSet(active("p", "1", 100, js...)))
			got := eval(t, s, "a", 200, map[string]predicate.Value{"x": predicate.Int(x)})
			gotRule := ""
			if got.Winner != nil {
				gotRule = got.Winner.RuleID
			}
			if string(got.Effect) != wantEffect || gotRule != wantRule {
				t.Fatalf("set %d round %d: got %s/%s want %s/%s\nrules %+v", n, round, got.Effect, gotRule, wantEffect, wantRule, rules)
			}
		}
		if wantRule == "" {
			sawDeny++
		} else {
			sawWinner++
		}
	}
	if sawDeny < 300 || sawWinner < 1500 {
		t.Fatalf("generator too narrow: %d default denies, %d winners", sawDeny, sawWinner)
	}
}

// ---- the real fintech policy (pack fin.pol@3.1) --------------------------------------------------------------------

func typeOf(spec map[string]any) predicate.Type {
	switch spec["type"] {
	case "Reference":
		return predicate.Reference
	case "Integer":
		return predicate.Integer
	case "String":
		return predicate.String
	case "Boolean":
		return predicate.Boolean
	case "Timestamp":
		return predicate.Timestamp
	case "Duration":
		return predicate.Duration
	case "Enum":
		var vs []string
		for _, v := range spec["values"].([]any) {
			vs = append(vs, v.(string))
		}
		return predicate.EnumOf(vs...)
	}
	panic(fmt.Sprintf("unknown type %v", spec["type"]))
}

func loadFintech(t *testing.T) (policy.Version, policy.Vocabulary) {
	t.Helper()
	raw, err := os.ReadFile("../../../../packs/fintech_v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var p struct {
		Policy       json.RawMessage `json:"policy"`
		Capabilities []struct {
			ID string `json:"capability_id"`
		} `json:"capabilities"`
		Descriptors []struct {
			Entity string                    `json:"entity"`
			Fields map[string]map[string]any `json:"fields"`
		} `json:"evidence_descriptors"`
		Variables map[string]map[string]any `json:"predicate_variables"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	sc := predicate.Scope{Entities: map[string]map[string]predicate.Type{}, Variables: map[string]predicate.Type{}}
	for _, d := range p.Descriptors {
		sc.Entities[d.Entity] = map[string]predicate.Type{}
		for f, spec := range d.Fields {
			sc.Entities[d.Entity][f] = typeOf(spec)
		}
	}
	for name, spec := range p.Variables {
		sc.Variables[name] = typeOf(spec)
	}
	v := policy.Vocabulary{Scope: sc}
	for _, c := range p.Capabilities {
		v.Targets = append(v.Targets, c.ID)
	}
	doc, err := policy.Parse(p.Policy, v)
	if err != nil {
		t.Fatalf("the shipped fintech policy must load: %v", err)
	}
	return must(doc.WithMeta(policy.Meta{Status: policy.Active, EffectiveFrom: 100, PublishedAt: 100})), v
}

func TestTheShippedFintechPolicy(t *testing.T) {
	ver, _ := loadFintech(t)
	if ver.PolicyID() != "fin.pol" || ver.Version() != "3.1" {
		t.Fatalf("%s@%s", ver.PolicyID(), ver.Version())
	}
	s := must(policy.NewSet(ver))
	at := func(target string, env predicate.Env) policy.Result {
		r, err := s.Evaluate("fin.pol", policy.Request{Target: target, Now: 200, AdmittedAt: 200, Env: env})
		if err != nil {
			t.Fatalf("%s: %v", target, err)
		}
		return r
	}
	resolved := map[string]predicate.Value{"subject_resource": predicate.Ref("txn_1"), "subject_resource.status": predicate.Enum("RESOLVED"),
		"logical_resolution_epoch": predicate.Int(1), "case_id": predicate.Ref("case_1")}

	if r := at("fin.cap.lookup_transaction", predicate.Env{Variables: resolved}); r.Effect != policy.Permit {
		t.Errorf("a read is permitted: %+v", r)
	}
	if r := at("fin.cap.transfer_funds", predicate.Env{Variables: resolved}); r.Effect != policy.Reject || r.Reason != "PROHIBITED_CAPABILITY" {
		t.Errorf("a prohibited capability is rejected: %+v", r)
	}
	// The shipped pack has a CONFIRM for a complaint and no PERMIT for it, so by PERD §23.4 the complaint is denied until the
	// pack carries a PERMIT (escalated to the Architect, 3 Nov 2026). The CONFIRM rule is still the strongest match.
	if r := at("fin.cap.log_complaint_reference", predicate.Env{Variables: resolved}); r.Effect != policy.Reject || !r.ConfirmWithoutPermit ||
		r.Applied[0].Params["approver_requirement"].Str != "SELF" {
		t.Errorf("a complaint with no PERMIT is denied: %+v", r)
	}
	noCase := predicate.Env{Variables: resolved, Entities: map[string][]predicate.Row{"case": {}}}
	if r := at("fin.cap.create_escalation_case", noCase); r.Effect != policy.Permit {
		t.Errorf("an escalation case on a resolved subject with no open case: %+v", r)
	}
	open := predicate.Env{Variables: resolved, Entities: map[string][]predicate.Row{"case": {{"subject": predicate.Ref("txn_1"), "epoch": predicate.Int(1), "status": predicate.Enum("OPEN")}}}}
	if r := at("fin.cap.create_escalation_case", open); r.Effect != policy.Reject || r.Reason != "fin.reason.open_case_exists" {
		t.Errorf("a second open case for the same subject and epoch is rejected: %+v", r)
	}
	unresolved := map[string]predicate.Value{"subject_resource": predicate.Ref("txn_1"), "subject_resource.status": predicate.Enum("AMBIGUOUS"),
		"logical_resolution_epoch": predicate.Int(1), "case_id": predicate.Ref("case_1")}
	if r := at("fin.cap.create_escalation_case", predicate.Env{Variables: unresolved, Entities: map[string][]predicate.Row{"case": {}}}); r.Effect != policy.Reject || r.Reason != "NO_PERMITTING_RULE" {
		t.Errorf("no write on an unresolved subject: %+v", r)
	}
	if r := at("fin.cap.never_registered", predicate.Env{Variables: resolved}); r.Effect != policy.Reject || r.Reason != "NO_PERMITTING_RULE" {
		t.Errorf("default deny: %+v", r)
	}
}

// REQ-TRU-004, PERD §23.4 ("No matching PERMIT: REJECT NO_PERMITTING_RULE"), INV-03: a CONFIRM never grants authority by
// itself. A winning CONFIRM with no PERMIT matched is the default-deny result, marked so the Record can tell it from a
// target no rule names; with a matching PERMIT the CONFIRM stands and the action needs the approver.
func TestAConfirmAloneIsDeniedAndWithAPermitRequiresConfirmation(t *testing.T) {
	alone := must(policy.NewSet(active("p", "1", 100,
		rule("ask", "WORKFLOW", "CONFIRM", "a", "true", "NEEDS_APPROVER", "AT_DECISION"))))
	got := eval(t, alone, "a", 200, nil)
	if got.Effect != policy.Reject || got.Reason != "NO_PERMITTING_RULE" || got.PermitMatched || !got.ConfirmWithoutPermit {
		t.Fatalf("CONFIRM alone: %+v", got)
	}
	if got.Winner != nil || len(got.Applied) != 1 {
		t.Fatalf("the CONFIRM must stay in Applied, not win: %+v", got)
	}

	both := must(policy.NewSet(active("p", "1", 100,
		rule("ask", "WORKFLOW", "CONFIRM", "a", "true", "NEEDS_APPROVER", "AT_DECISION"),
		rule("ok", "DEFAULT", "PERMIT", "a", "true", "R", "AT_DECISION"))))
	got = eval(t, both, "a", 200, nil)
	if got.Effect != policy.Confirm || got.Reason != "NEEDS_APPROVER" || !got.PermitMatched || got.ConfirmWithoutPermit {
		t.Fatalf("CONFIRM with PERMIT: %+v", got)
	}

	plain := eval(t, must(policy.NewSet(active("p", "1", 100))), "a", 200, nil)
	if plain.ConfirmWithoutPermit {
		t.Fatalf("default deny with no CONFIRM must not carry the marker: %+v", plain)
	}
}
