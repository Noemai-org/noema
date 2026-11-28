package proxy_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// EVD-05, the hostility-proxy part (A717; REQ-CTX-008, REQ-TST-004, REQ-ENV-004; AGENT-12): the faulted exchanges that the Evidence read path
// (services/core/evidence/readhostility) is tested against.
//
// Each exchange is the REAL proxy, over a real connection, using config/routes.standin.json, with one fault from a real fault plan, in front of a
// test-only upstream that stands in for the stand-in's three transaction reads (the AT tests of this package use the same kind of upstream). The
// upstream's bodies are written in the stand-in's wire shape; TestReadHostilityUpstreamBodiesHaveTheStandinsWireShape fails if their members drift
// from the recordings of the real stand-in (standin_recordings.json), and the core's baseline reads must be KNOWN through the real StandinClient. A
// transport fault cannot be a byte recording: for the timing effects the file also records what a real client observed (an answer, an answer after
// the hold, or a dropped connection) and the hold in milliseconds, and the core replays that behaviour. Nothing is written by hand: the file is
// deterministic (the proxy is seeded); this test regenerates it in memory and fails if the committed copy differs. To re-record:
//
//	NOEMA_RECORD_READ_HOSTILITY=1 go test ./internal/proxy -run TestReadHostilityExchanges
//
// SYNTHETIC (AGENT-25). It says nothing about how Tushe behaves.
const (
	readHostilityFile = "../../../../services/core/evidence/readhostility/testdata/read_hostility_recordings.json"
	readHostilityEnv  = "NOEMA_RECORD_READ_HOSTILITY"
	readHostilitySeed = 17
	hostilityHoldMS   = 150
)

// readHostilityRead is one of the three reads of a transaction: where the proxy routes it, the upstream's final answer, an earlier answer that
// was not final (for STALE_READ), the member whose removal or renaming is a schema change, and the member that holds an enumerated value.
type readHostilityRead struct {
	name, target, operation, leaf string
	final, earlier                map[string]any
	requiredField, enumField      string
}

// the stand-in's labels and members, as its README and recordings give them (SYNTHETIC).
func standinLabel(m map[string]any) map[string]any {
	m["data_source"], m["synthetic"], m["tenant_id"], m["unavailable"] = "stand-in", true, "ten_aa", false
	return m
}

func hostilityReads() []readHostilityRead {
	ledger := func(posting string) map[string]any {
		return standinLabel(map[string]any{"entries": []any{map[string]any{"account_ref": "acc_1", "amount_minor": 250000, "currency": "NGN", "entry_id": "le_TX1_1",
			"entry_type": "DEBIT", "posted_at": "2026-10-01T07:10:00+01:00", "posting_status": posting, "transaction_ref": "TX1", "version": 1}}})
	}
	sw := func(status string, terminal bool) map[string]any {
		return standinLabel(map[string]any{"destination_ack": terminal, "is_terminal": terminal, "raw_code": "SYN-00", "raw_code_label": "SYNTHETIC",
			"raw_payload_hash": "sha256:" + strings.Repeat("0", 64), "received_at": "2026-10-01T07:10:00+01:00", "semantic_status": status, "switch_ref": "sw_TX1", "transaction_ref": "TX1"})
	}
	dest := func(credit, settlement string) map[string]any {
		return standinLabel(map[string]any{"credit_state": credit, "effective_at": "2026-10-01T07:10:00+01:00", "observed_at": "2026-10-01T09:40:00+01:00",
			"settlement_state": settlement, "transaction_ref": "TX1"})
	}
	return []readHostilityRead{
		{"ledger", "mock-ledger", "get_entries", "ledger-entries", ledger("POSTED"), ledger("PENDING"), "entries", "posting_status"},
		{"switch", "mock-switch", "get_status", "switch-status", sw("ACCEPTED", true), sw("PENDING", false), "semantic_status", "semantic_status"},
		{"destination", "mock-destination", "get_destination_status", "destination-status", dest("CREDITED", "SETTLED"), dest("CREDITED", "PENDING"), "credit_state", "credit_state"},
	}
}

