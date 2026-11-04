package proxy_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Noemai-org/noema/benchmark/hostility_proxy/internal/numbers"
	"github.com/Noemai-org/noema/benchmark/hostility_proxy/internal/proxy"
	"github.com/Noemai-org/noema/benchmark/hostility_proxy/internal/routes"
)

// The recorded, seeded runs of the Noema-covered acceptance exceptions that the hostility proxy produces (task ENV-14).
// Tushe specification v1.0 §22: AT-04 "Stale state", AT-05 "Event loss", AT-07 "Out-of-order event". The worksheet of 13 Oct 2026
// says Noema covers them: "Noema's proxy returns a prior version after a write, from a fault plan"; "a configured event is dropped on
// Noema's side"; "events are reordered on Noema's side". Each test runs its scenario from a real fault plan on a real connection,
// twice, requires the two runs to be identical, and requires the committed record in synthetic/at_runs to equal them (the drift
// guard). To re-record: NOEMA_RECORD_AT=1 go test ./internal/proxy -run TestAT. SYNTHETIC (AGENT-25).
//
// What they do not show: Tushe's own behaviour. The upstream here is a test-only service that stands in for the system the proxy
// fronts; no Tushe semantics are inferred. Tushe's delivery log and sequence numbers are Tushe's, and the proxy's own trace is the
// diagnostic shown here.
const (
	atDir = "../../../../synthetic/at_runs/"
	atEnv = "NOEMA_RECORD_AT"
)

const atRoutes = `{"schema_version":"1.0","synthetic":true,"routes":[
 {"method":"GET","path":"/v1/resources/{id}","upstream":"up","target":"mock-ledger","operation":"get_resource"},
 {"method":"PUT","path":"/v1/resources/{id}","upstream":"up","target":"mock-ledger","operation":"put_resource"},
 {"method":"GET","path":"/v1/events","upstream":"up","target":"mock-destination","operation":"events_feed","event_feed":{"events":"events"}}]}`

// atUpstream is a test-only system with state: a resource with a version, and an event emitted for every change.
type atUpstream struct {
	mu      sync.Mutex
	value   string
	version int
	events  []map[string]any
}

