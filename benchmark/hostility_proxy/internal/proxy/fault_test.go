package proxy_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"
)

// REQ-ENV-005: "Each proxied fault SHALL have a test proving it is observed by the adapter exactly as specified."
// Here the adapter is a real HTTP client; the faults are the proxy's own, applied on a real connection.

func impatient(d time.Duration) *http.Client {
	return &http.Client{Timeout: d, Transport: &http.Transport{DisableKeepAlives: true}}
}

func TestWithoutAPlanTheProxyOnlyForwards(t *testing.T) {
	r := newRig(t)
	res := r.get("/v1/status/TX88213")
	if res.status != 200 || decode(t, res.body)["semantic_status"] != "SUCCESS" {
		t.Fatalf("%d %s", res.status, res.body)
	}
	if len(r.trace().Entries) != 0 {
		t.Fatal("no plan, no fault")
	}
}

func TestLatency(t *testing.T) {
	r := newRig(t)
	r.load(fault("mock-switch", "get_status", "LATENCY", `{"nth_call":1}`, `{"delay_ms":250}`), "")
	start := time.Now()
	res := r.get("/v1/status/TX1")
	if res.status != 200 || time.Since(start) < 240*time.Millisecond {
		t.Fatalf("%d after %v", res.status, time.Since(start))
	}
	start = time.Now()
	r.get("/v1/status/TX1")
	if time.Since(start) > 200*time.Millisecond {
		t.Fatal("only the first call is delayed")
	}
}

func TestNearTimeoutDeliversTheAnswerJustLate(t *testing.T) {
	r := newRig(t)
	r.load(fault("mock-switch", "get_status", "NEAR_TIMEOUT_AFTER", `{"nth_call":1}`, `{"delay_ms":300}`), "")
	if _, err := r.do(impatient(150*time.Millisecond), "GET", r.data.URL, "/v1/status/TX1", "", ""); err == nil {
		t.Fatal("a caller with a 150 ms timeout must time out")
	}
	r2 := newRig(t)
	r2.load(fault("mock-switch", "get_status", "NEAR_TIMEOUT_BEFORE", `{"nth_call":1}`, `{"delay_ms":100}`), "")
	res, err := r2.do(impatient(2*time.Second), "GET", r2.data.URL, "/v1/status/TX1", "", "")
	if err != nil || res.status != 200 {
		t.Fatalf("a patient caller still gets the answer: %v %d", err, res.status)
	}
}

func TestTimeoutBeforeCommitNeverReachesTushe(t *testing.T) {
	r := newRig(t)
	r.load(fault("mock-customer", "post_lookup", "TIMEOUT_BEFORE_COMMIT", `{"nth_call":1}`, `{"delay_ms":50}`), "")
	if _, err := r.do(r.client, "POST", r.data.URL, "/v1/lookups", "", `{"q":"x"}`); err == nil {
		t.Fatal("the connection must be dropped")
	}
	if r.tushe.hits.Load() != 0 {
		t.Fatal("a timeout before commit must not reach Tushe")
	}
	if res := r.post("/v1/lookups", `{"q":"x"}`); res.status != 200 || r.tushe.hits.Load() != 1 {
		t.Fatal("the retry goes through")
	}
}

func TestTimeoutAfterCommitReachesTusheThenLosesTheAnswer(t *testing.T) {
	r := newRig(t)
	r.load(fault("mock-customer", "post_lookup", "TIMEOUT_AFTER_COMMIT", `{"nth_call":1}`, `{"delay_ms":200}`), "")
	start := time.Now()
	if res, err := r.do(r.client, "POST", r.data.URL, "/v1/lookups", "", `{"q":"x"}`); err == nil {
		t.Fatalf("the answer must be lost, got %d", res.status)
	}
	if time.Since(start) < 190*time.Millisecond {
		t.Fatalf("the answer must be held for delay_ms, was %v", time.Since(start))
	}
	if r.tushe.hits.Load() != 1 {
		t.Fatalf("Tushe must have processed the request exactly once, saw %d", r.tushe.hits.Load())
	}
}

