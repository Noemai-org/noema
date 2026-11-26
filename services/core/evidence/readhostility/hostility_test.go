package readhostility_test

// EVD-05, the hostility-proxy part (A717; REQ-CTX-008, REQ-CTX-002, REQ-TST-004; INV-07, INV-19, INV-23; US-21; EC-E13 is not here, see README): what the
// Evidence read path makes of each fault the hostility proxy produces. A read that a fault spoils fails closed: it is an item that says UNAVAILABLE or
// UNKNOWN, with no payload, and it is never KNOWN. The other two reads of the transaction are still made. All data is SYNTHETIC.

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/evidence/layer"
)

var readIndex = map[string]int{"ledger": 0, "switch": 1, "destination": 2}
var readNames = []string{"ledger", "switch", "destination"}

// scenario is the three reads of one transaction with the target read faulted: the other two are the proxy's unfaulted answers.
func scenario(t testing.TB, all []exchange, read, family string) (rp *replay, target exchange, paths map[string]string) {
	t.Helper()
	byPath := map[string]exchange{}
	paths = map[string]string{}
	for _, name := range readNames {
		want := "baseline"
		if name == read {
			want = family
		}
		found := false
		for _, e := range all {
			if e.Read == name && e.Family == want {
				byPath[e.Request.Path] = e
				paths[name] = e.Request.Path
				if name == read {
					target = e
				}
				found = true
			}
		}
		if !found {
			t.Fatalf("no recording of %s for %s", want, name)
		}
	}
	return newReplay(t, byPath), target, paths
}

// run replays one scenario and returns the observations with the replay's request counts.
func run(t testing.TB, all []exchange, read, family string, timeout time.Duration) (obs []layer.Observation, target exchange, rp *replay, paths map[string]string) {
	t.Helper()
	rp, target, paths = scenario(t, all, read, family)
	return readOnce(t, rp, timeout), target, rp, paths
}

const generous = 5 * time.Second

// othersAreKnown requires the reads that were not faulted to be what they were without any fault, and read once each.
func othersAreKnown(t *testing.T, obs []layer.Observation, read string, rp *replay, paths map[string]string) {
	t.Helper()
	othersAreKnownWithin(t, obs, read, rp, paths, 1)
}

// othersAreKnownWithin is othersAreKnown with the most requests the faulted read may cost. The reader and the adapter never retry; a request that the source
// drops on a connection that was already in use is replayed once by Go's own HTTP transport (it replays an idempotent GET that got no response on a reused
// connection), so a dropped read may reach the source twice. Every other outcome is one request.
func othersAreKnownWithin(t *testing.T, obs []layer.Observation, read string, rp *replay, paths map[string]string, targetMax int) {
	t.Helper()
	for _, name := range readNames {
		limit := 1
		if name == read {
			limit = targetMax
		}
		if n := rp.hitsOn(paths[name]); n < 1 || n > limit {
			t.Errorf("the %s read reached the source %d times, want %d at most and no adapter retry", name, n, limit)
		}
		if name == read {
			continue
		}
		if s := status(obs[readIndex[name]]); s != "KNOWN" || obs[readIndex[name]].Payload == nil {
			t.Errorf("the %s read, not faulted, is %s (payload %v), want KNOWN", name, s, obs[readIndex[name]].Payload != nil)
		}
	}
}

// REQ-CTX-002: the recordings are readable. Without a fault every read is KNOWN, carries the hash of the wire bytes and the stand-in's label.
func TestWithoutAFaultEveryReadIsKnownWithTheHashOfTheWireBytes(t *testing.T) {
	all := load(t)
	for _, read := range readNames {
		obs, target, rp, paths := run(t, all, read, "baseline", generous)
		othersAreKnown(t, obs, read, rp, paths)
		o := obs[readIndex[read]]
		if status(o) != "KNOWN" || o.Payload == nil || o.RawHash != target.bodyHash() || o.NoBytes || o.DataSource != "stand-in" || !o.Synthetic {
			t.Errorf("%s: status %s, payload %v, raw %q (want %q), nobytes %v, label %q/%v", read, status(o), o.Payload != nil, o.RawHash, target.bodyHash(), o.NoBytes, o.DataSource, o.Synthetic)
		}
	}
}