func (u *atUpstream) handler() http.Handler {
	mux := http.NewServeMux()
	write := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	mux.HandleFunc("GET /v1/resources/{id}", func(w http.ResponseWriter, r *http.Request) {
		u.mu.Lock()
		defer u.mu.Unlock()
		write(w, map[string]any{"id": r.PathValue("id"), "value": u.value, "version": u.version})
	})
	mux.HandleFunc("PUT /v1/resources/{id}", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Value string `json:"value"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		u.mu.Lock()
		defer u.mu.Unlock()
		u.value, u.version = in.Value, u.version+1
		u.events = append(u.events, map[string]any{"sequence": len(u.events) + 1, "type": "resource.updated", "version": u.version, "value": u.value})
		write(w, map[string]any{"id": r.PathValue("id"), "value": u.value, "version": u.version})
	})
	mux.HandleFunc("GET /v1/events", func(w http.ResponseWriter, r *http.Request) {
		u.mu.Lock()
		defer u.mu.Unlock()
		write(w, map[string]any{"events": append([]map[string]any{}, u.events...)})
	})
	return mux
}

type atEnvironment struct {
	t       *testing.T
	up      *atUpstream
	upURL   string
	dataURL string
	client  *http.Client
	ctl     func(method, path, body string) (int, string)
}

func newAT(t *testing.T) *atEnvironment {
	t.Helper()
	up := &atUpstream{value: "initial"}
	us := httptest.NewServer(up.handler())
	t.Cleanup(us.Close)
	table, err := routes.Parse([]byte(atRoutes))
	if err != nil {
		t.Fatal(err)
	}
	fixtures, err := proxy.LoadFixtures("../../config/fixtures")
	if err != nil {
		t.Fatal(err)
	}
	mapper, err := numbers.New([]byte(numberKey))
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(us.URL)
	srv, err := proxy.New(proxy.Options{Routes: table, Upstreams: map[string]*url.URL{"up": u}, Fixtures: fixtures, Numbers: mapper,
		BenchToken: benchToken, Log: slog.New(slog.NewJSONHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	data, control := httptest.NewServer(srv.DataHandler()), httptest.NewServer(srv.ControlHandler())
	t.Cleanup(data.Close)
	t.Cleanup(control.Close)
	client := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{DisableKeepAlives: true}}
	e := &atEnvironment{t: t, up: up, upURL: us.URL, dataURL: data.URL, client: client}
	e.ctl = func(method, path, body string) (int, string) {
		req, _ := http.NewRequest(method, control.URL+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+benchToken)
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(b)
	}
	return e
}

func (e *atEnvironment) load(seed int, faults, clock string) {
	e.t.Helper()
	doc := strings.Replace(planDoc("PROXY", faults, clock), `"seed":42`, fmt.Sprintf(`"seed":%d`, seed), 1)
	if status, body := e.ctl("PUT", "/control/v1/fault-plan", doc); status != 200 {
		e.t.Fatalf("plan refused: %d %s", status, body)
	}
}

// call is a request through the proxy (viaProxy) or straight to the upstream (the authoritative view).
func (e *atEnvironment) call(viaProxy bool, method, path, body string) (int, map[string]any) {
	e.t.Helper()
	base := e.upURL
	if viaProxy {
		base = e.dataURL
	}
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, _ := http.NewRequest(method, base+path, rd)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := e.client.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	var out map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	_ = dec.Decode(&out)
	return res.StatusCode, out
}

func (e *atEnvironment) trace() []map[string]any {
	e.t.Helper()
	_, body := e.ctl("GET", "/control/v1/trace", "")
	var v struct {
		Entries []map[string]any `json:"entries"`
	}
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		e.t.Fatal(err)
	}
	return v.Entries
}

func (e *atEnvironment) advance(by string) {
	e.t.Helper()
	if status, body := e.ctl("POST", "/control/v1/clock/advance", `{"by":"`+by+`"}`); status != 200 {
		e.t.Fatalf("clock: %d %s", status, body)
	}
}

type atStep struct {
	Step     string         `json:"step"`
	Via      string         `json:"via"` // "proxy" or "authoritative (straight to the system)"
	Request  string         `json:"request"`
	Status   int            `json:"status"`
	Observed map[string]any `json:"observed"`
}

type atDoc struct {
	Synthetic bool             `json:"synthetic"`
	Test      string           `json:"test"`
	Spec      string           `json:"spec"`
	Source    string           `json:"source"`
	Seed      int              `json:"seed"`
	Steps     []atStep         `json:"steps"`
	Trace     []map[string]any `json:"proxy_trace"`
	Verdict   string           `json:"verdict"`
}

func step(name string, viaProxy bool, request string, status int, observed map[string]any) atStep {
	via := "authoritative (straight to the system)"
	if viaProxy {
		via = "proxy"
	}
	return atStep{Step: name, Via: via, Request: request, Status: status, Observed: observed}
}

func pick(m map[string]any, keys ...string) map[string]any {
	out := map[string]any{}
	for _, k := range keys {
		if v, ok := m[k]; ok {
			out[k] = v
		}
	}
	return out
}

// seqs reads the sequence numbers of an events response, in the order delivered.
func seqs(m map[string]any) []any {
	list, _ := m["events"].([]any)
	out := []any{}
	for _, it := range list {
		out = append(out, it.(map[string]any)["sequence"])
	}
	return out
}

func renderAT(t *testing.T, d any) []byte {
	t.Helper()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(d); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// recordedRun runs the scenario twice, requires identical runs and a passing verdict, and compares with (or writes) the record.
func recordedRun(t *testing.T, file string, run func(t *testing.T) any, verdict func(any) string) {
	t.Helper()
	first, second := run(t), run(t)
	if v := verdict(first); v != "PASS" {
		t.Fatalf("the acceptance condition does not hold:\n%s", renderAT(t, first))
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("the same plan and seed must give the same run:\n%s\n%s", renderAT(t, first), renderAT(t, second))
	}
	got := renderAT(t, first)
	path := atDir + file
	if os.Getenv(atEnv) != "" {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s", path)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v: record it with %s=1", err, atEnv)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s is stale: re-record with %s=1 go test ./internal/proxy -run TestAT\nrecorded:\n%s\nnow:\n%s", file, atEnv, want, got)
	}
}

// ---- AT-04 Stale state: "Write, then read under a stale-state profile." Pass: "Read returns the configured prior version; authoritative state stays correct."

func runAT04(t *testing.T) any {
	e := newAT(t)
	e.load(42, fault("mock-ledger", "get_resource", "STALE_READ", `{"nth_call":2}`, `{"seconds":30}`), simClock)
	d := atDoc{Synthetic: true, Test: "AT-04 Stale state", Spec: "Write, then read under a stale-state profile. Pass: the read returns the configured prior version; authoritative state stays correct.",
		Source: "hostility proxy, fault STALE_READ seconds=30 on get_resource (nth_call 2), simulated clock", Seed: 42}
	st, body := e.call(true, "GET", "/v1/resources/r1", "")
	d.Steps = append(d.Steps, step("read before the write (the proxy sees this state)", true, "GET /v1/resources/r1", st, pick(body, "value", "version")))
	st, body = e.call(true, "PUT", "/v1/resources/r1", `{"value":"updated"}`)
	d.Steps = append(d.Steps, step("write through the proxy", true, "PUT /v1/resources/r1", st, pick(body, "value", "version")))
	e.advance("PT60S")
	st, stale := e.call(true, "GET", "/v1/resources/r1", "")
	d.Steps = append(d.Steps, step("read under the stale-state profile, 60 s after the write", true, "GET /v1/resources/r1", st, pick(stale, "value", "version")))
	st, auth := e.call(false, "GET", "/v1/resources/r1", "")
	d.Steps = append(d.Steps, step("authoritative state, read straight from the system", false, "GET /v1/resources/r1", st, pick(auth, "value", "version")))
	st, live := e.call(true, "GET", "/v1/resources/r1", "")
	d.Steps = append(d.Steps, step("the next read, no fault: live again", true, "GET /v1/resources/r1", st, pick(live, "value", "version")))
	d.Trace = e.trace()
	served := ""
	if len(d.Trace) == 1 {
		served, _ = d.Trace[0]["detail"].(map[string]any)["served"].(string)
	}
	ok := stale["value"] == "initial" && fmt.Sprint(stale["version"]) == "0" && auth["value"] == "updated" && fmt.Sprint(auth["version"]) == "1" &&
		live["value"] == "updated" && served == "stale"
	d.Verdict = map[bool]string{true: "PASS", false: "FAIL"}[ok]
	return d
}

func TestAT04RecordedSeededRun(t *testing.T) {
	recordedRun(t, "AT-04.json", runAT04, func(v any) string { return v.(atDoc).Verdict })
}

// ---- AT-05 Event loss: "Drop a configured event." Pass: "State changes; event absent from normal delivery; diagnostics show the drop."

func runAT05(t *testing.T) any {
	e := newAT(t)
	e.load(42, fault("mock-destination", "events_feed", "DROP_WEBHOOK", `{"nth_call":1}`, `{}`), "")
	d := atDoc{Synthetic: true, Test: "AT-05 Event loss", Spec: "Drop a configured event. Pass: state changes; the event is absent from normal delivery; diagnostics show the drop.",
		Source: "hostility proxy, fault DROP_WEBHOOK on events_feed (nth_call 1): the first event of that delivery is dropped", Seed: 42}
	for _, v := range []string{"first", "second"} {
		st, body := e.call(true, "PUT", "/v1/resources/r1", `{"value":"`+v+`"}`)
		d.Steps = append(d.Steps, step("state changes: write "+v+" (the system emits an event)", true, "PUT /v1/resources/r1", st, pick(body, "value", "version")))
	}
	st, delivered := e.call(true, "GET", "/v1/events", "")
	d.Steps = append(d.Steps, step("normal delivery through the proxy", true, "GET /v1/events", st, map[string]any{"sequences": seqs(delivered)}))
	st, direct := e.call(false, "GET", "/v1/events", "")
	d.Steps = append(d.Steps, step("what the system emitted, read straight from it (stands in for the producer's delivery log)", false, "GET /v1/events", st, map[string]any{"sequences": seqs(direct)}))
	st, state := e.call(false, "GET", "/v1/resources/r1", "")
	d.Steps = append(d.Steps, step("authoritative state", false, "GET /v1/resources/r1", st, pick(state, "value", "version")))
	d.Trace = e.trace()
	detail := map[string]any{}
	if len(d.Trace) == 1 {
		detail, _ = d.Trace[0]["detail"].(map[string]any)
	}
	ok := reflect.DeepEqual(seqs(delivered), []any{json.Number("2")}) && reflect.DeepEqual(seqs(direct), []any{json.Number("1"), json.Number("2")}) &&
		fmt.Sprint(state["version"]) == "2" && detail["applied"] == true && fmt.Sprint(detail["dropped_index"]) == "0"
	d.Verdict = map[bool]string{true: "PASS", false: "FAIL"}[ok]
	return d
}

func TestAT05RecordedSeededRun(t *testing.T) {
	recordedRun(t, "AT-05.json", runAT05, func(v any) string { return v.(atDoc).Verdict })
}

// ---- AT-07 Out-of-order event: "Reorder configured events." Pass: "Events arrive in the configured order with sequence metadata intact."

type at07Run struct {
	Synthetic bool    `json:"synthetic"`
	Test      string  `json:"test"`
	Spec      string  `json:"spec"`
	Source    string  `json:"source"`
	Runs      []atDoc `json:"runs"`
	Verdict   string  `json:"verdict"`
}

func runAT07(t *testing.T) any {
	out := at07Run{Synthetic: true, Test: "AT-07 Out-of-order event",
		Spec:   "Reorder configured events. Pass: events arrive in the configured order with sequence metadata intact.",
		Source: "hostility proxy, fault REORDER_EVENTS on events_feed (nth_call 1): the plan's seed picks the adjacent pair; one run per seed"}
	ok := true
	swaps := map[string]bool{}
	for _, seed := range []int{1, 2, 3, 4} {
		e := newAT(t)
		e.load(seed, fault("mock-destination", "events_feed", "REORDER_EVENTS", `{"nth_call":1}`, `{}`), "")
		d := atDoc{Synthetic: true, Test: fmt.Sprintf("AT-07 seed %d", seed), Source: "REORDER_EVENTS", Seed: seed}
		for i := 1; i <= 4; i++ {
			e.call(false, "PUT", "/v1/resources/r1", fmt.Sprintf(`{"value":"v%d"}`, i))
		}
		st, delivered := e.call(true, "GET", "/v1/events", "")
		d.Steps = append(d.Steps, step("delivery through the proxy", true, "GET /v1/events", st, map[string]any{"sequences": seqs(delivered)}))
		st, direct := e.call(false, "GET", "/v1/events", "")
		d.Steps = append(d.Steps, step("what the system emitted, in its own order", false, "GET /v1/events", st, map[string]any{"sequences": seqs(direct)}))
		d.Trace = e.trace()
		// sequence metadata intact: the same events, each unchanged, in the proxy's reordered order
		dl, _ := delivered["events"].([]any)
		dr, _ := direct["events"].([]any)
		intact := len(dl) == len(dr) && len(dl) == 4
		moved := 0
		seq := func(ev any) int { n, _ := ev.(map[string]any)["sequence"].(json.Number).Int64(); return int(n) }
		for i := range dl {
			if intact && !reflect.DeepEqual(dl[i], dr[seq(dl[i])-1]) { // the event with this sequence number, unchanged
				intact = false
			}
			if intact && seq(dl[i]) != seq(dr[i]) {
				moved++
			}
		}
		good := intact && moved == 2 && len(d.Trace) == 1
		swaps[fmt.Sprint(seqs(delivered))] = true
		d.Verdict = map[bool]string{true: "PASS", false: "FAIL"}[good]
		ok = ok && good
		out.Runs = append(out.Runs, d)
	}
	ok = ok && len(swaps) >= 2 // the seed can choose a different pair
	out.Verdict = map[bool]string{true: "PASS", false: "FAIL"}[ok]
	return out
}

func TestAT07RecordedSeededRun(t *testing.T) {
	recordedRun(t, "AT-07.json", runAT07, func(v any) string { return v.(at07Run).Verdict })
}
