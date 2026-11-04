package proxy_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// REQ-ENV-005 as amended by DEC-2026-10-31 D15 (task ENV-15): corrupted-payload fault families. Each must be
// observed by an adapter as UNAVAILABLE or a schema rejection, never as a fact or a success. The adapter here is
// `observe`, a strict client of the status route's contract; the real case adapter is driven through the proxy in
// the services/core adapter tests. SYNTHETIC (AGENT-25).

// observed is what a strict adapter makes of one response of GET /v1/status/{ref}.
type observed string

const (
	fact        observed = "FACT"        // a usable status: the one thing a corrupted payload must never become
	unavailable observed = "UNAVAILABLE" // not a 2xx, or no usable body
	rejected    observed = "REJECTED"    // a 2xx that the schema refuses
)

var statusFields = []string{"transaction_ref", "semantic_status", "is_terminal", "raw_code", "observed_at"}

func observe(res response) observed {
	if res.status < 200 || res.status > 299 {
		return unavailable
	}
	if res.body == "" {
		return unavailable
	}
	ct := res.header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		return rejected
	}
	if !json.Valid([]byte(res.body)) {
		return rejected
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal([]byte(res.body), &doc); err != nil {
		return rejected
	}
	for _, f := range statusFields {
		if _, ok := doc[f]; !ok {
			return rejected
		}
	}
	if len(doc) != len(statusFields) {
		return rejected // an unknown field is drift, not a fact
	}
	return fact
}

func TestAnUncorruptedStatusIsAFact(t *testing.T) {
	r := newRig(t)
	if got := observe(r.get("/v1/status/TX1")); got != fact {
		t.Fatalf("the observer must accept a clean response, or the other tests prove nothing: %s", got)
	}
}

// Each family, one call each, in one plan: every one is observed as not-a-fact, and the call after the plan is a fact.
func TestEveryCorruptedPayloadFamilyIsNeverAFactOrASuccess(t *testing.T) {
	families := []struct {
		name, effect, params string
		want                 observed
	}{
		{"truncated body", "MALFORMED_RESPONSE", `{}`, rejected},
		{"invalid json", "INVALID_JSON", `{}`, rejected},
		{"wrong content type", "WRONG_CONTENT_TYPE", `{}`, rejected},
		{"empty body, success status", "EMPTY_BODY_SUCCESS", `{}`, unavailable},
		{"extra field", "SCHEMA_DRIFT", `{"change":"add","field":"unexpected_field"}`, rejected},
		{"missing required field", "SCHEMA_DRIFT", `{"change":"remove","field":"semantic_status"}`, rejected},
	}
	var faults []string
	for i, f := range families {
		faults = append(faults, fault("mock-switch", "get_status", f.effect, `{"nth_call":`+itoa(i+1)+`}`, f.params))
	}
	r := newRig(t)
	r.load(strings.Join(faults, ","), "")
	for _, f := range families {
		res := r.get("/v1/status/TX1")
		if got := observe(res); got != f.want {
			t.Errorf("%s: observed as %s, want %s (status %d, content-type %q, body %q)", f.name, got, f.want, res.status, res.header.Get("Content-Type"), res.body)
		}
		if observe(res) == fact {
			t.Errorf("%s was observed as a fact", f.name)
		}
	}
	if got := observe(r.get("/v1/status/TX1")); got != fact {
		t.Fatalf("after the plan the call is clean: %s", got)
	}
	if n := len(r.trace().Entries); n != len(families) {
		t.Fatalf("every fault is traced: %d of %d", n, len(families))
	}
}

func itoa(n int) string { return fmt.Sprint(n) }

func TestInvalidJSONIsSyntacticallyInvalidAndNotATruncation(t *testing.T) {
	r := newRig(t)
	r.load(fault("mock-switch", "get_status", "INVALID_JSON", `{"nth_call":1}`, `{}`), "")
	res := r.get("/v1/status/TX1")
	if res.status != 200 || res.header.Get("Content-Type") != "application/json" {
		t.Fatalf("status and content type are untouched: %d %q", res.status, res.header.Get("Content-Type"))
	}
	if json.Valid([]byte(res.body)) {
		t.Fatalf("the body must not parse: %s", res.body)
	}
	clean := r.get("/v1/status/TX1")
	if len(res.body) < len(clean.body) {
		t.Fatalf("this is not a truncation: %d bytes against %d", len(res.body), len(clean.body))
	}
	if tr := r.trace(); len(tr.Entries) != 1 || tr.Entries[0].Effect != "INVALID_JSON" || tr.Entries[0].Detail["applied"] != true {
		t.Fatalf("%+v", tr.Entries)
	}
}