// REQ-CTX-008 ("Unknown enum values SHALL never map to known states"; "schema drift: renamed or missing required fields, or an unknown enum value: UNKNOWN"),
// REQ-ENV-005 as amended (a corrupted payload is never a fact), EC-S19. An added field is also UNKNOWN: the PERD says "ignored" and the adapter is stricter;
// the stricter reading stands until the Architect says otherwise (see the PR, "decisions needed").
func TestAnAnswerTheProxyCorruptedIsUnknownWithNoPayload(t *testing.T) {
	all := load(t)
	for _, family := range []string{"truncated_body", "invalid_json", "wrong_content_type", "empty_body_success", "drift_add", "drift_remove", "drift_rename", "drift_unknown_enum"} {
		for _, read := range readNames {
			t.Run(read+"/"+family, func(t *testing.T) {
				obs, target, rp, paths := run(t, all, read, family, generous)
				othersAreKnown(t, obs, read, rp, paths)
				o := obs[readIndex[read]]
				if s := status(o); s != "UNKNOWN" || o.Read.Class != layer.ReadMalformed {
					t.Fatalf("status %s, class %s, want UNKNOWN from a MALFORMED read", s, o.Read.Class)
				}
				if o.Payload != nil || o.EffectiveAt != (time.Time{}) {
					t.Errorf("a corrupted answer left a payload or an effective time: %v %v", o.Payload, o.EffectiveAt)
				}
				if target.Response.Body != "" && (o.RawHash != target.bodyHash() || o.NoBytes) {
					t.Errorf("the item does not carry the hash of the bytes that arrived: %q, want %q (nobytes %v)", o.RawHash, target.bodyHash(), o.NoBytes)
				}
				if o.DataSource != "stand-in" || !o.Synthetic {
					t.Errorf("label %q/%v", o.DataSource, o.Synthetic)
				}
			})
		}
	}
}

// REQ-CTX-008 (429 with and without Retry-After; 5xx), INV-07: a read that the source refused is UNAVAILABLE, never an empty or a stale answer, and it costs one
// request. The honouring of Retry-After and the [REC] 100, 300, 900 ms backoff are not built (the retry budget is OD-P-112, AGENT-16): this is the no-retry default.
func TestARefusedReadIsUnavailableAndCostsOneRequest(t *testing.T) {
	all := load(t)
	want := map[string]layer.ReadClass{"http_500": layer.ReadServerError, "service_unavailable": layer.ReadServerError,
		"http_429_retry_after": layer.ReadRateLimited, "http_429_no_retry_after": layer.ReadRateLimited}
	for family, class := range want {
		for _, read := range readNames {
			t.Run(read+"/"+family, func(t *testing.T) {
				obs, target, rp, paths := run(t, all, read, family, generous)
				othersAreKnown(t, obs, read, rp, paths) // includes: one request on every path, so no retry of the refused read
				o := obs[readIndex[read]]
				if s := status(o); s != "UNAVAILABLE" || o.Read.Class != class {
					t.Fatalf("status %s, class %s, want UNAVAILABLE from %s", s, o.Read.Class, class)
				}
				if o.Payload != nil || o.EffectiveAt != (time.Time{}) || o.Read.Determinate {
					t.Errorf("a refused read left a payload or an effective time: %v", o.Payload)
				}
				if o.RawHash != target.bodyHash() || o.NoBytes {
					t.Errorf("the refusal's body is evidence too: raw %q (want %q), nobytes %v", o.RawHash, target.bodyHash(), o.NoBytes)
				}
			})
		}
	}
	// the two 429 recordings differ exactly in the header the effect sets
	for _, e := range all {
		switch {
		case e.Family == "http_429_retry_after" && (e.Response.Status != 429 || e.Response.RetryAfter != "7"):
			t.Errorf("%s: recorded %d with Retry-After %q", e.Scenario, e.Response.Status, e.Response.RetryAfter)
		case e.Family == "http_429_no_retry_after" && (e.Response.Status != 429 || e.Response.RetryAfter != ""):
			t.Errorf("%s: recorded %d with Retry-After %q", e.Scenario, e.Response.Status, e.Response.RetryAfter)
		}
	}
}

