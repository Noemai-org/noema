package api_test

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// Fault tests (REQ-ENV-008 failure behaviour, REQ-ENV-004, AGENT-12): every fault is injected on the real wire
// and observed by a real client; nothing is mocked.

func patient(e *env) *http.Client { return e.client }

func impatient(d time.Duration) *http.Client {
	return &http.Client{Timeout: d, Transport: &http.Transport{DisableKeepAlives: true}}
}

func (e *env) createWith(c *http.Client, k string) (resp, error) {
	e.t.Helper()
	return e.send(c, "POST", e.caseURL+"/v1/cases", runtimeToken, newKey(k), caseBody())
}

// createWithFor is createWith for another subject, so it is a second case and not a repeat of the first.
func (e *env) createWithFor(c *http.Client, k string, i int) (resp, error) {
	e.t.Helper()
	return e.send(c, "POST", e.caseURL+"/v1/cases", runtimeToken, newKey(k), caseBodyFor(fmt.Sprintf("TX%05d", i)))
}

type traceRow struct {
	Operation, Effect string
	Committed, Lost   bool
	Status            int
}

func (e *env) faultTrace() []traceRow {
	e.t.Helper()
	r := e.bench("GET", "/truth/v1/commands?faults_only=true", nil)
	var out []traceRow
	for _, c := range r.json["commands"].([]any) {
		m := c.(map[string]any)
		out = append(out, traceRow{m["operation"].(string), m["fault_effect"].(string), m["committed"].(bool), m["response_suppressed"].(bool), int(m["status_code"].(float64))})
	}
	return out
}

// US-06, EC-E02: the case is committed and the response is lost. The retry with the same key finds it.
func TestCommitThenDropIsReproducibleFromAPlan(t *testing.T) {
	e := newEnv(t)
	e.loadPlan(plan(fault("create_case", `{"nth_call":1}`, "DROP_RESPONSE", `{}`), simClock))
	if _, err := e.createWith(patient(e), "idk_1"); err == nil {
		t.Fatal("the response must be lost: the client must see a transport error, not a status")
	}
	if n := e.count("cases"); n != 1 {
		t.Fatalf("the command must have committed before the response was dropped: %d cases", n)
	}
	retry := e.createCase("idk_1")
	if retry.status != 200 || retry.header.Get("Idempotent-Replay") != "true" {
		t.Fatalf("the retry must find the committed case: %d %s", retry.status, retry.raw)
	}
	if e.count("cases") != 1 {
		t.Fatal("exactly one effect")
	}
	trace := e.faultTrace()
	if len(trace) != 1 || trace[0] != (traceRow{"create_case", "DROP_RESPONSE", true, true, 0}) {
		t.Fatalf("fault trace: %+v", trace)
	}
	cmds := e.bench("GET", "/truth/v1/commands", nil).json["commands"].([]any)
	first := cmds[0].(map[string]any)
	if first["scenario_id"] != "SCN-TEST" || first["seed"].(float64) != 42 || first["fault_plan_id"] != "fp_test" {
		t.Fatalf("every command is logged with scenario, seed and fault-plan ID: %v", first)
	}
}

func TestTimeoutAfterCommitHoldsThenDrops(t *testing.T) {
	e := newEnv(t)
	e.loadPlan(plan(fault("create_case", `{"nth_call":1}`, "TIMEOUT_AFTER_COMMIT", `{"delay_ms":300}`), ""))
	start := time.Now()
	if _, err := e.createWith(impatient(100*time.Millisecond), "idk_1"); err == nil {
		t.Fatal("the client must time out")
	}
	if time.Since(start) > 250*time.Millisecond {
		t.Fatal("the client gave up at its own timeout")
	}
	if e.count("cases") != 1 {
		t.Fatal("the case must be committed")
	}
	// A patient client waits the whole delay and then loses the connection: it never gets a status.
	e.loadPlan(plan(fault("create_case", `{"nth_call":1}`, "TIMEOUT_AFTER_COMMIT", `{"delay_ms":150}`), ""))
	start = time.Now()
	r, err := e.createWithFor(patient(e), "idk_2", 2)
	if err == nil {
		t.Fatalf("got a response %d", r.status)
	}
	if time.Since(start) < 140*time.Millisecond {
		t.Fatalf("the response must be held for delay_ms, was %v", time.Since(start))
	}
	if e.count("cases") != 2 {
		t.Fatal("both cases committed")
	}
}