func TestDropResponse(t *testing.T) {
	r := newRig(t)
	r.load(fault("mock-customer", "post_lookup", "DROP_RESPONSE", `{"nth_call":1}`, `{}`), "")
	if _, err := r.do(r.client, "POST", r.data.URL, "/v1/lookups", "", `{"q":"x"}`); err == nil {
		t.Fatal("the connection must be dropped")
	}
	if r.tushe.hits.Load() != 1 {
		t.Fatal("the request must have reached Tushe")
	}
}

func TestHTTP5XXBeforeAndAfterCommit(t *testing.T) {
	r := newRig(t)
	r.load(fault("mock-customer", "post_lookup", "HTTP_5XX", `{"nth_call":1}`, `{"status":502}`)+","+
		fault("mock-customer", "post_lookup", "HTTP_5XX", `{"nth_call":2}`, `{"status":504,"after_commit":true}`), "")
	first := r.post("/v1/lookups", `{"q":"x"}`)
	if first.status != 502 || r.tushe.hits.Load() != 0 {
		t.Fatalf("before commit: %d, %d hits", first.status, r.tushe.hits.Load())
	}
	second := r.post("/v1/lookups", `{"q":"x"}`)
	if second.status != 504 || r.tushe.hits.Load() != 1 {
		t.Fatalf("after commit: %d, %d hits", second.status, r.tushe.hits.Load())
	}
	if !strings.HasPrefix(first.header.Get("Content-Type"), "application/json") {
		t.Fatal("an error body is JSON")
	}
}

func TestHTTP429AndServiceUnavailable(t *testing.T) {
	r := newRig(t)
	r.load(fault("mock-switch", "get_status", "HTTP_429", `{"nth_call":1}`, `{"retry_after_seconds":7}`)+","+
		fault("mock-switch", "get_status", "HTTP_429", `{"nth_call":2}`, `{}`)+","+
		fault("mock-switch", "get_status", "SERVICE_UNAVAILABLE", `{"nth_call":3}`, `{}`), "")
	a, b, c := r.get("/v1/status/TX1"), r.get("/v1/status/TX1"), r.get("/v1/status/TX1")
	if a.status != 429 || a.header.Get("Retry-After") != "7" || b.status != 429 || b.header.Get("Retry-After") != "" || c.status != 503 {
		t.Fatalf("%d %q / %d %q / %d", a.status, a.header.Get("Retry-After"), b.status, b.header.Get("Retry-After"), c.status)
	}
	if r.tushe.hits.Load() != 0 {
		t.Fatal("none of them reaches Tushe")
	}
	if d := r.get("/v1/status/TX1"); d.status != 200 {
		t.Fatalf("after the faults the call succeeds: %d", d.status)
	}
}

func TestMalformedResponseIsHalfABody(t *testing.T) {
	r := newRig(t)
	full := r.get("/v1/status/TX1")
	r.load(fault("mock-switch", "get_status", "MALFORMED_RESPONSE", `{"nth_call":1}`, `{}`), "")
	res := r.get("/v1/status/TX1")
	if res.status != 200 || len(res.body) != len(full.body)/2 {
		t.Fatalf("%d, %d bytes of %d", res.status, len(res.body), len(full.body))
	}
	var v any
	if err := jsonUnmarshal(res.body, &v); err == nil {
		t.Fatal("the body must not parse")
	}
}

func TestSchemaDrift(t *testing.T) {
	r := newRig(t)
	for i, c := range []struct{ change, field, check string }{
		{"unknown_enum", "semantic_status", `"semantic_status":"UNKNOWN_ENUM_VALUE"`},
		{"remove", "is_terminal", ""},
		{"rename", "raw_code", `"raw_code_renamed":"00"`},
		{"add", "extra", `"extra":"synthetic_extra_value"`},
	} {
		r.load(fault("mock-switch", "get_status", "SCHEMA_DRIFT", `{"nth_call":1}`, `{"change":"`+c.change+`","field":"`+c.field+`"}`), "")
		res := r.get("/v1/status/TX1")
		if c.check != "" && !strings.Contains(res.body, c.check) {
			t.Errorf("%d %s: %s", i, c.change, res.body)
		}
		if c.change == "remove" && strings.Contains(res.body, "is_terminal") {
			t.Errorf("remove: %s", res.body)
		}
		if tr := r.trace(); len(tr.Entries) != 1 || tr.Entries[0].Detail["applied"] != true {
			t.Errorf("%s: %+v", c.change, tr.Entries)
		}
	}
}