func (r readHostilityRead) path() string { return "/standin/v1/transactions/TX1/" + r.leaf }

// readHostilityFamilies are the faults, in the order of the file. "kind" says how the exchange is made: "plain" is one request; "stale" is two
// (an earlier answer, a change upstream, the clock moved, then the stale read).
var readHostilityFamilies = []struct{ family, effect, params, kind string }{
	{"baseline", "", "", "plain"},
	{"http_500", "HTTP_5XX", `{"status":500}`, "plain"},
	{"service_unavailable", "SERVICE_UNAVAILABLE", `{}`, "plain"},
	{"http_429_retry_after", "HTTP_429", `{"retry_after_seconds":7}`, "plain"},
	{"http_429_no_retry_after", "HTTP_429", `{}`, "plain"},
	{"truncated_body", "MALFORMED_RESPONSE", `{}`, "plain"},
	{"invalid_json", "INVALID_JSON", `{}`, "plain"},
	{"wrong_content_type", "WRONG_CONTENT_TYPE", `{"content_type":"text/html"}`, "plain"},
	{"empty_body_success", "EMPTY_BODY_SUCCESS", `{}`, "plain"},
	{"drift_add", "SCHEMA_DRIFT", `{"change":"add","field":"unexpected_field"}`, "plain"},
	{"drift_remove", "SCHEMA_DRIFT", `{"change":"remove","field":"@required"}`, "plain"},
	{"drift_rename", "SCHEMA_DRIFT", `{"change":"rename","field":"@required"}`, "plain"},
	{"drift_unknown_enum", "SCHEMA_DRIFT", `{"change":"unknown_enum","field":"@enum"}`, "plain"},
	{"clock_skew_ahead", "CLOCK_SKEW", `{"skew_seconds":900}`, "plain"},
	{"clock_skew_behind", "CLOCK_SKEW", `{"skew_seconds":-900}`, "plain"},
	{"latency", "LATENCY", `{"delay_ms":150}`, "plain"},
	{"near_timeout_before", "NEAR_TIMEOUT_BEFORE", `{"delay_ms":150}`, "plain"},
	{"near_timeout_after", "NEAR_TIMEOUT_AFTER", `{"delay_ms":150}`, "plain"},
	{"timeout_before_commit", "TIMEOUT_BEFORE_COMMIT", `{"delay_ms":150}`, "plain"},
	{"timeout_after_commit", "TIMEOUT_AFTER_COMMIT", `{"delay_ms":150}`, "plain"},
	{"drop_response", "DROP_RESPONSE", `{}`, "plain"},
	{"stale_read", "STALE_READ", `{"seconds":5}`, "stale"},
}

type readHostilityExchange struct {
	Scenario  string          `json:"scenario"`
	Read      string          `json:"read"`
	Family    string          `json:"family"`
	Target    string          `json:"target"`
	Operation string          `json:"operation"`
	Effect    string          `json:"effect"`
	Params    json.RawMessage `json:"params"`
	Request   struct {
		Path string `json:"path"`
	} `json:"request"`
	Outcome string `json:"outcome"` // answered, or dropped (no answer: the connection was cut)
	HoldMS  int    `json:"hold_ms"` // the hold the effect asks for; 0 when it has none
	// Served is how the proxy says it served a STALE_READ: stale, or live when nothing was old enough.
	Served   string `json:"served,omitempty"`
	Response struct {
		Status      int    `json:"status"`
		ContentType string `json:"content_type"`
		RetryAfter  string `json:"retry_after,omitempty"`
		Body        string `json:"body"`
	} `json:"response"`
}

// hostilityUpstream serves one body per path and lets the test change it.
type hostilityUpstream struct {
	mu     sync.Mutex
	bodies map[string]map[string]any
}