func TestTimeoutBeforeCommitLeavesNoTrace(t *testing.T) {
	e := newEnv(t)
	e.loadPlan(plan(fault("create_case", `{"nth_call":1}`, "TIMEOUT_BEFORE_COMMIT", `{"delay_ms":50}`), ""))
	if _, err := e.createWith(patient(e), "idk_1"); err == nil {
		t.Fatal("the connection must be dropped")
	}
	if e.count("cases") != 0 {
		t.Fatal("nothing may be committed")
	}
	if r := e.createCase("idk_1"); r.status != 201 {
		t.Fatalf("the retry is a first create: %d %s", r.status, r.raw)
	}
	trace := e.faultTrace()
	if len(trace) != 1 || trace[0].Committed || !trace[0].Lost {
		t.Fatalf("%+v", trace)
	}
}

func TestHTTP5XXBeforeAndAfterCommit(t *testing.T) {
	e := newEnv(t)
	e.loadPlan(plan(fault("create_case", `{"nth_call":1}`, "HTTP_5XX", `{"status":503}`)+","+
		fault("create_case", `{"nth_call":2}`, "HTTP_5XX", `{"status":502,"after_commit":true}`), ""))
	before := e.createCase("idk_1")
	if before.status != 503 || before.errCode() != "UNAVAILABLE" || before.json["error"].(map[string]any)["retryable"] != true {
		t.Fatalf("%d %s", before.status, before.raw)
	}
	if e.count("cases") != 0 {
		t.Fatal("a 5xx before commit applies nothing")
	}
	after := e.createCase("idk_1")
	if after.status != 502 {
		t.Fatalf("%d %s", after.status, after.raw)
	}
	if e.count("cases") != 1 {
		t.Fatal("a 5xx after commit means the case exists although the caller was told an error")
	}
	if r := e.createCase("idk_1"); r.status != 200 || r.header.Get("Idempotent-Replay") != "true" {
		t.Fatalf("%d %s", r.status, r.raw)
	}
	tr := e.faultTrace()
	if len(tr) != 2 || tr[0] != (traceRow{"create_case", "HTTP_5XX", false, false, 503}) || tr[1] != (traceRow{"create_case", "HTTP_5XX", true, false, 502}) {
		t.Fatalf("%+v", tr)
	}
}

func TestHTTP429WithAndWithoutRetryAfter(t *testing.T) {
	e := newEnv(t)
	e.loadPlan(plan(fault("create_case", `{"nth_call":1}`, "HTTP_429", `{"retry_after_seconds":7}`)+","+fault("create_case", `{"nth_call":2}`, "HTTP_429", `{}`), ""))
	with := e.createCase("idk_1")
	if with.status != 429 || with.header.Get("Retry-After") != "7" || with.errCode() != "RATE_LIMITED" || with.json["error"].(map[string]any)["retryable"] != true {
		t.Fatalf("%d %v %s", with.status, with.header, with.raw)
	}
	without := e.createCase("idk_1")
	if without.status != 429 || without.header.Get("Retry-After") != "" {
		t.Fatalf("%d %v", without.status, without.header)
	}
	if e.count("cases") != 0 {
		t.Fatal("a 429 applies nothing")
	}
	if r := e.createCase("idk_1"); r.status != 201 {
		t.Fatalf("after the faults, the call succeeds: %d", r.status)
	}
}