// REQ-CTX-008 ("Clock skew: Recorded"): the skewed times are kept as the source sent them, and nothing corrects them or changes the status. What a skew means is
// the reconciliation's (EC-S18); the evidence item only keeps the claim. The effective time of the item is the skewed one.
func TestClockSkewIsRecordedAsSentAndNeverCorrected(t *testing.T) {
	all := load(t)
	field := map[string][]string{"ledger": {"entries.0.posted_at"}, "switch": {"received_at"}, "destination": {"effective_at", "observed_at"}}
	for _, family := range []string{"clock_skew_ahead", "clock_skew_behind"} {
		for _, read := range readNames {
			t.Run(read+"/"+family, func(t *testing.T) {
				obs, target, rp, paths := run(t, all, read, family, generous)
				othersAreKnown(t, obs, read, rp, paths)
				base, _, _, _ := run(t, all, read, "baseline", generous)
				o, b := obs[readIndex[read]], base[readIndex[read]]
				if status(o) != "KNOWN" || o.Payload == nil || o.RawHash != target.bodyHash() {
					t.Fatalf("status %s, payload %v, raw %q", status(o), o.Payload != nil, o.RawHash)
				}
				var wire map[string]any
				if err := json.Unmarshal([]byte(target.Response.Body), &wire); err != nil {
					t.Fatal(err)
				}
				for _, f := range field[read] {
					if got, want := dig(o.Payload, f), dig(wire, f); got != want || got == "" || got == dig(b.Payload, f) {
						t.Errorf("%s: payload %q, wire %q, unskewed %q: the recorded time must be the skewed one as sent", f, got, want, dig(b.Payload, f))
					}
				}
				shift := 900 * time.Second
				if family == "clock_skew_behind" {
					shift = -shift
				}
				if got := o.EffectiveAt.Sub(b.EffectiveAt); got != shift {
					t.Errorf("effective time moved by %v from the unskewed read, want %v as sent", got, shift)
				}
			})
		}
	}
}

// dig reads a string at a dotted path through objects and arrays of a decoded JSON value ("" when absent).
func dig(v any, path string) string {
	for _, part := range strings.Split(path, ".") {
		switch t := v.(type) {
		case map[string]any:
			v = t[part]
		case []any:
			var i int
			if _, err := fmt.Sscanf(part, "%d", &i); err != nil || i < 0 || i >= len(t) {
				return ""
			}
			v = t[i]
		default:
			return ""
		}
	}
	s, _ := v.(string)
	return s
}

// REQ-CTX-008 ("Near-timeout responses: accepted only if received within the timeout"), EC-T09 as a read: the same recorded hold is within the read's deadline
// or beyond it. Beyond it the read is UNAVAILABLE, no bytes arrived, and the late answer is never used.
func TestANearTimeoutAnswerIsAcceptedOnlyWithinTheReadDeadline(t *testing.T) {
	all := load(t)
	for _, family := range []string{"latency", "near_timeout_before", "near_timeout_after"} {
		for _, read := range readNames {
			t.Run(read+"/"+family, func(t *testing.T) {
				_, target, _, _ := run(t, all, read, family, generous)
				if target.HoldMS <= 0 || target.Outcome != "answered" {
					t.Fatalf("recorded hold %d ms, outcome %q: the proxy answers late in this family", target.HoldMS, target.Outcome)
				}
				within, _, rp, paths := run(t, all, read, family, 20*target.hold())
				othersAreKnown(t, within, read, rp, paths)
				if o := within[readIndex[read]]; status(o) != "KNOWN" || o.Payload == nil || o.RawHash != target.bodyHash() {
					t.Errorf("within the deadline: status %s, payload %v", status(o), o.Payload != nil)
				}
				beyond, _, rp, paths := run(t, all, read, family, target.hold()/3)
				o := beyond[readIndex[read]]
				if status(o) != "UNAVAILABLE" || o.Read.Class != layer.ReadTimeout || !o.NoBytes || o.RawHash != "" || o.Payload != nil {
					t.Fatalf("beyond the deadline: status %s, class %s, nobytes %v, raw %q, payload %v; want UNAVAILABLE, TIMEOUT, no bytes, no payload", status(o), o.Read.Class, o.NoBytes, o.RawHash, o.Payload != nil)
				}
				if rp.hitsOn(paths[read]) != 1 {
					t.Errorf("%d requests, want 1 (no retry)", rp.hitsOn(paths[read]))
				}
			})
		}
	}
}

