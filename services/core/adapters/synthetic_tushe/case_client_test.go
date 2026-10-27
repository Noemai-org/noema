package synthetic_tushe

// Contract tests of CaseClient against exchanges recorded from the real mock-case (REQ-ENV-001, REQ-EXE-007,
// REQ-ENV-008, REQ-TST-003, REQ-TST-004). Each scenario replays the recorded response and fails if the
// request the adapter sends differs from the request the real mock-case accepted. Expected results are
// written here from the PERD (27.1 E10, 34.4, REQ-EXE-007), not copied from the adapter's output.
//
// The recordings are SYNTHETIC. The live run against a running mock-case is not part of this file.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/adapters"
)

const testToken = "synthetic-test-credential-not-a-secret"

type recording struct {
	Scenario string `json:"scenario"`
	Request  struct {
		Method  string            `json:"method"`
		Path    string            `json:"path"`
		Query   string            `json:"query"`
		Headers map[string]string `json:"headers"`
		Body    any               `json:"body"`
	} `json:"request"`
	Response map[string]any `json:"response"`
}

func loadRecordings(t *testing.T) map[string]recording {
	t.Helper()
	raw, err := os.ReadFile("testdata/mock_case_recordings.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Recordings []recording `json:"recordings"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	out := map[string]recording{}
	for _, r := range doc.Recordings {
		out[r.Scenario] = r
	}
	return out
}

func tokenFile(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "runtime_token")
	if err := os.WriteFile(p, []byte(testToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// replay serves one recorded exchange and fails the test if the adapter's request is not the recorded one.
func replay(t *testing.T, rc recording, hits *int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(hits, 1)
		if r.Header.Get("Authorization") != "Bearer "+testToken {
			t.Errorf("%s: the runtime credential was not sent as a bearer token", rc.Scenario)
		}
		if r.Method != rc.Request.Method || r.URL.Path != rc.Request.Path {
			t.Errorf("%s: request %s %s, recorded %s %s", rc.Scenario, r.Method, r.URL.Path, rc.Request.Method, rc.Request.Path)
		}
		got, _ := url.ParseQuery(r.URL.RawQuery)
		want, _ := url.ParseQuery(rc.Request.Query)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: query %v, recorded %v", rc.Scenario, got, want)
		}
		for k, v := range rc.Request.Headers {
			if r.Header.Get(k) != v {
				t.Errorf("%s: header %s = %q, recorded %q", rc.Scenario, k, r.Header.Get(k), v)
			}
		}
		if rc.Request.Body != nil {
			var body any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if !reflect.DeepEqual(body, rc.Request.Body) {
				t.Errorf("%s: body %v, recorded %v", rc.Scenario, body, rc.Request.Body)
			}
		}
		if msg, ok := rc.Response["transport_error"].(string); ok {
			if strings.Contains(msg, "deadline") {
				select { // the recorded exchange never answered inside the caller's timeout
				case <-r.Context().Done():
				case <-time.After(5 * time.Second):
				}
				return
			}
			conn, _, err := w.(http.Hijacker).Hijack() // the recorded connection was dropped with no response
			if err == nil {
				conn.Close()
			}
			return
		}
		hd, _ := rc.Response["headers"].(map[string]any)
		for k, v := range hd {
			w.Header().Set(k, v.(string))
		}
		w.WriteHeader(int(rc.Response["status"].(float64)))
		body, _ := json.Marshal(rc.Response["body"])
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newClient(t *testing.T, srv *httptest.Server) *CaseClient {
	t.Helper()
	c, err := NewCaseClient(srv.URL, tokenFile(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func createIn(key string) adapters.CreateCaseInput {
	return adapters.CreateCaseInput{IdempotencyKey: key, TenantID: "ten_a", PrincipalID: "usr_1", TransactionRef: "TX88213",
		ReasonCode: "CREDIT_MISSING", CreatedByIntent: "int_01J9X3", Epoch: 0, Timeout: 3 * time.Second}
}

// createInFor is createIn for another subject: each faulted create was recorded on a subject of its own.
func createInFor(key, tx string) adapters.CreateCaseInput {
	in := createIn(key)
	in.TransactionRef = tx
	return in
}

func deadline(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func caseID(recs map[string]recording) string {
	return recs["create_201"].Response["body"].(map[string]any)["case_id"].(string)
}

// REQ-EXE-007, PERD 27.1 E10: every write status maps to its transport class, with the raw code kept next
// to the mapping.
func TestCreateCaseClassifiesEveryRecordedWriteOutcome(t *testing.T) {
	recs := loadRecordings(t)
	cases := []struct {
		scenario string
		input    func() adapters.CreateCaseInput
		class    adapters.TransportClass
		raw      string
		ack      string // ExternalAck
		collide  bool
		retry    time.Duration
	}{
		{"create_201", func() adapters.CreateCaseInput { return createIn("idk_v1_live01") }, adapters.ClassAck2xx, "201", "CMP-2026-00001", false, 0},
		// a replay with the same payload is an acknowledgement with the EXISTING reference (REQ-EXE-007)
		{"create_replay_200", func() adapters.CreateCaseInput { return createIn("idk_v1_live01") }, adapters.ClassAck2xx, "200:REPLAY", "CMP-2026-00001", false, 0},
		// the same key with another payload is refused, never acknowledged, and flagged (EC-E06)
		{"create_key_collision_409", func() adapters.CreateCaseInput {
			in := createIn("idk_v1_live01")
			in.TransactionRef = "TX99999"
			return in
		}, adapters.ClassRejected4xx, "409:IDEMPOTENCY_CONFLICT", "", true, 0},
		{"create_injected_collision_409", func() adapters.CreateCaseInput { return createInFor("idk_v1_live03", "TX00003") }, adapters.ClassRejected4xx, "409:IDEMPOTENCY_CONFLICT", "", true, 0},
		// semantic "pending" behind a 200: preserved as acknowledged with no reference; Outcome decides (EC-E03)
		{"create_http200_pending", func() adapters.CreateCaseInput { return createInFor("idk_v1_live02", "TX00002") }, adapters.ClassAck2xx, "200:PENDING", "", false, 0},
		// a 5xx, committed or not, is never read as failure: Execution maps it to OUTCOME_UNKNOWN
		{"create_http_503", func() adapters.CreateCaseInput { return createInFor("idk_v1_live04", "TX00004") }, adapters.ClassServer5xx, "503:UNAVAILABLE", "", false, 0},
		{"create_http_500_after_commit", func() adapters.CreateCaseInput { return createInFor("idk_v1_live05", "TX00005") }, adapters.ClassServer5xx, "500:UNAVAILABLE", "", false, 0},
		{"create_http_429", func() adapters.CreateCaseInput { return createInFor("idk_v1_live06", "TX00006") }, adapters.ClassRejected4xx, "429:RATE_LIMITED", "", false, 7 * time.Second},
		// no response at all: the case is committed in mock-case, and the adapter reports only what it saw
		{"create_drop_response", func() adapters.CreateCaseInput { return createInFor("idk_v1_live07", "TX00007") }, adapters.ClassNoResponse, "NO_RESPONSE", "", false, 0},
		{"create_timeout_after_commit", func() adapters.CreateCaseInput {
			in := createInFor("idk_v1_live08", "TX00008")
			in.Timeout = 300 * time.Millisecond
			return in
		}, adapters.ClassTimeout, "TIMEOUT", "", false, 0},
	}
	for _, c := range cases {
		t.Run(c.scenario, func(t *testing.T) {
			var hits int32
			cli := newClient(t, replay(t, recs[c.scenario], &hits))
			got := cli.CreateCase(context.Background(), c.input())
			if got.Class != c.class || got.RawCode != c.raw || got.ExternalAck != c.ack || got.KeyCollision != c.collide || got.RetryAfter != c.retry {
				t.Fatalf("got class=%s raw=%s ack=%q collision=%v retryAfter=%v", got.Class, got.RawCode, got.ExternalAck, got.KeyCollision, got.RetryAfter)
			}
			if n := atomic.LoadInt32(&hits); n != 1 {
				t.Fatalf("%d requests for one write: an adapter never retries (REQ-EXE-006)", n)
			}
		})
	}
}

// A create is conditional on there being no case for its subject and epoch. When the target holds one that another key
// made, the write did not happen. That is a business rejection (E10, REQ-EXE-007), not a stale version: a create has no
// version token. It has its own class, DUPLICATE_CREATE (errors.v2.json, D6). The case that exists is named, never
// taken as this write's result.
func TestCreateCaseRefusedBecauseTheSubjectHasACase(t *testing.T) {
	recs := loadRecordings(t)
	var hits int32
	cli := newClient(t, replay(t, recs["create_precondition_failed_412"], &hits))
	got := cli.CreateCase(context.Background(), createIn("idk_v1_live09"))
	if got.Class != adapters.ClassDuplicateCreate || got.RawCode != "412:PRECONDITION_FAILED" || got.KeyCollision || got.ExternalAck != "" || got.Case != nil {
		t.Fatalf("refused create: %+v", got)
	}
	if got.ExistingCaseID != caseID(recs) || got.CurrentVersionToken != "v1" {
		t.Fatalf("the refusal names the case that exists and its token: %+v", got)
	}
	if n := atomic.LoadInt32(&hits); n != 1 {
		t.Fatalf("%d requests for one write: an adapter never retries (REQ-EXE-006)", n)
	}
}

// Any other 412 is a rejection by the target, not a stale precondition.
func TestAnotherPreconditionStatusIsAPlainRejection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPreconditionFailed)
		_, _ = w.Write([]byte(`{"error":{"code":"SOMETHING_ELSE","message":"x","retryable":false}}`))
	}))
	t.Cleanup(srv.Close)
	got := newClient(t, srv).CreateCase(context.Background(), createIn("idk_x"))
	if got.Class != adapters.ClassRejected4xx || got.RawCode != "412:SOMETHING_ELSE" {
		t.Fatalf("%+v", got)
	}
}

// PERD 34.4: a route with an expected version; a mismatch is a distinct error and names the current token.
func TestRouteCaseRecordedOutcomes(t *testing.T) {
	recs := loadRecordings(t)
	id := caseID(recs)
	run := func(scenario, queue, token string) adapters.WriteResult {
		var hits int32
		cli := newClient(t, replay(t, recs[scenario], &hits))
		return cli.RouteCase(context.Background(), adapters.RouteCaseInput{TenantID: "ten_a", CaseID: id, OwnerQueue: queue,
			ExpectedVersionToken: token, Timeout: 3 * time.Second})
	}
	if got := run("route_200", "DISPUTES", "v1"); got.Class != adapters.ClassAck2xx || got.RawCode != "200" || got.Case == nil || got.Case.Status != "ROUTED" || got.Case.VersionToken != "v2" {
		t.Fatalf("route: %+v", got)
	}
	if got := run("route_stale_409", "DISPUTES", "v999"); got.Class != adapters.ClassVersionConflict || got.RawCode != "409:VERSION_MISMATCH" || got.CurrentVersionToken != "v1" || got.KeyCollision {
		t.Fatalf("stale route: %+v", got)
	}
	// a competing modification injected by a real FaultPlan is the same distinct error, never an acknowledgement
	if got := run("route_concurrent_modification", "FRAUD_REVIEW", "v2"); got.Class != adapters.ClassVersionConflict || got.CurrentVersionToken != "v3" {
		t.Fatalf("concurrent modification: %+v", got)
	}
}

// REQ-CTX-008 and PERD 34.4: a read returns a version token and an explicit completeness marker; absence is
// only "none" when the page is complete.
func TestReadsRecorded(t *testing.T) {
	recs := loadRecordings(t)
	id := caseID(recs)
	var hits int32

	cli := newClient(t, replay(t, recs["get_200"], &hits))
	c, err := cli.GetCase(deadline(t), "ten_a", id)
	if err != nil || c.CaseID != id || c.VersionToken != "v1" || c.Status != "OPEN" || c.CreatedByIntent != "int_01J9X3" || c.Epoch != 0 {
		t.Fatalf("get: %+v %v", c, err)
	}

	cli = newClient(t, replay(t, recs["get_404"], &hits))
	_, err = cli.GetCase(deadline(t), "ten_a", "cas_nope")
	if re, ok := err.(*adapters.ReadError); !ok || re.Failure != adapters.ReadNotFound {
		t.Fatalf("404 must be NOT_FOUND, got %v", err)
	}

	cli = newClient(t, replay(t, recs["find_by_key_one"], &hits))
	p, err := cli.FindCases(deadline(t), adapters.CaseQuery{TenantID: "ten_a", IdempotencyKey: "idk_v1_live01"})
	if err != nil || !p.Complete || len(p.Items) != 1 || p.Items[0].IdempotencyKey != "idk_v1_live01" {
		t.Fatalf("find by key: %+v %v", p, err)
	}

	cli = newClient(t, replay(t, recs["find_by_key_none"], &hits))
	absent, _ := url.ParseQuery(recs["find_by_key_none"].Request.Query)
	p, err = cli.FindCases(deadline(t), adapters.CaseQuery{TenantID: "ten_a", IdempotencyKey: absent.Get("idempotency_key")})
	if err != nil || !p.Complete || len(p.Items) != 0 {
		t.Fatalf("an empty complete page is the only 'none': %+v %v", p, err)
	}

	cli = newClient(t, replay(t, recs["find_paginated"], &hits))
	p, err = cli.FindCases(deadline(t), adapters.CaseQuery{TenantID: "ten_a", PrincipalID: "usr_1"})
	if err != nil || p.Complete || p.NextCursor == "" {
		t.Fatalf("a paginated answer must be incomplete with a cursor, never read as absence: %+v %v", p, err)
	}
}

// AGENT-09, EC-X02: text injected into an answer is data. The adapter returns it as a field value and
// never interprets it.
func TestInjectedTextIsReturnedAsData(t *testing.T) {
	recs := loadRecordings(t)
	var hits int32
	cli := newClient(t, replay(t, recs["get_injection_payload"], &hits))
	c, err := cli.GetCase(deadline(t), "ten_a", caseID(recs))
	if err != nil || !strings.Contains(c.ReasonCode, "SYNTHETIC INJECTION TEST") || c.Status != "ROUTED" {
		t.Fatalf("%+v %v", c, err)
	}
}

// INV-07: every transport failure on a read is UNAVAILABLE, never NOT_FOUND.
func TestReadTransportFailureIsUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _, _ := w.(http.Hijacker).Hijack()
		conn.Close()
	}))
	t.Cleanup(srv.Close)
	_, err := newClient(t, srv).GetCase(deadline(t), "ten_a", "case_x")
	if re, ok := err.(*adapters.ReadError); !ok || re.Failure != adapters.ReadUnavailable {
		t.Fatalf("got %v", err)
	}
}

