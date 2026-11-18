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
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Noemai-org/noema/benchmark/hostility_proxy/internal/numbers"
	"github.com/Noemai-org/noema/benchmark/hostility_proxy/internal/proxy"
	"github.com/Noemai-org/noema/benchmark/hostility_proxy/internal/routes"
)

// The /partner refusal probe (benchmark/runner/tushe_partner_probe.py; ruling A263) meets REAL failure answers: they are produced by this proxy's FaultPlans on a real connection, not by a
// handwritten server (AGENTS 8, AGENT-12; Codex P1 on noema#655). The upstream is a test-only service that answers like the documented API: the reads are served to a bearer credential and
// every administration route is refused with 403. SYNTHETIC (AGENT-25). Nothing here says anything about Tushe's behaviour.

const probeRoutes = `{"schema_version":"1.0","synthetic":true,"routes":[
 {"method":"GET","path":"/v1/events","upstream":"up","target":"mock-destination","operation":"events_read"},
 {"method":"GET","path":"/v1/audit-events","upstream":"up","target":"mock-destination","operation":"audit_read"},
 {"method":"GET","path":"/partners/api-keys/scopes","upstream":"up","target":"mock-switch","operation":"admin_scopes"},
 {"method":"GET","path":"/partner/events","upstream":"up","target":"mock-switch","operation":"admin_events"}]}`

const probeCredential = "synthetic-runtime-credential-value"

type probeEnv struct {
	t       *testing.T
	dataURL string
	ctl     func(method, path, body string) (int, string)
	cred    string
}

func newProbeEnv(t *testing.T) *probeEnv {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "Bearer "+probeCredential {
			w.WriteHeader(401)
			_, _ = io.WriteString(w, `{"error":"unauthorized"}`)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/partner") {
			w.WriteHeader(403)
			_, _ = io.WriteString(w, `{"error":"forbidden"}`)
			return
		}
		_, _ = io.WriteString(w, `{"events":[]}`)
	})
	up := httptest.NewServer(mux)
	t.Cleanup(up.Close)
	table, err := routes.Parse([]byte(probeRoutes))
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
	srv, err := proxy.New(proxy.Options{Routes: table, Upstreams: map[string]*url.URL{"up": u}, Fixtures: fixtures, Numbers: mapper,
		BenchToken: benchToken, Log: slog.New(slog.NewJSONHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	data, control := httptest.NewServer(srv.DataHandler()), httptest.NewServer(srv.ControlHandler())
	t.Cleanup(data.Close)
	t.Cleanup(control.Close)
	client := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{DisableKeepAlives: true}}
	e := &probeEnv{t: t, dataURL: data.URL, cred: filepath.Join(t.TempDir(), "cred")}
	if err := os.WriteFile(e.cred, []byte(probeCredential+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	e.ctl = func(method, path, body string) (int, string) {
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
	return e
}

func (e *probeEnv) plan(faults ...string) {
	e.t.Helper()
	if len(faults) == 0 {
		return
	}
	if status, body := e.ctl("PUT", "/control/v1/fault-plan", planDoc("PROXY", strings.Join(faults, ","), "")); status != 200 {
		e.t.Fatalf("plan refused: %d %s", status, body)
	}
}

// run executes the probe as a real process (python3, standard library only) and returns its exit code and report.
func (e *probeEnv) run() (int, map[string]any, string) {
	e.t.Helper()
	python := ""
	for _, name := range []string{"python3", "python"} {
		if p, err := exec.LookPath(name); err == nil {
			python = p
			break
		}
	}
	if python == "" {
		if os.Getenv("CI") != "" {
			e.t.Fatal("python is needed to run the probe and CI has none")
		}
		e.t.Skip("python is not installed here; CI runs this test")
	}
	cmd := exec.Command(python, "-I", "../../../runner/tushe_partner_probe.py", "--base-url", e.dataURL, "--credential-file", e.cred, "--scheme", "bearer", "--allow-loopback-http", "--timeout", "5")
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		e.t.Fatalf("running the probe: %v", err)
	}
	var doc map[string]any
	_ = json.Unmarshal(out.Bytes(), &doc)
	return code, doc, out.String()
}

func verdictOf(doc map[string]any) string {
	v, _ := doc["verdict"].(string)
	return v
}

func TestThePartnerProbeMeetsRealFailuresFromTheHostilityProxy(t *testing.T) {
	admin := func(effect, params string) string {
		return fault("mock-switch", "admin_scopes", effect, `{"nth_call":1}`, params)
	}
	cases := []struct {
		name   string
		faults []string
		code   int
		prefix string
	}{
		{"no fault: the control is served and both administration routes refuse", nil, 0, "MET"},
		{"a 500 on an administration route is not a refusal", []string{admin("HTTP_5XX", `{"status":500}`)}, 3, "NOT RUN"},
		{"a 503 on an administration route is not a refusal", []string{admin("SERVICE_UNAVAILABLE", `{}`)}, 3, "NOT RUN"},
		{"a 429 on an administration route is not a refusal", []string{admin("HTTP_429", `{"retry_after_seconds":1}`)}, 3, "NOT RUN"},
		{"a dropped connection on an administration route is not a refusal", []string{admin("DROP_RESPONSE", `{}`)}, 3, "NOT RUN"},
		{"an administration route that answers 2xx is NOT MET", []string{admin("EMPTY_BODY_SUCCESS", `{}`)}, 1, "NOT MET"},
		{"a served route is not erased by a later dropped connection (Codex P2 on noema#655)",
			[]string{admin("EMPTY_BODY_SUCCESS", `{}`), fault("mock-switch", "admin_events", "DROP_RESPONSE", `{"nth_call":1}`, `{}`)}, 1, "NOT MET"},
		{"a control that fails everywhere is not read as a refusal on the administration routes",
			[]string{fault("mock-destination", "events_read", "HTTP_5XX", `{"nth_call":1}`, `{"status":500}`), fault("mock-destination", "events_read", "HTTP_5XX", `{"nth_call":2}`, `{"status":500}`),
				fault("mock-destination", "audit_read", "HTTP_5XX", `{"nth_call":1}`, `{"status":500}`)}, 3, "NOT RUN"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newProbeEnv(t)
			e.plan(c.faults...)
			code, doc, raw := e.run()
			if code != c.code || !strings.HasPrefix(verdictOf(doc), c.prefix) {
				t.Fatalf("exit %d, verdict %q, want exit %d and %q\n%s", code, verdictOf(doc), c.code, c.prefix, raw)
			}
			if strings.Contains(raw, probeCredential) {
				t.Fatal("the credential is in the probe's output")
			}
		})
	}
}

func TestThePartnerProbeNeverPrintsAMultiLineCredential(t *testing.T) {
	e := newProbeEnv(t)
	if err := os.WriteFile(e.cred, []byte(fmt.Sprintf("%s\nsecond-line-of-a-bad-file\n", probeCredential)), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, raw := e.run()
	if code != 3 || strings.Contains(raw, probeCredential) || strings.Contains(raw, "second-line-of-a-bad-file") {
		t.Fatalf("exit %d; a bad credential file must be NOT RUN and never echoed:\n%s", code, raw)
	}
}