func TestClockSkewShiftsTimestampsAndNothingElse(t *testing.T) {
	r := newRig(t)
	r.load(fault("mock-switch", "get_status", "CLOCK_SKEW", `{"nth_call":1}`, `{"skew_seconds":90}`), "")
	res := decode(t, r.get("/v1/status/TX1").body)
	if res["observed_at"] != "2026-10-01T09:42:30+01:00" || res["raw_code"] != "00" {
		t.Fatalf("%v", res)
	}
	if again := decode(t, r.get("/v1/status/TX1").body); again["observed_at"] != "2026-10-01T09:41:00+01:00" {
		t.Fatalf("only the faulted call is skewed: %v", again)
	}
}

func TestInjectionPayload(t *testing.T) {
	r := newRig(t)
	r.load(fault("mock-customer", "get_beneficiaries", "INJECTION_PAYLOAD", `{"nth_call":1}`, `{"field":"name"}`), "")
	res := r.get("/v1/beneficiaries")
	if !strings.Contains(res.body, "SYNTHETIC INJECTION TEST") || strings.Contains(res.body, "Tunde Adeyemi") {
		t.Fatalf("%s", res.body)
	}
}

func TestImpossibleStateServesTheFixtureAndNeverAsksTushe(t *testing.T) {
	r := newRig(t)
	r.load(fault("mock-switch", "get_status", "IMPOSSIBLE_STATE", `{"transaction_ref":"TX88213"}`, `{"fixture":"credited_before_debited"}`), "")
	res := r.get("/v1/status/TX88213")
	body := decode(t, res.body)
	if res.status != 200 || body["credit_state"] != "CREDITED" || body["debit_state"] != "PENDING" || r.tushe.hits.Load() != 0 {
		t.Fatalf("%d %s (%d hits)", res.status, res.body, r.tushe.hits.Load())
	}
	if regexp.MustCompile(`9999[0-9]{6}`).MatchString(res.body) {
		t.Fatalf("a fixture goes through the number rewrite too: %s", res.body)
	}
	if strings.Contains(res.body, `"synthetic"`) {
		t.Fatal("the label is for the file; the system under test must not see it")
	}
	if other := r.get("/v1/status/TX1"); decode(t, other.body)["semantic_status"] != "SUCCESS" {
		t.Fatal("another transaction is not affected")
	}
}

// STALE_READ: the state as it was `seconds` ago, from what the proxy itself saw.
func TestStaleReadServesTheStateAsItWas(t *testing.T) {
	r := newRig(t)
	r.load(fault("mock-ledger", "get_transaction", "STALE_READ", `{"nth_call":3}`, `{"seconds":5}`), simClock)
	if decode(t, r.get("/v1/transactions/TX1").body)["status"] != "PENDING" { // call 1 at t0
		t.Fatal("setup")
	}
	r.tushe.setStatus("SUCCESSFUL")
	r.ctl("POST", "/control/v1/clock/advance", `{"step":"later"}`)               // t0 + 10 s
	if decode(t, r.get("/v1/transactions/TX1").body)["status"] != "SUCCESSFUL" { // call 2, live
		t.Fatal("a normal read is live")
	}
	r.tushe.setStatus("FAILED")
	// call 3 with STALE_READ 5 s: the newest response at least 5 s old is call 1's (call 2 is 0 s old).
	res := decode(t, r.get("/v1/transactions/TX1").body)
	if res["status"] != "PENDING" {
		t.Fatalf("stale read must show the old state, got %v", res["status"])
	}
	tr := r.trace()
	if len(tr.Entries) != 1 || tr.Entries[0].Detail["served"] != "stale" {
		t.Fatalf("%+v", tr.Entries)
	}
}

