package synthetic_tushe

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"

	"github.com/Noemai-org/noema/services/core/adapters"
)

// REQ-ENV-005 as amended (DEC-2026-10-31 D15, task ENV-15): "Each must be observed by the adapter as UNAVAILABLE or a
// schema rejection, never as a fact or a success." The exchanges in testdata/corrupted_recordings.json were made by the
// real hostility proxy, over a real connection, in front of responses recorded from the real mock-case; nothing in
// them is written by hand (benchmark/hostility_proxy, TestCorruptedCaseExchanges). SYNTHETIC (AGENT-25).

type corruptedExchange struct {
	Scenario  string `json:"scenario"`
	Base      string `json:"base"`
	Operation string `json:"operation"`
	Family    string `json:"family"`
	Response  struct {
		Status      int    `json:"status"`
		ContentType string `json:"content_type"`
		Body        string `json:"body"`
	} `json:"response"`
}

func loadCorrupted(t *testing.T) []corruptedExchange {
	t.Helper()
	raw, err := os.ReadFile("testdata/corrupted_recordings.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Exchanges []corruptedExchange `json:"exchanges"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc.Exchanges
}

// serveRaw answers with exactly the recorded bytes, and fails the test if the adapter's request is not the one the
// base exchange recorded.
func serveRaw(t *testing.T, base recording, ex corruptedExchange) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != base.Request.Method || r.URL.Path != base.Request.Path {
			t.Errorf("%s: request %s %s, recorded %s %s", ex.Scenario, r.Method, r.URL.Path, base.Request.Method, base.Request.Path)
		}
		w.Header().Set("Content-Type", ex.Response.ContentType)
		w.WriteHeader(ex.Response.Status)
		_, _ = w.Write([]byte(ex.Response.Body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestEveryCorruptedPayloadIsNeverAFactOrASuccess(t *testing.T) {
	recs := loadRecordings(t)
	exchanges := loadCorrupted(t)
	if len(exchanges) != 18 {
		t.Fatalf("3 operations x 6 families = 18 exchanges, found %d: re-record", len(exchanges))
	}
	families := map[string]int{}
	for _, ex := range exchanges {
		families[ex.Family]++
		base, ok := recs[ex.Base]
		if !ok {
			t.Fatalf("%s: no base recording %q", ex.Scenario, ex.Base)
		}
		t.Run(ex.Scenario, func(t *testing.T) {
			cli := newClient(t, serveRaw(t, base, ex))
			switch ex.Operation {
			case "get_case":
				id := caseID(recs)
				rec, err := cli.GetCase(deadline(t), "ten_a", id)
				requireNotAFact(t, err)
				if rec != (adapters.CaseRecord{}) {
					t.Fatalf("a corrupted read returned data: %+v", rec)
				}
			case "find_cases":
				q, _ := url.ParseQuery(base.Request.Query)
				page, err := cli.FindCases(deadline(t), adapters.CaseQuery{TenantID: q.Get("tenant_id"), IdempotencyKey: q.Get("idempotency_key")})
				requireNotAFact(t, err)
				if len(page.Items) != 0 || page.Complete {
					t.Fatalf("a corrupted page must not say anything, least of all that nothing exists: %+v", page)
				}
			case "create_case":
				got := cli.CreateCase(deadline(t), createIn(base.Request.Headers["Idempotency-Key"]))
				// A write the adapter cannot read may have happened: the outcome is unknown, never a success.
				if got.Class != adapters.ClassNoResponse || got.ExternalAck != "" || got.RawCode != "201:UNRECOGNISED" {
					t.Fatalf("a corrupted answer to a write must be an unknown outcome with no acknowledgement: %+v", got)
				}
			default:
				t.Fatalf("unknown operation %q", ex.Operation)
			}
		})
	}
	for _, f := range []string{"truncated_body", "invalid_json", "wrong_content_type", "empty_body_success", "extra_field", "missing_required_field"} {
		if families[f] != 3 {
			t.Errorf("family %s has %d exchanges, want one per operation", f, families[f])
		}
	}
}

// requireNotAFact accepts the two observations the requirement allows for a read: the target is unavailable, or its
// answer is rejected by the schema. A nil error is the failure this test exists to catch.
func requireNotAFact(t *testing.T, err error) {
	t.Helper()
	re, ok := err.(*adapters.ReadError)
	if !ok {
		t.Fatalf("a corrupted payload was observed as a fact: error %v", err)
	}
	if re.Failure != adapters.ReadUnavailable && re.Failure != adapters.ReadMalformed {
		t.Fatalf("observed as %s, want UNAVAILABLE or MALFORMED", re.Failure)
	}
}