func TestLatencyDelaysThenSucceeds(t *testing.T) {
	e := newEnv(t)
	e.loadPlan(plan(fault("create_case", `{"nth_call":1}`, "LATENCY", `{"delay_ms":200}`), ""))
	start := time.Now()
	r := e.createCase("idk_1")
	if r.status != 201 || time.Since(start) < 190*time.Millisecond {
		t.Fatalf("%d after %v", r.status, time.Since(start))
	}
}

// EC-E13: PAGINATE forces a page size, with an explicit completeness marker, and no item is lost across pages.
func TestPaginateFaultForcesSmallPages(t *testing.T) {
	e := newEnv(t)
	for i := 0; i < 7; i++ {
		e.createCaseFor(fmt.Sprintf("idk_%d", i), i)
	}
	e.loadPlan(plan(fault("find_cases", `{}`, "PAGINATE", `{"page_size":3}`), ""))
	var refs []string
	cursor, pages := "", 0
	for {
		q := "/v1/cases?tenant_id=ten_a&principal_id=usr_1&limit=100"
		if cursor != "" {
			q += "&cursor=" + cursor
		}
		r := e.runtime("GET", q, nil, nil)
		pages++
		if len(r.items()) > 3 {
			t.Fatalf("page of %d under PAGINATE 3", len(r.items()))
		}
		for _, it := range r.items() {
			refs = append(refs, it["reference"].(string))
		}
		if r.json["complete"] == true {
			break
		}
		if pages == 1 && r.json["complete"] != false {
			t.Fatal("the first page is incomplete and must say so")
		}
		cursor, _ = r.json["next_cursor"].(string)
	}
	if len(refs) != 7 || pages != 3 {
		t.Fatalf("%d items over %d pages: %v", len(refs), pages, refs)
	}
}

// US-19, EC-K01: a competing modification lands between the caller's read and its route.
func TestConcurrentModificationFault(t *testing.T) {
	e := newEnv(t)
	c := e.createCase("idk_1")
	e.loadPlan(plan(fault("route_case", `{"nth_call":1}`, "CONCURRENT_MODIFICATION", `{}`), ""))
	path := "/v1/cases/" + c.str("case_id") + "/route"
	body := map[string]any{"tenant_id": "ten_a", "owner_queue": "DISPUTES", "expected_version_token": "v1"}
	r := e.runtime("POST", path, nil, body)
	if r.status != 409 || r.errCode() != "VERSION_MISMATCH" || !strings.Contains(string(r.raw), `"v2"`) {
		t.Fatalf("%d %s", r.status, r.raw)
	}
	read := e.runtime("GET", "/v1/cases/"+c.str("case_id")+"?tenant_id=ten_a", nil, nil)
	if read.str("version_token") != "v2" || read.str("owner_queue") != "PAYMENTS_OPERATIONS" {
		t.Fatalf("the competing modification is real, the route is not applied: %s", read.raw)
	}
	body["expected_version_token"] = "v2"
	if ok := e.runtime("POST", path, nil, body); ok.status != 200 || ok.str("version_token") != "v3" {
		t.Fatalf("routing from the new token works: %d %s", ok.status, ok.raw)
	}
}

// The caller must not be able to tell an injected fault from a real one: no header or field names it.
func TestFaultsAreBlindToTheCaller(t *testing.T) {
	e := newEnv(t)
	normal := e.createCase("idk_0")
	e.loadPlan(plan(fault("create_case", `{"nth_call":1}`, "HTTP_5XX", `{"status":503}`)+","+fault("create_case", `{"nth_call":2}`, "HTTP_429", `{"retry_after_seconds":1}`), ""))
	for i := 1; i <= 2; i++ {
		r := e.createCaseFor(fmt.Sprintf("idk_%d", i), i)
		for k := range r.header {
			switch http.CanonicalHeaderKey(k) {
			case "Content-Type", "Cache-Control", "X-Mock-Case-Synthetic", "Date", "Content-Length", "Retry-After":
			default:
				t.Errorf("header %s on an injected response", k)
			}
		}
		low := strings.ToLower(string(r.raw))
		for _, leak := range []string{"fault", "inject", "effect", "plan", "scenario", "seed", "mock"} {
			if strings.Contains(low, leak) {
				t.Errorf("injected response mentions %q: %s", leak, r.raw)
			}
		}
	}
	_ = normal
}