// Schema drift is UNKNOWN, never a guess (EVD-05): an unknown field, a missing token, an unknown status or an
// incoherent page is malformed on a read and an unrecognised, unknown-outcome answer on a write.
func TestSchemaDriftIsUnknown(t *testing.T) {
	serve := func(status int, body string) *CaseClient {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		}))
		t.Cleanup(srv.Close)
		return newClient(t, srv)
	}
	good := `"case_id":"c1","reference":"CMP-1","status":"OPEN","version_token":"v1"`
	reads := []string{
		`{` + good + `,"surprise":1}`,
		`{"case_id":"c1","reference":"CMP-1","status":"OPEN"}`,
		`{"case_id":"c1","reference":"CMP-1","status":"CLOSED","version_token":"v1"}`,
		`not json`,
	}
	for _, b := range reads {
		_, err := serve(200, b).GetCase(deadline(t), "ten_a", "c1")
		if re, ok := err.(*adapters.ReadError); !ok || re.Failure != adapters.ReadMalformed {
			t.Errorf("read %q: got %v", b, err)
		}
	}
	for _, b := range []string{`{"items":[],"complete":false}`, `{"items":[],"complete":true,"next_cursor":"x"}`, `{"items":[]}`} {
		_, err := serve(200, b).FindCases(deadline(t), adapters.CaseQuery{TenantID: "ten_a", IdempotencyKey: "k"})
		if re, ok := err.(*adapters.ReadError); !ok || re.Failure != adapters.ReadMalformed {
			t.Errorf("page %q: got %v", b, err)
		}
	}
	for _, b := range []string{`{` + good + `,"surprise":1}`, `{}`, `garbage`} {
		got := serve(201, b).CreateCase(context.Background(), createIn("k"))
		if got.Class != adapters.ClassNoResponse || got.RawCode != "201:UNRECOGNISED" || got.ExternalAck != "" {
			t.Errorf("write %q: %+v", b, got)
		}
	}
}