func TestStaleReadWithNothingOldEnoughIsLiveAndSaysSo(t *testing.T) {
	r := newRig(t)
	r.load(fault("mock-ledger", "get_transaction", "STALE_READ", `{"nth_call":1}`, `{"seconds":60}`), simClock)
	r.tushe.setStatus("SUCCESSFUL")
	if decode(t, r.get("/v1/transactions/TX1").body)["status"] != "SUCCESSFUL" {
		t.Fatal("with no history the read is live")
	}
	if tr := r.trace(); tr.Entries[0].Detail["served"] != "live" {
		t.Fatalf("%+v", tr.Entries)
	}
}

func eventSeqs(t testing.TB, body, key string) []int {
	t.Helper()
	v := decode(t, body)
	list, _ := v[key].([]any)
	var out []int
	for _, e := range list {
		n, _ := e.(map[string]any)["sequence"].(json.Number).Int64()
		out = append(out, int(n))
	}
	return out
}

func TestEventFeedFaults(t *testing.T) {
	cases := []struct {
		name, effect, params string
		want                 []int
	}{
		{"drop", "DROP_WEBHOOK", `{}`, []int{2, 3, 4}},
		{"duplicate", "DUPLICATE_WEBHOOK", `{}`, []int{1, 1, 2, 3, 4}},
		{"late", "LATE_EVENT", `{"delay_ms":5000}`, []int{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t)
			r.load(fault("mock-destination", "events_feed", c.effect, `{"nth_call":1}`, c.params), simClock)
			got := eventSeqs(t, r.get("/v1/events").body, "events")
			if len(got) != len(c.want) {
				t.Fatalf("got %v, want %v", got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Fatalf("got %v, want %v", got, c.want)
				}
			}
			if tr := r.trace(); len(tr.Entries) != 1 || tr.Entries[0].Detail["applied"] != true {
				t.Fatalf("%+v", tr.Entries)
			}
		})
	}
}

func TestReorderSwapsOneAdjacentPairAndTheSeedChoosesWhich(t *testing.T) {
	seen := map[string]bool{}
	for _, seed := range []string{"1", "2", "3", "4", "5", "6"} {
		r := newRig(t)
		res := r.ctl("PUT", "/control/v1/fault-plan", strings.Replace(planDoc("PROXY", fault("mock-destination", "events_feed", "REORDER_EVENTS", `{"nth_call":1}`, `{}`), ""), `"seed":42`, `"seed":`+seed, 1))
		if res.status != 200 {
			t.Fatal(res.body)
		}
		got := eventSeqs(t, r.get("/v1/events").body, "events")
		swaps := 0
		for i := range got {
			if got[i] != i+1 {
				swaps++
			}
		}
		if swaps != 2 {
			t.Fatalf("seed %s: exactly one adjacent pair is swapped, got %v", seed, got)
		}
		seen[fmt.Sprint(got)] = true
	}
	if len(seen) < 2 {
		t.Fatal("the seed must be able to choose a different pair")
	}
}

// LATE_EVENT: the events are withheld, then arrive in a later response, after the events that followed them.
func TestLateEventArrivesOutOfOrderAfterItsDelay(t *testing.T) {
	r := newRig(t)
	r.load(fault("mock-destination", "events_feed", "LATE_EVENT", `{"nth_call":1}`, `{"delay_ms":5000}`), simClock)
	if got := eventSeqs(t, r.get("/v1/events").body, "events"); len(got) != 0 {
		t.Fatalf("withheld: %v", got)
	}
	r.tushe.mu.Lock()
	r.tushe.events = append(r.tushe.events, map[string]any{"sequence": 5, "type": "settlement.updated"})
	r.tushe.mu.Unlock()
	if got := eventSeqs(t, r.get("/v1/events").body, "events"); len(got) != 5 || got[0] != 1 {
		// not yet due: the response is the live one (1..5) and nothing is released twice
		t.Logf("live response: %v", got)
	}
	tr := r.trace()
	if len(tr.Entries) != 1 {
		t.Fatalf("one fault fired: %+v", tr.Entries)
	}
	r.ctl("POST", "/control/v1/clock/advance", `{"by":"PT6S"}`)
	got := eventSeqs(t, r.get("/v1/events").body, "events")
	// live events 1..5 then the four withheld ones: late and out of order
	want := []int{1, 2, 3, 4, 5, 1, 2, 3, 4}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
	if again := eventSeqs(t, r.get("/v1/events").body, "events"); len(again) != 5 {
		t.Fatalf("withheld events are released once: %v", again)
	}
	if v := r.ctl("GET", "/control/v1/trace", ""); !strings.Contains(v.body, `"held_events":0`) {
		t.Fatalf("%s", v.body)
	}
}

