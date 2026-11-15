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
	"testing"
	"time"

	"github.com/Noemai-org/noema/benchmark/hostility_proxy/internal/numbers"
	"github.com/Noemai-org/noema/benchmark/hostility_proxy/internal/proxy"
	"github.com/Noemai-org/noema/benchmark/hostility_proxy/internal/routes"
)

// The faulted exchanges the core's StandinClient is tested against (task ENV-06: "each FaultPlan effect is observed by the adapter as
// specified"; REQ-ENV-005 as amended; AGENT-12).
//
// Each exchange is the REAL proxy, over a real connection, using config/routes.standin.json, in front of a response recorded from the REAL
// stand-in service (synthetic/standin: record_test.go), with one fault from a real fault plan. Nothing is written by hand. The file is
// deterministic (the proxy is seeded); this test regenerates it in memory and fails if the committed copy differs. To re-record:
//
//	NOEMA_RECORD_STANDIN_FAULTED=1 go test ./internal/proxy -run TestStandinFaultedExchanges
//
// SYNTHETIC (AGENT-25). It says nothing about how Tushe behaves.
const (
	standinBaseFile    = adapterTestdata + "standin_recordings.json"
	standinFaultedFile = adapterTestdata + "standin_faulted_recordings.json"
	standinRecordEnv   = "NOEMA_RECORD_STANDIN_FAULTED"
)

type standinBase struct {
	Scenario  string `json:"scenario"`
	Principal string `json:"principal"`
	Request   struct {
		Path  string `json:"path"`
		Query string `json:"query"`
	} `json:"request"`
	Response struct {
		Status      int    `json:"status"`
		ContentType string `json:"content_type"`
		Body        string `json:"body"`
	} `json:"response"`
}

type standinFaulted struct {
	Scenario  string          `json:"scenario"`
	Base      string          `json:"base"`
	Target    string          `json:"target"`
	Operation string          `json:"operation"`
	Effect    string          `json:"effect"`
	Params    json.RawMessage `json:"params"`
	Request   struct {
		Path  string `json:"path"`
		Query string `json:"query"`
	} `json:"request"`
	Response struct {
		Status      int    `json:"status"`
		ContentType string `json:"content_type"`
		RetryAfter  string `json:"retry_after,omitempty"`
		Body        string `json:"body"`
	} `json:"response"`
}

// the reads to fault: the base scenario of the stand-in's recordings, the logical service and operation a fault plan addresses it by, and
// a field whose removal is a schema change for that answer.
var standinReads = []struct{ base, target, operation, requiredField string }{
	{"ledger_posted", "mock-ledger", "get_entries", "entries"},
	{"switch_timed_out", "mock-switch", "get_status", "semantic_status"},
	{"destination_pending", "mock-destination", "get_destination_status", "credit_state"},
	{"candidates_of_usr_a", "mock-ledger", "list_transactions", "complete"},
}

func standinCorruptions(requiredField string) []struct{ family, effect, params string } {
	return []struct{ family, effect, params string }{
		{"truncated_body", "MALFORMED_RESPONSE", `{}`},
		{"invalid_json", "INVALID_JSON", `{}`},
		{"wrong_content_type", "WRONG_CONTENT_TYPE", `{"content_type":"text/html"}`},
		{"empty_body_success", "EMPTY_BODY_SUCCESS", `{}`},
		{"extra_field", "SCHEMA_DRIFT", `{"change":"add","field":"unexpected_field"}`},
		{"missing_required_field", "SCHEMA_DRIFT", `{"change":"remove","field":"` + requiredField + `"}`},
		{"http_503", "HTTP_5XX", `{"status":503}`},
		{"http_429", "HTTP_429", `{"retry_after_seconds":7}`},
	}
}