// A write the adapter cannot build is its own defect and sends nothing; a read without a deadline sends
// nothing either (a context deadline on every call).
func TestBadInputSendsNothing(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { atomic.AddInt32(&hits, 1) }))
	t.Cleanup(srv.Close)
	cli := newClient(t, srv)
	bad := createIn("")
	if r := cli.CreateCase(context.Background(), bad); r.Class != adapters.ClassDefect {
		t.Fatalf("no key: %+v", r)
	}
	bad = createIn("k")
	bad.Timeout = 0
	if r := cli.CreateCase(context.Background(), bad); r.Class != adapters.ClassDefect {
		t.Fatalf("no timeout: %+v", r)
	}
	if r := cli.RouteCase(context.Background(), adapters.RouteCaseInput{TenantID: "t", CaseID: "c", OwnerQueue: "q", Timeout: time.Second}); r.Class != adapters.ClassDefect {
		t.Fatalf("route with no expected version: %+v", r)
	}
	if _, err := cli.GetCase(context.Background(), "ten_a", "c1"); err == nil {
		t.Fatal("a read with no deadline must be refused")
	}
	for _, q := range []adapters.CaseQuery{
		{TenantID: "ten_a"}, // no scope: there is no global search
		{TenantID: "ten_a", IdempotencyKey: "k", PrincipalID: "p"},
		{IdempotencyKey: "k"},
	} {
		if _, err := cli.FindCases(deadline(t), q); err == nil {
			t.Fatalf("query %+v must be refused", q)
		}
	}
	if n := atomic.LoadInt32(&hits); n != 0 {
		t.Fatalf("%d requests were sent for input the adapter refused", n)
	}
}