func (u *hostilityUpstream) set(path string, v map[string]any) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.bodies[path] = v
}

func (u *hostilityUpstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	u.mu.Lock()
	v, ok := u.bodies[r.URL.Path]
	u.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	b, err := json.Marshal(v) // keys sorted, so the same body every time
	if err != nil {
		http.Error(w, "synthetic upstream: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(append(b, '\n'))
}

func recordReadHostility(t *testing.T) []byte {
	t.Helper()
	var out []readHostilityExchange
	for _, rd := range hostilityReads() {
		for _, f := range readHostilityFamilies {
			up := &hostilityUpstream{bodies: map[string]map[string]any{rd.path(): rd.final}}
			data, ctl := standinFaultRig(t, up)
			params := strings.NewReplacer("@required", rd.requiredField, "@enum", rd.enumField).Replace(f.params)
			ex := readHostilityExchange{Scenario: rd.name + "__" + f.family, Read: rd.name, Family: f.family, Target: rd.target, Operation: rd.operation, Effect: f.effect}
			ex.Request.Path = rd.path()
			if f.effect != "" {
				ex.Params = json.RawMessage(params)
				match := `{"nth_call":1}`
				clock := ""
				if f.kind == "stale" {
					match, clock = `{"nth_call":2}`, simClock
				}
				plan := `{"fault_plan_id":"fp_read_` + f.family + `","scenario_id":"SCN-READ-HOSTILITY-001","seed":` + itoa(readHostilitySeed) + `,"deterministic":true,"source":"PROXY","faults":[` +
					`{"target":"` + rd.target + `","operation":"` + rd.operation + `","effect":"` + f.effect + `","match":` + match + `,"params":` + params + `}]`
				if clock != "" {
					plan += `,"clock":` + clock
				}
				if status, body := ctl("PUT", "/control/v1/fault-plan", plan+"}"); status != 200 {
					t.Fatalf("%s: plan refused: %d %s", ex.Scenario, status, body)
				}
				var p struct {
					DelayMS int `json:"delay_ms"`
				}
				_ = json.Unmarshal([]byte(params), &p)
				ex.HoldMS = p.DelayMS
			}
			client := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{DisableKeepAlives: true}}
			if f.kind == "stale" {
				up.set(rd.path(), rd.earlier)
				first, err := client.Get(data.URL + rd.path())
				if err != nil {
					t.Fatalf("%s: %v", ex.Scenario, err)
				}
				_, _ = io.Copy(io.Discard, first.Body)
				_ = first.Body.Close() // read to the end; a close error cannot change what was recorded
				up.set(rd.path(), rd.final)
				if status, body := ctl("POST", "/control/v1/clock/advance", `{"step":"later"}`); status != 200 {
					t.Fatalf("%s: clock: %d %s", ex.Scenario, status, body)
				}
			}
			res, err := client.Get(data.URL + rd.path())
			if err != nil {
				ex.Outcome = "dropped"
			} else {
				rb, _ := io.ReadAll(res.Body)
				_ = res.Body.Close() // read to the end; a close error cannot change what was recorded
				ex.Outcome = "answered"
				ex.Response.Status, ex.Response.ContentType, ex.Response.RetryAfter, ex.Response.Body = res.StatusCode, res.Header.Get("Content-Type"), res.Header.Get("Retry-After"), string(rb)
			}
			if f.kind == "stale" {
				ex.Served = servedBy(t, ctl)
			}
			out = append(out, ex)
		}
	}
	file := map[string]any{
		"_provenance": map[string]any{
			"label":      "SYNTHETIC. Faulted reads of a transaction (ledger, switch, destination) for the Evidence read path (EVD-05, the hostility-proxy part, A717; REQ-CTX-008).",
			"method":     "The real hostility proxy (benchmark/hostility_proxy, config/routes.standin.json), over a real connection, with one fault from a real fault plan (seed 17), in front of a test-only upstream that answers in the stand-in's wire shape. Nothing is written by hand. An exchange with outcome dropped had no answer; hold_ms is the hold the effect asks for.",
			"regenerate": readHostilityEnv + "=1 go test ./internal/proxy -run TestReadHostilityExchanges (from benchmark/hostility_proxy). That test regenerates the file in memory and fails if the committed copy differs.",
		},
		"exchanges": out,
	}
	enc, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(enc, '\n')
}

// servedBy reads how the proxy served the last request of a STALE_READ plan from its trace.
func servedBy(t *testing.T, ctl func(method, path, body string) (int, string)) string {
	t.Helper()
	status, body := ctl("GET", "/control/v1/trace", "")
	if status != 200 {
		t.Fatalf("trace: %d %s", status, body)
	}
	var tr struct {
		Entries []struct {
			Effect string         `json:"effect"`
			Detail map[string]any `json:"detail"`
		} `json:"entries"`
	}
	if err := json.Unmarshal([]byte(body), &tr); err != nil {
		t.Fatal(err)
	}
	for _, e := range tr.Entries {
		if e.Effect == "STALE_READ" {
			s, _ := e.Detail["served"].(string)
			return s
		}
	}
	return ""
}

func TestReadHostilityExchanges(t *testing.T) {
	got := recordReadHostility(t)
	if os.Getenv(readHostilityEnv) != "" {
		if err := os.MkdirAll(readHostilityFile[:strings.LastIndex(readHostilityFile, "/")], 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(readHostilityFile, got, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s (%d bytes)", readHostilityFile, len(got))
		return
	}
	want, err := os.ReadFile(readHostilityFile)
	if err != nil {
		t.Fatalf("%v: record it with %s=1", err, readHostilityEnv)
	}
	if !bytes.Equal(bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n")), got) {
		t.Fatalf("the committed read-hostility exchanges are stale: re-record with %s=1 go test ./internal/proxy -run TestReadHostilityExchanges", readHostilityEnv)
	}
}

// REQ-ENV-004: the same plan and seed give the same exchanges (two recordings are identical), so the committed file is a function of the proxy and
// not of the run.
func TestReadHostilityRecordingIsReproducible(t *testing.T) {
	if !bytes.Equal(recordReadHostility(t), recordReadHostility(t)) {
		t.Fatal("two recordings of the same plans and seed differ")
	}
}

// The test-only upstream must answer in the real stand-in's wire shape: the members of its bodies equal the members of the real stand-in's recorded answers.
func TestReadHostilityUpstreamBodiesHaveTheStandinsWireShape(t *testing.T) {
	raw, err := os.ReadFile(standinBaseFile)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Exchanges []standinBase `json:"exchanges"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	realRec := map[string]string{"ledger": "ledger_posted", "switch": "switch_accepted", "destination": "destination_credited"}
	byName := map[string]string{}
	for _, e := range doc.Exchanges {
		byName[e.Scenario] = e.Response.Body
	}
	keys := func(m map[string]any) []string {
		var ks []string
		for k := range m {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		return ks
	}
	for _, rd := range hostilityReads() {
		var want map[string]any
		if err := json.Unmarshal([]byte(byName[realRec[rd.name]]), &want); err != nil {
			t.Fatalf("%s: %v", rd.name, err)
		}
		for label, body := range map[string]map[string]any{"final": rd.final, "earlier": rd.earlier} {
			if g, w := strings.Join(keys(body), ","), strings.Join(keys(want), ","); g != w {
				t.Errorf("%s %s: members %s, the real stand-in sends %s", rd.name, label, g, w)
			}
		}
		if rd.name == "ledger" {
			g, w := keys(rd.final["entries"].([]any)[0].(map[string]any)), keys(want["entries"].([]any)[0].(map[string]any))
			if strings.Join(g, ",") != strings.Join(w, ",") {
				t.Errorf("ledger entry members %v, the real stand-in sends %v", g, w)
			}
		}
	}
}
