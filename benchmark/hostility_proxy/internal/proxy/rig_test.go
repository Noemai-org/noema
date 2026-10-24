package proxy_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Noemai-org/noema/benchmark/hostility_proxy/internal/numbers"
	"github.com/Noemai-org/noema/benchmark/hostility_proxy/internal/proxy"
	"github.com/Noemai-org/noema/benchmark/hostility_proxy/internal/routes"
)

const (
	numberKey  = "proxy-number-key-proxy-number-key-0001"
	benchToken = "proxy-bench-token-proxy-bench-token-xx"
)

// fakeTushe stands in for the hosted Tushe sandbox: it serves data, some of it with Tushe's test account numbers,
// and counts what reaches it. It injects no fault of its own. Every fault in these tests is the proxy's.
type fakeTushe struct {
	srv  *httptest.Server
	mu   sync.Mutex
	hits atomic.Int64
	// state the tests change between calls
	status string
	events []map[string]any
	last   struct{ path, query, body string }
}

func newFakeTushe(t testing.TB) *fakeTushe {
	f := &fakeTushe{status: "PENDING"}
	for i := 1; i <= 4; i++ {
		f.events = append(f.events, map[string]any{"sequence": i, "type": "settlement.updated", "account": "9999041234", "at": "2026-10-01T09:4" + string(rune('0'+i)) + ":00+01:00"})
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/transactions/{ref}", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.note(r, nil)
		writeJSON(w, map[string]any{"transaction_ref": r.PathValue("ref"), "account": "9999041234", "counterparty": "9999100200", "amount_minor": 500000,
			"amount_text": "999904123400", "status": f.status, "posted_at": "2026-10-01T09:40:00+01:00", "semantic_status": "PENDING", "is_terminal": false})
	})
	mux.HandleFunc("GET /v1/status/{ref}", func(w http.ResponseWriter, r *http.Request) {
		f.note(r, nil)
		writeJSON(w, map[string]any{"transaction_ref": r.PathValue("ref"), "semantic_status": "SUCCESS", "is_terminal": true, "raw_code": "00", "observed_at": "2026-10-01T09:41:00+01:00"})
	})
	mux.HandleFunc("GET /v1/transactions", func(w http.ResponseWriter, r *http.Request) {
		f.note(r, nil)
		w.Header().Set("Location", "/v1/accounts/9999041234/entries")
		writeJSON(w, map[string]any{"items": []any{map[string]any{"account": "9999041234"}}})
	})
	mux.HandleFunc("GET /v1/accounts/{account}/entries", func(w http.ResponseWriter, r *http.Request) {
		f.note(r, nil)
		writeJSON(w, map[string]any{"account_seen_by_tushe": r.PathValue("account")})
	})
	mux.HandleFunc("GET /v1/beneficiaries", func(w http.ResponseWriter, r *http.Request) {
		f.note(r, nil)
		writeJSON(w, map[string]any{"beneficiaries": []any{map[string]any{"name": "Tunde Adeyemi", "account": "9999100200"}}})
	})
	mux.HandleFunc("POST /v1/lookups", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.note(r, body)
		var in any
		_ = json.Unmarshal(body, &in)
		writeJSON(w, map[string]any{"received": in, "created": true})
	})
	feed := func(key string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.note(r, nil)
			writeJSON(w, map[string]any{key: f.events, "next": "cursor-4"})
		}
	}
	mux.HandleFunc("GET /v1/events", feed("events"))
	mux.HandleFunc("GET /relay/deliveries", feed("deliveries"))
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeTushe) note(r *http.Request, body []byte) {
	f.hits.Add(1)
	f.last.path, f.last.query, f.last.body = r.URL.Path, r.URL.RawQuery, string(body)
}

func (f *fakeTushe) setStatus(s string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status = s
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	b, _ := json.Marshal(v)
	w.Write(b) //nolint:errcheck
}

// rig is a running proxy in front of a fakeTushe.
type rig struct {
	t       testing.TB
	tushe   *fakeTushe
	srv     *proxy.Server
	data    *httptest.Server
	control *httptest.Server
	client  *http.Client
}

func newRig(t testing.TB) *rig { return newRigWithKey(t, numberKey) }