func TestWrongContentTypeKeepsAValidBody(t *testing.T) {
	for _, c := range []struct{ params, want string }{
		{`{}`, "text/html"},
		{`{"content_type":"text/plain"}`, "text/plain"},
		{`{"content_type":"application/xml"}`, "application/xml"},
	} {
		r := newRig(t)
		r.load(fault("mock-switch", "get_status", "WRONG_CONTENT_TYPE", `{"nth_call":1}`, c.params), "")
		res := r.get("/v1/status/TX1")
		if got := res.header.Get("Content-Type"); got != c.want {
			t.Errorf("%s: content type %q, want %q", c.params, got, c.want)
		}
		if !json.Valid([]byte(res.body)) || res.status != 200 {
			t.Errorf("%s: the body is valid JSON under the wrong header: %d %s", c.params, res.status, res.body)
		}
	}
}

func TestEmptyBodySuccessIsA2xxWithNoBody(t *testing.T) {
	r := newRig(t)
	r.load(fault("mock-switch", "get_status", "EMPTY_BODY_SUCCESS", `{"nth_call":1}`, `{}`), "")
	res := r.get("/v1/status/TX1")
	if res.status < 200 || res.status > 299 || res.body != "" {
		t.Fatalf("a success status and nothing else: %d %q", res.status, res.body)
	}
	if got := res.header.Get("Content-Length"); got != "0" {
		t.Fatalf("content length %q", got)
	}
}

// A corruption leaves no marker the system under test could use to tell it from a real failure (REQ-ENV-005).
func TestCorruptedResponsesCarryNoMarker(t *testing.T) {
	for _, effect := range []string{"INVALID_JSON", "WRONG_CONTENT_TYPE", "EMPTY_BODY_SUCCESS"} {
		r := newRig(t)
		r.load(fault("mock-switch", "get_status", effect, `{"nth_call":1}`, `{}`), "")
		res := r.get("/v1/status/TX1")
		for k := range res.header {
			switch k {
			case "Content-Type", "Content-Length", "Date":
			default:
				t.Errorf("%s: header %s", effect, k)
			}
		}
		low := strings.ToLower(res.body)
		for _, leak := range []string{"fault", "inject", "proxy", "synthetic", "corrupt"} {
			if strings.Contains(low, leak) {
				t.Errorf("%s: body mentions %q", effect, leak)
			}
		}
	}
}

// PAGINATE (REQ-ENV-005, TEN-06): the list is split into pages with an explicit completeness marker and a cursor.
func TestPaginateSplitsTheListAndNothingIsLostOrDoubled(t *testing.T) {
	r := newRig(t)
	r.load(fault("mock-ledger", "list_transactions", "PAGINATE", `{"nth_call":1}`, `{"page_size":2}`), "")
	var got []int
	path := "/v1/transactions?account=9999041234"
	for page := 1; page <= 4; page++ {
		res := r.get(path)
		doc := decode(t, res.body)
		items, _ := doc["items"].([]any)
		for _, it := range items {
			n, _ := it.(map[string]any)["n"].(json.Number).Int64()
			got = append(got, int(n))
		}
		complete, _ := doc["complete"].(bool)
		cursor, _ := doc["next_cursor"].(string)
		switch page {
		case 1, 2:
			if len(items) != 2 || complete || cursor == "" {
				t.Fatalf("page %d: %d items, complete=%v, cursor=%q: %s", page, len(items), complete, cursor, res.body)
			}
		case 3:
			if len(items) != 1 || !complete || cursor != "" {
				t.Fatalf("the last page: %d items, complete=%v, cursor=%q: %s", len(items), complete, cursor, res.body)
			}
		}
		if complete {
			break
		}
		path = "/v1/transactions?account=9999041234&cursor=" + cursor
	}
	if len(got) != 5 {
		t.Fatalf("every item exactly once: %v", got)
	}
	for i, n := range got {
		if n != i+1 {
			t.Fatalf("order kept: %v", got)
		}
	}
	if q := r.tushe.last.query; strings.Contains(q, "cursor") {
		t.Fatalf("the proxy's cursor must not reach Tushe: %q", q)
	}
	if tr := r.trace(); len(tr.Entries) != 1 || tr.Entries[0].Effect != "PAGINATE" {
		t.Fatalf("the fault fired on the first call only: %+v", tr.Entries)
	}
}

func TestPaginateWithAShortListIsOnePageMarkedComplete(t *testing.T) {
	r := newRig(t)
	r.load(fault("mock-ledger", "list_transactions", "PAGINATE", `{"nth_call":1}`, `{"page_size":50}`), "")
	doc := decode(t, r.get("/v1/transactions?account=9999041234").body)
	if items, _ := doc["items"].([]any); len(items) != 5 || doc["complete"] != true {
		t.Fatalf("%v", doc)
	}
}

func TestAForgedCursorIsRefusedAndNeverForwarded(t *testing.T) {
	r := newRig(t)
	before := r.tushe.hits.Load()
	for _, c := range []string{"pgx1.-1.2", "pgx1.9999999.2", "pgx1.1.0", "pgx1.a.b", "pgx1.1", "pgx2.1.2"} {
		if res := r.get("/v1/transactions?cursor=" + c); res.status != 400 {
			t.Errorf("cursor %q: %d %s", c, res.status, res.body)
		}
	}
	if r.tushe.hits.Load() != before {
		t.Fatal("a refused cursor must not reach Tushe")
	}
}