func TestMatchKeysSelectTheCall(t *testing.T) {
	e := newEnv(t)
	e.loadPlan(plan(fault("create_case", `{"idempotency_key":"idk_target"}`, "HTTP_5XX", `{"status":500}`), ""))
	if r := e.createCase("idk_other"); r.status != 201 {
		t.Fatalf("%d", r.status)
	}
	if r := e.createCase("idk_target"); r.status != 500 {
		t.Fatalf("%d", r.status)
	}
	if r := e.createCase("idk_target"); r.status != 500 {
		t.Fatalf("with no nth_call the fault applies to every matching call: %d", r.status)
	}
}

func TestMalformedCallIsCountedButNeverFaulted(t *testing.T) {
	e := newEnv(t)
	e.loadPlan(plan(fault("create_case", `{"nth_call":2}`, "HTTP_5XX", `{"status":500}`), ""))
	if r := e.runtime("POST", "/v1/cases", ifAbsent(nil), caseBody()); r.status != 400 { // call 1, no key
		t.Fatalf("%d", r.status)
	}
	if r := e.createCase("idk_1"); r.status != 500 { // call 2
		t.Fatalf("the second call is faulted: %d", r.status)
	}
	bad := e.runtime("POST", "/v1/cases", newKey("idk_2"), `{`) // call 3: nothing to fault
	if bad.status != 400 {
		t.Fatalf("%d", bad.status)
	}
}

func TestPlanControl(t *testing.T) {
	e := newEnv(t)
	if r := e.bench("GET", "/control/v1/fault-plan", nil); r.str("fault_plan_id") != "fp_none" || r.json["loaded"] != false {
		t.Fatalf("%s", r.raw)
	}
	e.loadPlan(plan(fault("create_case", `{"nth_call":1}`, "HTTP_5XX", `{}`), ""))
	for _, bad := range []string{`{`, `{"fault_plan_id":"x"}`, plan(fault("create_case", `{}`, "SPLIT_STATE", `{}`), ""),
		strings.Replace(plan("", ""), `"MOCK_CASE"`, `"HARNESS"`, 1)} {
		r := e.bench("PUT", "/control/v1/fault-plan", bad)
		if r.status != 422 || r.errCode() != "INVALID_FAULT_PLAN" {
			t.Errorf("%q: %d %s", bad, r.status, r.raw)
		}
	}
	if e.bench("GET", "/control/v1/fault-plan", nil).str("fault_plan_id") != "fp_test" {
		t.Fatal("a refused plan must leave the loaded plan in place")
	}
	if e.createCase("idk_1").status != 500 {
		t.Fatal("the loaded plan still applies")
	}
	e.loadPlan(plan(fault("create_case", `{"nth_call":1}`, "HTTP_5XX", `{}`), "")) // loading resets the counters
	if e.createCase("idk_2").status != 500 {
		t.Fatal("reloading must start the counters again")
	}
	if r := e.bench("DELETE", "/control/v1/fault-plan", nil); r.str("fault_plan_id") != "fp_none" {
		t.Fatalf("%s", r.raw)
	}
	if e.createCase("idk_3").status != 201 {
		t.Fatal("no plan, no fault")
	}
}

