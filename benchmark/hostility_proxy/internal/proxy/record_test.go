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

// The corrupted exchanges the core's case adapter is tested against (REQ-ENV-005 as amended, task ENV-15).
//
// Each exchange is the REAL proxy, over a real connection, in front of a response recorded from the real mock-case
// (the adapter's own recordings), with one corrupted-payload fault from a real fault plan. Nothing is written by hand:
// the adapter must not be tested against a failure mode that was only imagined (AGENT-12). The file is deterministic
// (the proxy is seeded); this test regenerates it in memory and fails if the committed copy differs. To re-record:
//
//	NOEMA_RECORD_CORRUPTED=1 go test ./internal/proxy -run TestCorruptedCaseExchanges
const (
	adapterTestdata = "../../../../services/core/adapters/synthetic_tushe/testdata/"
	baseFile        = adapterTestdata + "mock_case_recordings.json"
	corruptedFile   = adapterTestdata + "corrupted_recordings.json"
	recordEnv       = "NOEMA_RECORD_CORRUPTED"
)

// A test-only route table: the case API of mock-case, behind the proxy under a placeholder logical service, because the
// proxy never produces faults for the `mock-case` target (fault-plan reading rule 1). SYNTHETIC; follows no Tushe interface.
const caseRoutes = `{"schema_version":"1.0","synthetic":true,"routes":[
 {"method":"GET","path":"/v1/cases/{case_id}","upstream":"case","target":"mock-destination","operation":"get_case"},
 {"method":"GET","path":"/v1/cases","upstream":"case","target":"mock-destination","operation":"find_cases"},
 {"method":"POST","path":"/v1/cases","upstream":"case","target":"mock-destination","operation":"create_case"}]}`

type baseExchange struct {
	Scenario string `json:"scenario"`
	Request  struct {
		Method  string            `json:"method"`
		Path    string            `json:"path"`
		Query   string            `json:"query"`
		Headers map[string]string `json:"headers"`
		Body    any               `json:"body"`
	} `json:"request"`
	Response struct {
		Status  int               `json:"status"`
		Headers map[string]string `json:"headers"`
		Body    any               `json:"body"`
	} `json:"response"`
}

type corruptedExchange struct {
	Scenario  string          `json:"scenario"`
	Base      string          `json:"base"`
	Operation string          `json:"operation"`
	Family    string          `json:"family"`
	Effect    string          `json:"effect"`
	Params    json.RawMessage `json:"params"`
	Response  struct {
		Status      int    `json:"status"`
		ContentType string `json:"content_type"`
		Body        string `json:"body"`
	} `json:"response"`
}

// the families of REQ-ENV-005 as amended, each with the effect that produces it and the field it names
var corruptions = []struct{ family, effect, params string }{
	{"truncated_body", "MALFORMED_RESPONSE", `{}`},
	{"invalid_json", "INVALID_JSON", `{}`},
	{"wrong_content_type", "WRONG_CONTENT_TYPE", `{"content_type":"text/html"}`},
	{"empty_body_success", "EMPTY_BODY_SUCCESS", `{}`},
	{"extra_field", "SCHEMA_DRIFT", `{"change":"add","field":"unexpected_field"}`},
	{"missing_required_field", "SCHEMA_DRIFT", `{"change":"remove","field":"version_token"}`},
}

// the exchanges to corrupt: a read by id, a read by key, a write that creates
var bases = []struct{ scenario, operation string }{{"get_200", "get_case"}, {"find_by_key_one", "find_cases"}, {"create_201", "create_case"}}

