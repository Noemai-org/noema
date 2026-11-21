package proxy_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/benchmark/hostility_proxy/internal/numbers"
	"github.com/Noemai-org/noema/benchmark/hostility_proxy/internal/proxy"
	"github.com/Noemai-org/noema/benchmark/hostility_proxy/internal/routes"
)

// The per-route request log (task ENV-09 follow-up; REQ-ENV-004, REQ-ENV-005; EC-T09, EC-E16): the proxy records every request that
// reaches its data side, whether or not a fault fired, so a test can say what the system under test called, how often, with which
// body and which idempotency key, and what it was told. Nothing in an entry depends on time or on the machine, so the same plan, seed
// and calls give the same log hash. Bodies and key values are kept only as SHA-256 digests; the log never holds a credential, a header
// or a body. SYNTHETIC (AGENT-25). It says nothing about how Tushe behaves.

type logView struct {
	Entries []struct {
		Seq               int    `json:"seq"`
		Target            string `json:"target"`
		Operation         string `json:"operation"`
		Method            string `json:"method"`
		Path              string `json:"path"`
		CallNo            int    `json:"call_no"`
		Fault             string `json:"fault"`
		Forwarded         bool   `json:"forwarded"`
		UpstreamStatus    int    `json:"upstream_status"`
		Outcome           string `json:"outcome"`
		Status            int    `json:"status"`
		BodySHA256        string `json:"request_body_sha256"`
		IdempotencyKeySHA string `json:"idempotency_key_sha256"`
	} `json:"entries"`
	Counts  map[string]int `json:"counts"`
	Dropped int            `json:"dropped"`
	Hash    string         `json:"log_hash"`
}

func (r *rig) requests(query string) logView {
	r.t.Helper()
	res := r.ctl("GET", "/control/v1/requests"+query, "")
	if res.status != 200 {
		r.t.Fatalf("requests: %d %s", res.status, res.body)
	}
	var v logView
	if err := json.Unmarshal([]byte(res.body), &v); err != nil {
		r.t.Fatal(err)
	}
	return v
}