// INV-07, REQ-CTX-008 (a read budget exhausted is UNAVAILABLE): a source that goes silent or cuts the connection is UNAVAILABLE with no bytes, whatever the
// read's deadline. (For a write the same silence is OUTCOME_UNKNOWN; that is the execution path's, and not a read's.)
func TestASilentOrDroppedSourceIsUnavailableWithNoBytes(t *testing.T) {
	all := load(t)
	for _, family := range []string{"timeout_before_commit", "timeout_after_commit", "drop_response"} {
		for _, read := range readNames {
			t.Run(read+"/"+family, func(t *testing.T) {
				_, target, _, _ := run(t, all, read, family, generous)
				if target.Outcome != "dropped" {
					t.Fatalf("outcome %q: the proxy gives no answer in this family", target.Outcome)
				}
				for _, timeout := range []time.Duration{generous, 50 * time.Millisecond} {
					obs, _, rp, paths := run(t, all, read, family, timeout)
					o := obs[readIndex[read]]
					if status(o) != "UNAVAILABLE" || !o.NoBytes || o.RawHash != "" || o.Payload != nil || o.Read.Determinate {
						t.Fatalf("timeout %v: status %s (class %s), nobytes %v, raw %q, payload %v", timeout, status(o), o.Read.Class, o.NoBytes, o.RawHash, o.Payload != nil)
					}
					othersAreKnownWithin(t, obs, read, rp, paths, 2)
				}
			})
		}
	}
}

// REQ-CTX-008 and INV-23 on a stale read: the proxy served the state as it was (the trace says "stale"); that earlier state was not final, and a read that is not
// final is UNKNOWN. A stale answer never turns a changing value into KNOWN. What this does not show: a stale answer whose earlier state WAS final carries
// nothing that tells it from a live one (the status function reads only the retrieval time), so the layer cannot yet call it STALE. That is stated in the PR.
func TestAStaleReadOfAStateThatWasNotFinalIsUnknown(t *testing.T) {
	all := load(t)
	for _, read := range readNames {
		t.Run(read, func(t *testing.T) {
			obs, target, rp, paths := run(t, all, read, "stale_read", generous)
			if target.Served != "stale" {
				t.Fatalf("the proxy served %q, want a stale answer", target.Served)
			}
			_, base, _, _ := run(t, all, read, "baseline", generous)
			if target.Response.Body == base.Response.Body {
				t.Fatal("the stale answer is the live one: the recording does not show an old state")
			}
			for _, name := range readNames {
				if name != read && status(obs[readIndex[name]]) != "KNOWN" {
					t.Errorf("the %s read is %s", name, status(obs[readIndex[name]]))
				}
				if rp.hitsOn(paths[name]) != 1 {
					t.Errorf("%s: %d requests, want 1", name, rp.hitsOn(paths[name]))
				}
			}
			o := obs[readIndex[read]]
			if s := status(o); s != "UNKNOWN" || o.Read.Determinate {
				t.Fatalf("status %s (determinate %v), want UNKNOWN: a state that can still change", s, o.Read.Determinate)
			}
		})
	}
}

// The recordings are complete: every fault family is recorded for every read, no family is missing from these tests, and none is silently dropped (a
// recording the tests do not cover would be a fault the path was never shown to survive).
func TestEveryRecordedFamilyIsCoveredForEveryRead(t *testing.T) {
	all := load(t)
	covered := map[string]bool{"baseline": true, "truncated_body": true, "invalid_json": true, "wrong_content_type": true, "empty_body_success": true, "drift_add": true,
		"drift_remove": true, "drift_rename": true, "drift_unknown_enum": true, "http_500": true, "service_unavailable": true, "http_429_retry_after": true,
		"http_429_no_retry_after": true, "clock_skew_ahead": true, "clock_skew_behind": true, "latency": true, "near_timeout_before": true, "near_timeout_after": true,
		"timeout_before_commit": true, "timeout_after_commit": true, "drop_response": true, "stale_read": true}
	count := map[string]int{}
	for _, e := range all {
		if !covered[e.Family] {
			t.Errorf("the family %q is recorded and no test covers it", e.Family)
		}
		count[e.Family]++
	}
	var missing []string
	for f := range covered {
		if count[f] != len(readNames) {
			missing = append(missing, fmt.Sprintf("%s=%d", f, count[f]))
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("a family must be recorded once for each of the %d reads: %v", len(readNames), missing)
	}
}