func caseRig(t *testing.T, upstream http.Handler) (data, control *httptest.Server, ctl func(method, path, body string) (int, string)) {
	t.Helper()
	up := httptest.NewServer(upstream)
	t.Cleanup(up.Close)
	table, err := routes.Parse([]byte(caseRoutes))
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
	srv, err := proxy.New(proxy.Options{Routes: table, Upstreams: map[string]*url.URL{"case": u}, Fixtures: fixtures, Numbers: mapper,
		BenchToken: benchToken, Log: slog.New(slog.NewJSONHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	data, control = httptest.NewServer(srv.DataHandler()), httptest.NewServer(srv.ControlHandler())
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
	return data, control, ctl
}

func recordCorrupted(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(baseFile)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Recordings []baseExchange `json:"recordings"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	byName := map[string]baseExchange{}
	for _, b := range doc.Recordings {
		byName[b.Scenario] = b
	}
	var out []corruptedExchange
	client := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{DisableKeepAlives: true}}
	for _, b := range bases {
		base, ok := byName[b.scenario]
		if !ok {
			t.Fatalf("the adapter's recordings have no %q", b.scenario)
		}
		for _, c := range corruptions {
			upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				for k, v := range base.Response.Headers {
					w.Header().Set(k, v)
				}
				w.WriteHeader(base.Response.Status)
				_ = json.NewEncoder(w).Encode(base.Response.Body)
			})
			data, _, ctl := caseRig(t, upstream)
			fault := `{"target":"mock-destination","operation":"` + b.operation + `","effect":"` + c.effect + `","match":{"nth_call":1},"params":` + c.params + `}`
			status, body := ctl("PUT", "/control/v1/fault-plan", `{"fault_plan_id":"fp_corrupt_`+c.family+`","scenario_id":"SCN-CORRUPT-001","seed":15,"deterministic":true,"source":"PROXY","faults":[`+fault+`]}`)
			if status != 200 {
				t.Fatalf("%s/%s: plan refused: %d %s", b.scenario, c.family, status, body)
			}
			var rd io.Reader
			if base.Request.Body != nil {
				bb, _ := json.Marshal(base.Request.Body)
				rd = bytes.NewReader(bb)
			}
			target := data.URL + base.Request.Path
			if base.Request.Query != "" {
				target += "?" + base.Request.Query
			}
			req, _ := http.NewRequest(base.Request.Method, target, rd)
			for k, v := range base.Request.Headers {
				req.Header.Set(k, v)
			}
			if rd != nil {
				req.Header.Set("Content-Type", "application/json")
			}
			res, err := client.Do(req)
			if err != nil {
				t.Fatalf("%s/%s: %v", b.scenario, c.family, err)
			}
			rb, _ := io.ReadAll(res.Body)
			_ = res.Body.Close() // read to the end; a close error cannot change what was recorded
			ex := corruptedExchange{Scenario: b.scenario + "__" + c.family, Base: b.scenario, Operation: b.operation, Family: c.family, Effect: c.effect, Params: json.RawMessage(c.params)}
			ex.Response.Status, ex.Response.ContentType, ex.Response.Body = res.StatusCode, res.Header.Get("Content-Type"), string(rb)
			out = append(out, ex)
		}
	}
	file := map[string]any{
		"_provenance": map[string]string{
			"label":      "SYNTHETIC. Corrupted-payload exchanges (REQ-ENV-005 as amended, DEC-2026-10-31 D15, task ENV-15).",
			"method":     "The real hostility proxy (benchmark/hostility_proxy), over a real connection, in front of the response the adapter's own recordings hold for the base scenario (recorded from the real mock-case), with one fault from a real fault-plan.v2 plan (seed 15). Nothing is written by hand. The route table is test-only and puts the case API behind a placeholder logical service, because the proxy never produces faults for the mock-case target.",
			"regenerate": "NOEMA_RECORD_CORRUPTED=1 go test ./internal/proxy -run TestCorruptedCaseExchanges (from benchmark/hostility_proxy). That test regenerates the file in memory and fails if the committed copy differs.",
		},
		"exchanges": out,
	}
	enc, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(enc, '\n')
}

func TestCorruptedCaseExchanges(t *testing.T) {
	got := recordCorrupted(t)
	if os.Getenv(recordEnv) != "" {
		if err := os.WriteFile(corruptedFile, got, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s (%d bytes)", corruptedFile, len(got))
		return
	}
	want, err := os.ReadFile(corruptedFile)
	if err != nil {
		t.Fatalf("%v: record it with %s=1", err, recordEnv)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("the committed corrupted exchanges are stale: re-record with %s=1 go test ./internal/proxy -run TestCorruptedCaseExchanges", recordEnv)
	}
}
