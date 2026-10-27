package api_test

import (
	"strings"
	"testing"
	"time"

	"github.com/Noemai-org/noema/synthetic/mock_case/internal/api"
)

// The five effects the case system produces beyond the first eight (PERD §37.1; EC-T09, EC-E03, EC-E06, EC-X02).
// Each is injected on the real wire and observed by a real client.

// EC-T09: NEAR_TIMEOUT_AFTER. The command commits at once; the real answer is held for delay_ms.
func TestNearTimeoutAfterCommitsThenAnswersLate(t *testing.T) {
	e := newEnv(t)
	e.loadPlan(plan(fault("create_case", `{"nth_call":1}`, "NEAR_TIMEOUT_AFTER", `{"delay_ms":400}`), ""))
	if _, err := e.createWith(impatient(100*time.Millisecond), "idk_1"); err == nil {
		t.Fatal("a caller that gives up before the answer sees a timeout")
	}
	if e.count("cases") != 1 {
		t.Fatal("the command commits before the delay, so the caller that gave up left a committed case")
	}
	// A caller that waits gets the real answer, late, not a dropped connection.
	e.loadPlan(plan(fault("create_case", `{"nth_call":1}`, "NEAR_TIMEOUT_AFTER", `{"delay_ms":150}`), ""))
	start := time.Now()
	r, err := e.createWithFor(patient(e), "idk_2", 2)
	if err != nil || r.status != 201 || r.str("case_id") == "" {
		t.Fatalf("the late answer is the real one: %v %d %s", err, r.status, r.raw)
	}
	if time.Since(start) < 140*time.Millisecond {
		t.Fatalf("the answer must be held for delay_ms, took %v", time.Since(start))
	}
	// The command log keeps the abandoned call (committed, answer never read) and the call that waited.
	trace := e.faultTrace()
	if len(trace) != 2 || trace[1] != (traceRow{"create_case", "NEAR_TIMEOUT_AFTER", true, false, 201}) {
		t.Fatalf("fault trace: %+v", trace)
	}
	if retry := e.createCase("idk_1"); retry.status != 200 || retry.header.Get("Idempotent-Replay") != "true" {
		t.Fatalf("the retry of the abandoned call finds its case: %d %s", retry.status, retry.raw)
	}
}

// EC-T09: NEAR_TIMEOUT_BEFORE. The delay comes before the commit.
func TestNearTimeoutBeforeCommitsOnlyForACallerThatWaits(t *testing.T) {
	e := newEnv(t)
	e.loadPlan(plan(fault("create_case", `{"nth_call":1}`, "NEAR_TIMEOUT_BEFORE", `{"delay_ms":400}`), ""))
	if _, err := e.createWith(impatient(100*time.Millisecond), "idk_1"); err == nil {
		t.Fatal("a caller that gives up before the answer sees a timeout")
	}
	time.Sleep(500 * time.Millisecond) // past the whole delay: a commit that was only postponed would have landed
	if e.count("cases") != 0 {
		t.Fatal("a caller that left before the delay ended leaves nothing committed")
	}
	if r := e.createCase("idk_1"); r.status != 201 {
		t.Fatalf("the retry is a first create: %d %s", r.status, r.raw)
	}
	e.loadPlan(plan(fault("create_case", `{"nth_call":1}`, "NEAR_TIMEOUT_BEFORE", `{"delay_ms":150}`), ""))
	start := time.Now()
	r, err := e.createWithFor(patient(e), "idk_2", 2)
	if err != nil || r.status != 201 {
		t.Fatalf("a caller that waits gets the real answer: %v %d %s", err, r.status, r.raw)
	}
	if time.Since(start) < 140*time.Millisecond {
		t.Fatalf("the answer must be held for delay_ms, took %v", time.Since(start))
	}
	trace := e.faultTrace()
	if len(trace) != 2 || trace[0] != (traceRow{"create_case", "NEAR_TIMEOUT_BEFORE", false, true, 0}) || trace[1] != (traceRow{"create_case", "NEAR_TIMEOUT_BEFORE", true, false, 201}) {
		t.Fatalf("fault trace (the call that left, then the call that waited): %+v", trace)
	}
}

// EC-E03: HTTP 200 with a pending body and nothing created, on each keyed write.
func TestHTTP200SemanticPendingCreatesNothing(t *testing.T) {
	e := newEnv(t)
	c := e.createCase("idk_case")
	for _, tc := range []struct {
		op, path string
		body     map[string]any
		kind     string
	}{
		{"create_case", "/v1/cases", caseBodyFor("TX99999"), "cases"},
		{"create_complaint", "/v1/cases/" + c.str("case_id") + "/complaints", map[string]any{"tenant_id": "ten_a"}, "complaints"},
		{"append_evidence", "/v1/cases/" + c.str("case_id") + "/evidence", map[string]any{"tenant_id": "ten_a", "payload": "synthetic note"}, "evidence"},
	} {
		e.loadPlan(plan(fault(tc.op, `{"nth_call":1}`, "HTTP_200_SEMANTIC_PENDING", `{}`), ""))
		before := e.count(tc.kind)
		r := e.runtime("POST", tc.path, ifAbsent(key("idk_pending_"+tc.op)), tc.body)
		if r.status != 200 || r.str("status") != "PENDING" || r.str("case_id") != "" || r.str("complaint_ref") != "" || r.str("attachment_id") != "" {
			t.Fatalf("%s: a 200 that says pending and carries no created object: %d %s", tc.op, r.status, r.raw)
		}
		if e.count(tc.kind) != before {
			t.Fatalf("%s: nothing may be created", tc.op)
		}
		trace := e.faultTrace()
		if last := trace[len(trace)-1]; last != (traceRow{tc.op, "HTTP_200_SEMANTIC_PENDING", false, false, 200}) {
			t.Fatalf("%s: fault trace: %+v", tc.op, trace)
		}
		again := e.runtime("POST", tc.path, ifAbsent(key("idk_pending_"+tc.op)), tc.body)
		if again.status != 201 || e.count(tc.kind) != before+1 {
			t.Fatalf("%s: the key stays free; the retry is a first create: %d %s", tc.op, again.status, again.raw)
		}
	}
}

