package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Noemai-org/noema/synthetic/mock_case/internal/api"
	"github.com/Noemai-org/noema/synthetic/mock_case/internal/config"
	"github.com/Noemai-org/noema/synthetic/mock_case/internal/store"
	"github.com/Noemai-org/noema/synthetic/mock_case/internal/testpg"
)

const (
	runtimeToken = "runtime-token-runtime-token-runtime-token"
	benchToken   = "bench-token-bench-token-bench-token-xx"
)

// env is a real mock-case: both handlers on real listeners, a real PostgreSQL behind them.
type env struct {
	t        testing.TB
	db       testpg.DB
	st       *store.Store
	srv      *api.Server
	caseURL  string
	benchURL string
	client   *http.Client
	logs     *lockedBuffer
}

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func newEnv(t testing.TB) *env {
	t.Helper()
	db := testpg.New(t)
	st, err := store.Open(context.Background(), db.AppDSN, db.AdminDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{"/app": "x", "/admin": "y", "/rt": runtimeToken, "/bt": benchToken}
	vars := map[string]string{"MOCK_CASE_BENCH_LISTEN": "127.0.0.1:0", "MOCK_CASE_DB_APP_PASSWORD_FILE": "/app", "MOCK_CASE_DB_ADMIN_PASSWORD_FILE": "/admin",
		"MOCK_CASE_RUNTIME_TOKEN_FILE": "/rt", "MOCK_CASE_BENCH_TOKEN_FILE": "/bt"}
	cfg, err := config.Load(func(k string) string { return vars[k] }, func(p string) ([]byte, error) {
		if v, ok := files[p]; ok {
			return []byte(v), nil
		}
		return nil, errors.New("no file " + p)
	})
	if err != nil {
		t.Fatal(err)
	}
	logs := &lockedBuffer{}
	srv := api.New(cfg, st, slog.New(slog.NewJSONHandler(logs, nil)))
	cs := httptest.NewServer(srv.CaseHandler())
	bs := httptest.NewServer(srv.BenchHandler())
	t.Cleanup(cs.Close)
	t.Cleanup(bs.Close)
	client := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{DisableKeepAlives: true}}
	return &env{t: t, db: db, st: st, srv: srv, caseURL: cs.URL, benchURL: bs.URL, client: client, logs: logs}
}

type resp struct {
	status int
	header http.Header
	raw    []byte
	json   map[string]any
}

func (r resp) str(path ...string) string {
	var cur any = r.json
	for _, p := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = m[p]
	}
	s, _ := cur.(string)
	return s
}

func (r resp) errCode() string { return r.str("error", "code") }

func (r resp) items() []map[string]any {
	var out []map[string]any
	raw, _ := r.json["items"].([]any)
	for _, it := range raw {
		out = append(out, it.(map[string]any))
	}
	return out
}

// send performs a request and returns the response, or the transport error.
func (e *env) send(client *http.Client, method, url, token string, hdr map[string]string, body any) (resp, error) {
	var rd io.Reader
	if body != nil {
		switch b := body.(type) {
		case string:
			rd = strings.NewReader(b)
		case []byte:
			rd = bytes.NewReader(b)
		default:
			raw, err := json.Marshal(body)
			if err != nil {
				e.t.Fatal(err)
			}
			rd = bytes.NewReader(raw)
		}
	}
	req, err := http.NewRequest(method, url, rd)
	if err != nil {
		e.t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	res, err := client.Do(req)
	if err != nil {
		return resp{}, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return resp{}, err
	}
	out := resp{status: res.StatusCode, header: res.Header, raw: raw}
	_ = json.Unmarshal(raw, &out.json)
	return out, nil
}

func (e *env) do(method, url, token string, hdr map[string]string, body any) resp {
	e.t.Helper()
	r, err := e.send(e.client, method, url, token, hdr, body)
	if err != nil {
		e.t.Fatalf("%s %s: %v", method, url, err)
	}
	return r
}

// runtime calls the case API as the core's case adapter; bench calls the truth and control API as the runner.
func (e *env) runtime(method, path string, hdr map[string]string, body any) resp {
	e.t.Helper()
	return e.do(method, e.caseURL+path, runtimeToken, hdr, body)
}

func (e *env) bench(method, path string, body any) resp {
	e.t.Helper()
	return e.do(method, e.benchURL+path, benchToken, nil, body)
}

func key(k string) map[string]string { return map[string]string{"Idempotency-Key": k} }

func caseBody() map[string]any {
	return map[string]any{"tenant_id": "ten_a", "principal_id": "usr_1", "transaction_ref": "TX88213", "reason_code": "CREDIT_MISSING",
		"created_by_intent": "int_01J9X3", "epoch": 0}
}

func (e *env) createCase(k string) resp {
	e.t.Helper()
	return e.runtime("POST", "/v1/cases", key(k), caseBody())
}

func (e *env) loadPlan(plan string) resp {
	e.t.Helper()
	r := e.bench("PUT", "/control/v1/fault-plan", plan)
	if r.status != 200 {
		e.t.Fatalf("plan refused: %d %s", r.status, r.raw)
	}
	return r
}

func plan(faults string, clock string) string {
	if clock != "" {
		clock = `,"clock":` + clock
	}
	return `{"fault_plan_id":"fp_test","scenario_id":"SCN-TEST","seed":42,"deterministic":true,"source":"MOCK_CASE","faults":[` + faults + `]` + clock + `}`
}

func fault(op, match, effect, params string) string {
	return `{"target":"mock-case","operation":"` + op + `","match":` + match + `,"effect":"` + effect + `","params":` + params + `}`
}

const simClock = `{"start":"2026-10-01T09:40:00+01:00","advances":[{"at_step":"after_receipt","by":"PT31S"}]}`

func (e *env) digest() (state, trace string) {
	e.t.Helper()
	d := e.bench("GET", "/truth/v1/digest", nil)
	return d.str("state_hash"), d.str("fault_trace_hash")
}

func (e *env) state() map[string]any {
	e.t.Helper()
	return e.bench("GET", "/truth/v1/state", nil).json
}

func (e *env) count(kind string) int {
	arr, _ := e.state()[kind].([]any)
	return len(arr)
}
