package proxy_test

import (
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/benchmark/hostility_proxy/internal/numbers"
	"github.com/Noemai-org/noema/benchmark/hostility_proxy/internal/proxy"
	"github.com/Noemai-org/noema/benchmark/hostility_proxy/internal/routes"
)

// REQ-ENV-002: the control API opens only for the bench credential; a runtime identity is refused.
func TestControlApiOpensOnlyForTheBenchCredential(t *testing.T) {
	r := newRig(t)
	for _, c := range []struct{ method, path string }{
		{"GET", "/control/v1/fault-plan"}, {"PUT", "/control/v1/fault-plan"}, {"DELETE", "/control/v1/fault-plan"}, {"GET", "/control/v1/trace"},
		{"GET", "/control/v1/stats"}, {"GET", "/control/v1/clock"}, {"POST", "/control/v1/clock/advance"}, {"POST", "/control/v1/numbers/map"},
		{"POST", "/control/v1/reset"}, {"GET", "/control/v1/nothing"}, {"GET", "/"},
	} {
		for who, token := range map[string]string{"no credential": "", "a guessed one": "guess-guess-guess-guess-guess", "the number key": numberKey} {
			res, err := r.do(r.client, c.method, r.control.URL, c.path, token, `{}`)
			if err != nil || res.status != 401 {
				t.Errorf("%s -> %s %s: %v %d", who, c.method, c.path, err, res.status)
			}
			if strings.Contains(res.body, "plan") || strings.Contains(res.body, "entries") {
				t.Errorf("a refusal leaked: %s", res.body)
			}
		}
	}
	// the data plane exposes none of it
	for _, p := range []string{"/control/v1/fault-plan", "/control/v1/trace", "/control/v1/reset"} {
		if res := r.get(p); res.status != 404 {
			t.Errorf("data plane %s: %d", p, res.status)
		}
	}
	if r.ctl("GET", "/control/v1/fault-plan", "").status != 200 {
		t.Fatal("the bench credential opens it")
	}
}

func TestPlanControl(t *testing.T) {
	r := newRig(t)
	if v := decode(t, r.ctl("GET", "/control/v1/fault-plan", "").body); v["fault_plan_id"] != "fp_none" || v["loaded"] != false {
		t.Fatalf("%v", v)
	}
	r.load(fault("mock-switch", "get_status", "SERVICE_UNAVAILABLE", `{}`, `{}`), "")
	for name, bad := range map[string]string{
		"not json": `{`, "wrong shape": `{"fault_plan_id":"x"}`,
		"effect the proxy does not produce": planDoc("PROXY", fault("mock-switch", "get_status", "HTTP_200_SEMANTIC_PENDING", `{}`, `{}`), ""),
		"no such route":                     planDoc("PROXY", fault("mock-switch", "get_nothing", "SERVICE_UNAVAILABLE", `{}`, `{}`), ""),
		"unknown fixture":                   planDoc("PROXY", fault("mock-switch", "get_status", "IMPOSSIBLE_STATE", `{}`, `{"fixture":"nope"}`), ""),
	} {
		res := r.ctl("PUT", "/control/v1/fault-plan", bad)
		if res.status != 422 || !strings.Contains(res.body, "INVALID_FAULT_PLAN") {
			t.Errorf("%s: %d %s", name, res.status, res.body)
		}
	}
	if r.get("/v1/status/TX1").status != 503 {
		t.Fatal("a refused plan must leave the loaded plan in place")
	}
	if v := decode(t, r.ctl("DELETE", "/control/v1/fault-plan", "").body); v["fault_plan_id"] != "fp_none" {
		t.Fatalf("%v", v)
	}
	if r.get("/v1/status/TX1").status != 200 {
		t.Fatal("no plan, no fault")
	}
}