func newRigWithKey(t testing.TB, key string) *rig {
	t.Helper()
	tushe := newFakeTushe(t)
	raw, err := os.ReadFile("../../config/routes.json")
	if err != nil {
		t.Fatal(err)
	}
	table, err := routes.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	fixtures, err := proxy.LoadFixtures("../../config/fixtures")
	if err != nil {
		t.Fatal(err)
	}
	mapper, err := numbers.New([]byte(key))
	if err != nil {
		t.Fatal(err)
	}
	up, _ := url.Parse(tushe.srv.URL)
	srv, err := proxy.New(proxy.Options{Routes: table, Upstreams: map[string]*url.URL{"tushe": up, "relay": up}, Fixtures: fixtures, Numbers: mapper,
		BenchToken: benchToken, Log: slog.New(slog.NewJSONHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	r := &rig{t: t, tushe: tushe, srv: srv, client: &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{DisableKeepAlives: true},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	r.data = httptest.NewServer(srv.DataHandler())
	r.control = httptest.NewServer(srv.ControlHandler())
	t.Cleanup(r.data.Close)
	t.Cleanup(r.control.Close)
	return r
}

type response struct {
	status int
	header http.Header
	body   string
}

func (r *rig) do(client *http.Client, method, base, path, token string, body string) (response, error) {
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, base+path, rd)
	if err != nil {
		r.t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := client.Do(req)
	if err != nil {
		return response{}, err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		return response{}, err
	}
	return response{res.StatusCode, res.Header, string(b)}, nil
}

// get and post go through the data plane as the system under test would.
func (r *rig) get(path string) response {
	r.t.Helper()
	res, err := r.do(r.client, "GET", r.data.URL, path, "", "")
	if err != nil {
		r.t.Fatalf("GET %s: %v", path, err)
	}
	return res
}

func (r *rig) post(path, body string) response {
	r.t.Helper()
	res, err := r.do(r.client, "POST", r.data.URL, path, "", body)
	if err != nil {
		r.t.Fatalf("POST %s: %v", path, err)
	}
	return res
}

func (r *rig) ctl(method, path, body string) response {
	r.t.Helper()
	res, err := r.do(r.client, method, r.control.URL, path, benchToken, body)
	if err != nil {
		r.t.Fatalf("%s %s: %v", method, path, err)
	}
	return res
}

func planDoc(source, faults, clock string) string {
	if clock != "" {
		clock = `,"clock":` + clock
	}
	return `{"fault_plan_id":"fp_test","scenario_id":"SCN-TEST","seed":42,"deterministic":true,"source":"` + source + `","faults":[` + faults + `]` + clock + `}`
}

func fault(target, op, effect, match, params string) string {
	return `{"target":"` + target + `","operation":"` + op + `","effect":"` + effect + `","match":` + match + `,"params":` + params + `}`
}

func (r *rig) load(faults, clock string) {
	r.t.Helper()
	res := r.ctl("PUT", "/control/v1/fault-plan", planDoc("PROXY", faults, clock))
	if res.status != 200 {
		r.t.Fatalf("plan refused: %d %s", res.status, res.body)
	}
}

type traceView struct {
	Entries []struct {
		Seq        int            `json:"seq"`
		Target     string         `json:"target"`
		Operation  string         `json:"operation"`
		CallNo     int            `json:"call_no"`
		FaultIndex int            `json:"fault_index"`
		Effect     string         `json:"effect"`
		Detail     map[string]any `json:"detail"`
	} `json:"entries"`
	Hash string `json:"trace_hash"`
}

func (r *rig) trace() traceView {
	r.t.Helper()
	var v traceView
	if err := json.Unmarshal([]byte(r.ctl("GET", "/control/v1/trace", "").body), &v); err != nil {
		r.t.Fatal(err)
	}
	return v
}

const simClock = `{"start":"2026-10-01T09:40:00+01:00","advances":[{"at_step":"later","by":"PT10S"}]}`

func decode(t testing.TB, s string) map[string]any {
	t.Helper()
	var m map[string]any
	dec := json.NewDecoder(bytes.NewReader([]byte(s)))
	dec.UseNumber()
	if err := dec.Decode(&m); err != nil {
		t.Fatalf("not JSON: %v: %q", err, s)
	}
	return m
}

func jsonUnmarshal(s string, v any) error { return json.Unmarshal([]byte(s), v) }