func TestSimulatedClockStartsAdvancesAndIsLabelledSynthetic(t *testing.T) {
	e := newEnv(t)
	if r := e.bench("POST", "/control/v1/clock/advance", map[string]any{"by": "PT1S"}); r.status != 409 || r.errCode() != "CLOCK_NOT_SIMULATED" {
		t.Fatalf("%d %s", r.status, r.raw)
	}
	e.loadPlan(plan("", simClock))
	c := e.bench("GET", "/control/v1/clock", nil)
	if c.str("clock_source") != "SYNTHETIC" || c.json["simulated"] != true || c.str("now") != "2026-10-01T08:40:00Z" {
		t.Fatalf("%s", c.raw)
	}
	first := e.createCase("idk_1")
	if first.str("created_at") != "2026-10-01T08:40:00Z" {
		t.Fatalf("created_at follows the simulated clock, in UTC: %s", first.raw)
	}
	if r := e.bench("POST", "/control/v1/clock/advance", map[string]any{"step": "after_receipt"}); r.str("now") != "2026-10-01T08:40:31Z" {
		t.Fatalf("%s", r.raw)
	}
	if r := e.bench("POST", "/control/v1/clock/advance", map[string]any{"by": "PT1M"}); r.str("now") != "2026-10-01T08:41:31Z" {
		t.Fatalf("%s", r.raw)
	}
	second := e.createCaseFor("idk_2", 2)
	if second.str("created_at") != "2026-10-01T08:41:31Z" {
		t.Fatalf("%s", second.raw)
	}
	for name, body := range map[string]any{"unknown step": map[string]any{"step": "never"}, "both": map[string]any{"by": "PT1S", "step": "after_receipt"},
		"neither": map[string]any{}, "bad duration": map[string]any{"by": "P1Y"}, "negative": map[string]any{"by": "PT-1S"}} {
		if r := e.bench("POST", "/control/v1/clock/advance", body); r.status != 400 && r.status != 404 {
			t.Errorf("%s: %d %s", name, r.status, r.raw)
		}
	}
	if e.bench("GET", "/control/v1/clock", nil).str("now") != "2026-10-01T08:41:31Z" {
		t.Fatal("a refused advance must not move the clock")
	}
}

// REQ-ENV-004: the same plan and seed, run twice, give identical identifiers, state and fault trace.
func TestReplayFromTheSameSeedIsIdentical(t *testing.T) {
	e := newEnv(t)
	faults := fault("create_case", `{"idempotency_key":"idk_1","nth_call":1}`, "DROP_RESPONSE", `{}`) + "," + fault("route_case", `{"nth_call":1}`, "HTTP_5XX", `{"status":503,"after_commit":true}`)
	run := func() (state, trace string, caseID string) {
		e.loadPlan(plan(faults, simClock))
		e.createWith(patient(e), "idk_1") //nolint:errcheck
		c := e.createCase("idk_1")
		e.bench("POST", "/control/v1/clock/advance", map[string]any{"step": "after_receipt"})
		e.runtime("POST", "/v1/cases/"+c.str("case_id")+"/route", nil, map[string]any{"tenant_id": "ten_a", "owner_queue": "DISPUTES", "expected_version_token": "v1"})
		e.runtime("POST", "/v1/cases/"+c.str("case_id")+"/complaints", key("idk_c"), map[string]any{"tenant_id": "ten_a"})
		e.runtime("POST", "/v1/cases/"+c.str("case_id")+"/evidence", key("idk_e"), map[string]any{"tenant_id": "ten_a", "payload": "synthetic"})
		s, f := e.digest()
		return s, f, c.str("case_id")
	}
	s1, f1, id1 := run()
	if r := e.bench("POST", "/control/v1/reset", nil); r.status != 200 {
		t.Fatalf("%d %s", r.status, r.raw)
	}
	if e.count("cases") != 0 || e.bench("GET", "/control/v1/fault-plan", nil).str("fault_plan_id") != "fp_none" {
		t.Fatal("reset empties the data and unloads the plan")
	}
	s2, f2, id2 := run()
	if s1 != s2 || f1 != f2 || id1 != id2 {
		t.Fatalf("replay differs:\nstate %s vs %s\ntrace %s vs %s\nid %s vs %s", s1, s2, f1, f2, id1, id2)
	}
	// A different seed changes the identifiers: the seed is doing real work.
	e.bench("POST", "/control/v1/reset", nil)
	e.loadPlan(strings.Replace(plan(faults, simClock), `"seed":42`, `"seed":43`, 1))
	e.createWith(patient(e), "idk_1") //nolint:errcheck
	if other := e.createCase("idk_1"); other.str("case_id") == id1 {
		t.Fatal("a different seed must give different identifiers")
	}
}