func sha(s string) string {
	sum := sha256.Sum256([]byte(s))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (r *rig) postWithKey(path, body, key string) response {
	r.t.Helper()
	req, err := http.NewRequest("POST", r.data.URL+path, strings.NewReader(body))
	if err != nil {
		r.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", key)
	req.Header.Set("Authorization", "Bearer tushe-runtime-credential-must-not-be-logged")
	res, err := r.client.Do(req)
	if err != nil {
		return response{status: 0}
	}
	defer func() { _ = res.Body.Close() }()
	b, _ := io.ReadAll(res.Body)
	return response{res.StatusCode, res.Header, string(b)}
}

func TestRequestLogRecordsUnfaultedCallsWithDigestsOnly(t *testing.T) {
	r := newRig(t)
	r.get("/v1/status/TX1")
	body := `{"q":"Tunde"}`
	r.postWithKey("/v1/lookups", body, "idem-key-001")
	v := r.requests("")
	if len(v.Entries) != 2 {
		t.Fatalf("want 2 entries, got %d", len(v.Entries))
	}
	a, b := v.Entries[0], v.Entries[1]
	if a.Seq != 1 || a.Target != "mock-switch" || a.Operation != "get_status" || a.Method != "GET" || a.CallNo != 1 || a.Fault != "" || !a.Forwarded || a.UpstreamStatus != 200 || a.Outcome != "answered" || a.Status != 200 {
		t.Fatalf("first entry wrong: %+v", a)
	}
	if b.Seq != 2 || b.Target != "mock-customer" || b.Operation != "post_lookup" || b.Method != "POST" || b.BodySHA256 != sha(body) || b.IdempotencyKeySHA != sha("idem-key-001") {
		t.Fatalf("second entry wrong: %+v", b)
	}
	raw := r.ctl("GET", "/control/v1/requests", "").body
	for _, secret := range []string{"idem-key-001", "tushe-runtime-credential-must-not-be-logged", "Tunde"} {
		if strings.Contains(raw, secret) {
			t.Fatalf("the log holds %q: %s", secret, raw)
		}
	}
	if v.Counts["mock-switch get_status"] != 1 || v.Counts["mock-customer post_lookup"] != 1 {
		t.Fatalf("counts wrong: %v", v.Counts)
	}
}

func TestRequestLogRecordsWhatTheFaultDid(t *testing.T) {
	r := newRig(t)
	r.load(fault("mock-switch", "get_status", "HTTP_429", `{"nth_call":1}`, `{"retry_after_seconds":3}`)+","+
		fault("mock-customer", "post_lookup", "DROP_RESPONSE", `{"nth_call":1}`, `{}`), simClock)
	r.get("/v1/status/TX1")
	r.postWithKey("/v1/lookups", `{"q":"x"}`, "k1")
	v := r.requests("")
	if len(v.Entries) != 2 {
		t.Fatalf("want 2 entries, got %d", len(v.Entries))
	}
	if e := v.Entries[0]; e.Fault != "HTTP_429" || e.Forwarded || e.UpstreamStatus != 0 || e.Outcome != "answered" || e.Status != 429 {
		t.Fatalf("429 entry wrong: %+v", e)
	}
	// EC-T09 and EC-E16: the request was applied upstream and the caller never heard. The log must say both.
	if e := v.Entries[1]; e.Fault != "DROP_RESPONSE" || !e.Forwarded || e.UpstreamStatus != 200 || e.Outcome != "dropped" || e.Status != 0 {
		t.Fatalf("dropped entry wrong: %+v", e)
	}
	if r.tushe.hits.Load() != 1 {
		t.Fatalf("the system must have been reached once (the dropped call), got %d", r.tushe.hits.Load())
	}
}

// queryMarker is the secret the test puts in a query string. It must never be able to appear in the
// log by chance: the log carries a random 32-hex-digit generation ID and a hash, and the earlier marker
// "abc" is made of hex digits, so about one run in a hundred found it in the generation ID and failed
// (a flaky test, found on main after noema#272; REQ-ENV-005, REQ-TST-004). It contains characters
// that are not hex digits, so a match can only mean the query string was logged.
const queryMarker = "zq-query-marker"

func TestTheQueryMarkerCannotAppearInTheLogByChance(t *testing.T) {
	const hexDigits = "0123456789abcdef"
	if strings.Trim(queryMarker, hexDigits) == "" {
		t.Fatal("the query marker is made only of hex digits, so a random ID or hash can contain it")
	}
	if !strings.ContainsAny(queryMarker, "ghijklmnopqrstuvwxyz") {
		t.Fatal("the query marker needs a letter that is not a hex digit")
	}
}

func TestRequestLogRecordsARouteThatIsNotInTheTable(t *testing.T) {
	r := newRig(t)
	if res := r.get("/v1/secret-admin?token=" + queryMarker); res.status != 404 {
		t.Fatalf("want 404, got %d", res.status)
	}
	v := r.requests("")
	if len(v.Entries) != 1 {
		t.Fatalf("want 1 entry, got %d", len(v.Entries))
	}
	e := v.Entries[0]
	if e.Target != "-" || e.Operation != "-" || e.Outcome != "refused" || e.Status != 404 || e.Forwarded || e.Path != "/v1/secret-admin" {
		t.Fatalf("unmatched entry wrong: %+v", e)
	}
	if strings.Contains(r.ctl("GET", "/control/v1/requests", "").body, queryMarker) {
		t.Fatal("the query string must not be logged")
	}
	if r.tushe.hits.Load() != 0 {
		t.Fatal("a route outside the table reached the system")
	}
}

func TestRequestLogFiltersByRoute(t *testing.T) {
	r := newRig(t)
	r.get("/v1/status/TX1")
	r.get("/v1/status/TX2")
	r.get("/v1/beneficiaries")
	v := r.requests("?target=mock-switch&operation=get_status")
	if len(v.Entries) != 2 || v.Entries[0].Seq != 1 || v.Entries[1].Seq != 2 {
		t.Fatalf("filter wrong: %+v", v.Entries)
	}
	if all := r.requests(""); len(all.Entries) != 3 || all.Hash == v.Hash {
		t.Fatalf("an unfiltered log must hold all three and hash differently")
	}
	if res := r.ctl("GET", "/control/v1/requests?operation=get_status", ""); res.status != 400 {
		t.Fatalf("operation without target must be refused, got %d", res.status)
	}
}

func TestRequestLogIsIdenticalForTheSamePlanSeedAndCalls(t *testing.T) {
	run := func() logView {
		r := newRig(t)
		r.load(fault("mock-customer", "post_lookup", "TIMEOUT_AFTER_COMMIT", `{"nth_call":1}`, `{"delay_ms":10}`), simClock)
		r.get("/v1/status/TX1")
		r.postWithKey("/v1/lookups", `{"q":"x"}`, "k1")
		r.postWithKey("/v1/lookups", `{"q":"x"}`, "k1")
		return r.requests("")
	}
	a, b := run(), run()
	if a.Hash == "" || a.Hash != b.Hash {
		t.Fatalf("log hash differs between identical runs: %q %q", a.Hash, b.Hash)
	}
	if len(a.Entries) != 3 || a.Entries[1].Outcome != "dropped" || a.Entries[2].Outcome != "answered" {
		t.Fatalf("unexpected log: %+v", a.Entries)
	}
}

func TestRequestLogIsClearedByResetAndByDeletingThePlan(t *testing.T) {
	r := newRig(t)
	r.get("/v1/status/TX1")
	r.ctl("POST", "/control/v1/reset", "")
	if v := r.requests(""); len(v.Entries) != 0 {
		t.Fatalf("reset must clear the log, got %d", len(v.Entries))
	}
	r.get("/v1/status/TX1")
	r.ctl("DELETE", "/control/v1/fault-plan", "")
	if v := r.requests(""); len(v.Entries) != 0 {
		t.Fatalf("deleting the plan must clear the log, got %d", len(v.Entries))
	}
}

func TestRequestLogNeedsTheBenchCredential(t *testing.T) {
	r := newRig(t)
	if res, err := r.do(r.client, "GET", r.control.URL, "/control/v1/requests", "", ""); err != nil || res.status != 401 {
		t.Fatalf("want 401 without a credential, got %d %v", res.status, err)
	}
}

func TestRequestLogIsBoundedAndSaysWhatItDropped(t *testing.T) {
	tushe := newFakeTushe(t)
	raw, err := os.ReadFile("../../config/routes.json")
	if err != nil {
		t.Fatal(err)
	}
	table, err := routes.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	mapper, err := numbers.New([]byte(numberKey))
	if err != nil {
		t.Fatal(err)
	}
	up, _ := url.Parse(tushe.srv.URL)
	srv, err := proxy.New(proxy.Options{Routes: table, Upstreams: map[string]*url.URL{"tushe": up, "relay": up}, Numbers: mapper, RequestLogLimit: 3,
		BenchToken: benchToken, Log: slog.New(slog.NewJSONHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	data, control := httptest.NewServer(srv.DataHandler()), httptest.NewServer(srv.ControlHandler())
	defer data.Close()
	defer control.Close()
	for i := 0; i < 5; i++ {
		res, err := http.Get(data.URL + "/v1/status/TX1")
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
	}
	req, _ := http.NewRequest("GET", control.URL+"/control/v1/requests", nil)
	req.Header.Set("Authorization", "Bearer "+benchToken)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	var v logView
	if err := json.NewDecoder(res.Body).Decode(&v); err != nil {
		t.Fatal(err)
	}
	if len(v.Entries) != 3 || v.Dropped != 2 || v.Entries[0].Seq != 1 {
		t.Fatalf("want the first 3 kept and 2 counted as dropped, got %d entries, dropped=%d", len(v.Entries), v.Dropped)
	}
	if v.Counts["mock-switch get_status"] != 5 {
		t.Fatalf("counts must cover every request, kept or not: %v", v.Counts)
	}
}
