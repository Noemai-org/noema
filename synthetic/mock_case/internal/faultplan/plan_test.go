package faultplan

import (
	"os"
	"strings"
	"testing"
	"time"
)

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("../../testdata/faultplans/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestPERDExamplePlanLoads(t *testing.T) {
	c, err := Parse(loadFixture(t, "commit_then_drop.json"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Plan.Seed != 42 || c.Start == nil || !c.Start.Equal(time.Date(2026, 10, 1, 8, 40, 0, 0, time.UTC)) {
		t.Fatalf("unexpected plan: %+v %v", c.Plan, c.Start)
	}
	if got := c.StepAdvance("after_receipt"); len(got) != 1 || got[0] != 31*time.Second {
		t.Fatalf("step advance: %v", got)
	}
}

func plan(faults, extra string) []byte {
	return []byte(`{"fault_plan_id":"fp_t","scenario_id":"SCN-T","seed":1,"deterministic":true,"source":"MOCK_CASE","faults":[` + faults + `]` + extra + `}`)
}

func fault(op, match, effect, params string) string {
	return `{"target":"mock-case","operation":"` + op + `","match":` + match + `,"effect":"` + effect + `","params":` + params + `}`
}

// Every row names the one reason the plan is refused (REQ-ENV-004: no plan that silently injects nothing).
func TestInvalidPlansAreRefused(t *testing.T) {
	cases := map[string]struct {
		data []byte
		want string
	}{
		"unknown top-level field":          {[]byte(`{"fault_plan_id":"fp_t","scenario_id":"S","seed":1,"deterministic":true,"source":"MOCK_CASE","faults":[],"bogus":1}`), "unknown field"},
		"trailing data":                    {append(plan("", ""), []byte(" {}")...), "trailing"},
		"bad plan id":                      {[]byte(`{"fault_plan_id":"plan","scenario_id":"S","seed":1,"deterministic":true,"source":"MOCK_CASE","faults":[]}`), "fault_plan_id"},
		"fp_none":                          {[]byte(`{"fault_plan_id":"fp_none","scenario_id":"S","seed":1,"deterministic":true,"source":"MOCK_CASE","faults":[]}`), "fp_none"},
		"no scenario":                      {[]byte(`{"fault_plan_id":"fp_t","scenario_id":"","seed":1,"deterministic":true,"source":"MOCK_CASE","faults":[]}`), "scenario_id"},
		"negative seed":                    {[]byte(`{"fault_plan_id":"fp_t","scenario_id":"S","seed":-1,"deterministic":true,"source":"MOCK_CASE","faults":[]}`), "seed"},
		"unknown source":                   {[]byte(`{"fault_plan_id":"fp_t","scenario_id":"S","seed":1,"deterministic":true,"source":"HARNESS","faults":[]}`), "source"},
		"deterministic with a chance":      {plan(`{"target":"mock-case","operation":"create_case","effect":"DROP_RESPONSE","probability":0.5}`, ""), "deterministic plan"},
		"mock-case has no chance":          {[]byte(`{"fault_plan_id":"fp_t","scenario_id":"S","seed":1,"deterministic":false,"source":"MOCK_CASE","faults":[{"target":"mock-case","operation":"create_case","effect":"DROP_RESPONSE","probability":0.5}]}`), "probabilistic"},
		"probability above one":            {plan(`{"target":"mock-case","operation":"create_case","effect":"DROP_RESPONSE","probability":2}`, ""), "probability"},
		"faults missing":                   {[]byte(`{"fault_plan_id":"fp_t","scenario_id":"S","seed":1,"deterministic":true,"source":"MOCK_CASE"}`), "faults is required"},
		"unknown target":                   {[]byte(strings.Replace(string(plan(fault("create_case", "{}", "DROP_RESPONSE", "{}"), "")), `"mock-case"`, `"mock-switchh"`, 1)), "unknown target"},
		"mock-case plan, other target":     {[]byte(strings.Replace(string(plan(fault("create_case", "{}", "DROP_RESPONSE", "{}"), "")), `"mock-case"`, `"mock-switch"`, 1)), "mock-case only"},
		"unknown effect on another target": {[]byte(`{"fault_plan_id":"fp_t","scenario_id":"S","seed":1,"deterministic":true,"source":"PROXY","faults":[{"target":"mock-switch","operation":"get_status","effect":"EXPLODE"}]}`), "unknown effect"},
		"unknown operation":                {plan(fault("close_case", "{}", "DROP_RESPONSE", "{}"), ""), "unknown operation"},
		"effect not produced":              {plan(fault("create_case", "{}", "SPLIT_STATE", "{}"), ""), "not one mock-case produces"},
		"match key not suppliable":         {plan(fault("route_case", `{"transaction_ref":"TX1"}`, "DROP_RESPONSE", "{}"), ""), "cannot be supplied"},
		"nth_call zero":                    {plan(fault("create_case", `{"nth_call":0}`, "DROP_RESPONSE", "{}"), ""), "nth_call"},
		"nth_call fraction":                {plan(fault("create_case", `{"nth_call":1.5}`, "DROP_RESPONSE", "{}"), ""), "nth_call"},
		"empty match value":                {plan(fault("create_case", `{"idempotency_key":""}`, "DROP_RESPONSE", "{}"), ""), "non-empty"},
		"delay missing":                    {plan(fault("create_case", "{}", "TIMEOUT_AFTER_COMMIT", "{}"), ""), "delay_ms"},
		"delay too large":                  {plan(fault("create_case", "{}", "LATENCY", `{"delay_ms":600001}`), ""), "delay_ms"},
		"delay negative":                   {plan(fault("create_case", "{}", "LATENCY", `{"delay_ms":-1}`), ""), "delay_ms"},
		"5xx status 404":                   {plan(fault("create_case", "{}", "HTTP_5XX", `{"status":404}`), ""), "status"},
		"5xx after_commit string":          {plan(fault("create_case", "{}", "HTTP_5XX", `{"after_commit":"yes"}`), ""), "after_commit"},
		"429 negative retry":               {plan(fault("create_case", "{}", "HTTP_429", `{"retry_after_seconds":-1}`), ""), "retry_after_seconds"},
		"paginate without size":            {plan(fault("find_cases", "{}", "PAGINATE", "{}"), ""), "page_size"},
		"paginate on a write":              {plan(fault("create_case", "{}", "PAGINATE", `{"page_size":1}`), ""), "list operations"},
		"concurrent mod on read":           {plan(fault("get_case", "{}", "CONCURRENT_MODIFICATION", "{}"), ""), "route_case"},
		"stray param":                      {plan(fault("create_case", "{}", "DROP_RESPONSE", `{"delay_ms":5}`), ""), "not a parameter"},
		"bad clock start":                  {plan("", `,"clock":{"start":"yesterday"}`), "clock.start"},
		"bad advance":                      {plan("", `,"clock":{"start":"2026-10-01T09:40:00Z","advances":[{"at_step":"s","by":"P1Y"}]}`), "clock.advances"},
		"bad step name":                    {plan("", `,"clock":{"start":"2026-10-01T09:40:00Z","advances":[{"at_step":"After Receipt","by":"PT1S"}]}`), "at_step"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Parse(tc.data)
			if err == nil {
				t.Fatal("plan was accepted")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

// A plan may carry faults for other components: they are counted, not applied (the PERD §37.1 example does).
func TestFaultsForOtherComponentsAreCountedAndIgnored(t *testing.T) {
	c, err := Parse([]byte(`{"fault_plan_id":"fp_t","scenario_id":"S","seed":1,"deterministic":true,"source":"TUSHE","faults":[
	  {"target":"mock-switch","operation":"get_status","effect":"HTTP_200_SEMANTIC_PENDING"},
	  {"target":"mock-case","operation":"create_case","match":{"nth_call":1},"effect":"DROP_RESPONSE"},
	  {"target":"mock-ledger","operation":"get_transaction","effect":"STALE_READ","params":{"seconds":5}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if c.Applied != 1 || c.Ignored != 2 {
		t.Fatalf("applied %d, ignored %d", c.Applied, c.Ignored)
	}
	e := NewEngine()
	e.Load(c)
	d := e.Next(OpCreateCase, nil)
	if d.Effect == nil || d.Effect.Index != 1 {
		t.Fatalf("the fault trace must name the fault's position in the plan (1), got %+v", d.Effect)
	}
}

func TestProbabilityZeroNeverFiresAndOneAlwaysFires(t *testing.T) {
	e := NewEngine()
	e.Load(mustParse(t, `{"target":"mock-case","operation":"get_case","effect":"DROP_RESPONSE","probability":0},`+
		`{"target":"mock-case","operation":"get_case","effect":"HTTP_5XX","probability":1}`))
	for i := 0; i < 3; i++ {
		if d := e.Next(OpGetCase, nil); d.Effect == nil || d.Effect.Name != EffectHTTP5XX || d.Effect.Index != 1 {
			t.Fatalf("%+v", d.Effect)
		}
	}
}

func TestEmptyPlanIsValid(t *testing.T) {
	if _, err := Parse(plan("", "")); err != nil {
		t.Fatal(err)
	}
}

func mustParse(t *testing.T, faults string) *Compiled {
	t.Helper()
	c, err := Parse(plan(faults, ""))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestNthCallFiresOnceAndOnlyOnTheNthMatchingCall(t *testing.T) {
	e := NewEngine()
	e.Load(mustParse(t, fault("create_case", `{"nth_call":2}`, "DROP_RESPONSE", "{}")))
	var fired []int64
	for i := 0; i < 5; i++ {
		d := e.Next(OpCreateCase, nil)
		if d.Effect != nil {
			fired = append(fired, d.CallNo)
		}
	}
	if len(fired) != 1 || fired[0] != 2 {
		t.Fatalf("fired on calls %v, want [2]", fired)
	}
}

func TestCountersIgnoreUnmatchedTraffic(t *testing.T) {
	e := NewEngine()
	e.Load(mustParse(t, fault("create_case", `{"idempotency_key":"idk_a","nth_call":2}`, "DROP_RESPONSE", "{}")))
	e.Next(OpCreateCase, map[string]string{"idempotency_key": "idk_a"})
	for i := 0; i < 10; i++ { // other keys do not move idk_a's counter
		if e.Next(OpCreateCase, map[string]string{"idempotency_key": "idk_b"}).Effect != nil {
			t.Fatal("fault fired for another key")
		}
	}
	if e.Next(OpCreateCase, map[string]string{"idempotency_key": "idk_a"}).Effect == nil {
		t.Fatal("second call with idk_a must fire")
	}
}

func TestNoNthCallFiresOnEveryMatchingCallAndFirstFaultWins(t *testing.T) {
	e := NewEngine()
	e.Load(mustParse(t, fault("get_case", "{}", "HTTP_429", `{"retry_after_seconds":3}`)+","+fault("get_case", "{}", "DROP_RESPONSE", "{}")))
	for i := 0; i < 3; i++ {
		d := e.Next(OpGetCase, nil)
		if d.Effect == nil || d.Effect.Name != EffectHTTP429 || d.Effect.Index != 0 || d.Effect.RetryAfter == nil || *d.Effect.RetryAfter != 3 {
			t.Fatalf("call %d: %+v", i+1, d.Effect)
		}
	}
}

func TestReplayIsIdentical(t *testing.T) {
	run := func() []string {
		e := NewEngine()
		e.Load(mustParse(t, fault("create_case", `{"nth_call":1}`, "TIMEOUT_AFTER_COMMIT", `{"delay_ms":5}`)+","+fault("route_case", `{"nth_call":3}`, "HTTP_5XX", `{"status":503,"after_commit":true}`)))
		var trace []string
		for _, op := range []string{OpCreateCase, OpRouteCase, OpCreateCase, OpRouteCase, OpRouteCase, OpRouteCase} {
			d := e.Next(op, nil)
			name := "-"
			if d.Effect != nil {
				name = d.Effect.Name
			}
			trace = append(trace, op+":"+name)
		}
		return trace
	}
	a, b := run(), run()
	if strings.Join(a, ",") != strings.Join(b, ",") {
		t.Fatalf("same plan, different trace:\n%v\n%v", a, b)
	}
	if !strings.Contains(strings.Join(a, ","), "route_case:HTTP_5XX") {
		t.Fatalf("expected the 5xx on the third route call: %v", a)
	}
}

func TestLoadResetsCountersAndNilUnloads(t *testing.T) {
	e := NewEngine()
	c := mustParse(t, fault("create_case", `{"nth_call":1}`, "DROP_RESPONSE", "{}"))
	e.Load(c)
	if e.Next(OpCreateCase, nil).Effect == nil {
		t.Fatal("first call must fire")
	}
	e.Load(c)
	if e.Next(OpCreateCase, nil).Effect == nil {
		t.Fatal("reloading must reset the counters")
	}
	e.Load(nil)
	if id, sc, seed := e.Identity(); id != NoPlanID || sc != NoScenario || seed != NoSeed {
		t.Fatalf("no plan must report fp_none: %s %s %d", id, sc, seed)
	}
	if e.Next(OpCreateCase, nil).Effect != nil {
		t.Fatal("no plan, no fault")
	}
}

func TestAfterCommitClassification(t *testing.T) {
	cases := map[string]bool{
		EffectTimeoutAfterCommit: true, EffectDropResponse: true, EffectTimeoutBeforeCommit: false,
		EffectLatency: false, EffectHTTP429: false, EffectHTTP5XX: false, EffectPaginate: false,
	}
	for name, want := range cases {
		if got := (&Effect{Name: name}).AfterCommit(); got != want {
			t.Errorf("%s: AfterCommit=%v want %v", name, got, want)
		}
	}
	if !(&Effect{Name: EffectHTTP5XX, AfterCmt: true}).AfterCommit() {
		t.Error("HTTP_5XX with after_commit commits first")
	}
}
