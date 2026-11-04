package proxy_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// TEN-06 (internal, task ENV-14; PERD §34.5 [REC]): "Every integration-hostility fault (429, pagination, schema drift,
// near-timeout, skew) can be produced on demand." Each family is produced here from a real fault plan loaded through the control
// API (seed 42, the plan's simulated clock) on a real connection, observed the way a caller would, and then produced again on a
// fresh proxy: the observation and the trace hash must be identical (REQ-ENV-004). Observations are facts, never durations, so the
// test does not depend on how fast the machine is. SYNTHETIC (AGENT-25). Nothing here says anything about Tushe's behaviour.

const ten06Seed = 42 // planDoc fixes this seed

// produce loads one plan on a fresh proxy, runs the observation, and returns what was observed and the trace hash.
func produce(t *testing.T, faults string, observe func(r *rig) string) (string, string) {
	t.Helper()
	r := newRig(t)
	r.load(faults, simClock)
	obs := observe(r)
	return obs, r.trace().Hash
}

func TestTEN06EveryHostilityFaultCanBeProducedOnDemand(t *testing.T) {
	families := 0
	defer func() {
		t.Logf("TEN_RESULT id=TEN-06 seed=%d families=%d pass=%t", ten06Seed, families, !t.Failed())
	}()

	cases := []struct {
		family, name, faults string
		observe              func(r *rig) string
		want                 string
	}{
		{"429", "rate limit with Retry-After, nothing reaches the system", fault("mock-switch", "get_status", "HTTP_429", `{"nth_call":1}`, `{"retry_after_seconds":7}`),
			func(r *rig) string {
				before := r.tushe.hits.Load()
				res := r.get("/v1/status/TX1")
				return fmt.Sprintf("status=%d retry_after=%s reached_system=%d", res.status, res.header.Get("Retry-After"), r.tushe.hits.Load()-before)
			}, "status=429 retry_after=7 reached_system=0"},

		{"pagination", "a list of five split into pages of two, every item exactly once", fault("mock-ledger", "list_transactions", "PAGINATE", `{"nth_call":1}`, `{"page_size":2}`),
			func(r *rig) string {
				var pages []string
				path := "/v1/transactions?account=9999041234"
				for i := 0; i < 6; i++ {
					doc := decode(t, r.get(path).body)
					var ns []string
					items, _ := doc["items"].([]any)
					for _, it := range items {
						ns = append(ns, fmt.Sprint(it.(map[string]any)["n"]))
					}
					complete, _ := doc["complete"].(bool)
					pages = append(pages, fmt.Sprintf("%s(complete=%t)", strings.Join(ns, ","), complete))
					cursor, _ := doc["next_cursor"].(string)
					if complete || cursor == "" {
						break
					}
					path = "/v1/transactions?account=9999041234&cursor=" + cursor
				}
				return strings.Join(pages, " ")
			}, "1,2(complete=false) 3,4(complete=false) 5(complete=true)"},

		{"schema drift", "an extra field", fault("mock-switch", "get_status", "SCHEMA_DRIFT", `{"nth_call":1}`, `{"change":"add","field":"unexpected_field"}`),
			func(r *rig) string {
				_, ok := decode(t, r.get("/v1/status/TX1").body)["unexpected_field"]
				return fmt.Sprintf("added=%t", ok)
			}, "added=true"},
		{"schema drift", "a removed field", fault("mock-switch", "get_status", "SCHEMA_DRIFT", `{"nth_call":1}`, `{"change":"remove","field":"semantic_status"}`),
			func(r *rig) string {
				_, ok := decode(t, r.get("/v1/status/TX1").body)["semantic_status"]
				return fmt.Sprintf("present=%t", ok)
			}, "present=false"},
		{"schema drift", "a renamed field", fault("mock-switch", "get_status", "SCHEMA_DRIFT", `{"nth_call":1}`, `{"change":"rename","field":"semantic_status"}`),
			func(r *rig) string {
				doc := decode(t, r.get("/v1/status/TX1").body)
				_, old := doc["semantic_status"]
				_, renamed := doc["semantic_status_renamed"]
				return fmt.Sprintf("old=%t renamed=%t", old, renamed)
			}, "old=false renamed=true"},
		{"schema drift", "a value outside the enum", fault("mock-switch", "get_status", "SCHEMA_DRIFT", `{"nth_call":1}`, `{"change":"unknown_enum","field":"semantic_status"}`),
			func(r *rig) string { return fmt.Sprint(decode(t, r.get("/v1/status/TX1").body)["semantic_status"]) }, "UNKNOWN_ENUM_VALUE"},

		{"near-timeout", "answer after the caller's timeout, nothing committed", fault("mock-switch", "get_status", "NEAR_TIMEOUT_BEFORE", `{"nth_call":1}`, `{"delay_ms":400}`),
			func(r *rig) string {
				_, err := r.do(impatient(100*time.Millisecond), "GET", r.data.URL, "/v1/status/TX1", "", "")
				time.Sleep(700 * time.Millisecond) // past the whole delay: a commit that was only postponed would have landed
				return fmt.Sprintf("caller_timed_out=%t reached_system=%d", err != nil, r.tushe.hits.Load())
			}, "caller_timed_out=true reached_system=0"},
		{"near-timeout", "answer after the caller's timeout, already committed", fault("mock-switch", "get_status", "NEAR_TIMEOUT_AFTER", `{"nth_call":1}`, `{"delay_ms":400}`),
			func(r *rig) string {
				_, err := r.do(impatient(100*time.Millisecond), "GET", r.data.URL, "/v1/status/TX1", "", "")
				deadline := time.Now().Add(3 * time.Second)
				for r.tushe.hits.Load() == 0 && time.Now().Before(deadline) { // observable state, not time
					time.Sleep(10 * time.Millisecond)
				}
				return fmt.Sprintf("caller_timed_out=%t reached_system=%d", err != nil, r.tushe.hits.Load())
			}, "caller_timed_out=true reached_system=1"},
		{"near-timeout", "a caller that waits gets the real answer, late", fault("mock-switch", "get_status", "NEAR_TIMEOUT_BEFORE", `{"nth_call":1}`, `{"delay_ms":150}`),
			func(r *rig) string {
				start := time.Now()
				res := r.get("/v1/status/TX1")
				return fmt.Sprintf("status=%d held_for_the_delay=%t", res.status, time.Since(start) >= 140*time.Millisecond)
			}, "status=200 held_for_the_delay=true"},

		{"skew", "every timestamp 90 s later", fault("mock-switch", "get_status", "CLOCK_SKEW", `{"nth_call":1}`, `{"skew_seconds":90}`),
			func(r *rig) string { return fmt.Sprint(decode(t, r.get("/v1/status/TX1").body)["observed_at"]) }, "2026-10-01T09:42:30+01:00"},
		{"skew", "every timestamp 90 s earlier", fault("mock-switch", "get_status", "CLOCK_SKEW", `{"nth_call":1}`, `{"skew_seconds":-90}`),
			func(r *rig) string { return fmt.Sprint(decode(t, r.get("/v1/status/TX1").body)["observed_at"]) }, "2026-10-01T09:39:30+01:00"},
	}

	seen := map[string]bool{}
	for _, c := range cases {
		t.Run(c.family+": "+c.name, func(t *testing.T) {
			obs1, hash1 := produce(t, c.faults, c.observe)
			if obs1 != c.want {
				t.Fatalf("observed %q, want %q", obs1, c.want)
			}
			obs2, hash2 := produce(t, c.faults, c.observe)
			if obs2 != obs1 || hash2 != hash1 {
				t.Fatalf("produced again on a fresh proxy it must be identical: %q/%s vs %q/%s", obs1, hash1, obs2, hash2)
			}
			var entries struct{ Entries []json.RawMessage }
			r := newRig(t)
			r.load(c.faults, simClock)
			c.observe(r)
			if err := json.Unmarshal([]byte(r.ctl("GET", "/control/v1/trace", "").body), &entries); err != nil || len(entries.Entries) == 0 {
				t.Fatalf("the fault must be in the trace: %v %d", err, len(entries.Entries))
			}
			seen[c.family] = true
		})
	}
	for _, f := range []string{"429", "pagination", "schema drift", "near-timeout", "skew"} {
		if !seen[f] {
			t.Errorf("family %s was not produced", f)
		}
	}
	families = len(seen)
}