func TestCommandLogPaging(t *testing.T) {
	e := newEnv(t)
	for i := 0; i < 5; i++ {
		e.createCaseFor(fmt.Sprintf("idk_%d", i), i)
	}
	r := e.bench("GET", "/truth/v1/commands?limit=2", nil)
	cmds := r.json["commands"].([]any)
	if len(cmds) != 2 || r.json["complete"] != false {
		t.Fatalf("%s", r.raw)
	}
	last := int(cmds[1].(map[string]any)["seq"].(float64))
	rest := e.bench("GET", fmt.Sprintf("/truth/v1/commands?after_seq=%d&limit=10", last), nil)
	if len(rest.json["commands"].([]any)) != 3 || rest.json["complete"] != true {
		t.Fatalf("%s", rest.raw)
	}
	for _, q := range []string{"limit=0", "limit=1001", "after_seq=-1", "faults_only=maybe", "other=1"} {
		if r := e.bench("GET", "/truth/v1/commands?"+q, nil); r.status != 400 {
			t.Errorf("%s: %d", q, r.status)
		}
	}
}

func TestTruthStateIsLabelledSyntheticAndComplete(t *testing.T) {
	e := newEnv(t)
	c := e.createCase("idk_1")
	e.runtime("POST", "/v1/cases/"+c.str("case_id")+"/complaints", key("idk_c"), map[string]any{"tenant_id": "ten_a"})
	e.runtime("POST", "/v1/cases/"+c.str("case_id")+"/evidence", key("idk_e"), map[string]any{"tenant_id": "ten_a", "payload": "x"})
	s := e.state()
	if s["clock_source"] != "SYNTHETIC" || len(s["cases"].([]any)) != 1 || len(s["complaints"].([]any)) != 1 || len(s["evidence"].([]any)) != 1 {
		t.Fatalf("%v", s)
	}
	counters := s["counters"].(map[string]any)
	if counters["case"] != 1.0 || counters["complaint"] != 1.0 || counters["evidence"] != 1.0 {
		t.Fatalf("%v", counters)
	}
}

// The PERD §37.1 example, loaded as written: its mock-switch fault is another component's and is only counted.
func TestPerdExamplePlanLoadsAndOnlyItsMockCaseFaultApplies(t *testing.T) {
	e := newEnv(t)
	raw, err := os.ReadFile("../../../../contracts/fixtures/valid/fault-plan/perd_example.json")
	if err != nil {
		t.Fatal(err)
	}
	r := e.bench("PUT", "/control/v1/fault-plan", raw)
	if r.status != 200 || r.json["applied_faults"] != 1.0 || r.json["ignored_faults"] != 1.0 {
		t.Fatalf("%d %s", r.status, r.raw)
	}
	if _, err := e.createWith(impatient(100*time.Millisecond), "idk_1"); err == nil {
		t.Fatal("the first create must be held and dropped (TIMEOUT_AFTER_COMMIT, 3500 ms)")
	}
	if e.count("cases") != 1 {
		t.Fatal("the case must be committed")
	}
	tr := e.faultTrace()
	if len(tr) != 1 || tr[0].Effect != "TIMEOUT_AFTER_COMMIT" || !tr[0].Committed {
		t.Fatalf("%+v", tr)
	}
	idx := e.bench("GET", "/truth/v1/commands?faults_only=true", nil).json["commands"].([]any)[0].(map[string]any)["fault_index"]
	if idx != 0.0 {
		t.Fatalf("fault_index is the position in the plan: %v", idx)
	}
}