// A plan meant for another producer is stored but applies nothing here: Tushe's and mock-case's faults are not the proxy's.
func TestOtherProducersPlansApplyNothing(t *testing.T) {
	r := newRig(t)
	for _, source := range []string{"TUSHE", "MOCK_CASE"} {
		faults := fault("mock-switch", "get_status", "DELAYED_SETTLEMENT", `{}`, `{}`) + "," + fault("mock-case", "create_case", "DROP_RESPONSE", `{}`, `{}`)
		if source == "MOCK_CASE" {
			faults = fault("mock-case", "create_case", "DROP_RESPONSE", `{}`, `{}`)
		}
		res := r.ctl("PUT", "/control/v1/fault-plan", planDoc(source, faults, ""))
		if res.status != 200 || !strings.Contains(res.body, `"applied_faults":0`) {
			t.Fatalf("%s: %d %s", source, res.status, res.body)
		}
		if r.get("/v1/status/TX1").status != 200 || len(r.trace().Entries) != 0 {
			t.Fatalf("%s: a fault was applied", source)
		}
	}
	// a PROXY plan with a mock-case fault: the mock-case fault is counted, not applied
	res := r.ctl("PUT", "/control/v1/fault-plan", planDoc("PROXY", fault("mock-switch", "get_status", "SERVICE_UNAVAILABLE", `{"nth_call":1}`, `{}`)+","+fault("mock-case", "create_case", "DROP_RESPONSE", `{}`, `{}`), ""))
	if !strings.Contains(res.body, `"applied_faults":1`) || !strings.Contains(res.body, `"ignored_faults":1`) {
		t.Fatal(res.body)
	}
}

func TestClockControl(t *testing.T) {
	r := newRig(t)
	if res := r.ctl("POST", "/control/v1/clock/advance", `{"by":"PT1S"}`); res.status != 409 {
		t.Fatalf("%d", res.status)
	}
	r.load("", simClock)
	if res := r.ctl("GET", "/control/v1/clock", ""); !strings.Contains(res.body, `"2026-10-01T08:40:00Z"`) || !strings.Contains(res.body, `"SYNTHETIC"`) {
		t.Fatal(res.body)
	}
	if res := r.ctl("POST", "/control/v1/clock/advance", `{"step":"later"}`); !strings.Contains(res.body, `08:40:10Z`) {
		t.Fatal(res.body)
	}
	for _, bad := range []string{`{}`, `{"by":"PT1S","step":"later"}`, `{"step":"never"}`, `{"by":"P1Y"}`, `{"by":"PT1S","x":1}`} {
		if res := r.ctl("POST", "/control/v1/clock/advance", bad); res.status != 400 && res.status != 404 {
			t.Errorf("%s: %d", bad, res.status)
		}
	}
}

func TestNumberMapForTheRunner(t *testing.T) {
	r := newRig(t)
	res := r.ctl("POST", "/control/v1/numbers/map", `{"numbers":["9999041234","9999100200"]}`)
	m, _ := numbers.New([]byte(numberKey))
	a, _ := m.Synthetic("9999041234")
	if res.status != 200 || !strings.Contains(res.body, `"9999041234":"`+a+`"`) {
		t.Fatalf("%d %s", res.status, res.body)
	}
	for _, bad := range []string{`{"numbers":[]}`, `{"numbers":["0803123456"]}`, `{"numbers":["99990412345"]}`, `{}`} {
		if res := r.ctl("POST", "/control/v1/numbers/map", bad); res.status != 400 && res.status != 422 {
			t.Errorf("%s: %d", bad, res.status)
		}
	}
}

func TestResetForgetsTheRun(t *testing.T) {
	r := newRig(t)
	r.load(fault("mock-switch", "get_status", "SERVICE_UNAVAILABLE", `{"nth_call":1}`, `{}`), simClock)
	r.get("/v1/status/TX1")
	if len(r.trace().Entries) != 1 {
		t.Fatal("setup")
	}
	r.ctl("POST", "/control/v1/reset", "")
	if len(r.trace().Entries) != 0 || !strings.Contains(r.ctl("GET", "/control/v1/clock", "").body, `"simulated":false`) {
		t.Fatal("reset must clear the trace and go back to the wall clock")
	}
	if r.get("/v1/status/TX1").status != 200 {
		t.Fatal("reset unloads the plan")
	}
}