// The credential is file-mounted and never leaks (AGENTS 10): not in an error, not to another host.
func TestCredentialHandling(t *testing.T) {
	if _, err := NewCaseClient("http://127.0.0.1:1", filepath.Join(t.TempDir(), "missing"), nil); err == nil || strings.Contains(err.Error(), testToken) {
		t.Fatalf("missing file: %v", err)
	}
	empty := filepath.Join(t.TempDir(), "empty")
	_ = os.WriteFile(empty, []byte("  \n"), 0o600)
	if _, err := NewCaseClient("http://127.0.0.1:1", empty, nil); err == nil {
		t.Fatal("an empty credential file must be refused")
	}
	for _, u := range []string{"", "ftp://host", "http://host/v1", "http://host?x=1", "host:8090"} {
		if _, err := NewCaseClient(u, tokenFile(t), nil); err == nil {
			t.Errorf("base URL %q must be refused", u)
		}
	}
	// a redirect must not carry the bearer token to another host
	var elsewhere int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { atomic.AddInt32(&elsewhere, 1) }))
	t.Cleanup(other.Close)
	redir := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(redir.Close)
	got := newClient(t, redir).CreateCase(context.Background(), createIn("k"))
	if got.Class != adapters.ClassNoResponse || atomic.LoadInt32(&elsewhere) != 0 {
		t.Fatalf("redirect: %+v, other host hit %d times", got, elsewhere)
	}
}

// REQ-ENV-002, INV-22: this package has no code for the truth or control listeners and holds no bench
// credential. The scan covers the package's own source, not this test file.
func TestNoTruthOrControlPath(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		raw, _ := os.ReadFile(f)
		s := strings.ToLower(string(raw))
		for _, banned := range []string{"/truth/", "/control/", "bench_token", ":8091"} {
			if strings.Contains(s, banned) {
				t.Errorf("%s contains %q: the runtime adapter must not reach the bench side", f, banned)
			}
		}
	}
}