// EC-E06: IDEMPOTENCY_COLLISION answers a fresh key as a collision, exactly as a real one is answered.
func TestIdempotencyCollisionAnswersAsARealCollisionAndCreatesNothing(t *testing.T) {
	e := newEnv(t)
	e.loadPlan(plan(fault("create_case", `{"nth_call":1}`, "IDEMPOTENCY_COLLISION", `{}`), ""))
	r := e.createCase("idk_1")
	if r.status != 409 || r.errCode() != "IDEMPOTENCY_CONFLICT" || r.json["error"].(map[string]any)["retryable"] != false {
		t.Fatalf("%d %s", r.status, r.raw)
	}
	e.runtime("POST", "/v1/cases", newKey("idk_real"), caseBody()) // nth_call 2: no fault
	other := caseBody()
	other["principal_id"] = "usr_2"
	genuine := e.runtime("POST", "/v1/cases", newKey("idk_real"), other)
	if genuine.status != 409 || genuine.errCode() != r.errCode() || genuine.str("error", "message") != r.str("error", "message") {
		t.Fatalf("the injected collision must read like a real one: %s vs %s", r.raw, genuine.raw)
	}
	if e.count("cases") != 1 {
		t.Fatalf("only the unfaulted create made a case: %d", e.count("cases"))
	}
	trace := e.faultTrace()
	if len(trace) != 1 || trace[0] != (traceRow{"create_case", "IDEMPOTENCY_COLLISION", false, false, 409}) {
		t.Fatalf("fault trace: %+v", trace)
	}
}

// EC-X02, AGENT-09: INJECTION_PAYLOAD puts instruction-like text in what the caller reads. The stored data is untouched.
func TestInjectionPayloadAppearsInTheAnswerOnly(t *testing.T) {
	e := newEnv(t)
	c := e.createCase("idk_1")
	id := c.str("case_id")
	ev := e.runtime("POST", "/v1/cases/"+id+"/evidence", key("idk_e1"), map[string]any{"tenant_id": "ten_a", "payload": "synthetic note"})
	evID := ev.str("attachment_id")
	e.loadPlan(plan(
		fault("get_case", `{"nth_call":1}`, "INJECTION_PAYLOAD", `{"field":"reason_code"}`)+","+
			fault("find_cases", `{"nth_call":1}`, "INJECTION_PAYLOAD", `{"field":"reason_code"}`)+","+
			fault("get_evidence", `{"nth_call":1}`, "INJECTION_PAYLOAD", `{"field":"payload"}`)+","+
			fault("find_evidence", `{"nth_call":1}`, "INJECTION_PAYLOAD", `{"field":"payload"}`), ""))
	hostile := func(v string) bool { return strings.HasSuffix(v, api.InjectedText) && strings.Contains(v, "SYNTHETIC") }

	if r := e.runtime("GET", "/v1/cases/"+id+"?tenant_id=ten_a", nil, nil); r.status != 200 || !hostile(r.str("reason_code")) || !strings.HasPrefix(r.str("reason_code"), "CREDIT_MISSING") {
		t.Fatalf("get_case: %s", r.raw)
	}
	if r := e.runtime("GET", "/v1/cases?tenant_id=ten_a&principal_id=usr_1", nil, nil); len(r.items()) != 1 || !hostile(r.items()[0]["reason_code"].(string)) {
		t.Fatalf("find_cases: %s", r.raw)
	}
	if r := e.runtime("GET", "/v1/evidence/"+evID+"?tenant_id=ten_a", nil, nil); !hostile(r.str("payload")) || !strings.HasPrefix(r.str("payload"), "synthetic note") {
		t.Fatalf("get_evidence: %s", r.raw)
	}
	if r := e.runtime("GET", "/v1/evidence?tenant_id=ten_a&case_id="+id, nil, nil); len(r.items()) != 1 || !hostile(r.items()[0]["payload"].(string)) {
		t.Fatalf("find_evidence: %s", r.raw)
	}
	// Only the first call of each operation was faulted, and the stored data never changed.
	if r := e.runtime("GET", "/v1/cases/"+id+"?tenant_id=ten_a", nil, nil); r.str("reason_code") != "CREDIT_MISSING" {
		t.Fatalf("the second read is clean: %s", r.raw)
	}
	for _, c := range e.state()["cases"].([]any) {
		if c.(map[string]any)["reason_code"] != "CREDIT_MISSING" {
			t.Fatalf("the truth endpoint shows the stored case: %v", c)
		}
	}
	for _, a := range e.state()["evidence"].([]any) {
		if a.(map[string]any)["payload"] != "synthetic note" {
			t.Fatalf("the truth endpoint shows the stored evidence: %v", a)
		}
	}
	if n := len(e.faultTrace()); n != 4 {
		t.Fatalf("four injected reads in the trace, got %d", n)
	}
}
