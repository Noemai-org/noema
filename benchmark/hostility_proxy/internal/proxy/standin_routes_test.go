package proxy_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Noemai-org/noema/benchmark/hostility_proxy/internal/numbers"
	"github.com/Noemai-org/noema/benchmark/hostility_proxy/internal/proxy"
	"github.com/Noemai-org/noema/benchmark/hostility_proxy/internal/routes"
)

// ENV-05 / REQ-ENV-005 / DEC-2026-11-06 A10: the hostility proxy in front of the Noema-side stand-in (synthetic/standin). The
// route table is config/routes.standin.json. The stand-in is a read store with six read routes; the proxy forwards exactly
// those, passes the caller's credentials through untouched (it holds none), and applies its own faults to them.

const standinPrincipalAssertion = "v2.dXNyX2E.dGVuX2FscGhh.1790847660.36f86867759aea52907e4c76ed41bd1608534527a1f3ecbe5d7f8f57c8d9ed42" // the README's worked vector, SYNTHETIC

// fakeStandin answers the stand-in's six read routes and records the headers it received.
type fakeStandin struct {
	srv  *httptest.Server
	mu   sync.Mutex
	seen http.Header
	hits int
}

func newFakeStandin(t testing.TB) *fakeStandin {
	f := &fakeStandin{}
	note := func(r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.seen = r.Header.Clone()
		f.hits++
	}
	mux := http.NewServeMux()
	for _, p := range []string{"/standin/v1/principal", "/standin/v1/beneficiaries"} {
		mux.HandleFunc("GET "+p, func(w http.ResponseWriter, r *http.Request) {
			note(r)
			writeJSON(w, map[string]any{"data_source": "stand-in", "synthetic": true})
		})
	}
	mux.HandleFunc("GET /standin/v1/transactions", func(w http.ResponseWriter, r *http.Request) {
		note(r)
		var items []any
		for i := 1; i <= 5; i++ {
			items = append(items, map[string]any{"transaction_ref": "tx:" + string(rune('0'+i))})
		}
		writeJSON(w, map[string]any{"data_source": "stand-in", "synthetic": true, "items": items, "complete": true})
	})
	for _, leaf := range []string{"ledger-entries", "switch-status", "destination-status"} {
		mux.HandleFunc("GET /standin/v1/transactions/{ref}/"+leaf, func(w http.ResponseWriter, r *http.Request) {
			note(r)
			writeJSON(w, map[string]any{"data_source": "stand-in", "synthetic": true, "transaction_ref": r.PathValue("ref"), "leaf": leaf})
		})
	}
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func newStandinRig(t testing.TB) (*rig, *fakeStandin) {
	t.Helper()
	back := newFakeStandin(t)
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
	up, _ := url.Parse(back.srv.URL)
	srv, err := proxy.New(proxy.Options{Routes: table, Upstreams: map[string]*url.URL{"standin": up}, Fixtures: fixtures, Numbers: mapper,
		BenchToken: benchToken, Log: slog.New(slog.NewJSONHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	r := &rig{t: t, srv: srv, client: &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{DisableKeepAlives: true},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	r.data = httptest.NewServer(srv.DataHandler())
	r.control = httptest.NewServer(srv.ControlHandler())
	t.Cleanup(r.data.Close)
	t.Cleanup(r.control.Close)
	return r, back
}

func (r *rig) getWith(path string, h map[string]string) response {
	r.t.Helper()
	req, err := http.NewRequest("GET", r.data.URL+path, nil)
	if err != nil {
		r.t.Fatal(err)
	}
	for k, v := range h {
		req.Header.Set(k, v)
	}
	res, err := r.client.Do(req)
	if err != nil {
		r.t.Fatalf("GET %s: %v", path, err)
	}
	defer func() { _ = res.Body.Close() }()
	b, _ := io.ReadAll(res.Body)
	return response{res.StatusCode, res.Header, string(b)}
}

func TestEveryStandinReadRouteIsForwardedWithTheCallersCredentialsUntouched(t *testing.T) {
	r, back := newStandinRig(t)
	h := map[string]string{"Authorization": "Bearer runtime-token-runtime-token-runtime-tok", "X-Principal-Assertion": standinPrincipalAssertion}
	for _, p := range []string{"/standin/v1/principal", "/standin/v1/beneficiaries", "/standin/v1/transactions?account=acc_usr_a",
		"/standin/v1/transactions/tx:1/ledger-entries", "/standin/v1/transactions/tx:1/switch-status", "/standin/v1/transactions/tx:1/destination-status"} {
		res := r.getWith(p, h)
		if res.status != 200 || !strings.Contains(res.body, `"data_source":"stand-in"`) {
			t.Fatalf("%s: %d %s", p, res.status, res.body)
		}
		back.mu.Lock()
		gotAuth, gotAssertion := back.seen.Get("Authorization"), back.seen.Get("X-Principal-Assertion")
		back.mu.Unlock()
		if gotAuth != h["Authorization"] || gotAssertion != standinPrincipalAssertion {
			t.Fatalf("%s: the stand-in saw auth %q and assertion %q; the proxy must pass both through unchanged", p, gotAuth, gotAssertion)
		}
	}
}

func TestTheProxyForwardsNothingTheStandinDoesNotServe(t *testing.T) {
	r, back := newStandinRig(t)
	for _, c := range []struct{ method, path string }{{"POST", "/standin/v1/principal"}, {"PUT", "/control/v1/standin"}, {"GET", "/standin/v1/transactions/tx:1/ledger-entries/extra"},
		{"GET", "/v1/transactions"}} {
		req, _ := http.NewRequest(c.method, r.data.URL+c.path, strings.NewReader("{}"))
		res, err := r.client.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", c.method, c.path, err)
		}
		_ = res.Body.Close()
		if res.StatusCode == 200 {
			t.Fatalf("%s %s must not be forwarded", c.method, c.path)
		}
	}
	if back.hits != 0 {
		t.Fatalf("the stand-in received %d requests that no route allows", back.hits)
	}
}

func TestAServerErrorOnTheLedgerReadIsObservedAsOneAndOthersAreUntouched(t *testing.T) {
	r, _ := newStandinRig(t)
	r.load(fault("mock-ledger", "get_entries", "HTTP_5XX", `{"nth_call":1}`, `{"status":503}`), "")
	if res := r.getWith("/standin/v1/transactions/tx:1/ledger-entries", nil); res.status != 503 {
		t.Fatalf("the first ledger read: %d, want the injected 503", res.status)
	}
	if res := r.getWith("/standin/v1/transactions/tx:1/switch-status", nil); res.status != 200 {
		t.Fatalf("a switch read must be untouched: %d", res.status)
	}
	if res := r.getWith("/standin/v1/transactions/tx:1/ledger-entries", nil); res.status != 200 {
		t.Fatalf("only the first ledger read is faulted: %d", res.status)
	}
}

func TestPaginationOnTheStandinCandidateSearchIsCompleteOnlyAtTheEnd(t *testing.T) {
	r, _ := newStandinRig(t)
	r.load(fault("mock-ledger", "list_transactions", "PAGINATE", `{}`, `{"page_size":2}`), "")
	res := r.getWith("/standin/v1/transactions?account=acc_usr_a", nil)
	d := decode(t, res.body)
	if res.status != 200 || d["complete"] != false || d["next_cursor"] == nil || len(d["items"].([]any)) != 2 {
		t.Fatalf("the first page must be 2 items, incomplete, with a cursor: %d %s", res.status, res.body)
	}
}

// Drift guard: every read route in the stand-in README's route table has a route here, and no route here is missing there.
func TestTheStandinRouteTableMatchesTheStandinReadme(t *testing.T) {
	readme, err := os.ReadFile("../../../../synthetic/standin/README.md")
	if err != nil {
		t.Fatal(err)
	}
	rowRE := regexp.MustCompile("(?m)^\\| `GET (/standin/v1/[^`]+)` \\|")
	var documented []string
	for _, m := range rowRE.FindAllStringSubmatch(string(readme), -1) {
		documented = append(documented, strings.ReplaceAll(m[1], "{ref}", "{transaction_ref}"))
	}
	if len(documented) != 6 {
		t.Fatalf("the README documents %d read routes, want 6: %v", len(documented), documented)
	}
	raw, err := os.ReadFile("../../config/routes.standin.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range documented {
		if !strings.Contains(string(raw), `"path": "`+p+`"`) {
			t.Errorf("the README documents %s and the proxy table has no route for it", p)
		}
	}
	if n := strings.Count(string(raw), `"method":`); n != len(documented) {
		t.Errorf("the proxy table has %d routes, the README documents %d", n, len(documented))
	}
}