func standinFaultRig(t *testing.T, upstream http.Handler) (data *httptest.Server, ctl func(method, path, body string) (int, string)) {
	t.Helper()
	up := httptest.NewServer(upstream)
	t.Cleanup(up.Close)
	raw, err := os.ReadFile("../../config/routes.standin.json")
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
	mapper, err := numbers.New([]byte(numberKey))
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(up.URL)
	srv, err := proxy.New(proxy.Options{Routes: table, Upstreams: map[string]*url.URL{"standin": u}, Fixtures: fixtures, Numbers: mapper,
		BenchToken: benchToken, Log: slog.New(slog.NewJSONHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	data, control := httptest.NewServer(srv.DataHandler()), httptest.NewServer(srv.ControlHandler())
	t.Cleanup(data.Close)
	t.Cleanup(control.Close)
	client := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{DisableKeepAlives: true}}
	ctl = func(method, path, body string) (int, string) {
		req, _ := http.NewRequest(method, control.URL+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+benchToken)
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = res.Body.Close() }()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(b)
	}
	return data, ctl
}

func recordStandinFaulted(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(standinBaseFile)
	if err != nil {
		t.Fatalf("%v: record the stand-in's own exchanges first (synthetic/standin, NOEMA_RECORD_STANDIN=1)", err)
	}
	var doc struct {
		Exchanges []standinBase `json:"exchanges"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	byName := map[string]standinBase{}
	for _, b := range doc.Exchanges {
		byName[b.Scenario] = b
	}
	client := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{DisableKeepAlives: true}}
	var out []standinFaulted
	for _, r := range standinReads {
		base, ok := byName[r.base]
		if !ok {
			t.Fatalf("the stand-in's recordings have no %q", r.base)
		}
		for _, c := range standinCorruptions(r.requiredField) {
			upstream := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", base.Response.ContentType)
				w.WriteHeader(base.Response.Status)
				_, _ = w.Write([]byte(base.Response.Body))
			})
			data, ctl := standinFaultRig(t, upstream)
			fault := `{"target":"` + r.target + `","operation":"` + r.operation + `","effect":"` + c.effect + `","match":{"nth_call":1},"params":` + c.params + `}`
			status, body := ctl("PUT", "/control/v1/fault-plan", `{"fault_plan_id":"fp_standin_`+c.family+`","scenario_id":"SCN-STANDIN-001","seed":15,"deterministic":true,"source":"PROXY","faults":[`+fault+`]}`)
			if status != 200 {
				t.Fatalf("%s/%s: plan refused: %d %s", r.base, c.family, status, body)
			}
			target := data.URL + base.Request.Path
			if base.Request.Query != "" {
				target += "?" + base.Request.Query
			}
			req, _ := http.NewRequest(http.MethodGet, target, nil)
			res, err := client.Do(req)
			if err != nil {
				t.Fatalf("%s/%s: %v", r.base, c.family, err)
			}
			rb, _ := io.ReadAll(res.Body)
			_ = res.Body.Close() // read to the end; a close error cannot change what was recorded
			ex := standinFaulted{Scenario: r.base + "__" + c.family, Base: r.base, Target: r.target, Operation: r.operation, Effect: c.effect, Params: json.RawMessage(c.params)}
			ex.Request.Path, ex.Request.Query = base.Request.Path, base.Request.Query
			ex.Response.Status, ex.Response.ContentType, ex.Response.RetryAfter, ex.Response.Body = res.StatusCode, res.Header.Get("Content-Type"), res.Header.Get("Retry-After"), string(rb)
			out = append(out, ex)
		}
	}
	file := map[string]any{
		"_provenance": map[string]string{
			"label":      "SYNTHETIC. Faulted exchanges of the Noema-side stand-in (REQ-ENV-005 as amended, task ENV-06).",
			"method":     "The real hostility proxy (benchmark/hostility_proxy, config/routes.standin.json), over a real connection, in front of a response recorded from the real stand-in service (standin_recordings.json), with one fault from a real fault plan (seed 15). Nothing is written by hand.",
			"regenerate": standinRecordEnv + "=1 go test ./internal/proxy -run TestStandinFaultedExchanges (from benchmark/hostility_proxy). That test regenerates the file in memory and fails if the committed copy differs.",
		},
		"exchanges": out,
	}
	enc, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(enc, '\n')
}

func TestStandinFaultedExchanges(t *testing.T) {
	got := recordStandinFaulted(t)
	if os.Getenv(standinRecordEnv) != "" {
		if err := os.WriteFile(standinFaultedFile, got, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s (%d bytes)", standinFaultedFile, len(got))
		return
	}
	want, err := os.ReadFile(standinFaultedFile)
	if err != nil {
		t.Fatalf("%v: record it with %s=1", err, standinRecordEnv)
	}
	if !bytes.Equal(bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n")), got) {
		t.Fatalf("the committed stand-in faulted exchanges are stale: re-record with %s=1 go test ./internal/proxy -run TestStandinFaultedExchanges", standinRecordEnv)
	}
}
