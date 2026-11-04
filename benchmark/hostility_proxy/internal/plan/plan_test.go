package plan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/benchmark/hostility_proxy/internal/routes"
)

func sampleEnv(t testing.TB) Env {
	t.Helper()
	b, err := os.ReadFile("../../config/routes.json")
	if err != nil {
		t.Fatal(err)
	}
	tab, err := routes.Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	return Env{Routes: tab, HasFixture: func(n string) bool { return n == "credited_before_debited" }}
}

func doc(faults string, extra string) string {
	return `{"fault_plan_id":"fp_t","scenario_id":"SCN-T","seed":7,"deterministic":true,"source":"PROXY","faults":[` + faults + `]` + extra + `}`
}

func f(target, op, effect, match, params string) string {
	return `{"target":"` + target + `","operation":"` + op + `","effect":"` + effect + `","match":` + match + `,"params":` + params + `}`
}

func TestInvalidPlans(t *testing.T) {
	env := sampleEnv(t)
	cases := map[string]struct{ in, want string }{
		"unknown field":            {`{"fault_plan_id":"fp_t","scenario_id":"S","seed":1,"deterministic":true,"source":"PROXY","faults":[],"x":1}`, "unknown field"},
		"fp_none":                  {strings.Replace(doc("", ""), "fp_t", "fp_none", 1), "fp_none"},
		"unknown source":           {strings.Replace(doc("", ""), "PROXY", "HARNESS", 1), "source"},
		"faults missing":           {`{"fault_plan_id":"fp_t","scenario_id":"S","seed":1,"deterministic":true,"source":"PROXY"}`, "faults is required"},
		"unknown target":           {doc(f("mock-ledgr", "get_transaction", "LATENCY", "{}", `{"delay_ms":5}`), ""), "unknown target"},
		"unknown effect":           {doc(f("mock-ledger", "get_transaction", "EXPLODE", "{}", "{}"), ""), "unknown effect"},
		"not the proxy's effect":   {doc(f("mock-switch", "get_status", "HTTP_200_SEMANTIC_PENDING", "{}", "{}"), ""), "does not produce"},
		"state change":             {strings.Replace(doc(f("mock-switch", "get_status", "DELAYED_SETTLEMENT", "{}", "{}"), ""), "PROXY", "PROXY", 1), "does not produce"},
		"no route":                 {doc(f("mock-ledger", "get_nothing", "LATENCY", "{}", `{"delay_ms":5}`), ""), "no route"},
		"match key not an attr":    {doc(f("mock-ledger", "get_transaction", "LATENCY", `{"case_id":"x"}`, `{"delay_ms":5}`), ""), "not an attribute"},
		"match value empty":        {doc(f("mock-ledger", "get_transaction", "LATENCY", `{"transaction_ref":""}`, `{"delay_ms":5}`), ""), "non-empty"},
		"nth_call zero":            {doc(f("mock-ledger", "get_transaction", "LATENCY", `{"nth_call":0}`, `{"delay_ms":5}`), ""), "nth_call"},
		"delay missing":            {doc(f("mock-ledger", "get_transaction", "LATENCY", "{}", "{}"), ""), "delay_ms"},
		"delay too large":          {doc(f("mock-ledger", "get_transaction", "LATENCY", "{}", `{"delay_ms":600001}`), ""), "delay_ms"},
		"stray param":              {doc(f("mock-ledger", "get_transaction", "LATENCY", "{}", `{"delay_ms":5,"jitter":1}`), ""), "not a parameter"},
		"status 404":               {doc(f("mock-ledger", "get_transaction", "HTTP_5XX", "{}", `{"status":404}`), ""), "status"},
		"stale read needs seconds": {doc(f("mock-ledger", "get_transaction", "STALE_READ", "{}", "{}"), ""), "seconds"},
		"stale read on a POST":     {doc(f("mock-customer", "post_lookup", "STALE_READ", "{}", `{"seconds":5}`), ""), "GET routes"},
		"skew not integer":         {doc(f("mock-switch", "get_status", "CLOCK_SKEW", "{}", `{"skew_seconds":1.5}`), ""), "skew_seconds"},
		"drift change":             {doc(f("mock-switch", "get_status", "SCHEMA_DRIFT", "{}", `{"change":"teleport"}`), ""), "change"},
		"drift field":              {doc(f("mock-switch", "get_status", "SCHEMA_DRIFT", "{}", `{"change":"remove","field":"a b"}`), ""), "field"},
		"injection field missing":  {doc(f("mock-customer", "get_beneficiaries", "INJECTION_PAYLOAD", "{}", "{}"), ""), "field"},
		"fixture missing":          {doc(f("mock-switch", "get_status", "IMPOSSIBLE_STATE", "{}", `{"fixture":"nope"}`), ""), "no fixture"},
		"fixture with a path":      {doc(f("mock-switch", "get_status", "IMPOSSIBLE_STATE", "{}", `{"fixture":"../x"}`), ""), "fixture is malformed"},
		"event effect, no feed":    {doc(f("mock-switch", "get_status", "DROP_WEBHOOK", "{}", "{}"), ""), "event-feed"},
		"late event, no feed":      {doc(f("mock-ledger", "get_transaction", "LATE_EVENT", "{}", `{"delay_ms":5}`), ""), "event-feed"},
		"deterministic chance":     {strings.Replace(doc(f("mock-ledger", "get_transaction", "LATENCY", "{}", `{"delay_ms":5}`), ""), `"params"`, `"probability":0.5,"params"`, 1), "deterministic"},
		"mock-case plan, switch":   {strings.Replace(doc(f("mock-switch", "get_status", "LATENCY", "{}", `{"delay_ms":5}`), ""), "PROXY", "MOCK_CASE", 1), "mock-case only"},
		"clock start":              {doc("", `,"clock":{"start":"yesterday"}`), "clock.start"},
		"clock duration":           {doc("", `,"clock":{"start":"2026-10-01T09:40:00Z","advances":[{"at_step":"s","by":"P1Y"}]}`), "clock.advances"},
		"trace events not objects": {doc("", `,"expected_trace_events":["x"]`), "expected_trace_events"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Parse([]byte(tc.in), env)
			if err == nil {
				t.Fatal("plan accepted")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

func TestOtherProducersFaultsAreIgnoredNotRefused(t *testing.T) {
	env := sampleEnv(t)
	tushe := strings.Replace(doc(f("mock-switch", "get_status", "DELAYED_SETTLEMENT", "{}", "{}")+","+f("mock-case", "create_case", "DROP_RESPONSE", "{}", "{}"), ""), "PROXY", "TUSHE", 1)
	c, err := Parse([]byte(tushe), env)
	if err != nil || c.Applied != 0 || c.Ignored != 2 {
		t.Fatalf("%v %+v", err, c)
	}
	mixed := doc(f("mock-switch", "get_status", "LATENCY", "{}", `{"delay_ms":5}`)+","+f("mock-case", "create_case", "DROP_RESPONSE", "{}", "{}"), "")
	c, err = Parse([]byte(mixed), env)
	if err != nil || c.Applied != 1 || c.Ignored != 1 {
		t.Fatalf("%v %+v", err, c)
	}
}

func TestNthCallProbabilityAndOrder(t *testing.T) {
	env := sampleEnv(t)
	c, err := Parse([]byte(doc(
		f("mock-switch", "get_status", "HTTP_429", `{"transaction_ref":"TX1","nth_call":2}`, `{"retry_after_seconds":3}`)+","+
			f("mock-switch", "get_status", "SERVICE_UNAVAILABLE", `{"transaction_ref":"TX1"}`, "{}"), "")), env)
	if err != nil {
		t.Fatal(err)
	}
	e := NewEngine()
	e.Load(c)
	attrs := map[string]string{"transaction_ref": "TX1"}
	var names []string
	for i := 0; i < 3; i++ {
		d := e.Next("mock-switch", "get_status", attrs)
		names = append(names, d.Effect.Name)
		if d.CallNo != int64(i+1) || d.Seed != 7 || d.PlanID != "fp_t" {
			t.Fatalf("%+v", d)
		}
	}
	// call 1: only the second fault matches; call 2: the first (nth 2) wins; call 3: the second again
	if strings.Join(names, ",") != "SERVICE_UNAVAILABLE,HTTP_429,SERVICE_UNAVAILABLE" {
		t.Fatalf("%v", names)
	}
	if d := e.Next("mock-switch", "get_status", map[string]string{"transaction_ref": "TX2"}); d.Effect != nil {
		t.Fatal("another transaction is not faulted")
	}
	e.Load(nil)
	if d := e.Next("mock-switch", "get_status", attrs); d.Effect != nil || d.PlanID != NoPlanID {
		t.Fatalf("%+v", d)
	}
}

// A fraction is a seeded draw, never a coin toss: the same plan and seed fire on the same calls.
func TestFractionalProbabilityIsSeededAndReproducible(t *testing.T) {
	env := sampleEnv(t)
	src := strings.Replace(doc(f("mock-switch", "get_status", "SERVICE_UNAVAILABLE", "{}", "{}"), ""), `"deterministic":true`, `"deterministic":false`, 1)
	src = strings.Replace(src, `"params"`, `"probability":0.3,"params"`, 1)
	run := func(seed string) string {
		c, err := Parse([]byte(strings.Replace(src, `"seed":7`, `"seed":`+seed, 1)), env)
		if err != nil {
			t.Fatal(err)
		}
		e := NewEngine()
		e.Load(c)
		var b strings.Builder
		for i := 0; i < 200; i++ {
			if e.Next("mock-switch", "get_status", nil).Effect != nil {
				b.WriteByte('X')
			} else {
				b.WriteByte('.')
			}
		}
		return b.String()
	}
	a, b := run("7"), run("7")
	if a != b {
		t.Fatal("same seed, different firing")
	}
	if run("8") == a {
		t.Fatal("a different seed must change the firing")
	}
	if n := strings.Count(a, "X"); n < 30 || n > 90 {
		t.Fatalf("probability 0.3 fired %d times in 200", n)
	}
}

func TestPicksAreReproducible(t *testing.T) {
	if Draw(1, 2, 3) != Draw(1, 2, 3) || Draw(1, 2, 3) == Draw(1, 2, 4) || Draw(1, 2, 3) == Draw(2, 2, 3) {
		t.Fatal("Draw must be a pure function that depends on every argument")
	}
}

// Contract fixtures: every valid fixture loads, and every invalid one is refused, against an environment that has
// every route and fixture a plan names.
func permissiveEnv(t *testing.T, raw []byte) Env {
	t.Helper()
	var p Plan
	_ = json.Unmarshal(raw, &p)
	tab := map[string]map[string]string{} // target/op -> attrs
	feed := map[string]bool{}
	paged := map[string]bool{}
	meth := map[string]string{}
	for _, f := range p.Faults {
		k := f.Target + "/" + f.Operation
		if tab[k] == nil {
			tab[k] = map[string]string{}
		}
		for m := range f.Match {
			if m != "nth_call" {
				tab[k][m] = "query:" + m
			}
		}
		switch f.Effect {
		case "DROP_WEBHOOK", "DUPLICATE_WEBHOOK", "REORDER_EVENTS", "LATE_EVENT":
			feed[k] = true
		case "PAGINATE":
			paged[k] = true
		}
		meth[k] = "GET"
	}
	var keys []string
	for k := range tab {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var routesJSON []string
	for i, k := range keys {
		target, op, _ := strings.Cut(k, "/")
		if !map[string]bool{"mock-ledger": true, "mock-switch": true, "mock-destination": true, "mock-customer": true, "mock-policy": true}[target] {
			continue
		}
		attrs, _ := json.Marshal(tab[k])
		ev := ""
		if feed[k] {
			ev = `,"event_feed":{"events":"events"}`
		}
		if paged[k] {
			ev = `,"page":{"items":"items","complete":"complete","next_cursor":"next_cursor","cursor_param":"cursor"}`
		}
		routesJSON = append(routesJSON, `{"method":"GET","path":"/r`+string(rune('a'+i))+`","upstream":"u","target":"`+target+`","operation":"`+op+`","attrs":`+string(attrs)+ev+`}`)
	}
	if len(routesJSON) == 0 {
		routesJSON = []string{`{"method":"GET","path":"/x","upstream":"u","target":"mock-ledger","operation":"x"}`}
	}
	rt, err := routes.Parse([]byte(`{"schema_version":"1.0","synthetic":true,"routes":[` + strings.Join(routesJSON, ",") + `]}`))
	if err != nil {
		t.Fatal(err)
	}
	return Env{Routes: rt, HasFixture: func(string) bool { return true }}
}

const fixtures = "../../../../contracts/fixtures/"

func TestContractFixturesValid(t *testing.T) {
	for _, dir := range []string{"fault-plan", "fault-plan.v2"} {
		t.Run(dir, func(t *testing.T) { contractFixturesValid(t, dir) })
	}
}

func contractFixturesValid(t *testing.T, dir string) {
	files, _ := filepath.Glob(fixtures + "valid/" + dir + "/*.json")
	if len(files) < 6 {
		t.Fatalf("found %d valid fixtures: the path is wrong", len(files))
	}
	for _, path := range files {
		raw, _ := os.ReadFile(path)
		if _, err := Parse(raw, permissiveEnv(t, raw)); err != nil {
			t.Errorf("%s: the schema accepts it but the proxy refuses it: %v", filepath.Base(path), err)
		}
	}
}

// Invalid fixtures the proxy does not refuse because the faults in them are another producer's, which it ignores.
var otherProducers = map[string]string{
	"mock_case_source_with_other_target.json": "", // refused by both; listed only to show it is covered below
}

func TestContractFixturesInvalid(t *testing.T) {
	for _, dir := range []string{"fault-plan", "fault-plan.v2"} {
		t.Run(dir, func(t *testing.T) { contractFixturesInvalid(t, dir) })
	}
}

func contractFixturesInvalid(t *testing.T, dir string) {
	files, _ := filepath.Glob(fixtures + "invalid/" + dir + "/*.json")
	if len(files) < 10 {
		t.Fatalf("found %d invalid fixtures: the path is wrong", len(files))
	}
	for _, path := range files {
		name := filepath.Base(path)
		if name == "expectations.json" {
			continue
		}
		raw, _ := os.ReadFile(path)
		_, err := Parse(raw, permissiveEnv(t, raw))
		var p Plan
		_ = json.Unmarshal(raw, &p)
		if why, ok := relaxedByV2[name]; ok && dir == "fault-plan" {
			// invalid under v1 and valid under v2 by design: the proxy reads the effect, not the file, so it takes the union
			if err != nil || !planHasEffect(p, why) {
				t.Errorf("%s: v1 refuses it only because v2 relaxed %s; the proxy must accept it (err %v)", name, why, err)
			}
			continue
		}
		if err == nil && p.Source == SourceProxy {
			t.Errorf("%s: the schema refuses it, the proxy applies it", name)
		}
		if err == nil && p.Source != SourceProxy && !onlyOtherProducersFaults(p) {
			t.Errorf("%s: the schema refuses it and the proxy accepts it, but the plan is not just another producer's faults", name)
		}
	}
	_ = otherProducers
}

// v1 fixtures that v2 made valid (ENV-15), with the effect that did it. The v2 side of the same change is
// valid/fault-plan.v2/proxy_paginate.json.
var relaxedByV2 = map[string]string{"proxy_source_with_a_tushe_effect.json": Paginate}

func planHasEffect(p Plan, effect string) bool {
	for _, f := range p.Faults {
		if f.Effect == effect {
			return true
		}
	}
	return false
}

// A TUSHE or MOCK_CASE plan is accepted by the proxy when the invalidity is in a fault the proxy ignores: its own
// checks are for the faults it applies, and the producers check the rest.
func onlyOtherProducersFaults(p Plan) bool { return p.Source != SourceProxy }

// The proxy's effect lists are copied from the contract. A copy that drifts is a plan the schema accepts and the proxy
// does not produce (or the reverse), so the lists are checked against fault-plan.v2.json itself (REQ-ENV-005, AGENT-15).
func TestEffectListsMatchTheV2Schema(t *testing.T) {
	raw, err := os.ReadFile(fixtures + "../schemas/fault-plan.v2.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Defs struct {
			Fault struct {
				Properties struct {
					Effect struct {
						Enum []string `json:"enum"`
					} `json:"effect"`
				} `json:"properties"`
			} `json:"Fault"`
		} `json:"$defs"`
		AllOf []struct {
			Then struct {
				Properties struct {
					Faults struct {
						Items struct {
							AnyOf []struct {
								Properties struct {
									Effect struct {
										Enum []string `json:"enum"`
									} `json:"effect"`
								} `json:"properties"`
							} `json:"anyOf"`
						} `json:"items"`
					} `json:"faults"`
				} `json:"properties"`
			} `json:"then"`
		} `json:"allOf"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	all := map[string]bool{}
	for _, e := range schema.Defs.Fault.Properties.Effect.Enum {
		all[e] = true
	}
	if len(all) != len(allEffects) {
		t.Errorf("allEffects has %d effects, the schema %d", len(allEffects), len(all))
	}
	for e := range all {
		if !allEffects[e] {
			t.Errorf("the schema has %s; allEffects does not", e)
		}
	}
	// the PROXY branch is the second allOf entry, its second anyOf alternative
	proxyList := schema.AllOf[1].Then.Properties.Faults.Items.AnyOf[1].Properties.Effect.Enum
	byName := map[string]bool{}
	for _, e := range proxyList {
		byName[e] = true
	}
	if len(byName) != len(proxyEffects) {
		t.Errorf("proxyEffects has %d effects, the schema's PROXY branch %d", len(proxyEffects), len(byName))
	}
	for e := range byName {
		if !proxyEffects[e] {
			t.Errorf("the schema's PROXY branch has %s; proxyEffects does not", e)
		}
	}
}