func TestEventFaultsAlsoApplyOnThePullFromTheRelay(t *testing.T) {
	r := newRig(t)
	r.load(fault("mock-destination", "relay_pull", "DROP_WEBHOOK", `{"nth_call":1}`, `{}`), "")
	got := eventSeqs(t, r.get("/relay/deliveries").body, "deliveries")
	if len(got) != 3 || got[0] != 2 {
		t.Fatalf("%v", got)
	}
}

func TestMatchAndNthCallSelectTheCall(t *testing.T) {
	r := newRig(t)
	r.load(fault("mock-switch", "get_status", "SERVICE_UNAVAILABLE", `{"transaction_ref":"TX9","nth_call":2}`, `{}`), "")
	var codes []int
	for _, p := range []string{"TX1", "TX9", "TX1", "TX9", "TX9"} {
		codes = append(codes, r.get("/v1/status/"+p).status)
	}
	want := []int{200, 200, 200, 503, 200}
	for i := range want {
		if codes[i] != want[i] {
			t.Fatalf("got %v want %v", codes, want)
		}
	}
}

// The system under test must not be able to tell an injected fault from a real one.
func TestFaultedResponsesCarryNoMarker(t *testing.T) {
	r := newRig(t)
	plain := r.get("/v1/status/TX1")
	r.load(fault("mock-switch", "get_status", "CLOCK_SKEW", `{"nth_call":1}`, `{"skew_seconds":5}`)+","+
		fault("mock-switch", "get_status", "HTTP_5XX", `{"nth_call":2}`, `{}`)+","+
		fault("mock-switch", "get_status", "SCHEMA_DRIFT", `{"nth_call":3}`, `{"change":"remove","field":"raw_code"}`), "")
	for i := 0; i < 3; i++ {
		res := r.get("/v1/status/TX1")
		for k := range res.header {
			if _, ok := plain.header[k]; !ok && k != "Content-Length" {
				if i == 1 && (k == "Content-Type" || k == "Content-Length" || k == "Date") {
					continue
				}
				t.Errorf("call %d: header %s appears only on a faulted response", i+1, k)
			}
		}
		low := strings.ToLower(res.body)
		for _, leak := range []string{"fault", "inject", "proxy", "hostility", "synthetic", "plan", "seed"} {
			if strings.Contains(low, leak) {
				t.Errorf("call %d: the body mentions %q: %s", i+1, leak, res.body)
			}
		}
	}
}

// The proxy is not an open relay: only listed routes are forwarded.
func TestUnlistedRequestsAreNotForwarded(t *testing.T) {
	r := newRig(t)
	for _, c := range []struct{ method, path string }{{"GET", "/partner/oracle"}, {"DELETE", "/v1/transactions/TX1"}, {"GET", "/"}, {"POST", "/v1/status/TX1"}, {"HEAD", "/v1/status/TX1"}, {"GET", "/v1/../partner/oracle"}} {
		res, err := r.do(r.client, c.method, r.data.URL, c.path, "", "")
		if err != nil || res.status != 404 {
			t.Errorf("%s %s: %v %d", c.method, c.path, err, res.status)
		}
	}
	if r.tushe.hits.Load() != 0 {
		t.Fatalf("%d unlisted requests reached Tushe", r.tushe.hits.Load())
	}
}

func TestAnUnreachableUpstreamIsA502NotACrash(t *testing.T) {
	r := newRig(t)
	r.tushe.srv.Close()
	if res := r.get("/v1/status/TX1"); res.status != 502 {
		t.Fatalf("%d", res.status)
	}
	if !strings.Contains(r.ctl("GET", "/control/v1/stats", "").body, `"upstream_failures":1`) {
		t.Fatal("the failure is counted")
	}
}