// REQ-ENV-004: the same plan and seed, run twice, give identical fault traces and identical answers; a different
// seed changes what the seed chooses.
func TestReplayFromTheSameSeedIsIdentical(t *testing.T) {
	faults := fault("mock-destination", "events_feed", "REORDER_EVENTS", `{"nth_call":1}`, `{}`) + "," +
		fault("mock-customer", "get_beneficiaries", "INJECTION_PAYLOAD", `{"nth_call":1}`, `{"field":"name"}`) + "," +
		fault("mock-switch", "get_status", "CLOCK_SKEW", `{"nth_call":2}`, `{"skew_seconds":30}`) + "," +
		fault("mock-switch", "get_status", "SCHEMA_DRIFT", `{"nth_call":3}`, `{"change":"unknown_enum","field":"semantic_status"}`) + "," +
		fault("mock-ledger", "get_transaction", "STALE_READ", `{"nth_call":3}`, `{"seconds":1}`) + "," +
		fault("mock-destination", "relay_pull", "LATE_EVENT", `{"nth_call":1}`, `{"delay_ms":1000}`) + "," +
		fault("mock-customer", "post_lookup", "HTTP_5XX", `{"nth_call":1}`, `{"status":503}`)
	run := func(r *rig, seed string) (string, string) {
		res := r.ctl("PUT", "/control/v1/fault-plan", strings.Replace(planDoc("PROXY", faults, simClock), `"seed":42`, `"seed":`+seed, 1))
		if res.status != 200 {
			t.Fatal(res.body)
		}
		var seen strings.Builder
		for _, p := range []string{"/v1/events", "/v1/beneficiaries", "/v1/status/TX1", "/v1/status/TX1", "/v1/status/TX1", "/v1/transactions/TX1", "/v1/transactions/TX1", "/v1/transactions/TX1", "/relay/deliveries"} {
			if p == "/v1/transactions/TX1" {
				r.ctl("POST", "/control/v1/clock/advance", `{"step":"later"}`)
			}
			seen.WriteString(r.get(p).body)
		}
		seen.WriteString(r.post("/v1/lookups", `{"q":1}`).body)
		return seen.String(), r.trace().Hash
	}
	a := newRig(t)
	bodies1, hash1 := run(a, "42")
	a.ctl("POST", "/control/v1/reset", "")
	bodies2, hash2 := run(a, "42")
	if bodies1 != bodies2 || hash1 != hash2 {
		t.Fatalf("replay differs:\n%s\n%s\n%s %s", bodies1, bodies2, hash1, hash2)
	}
	b := newRig(t)
	bodies3, hash3 := run(b, "42")
	if bodies3 != bodies1 || hash3 != hash1 {
		t.Fatal("a fresh proxy with the same plan and seed must give the same run")
	}
	different := false
	for _, seed := range []string{"1", "2", "3", "4", "5", "6", "7", "8"} {
		c := newRig(t)
		if bodies4, hash4 := run(c, seed); bodies4 != bodies1 || hash4 != hash1 {
			different = true
			break
		}
	}
	if !different {
		t.Fatal("the seed must be able to change what the proxy chooses")
	}
	if n := strings.Count(a.ctl("GET", "/control/v1/trace", "").body, `"effect"`); n != 7 {
		t.Fatalf("seven faults fired, trace has %d", n)
	}
}

func TestNewRefusesARouteWithoutAnUpstreamURL(t *testing.T) {
	raw := `{"schema_version":"1.0","synthetic":true,"routes":[{"method":"GET","path":"/a","upstream":"tushe","target":"mock-ledger","operation":"op"}]}`
	tab, err := routes.Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	m, _ := numbers.New([]byte(numberKey))
	_, err = proxy.New(proxy.Options{Routes: tab, Upstreams: map[string]*url.URL{}, Numbers: m, Log: slog.New(slog.NewJSONHandler(io.Discard, nil))})
	if err == nil || !strings.Contains(err.Error(), "tushe") {
		t.Fatalf("%v", err)
	}
	_ = fmt.Sprint
}

func TestFixturesMustBeLabelledSynthetic(t *testing.T) {
	for name, doc := range map[string]string{
		"not labelled":  `{"synthetic":false,"status":200,"body":{}}`,
		"missing label": `{"status":200,"body":{}}`,
		"bad status":    `{"synthetic":true,"status":99,"body":{}}`,
		"no body":       `{"synthetic":true,"status":200}`,
		"unknown field": `{"synthetic":true,"status":200,"body":{},"x":1}`,
		"trailing":      `{"synthetic":true,"status":200,"body":{}} {}`,
		"body not json": `{"synthetic":true,"status":200,"body":nope}`,
	} {
		if _, err := proxy.ParseFixture([]byte(doc)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := proxy.ParseFixture([]byte(`{"synthetic":true,"status":200,"body":{"a":1}}`)); err != nil {
		t.Fatal(err)
	}
}
