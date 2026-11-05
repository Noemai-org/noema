package standin_test

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Noemai-org/noema/synthetic/standin/internal/standin"
)

// The Noema-side stand-in service for the US-01 and US-06 data (Option B for G1, Founder decision of 4 Nov 2026; contract:
// benchmark/fixtures/standin.py, noema#161). It is a read-store that the benchmark loads with `derive(fixture)` and that the core's adapters
// read, scoped to the calling principal. Evidence from a run on it is labelled "stand-in" and says nothing about Tushe. SYNTHETIC (AGENT-25).
// Requirements: REQ-ENV-001 (adapters find the PERD 34.4 operations), REQ-ENV-002 (benchmark credentials only on the control side),
// REQ-ENV-004 (same input, same bytes), REQ-CTX-006 (never a global search); INV-12 (identity from the session), INV-05/BD-08 (no write path).
// testdata/derive-golden.json was produced by derive() at noema#161 head 78248024 from the fixture in testdata/README.md.

const (
	benchToken   = "standin-bench-token-standin-bench-token-0001"
	runtimeToken = "standin-runtime-token-standin-runtime-token-0002"
	principalKey = "principal-principal-principal-principal-0003"
)

// fixedNow is the service's clock in the tests: 2026-10-01T09:40:00Z, SYNTHETIC.
var fixedNow = time.Unix(1790847600, 0)

// mint is the adapter's side of the identity contract, written here independently of the service (README, "Identity"):
// v1.<base64url(principal)>.<expiry unix seconds>.<hex MAC>, where the MAC is HMAC-SHA-256 keyed with the principal key over the four lines
// noema-standin-principal-v1, <aud>, <principal>, <expiry> (README, "Identity").
func mint(key, aud, principal string, exp int64) string {
	m := hmac.New(sha256.New, []byte(key))
	_, _ = fmt.Fprintf(m, "noema-standin-principal-v1\n%s\n%s\n%d", aud, principal, exp)
	return fmt.Sprintf("v1.%s.%d.%s", base64.RawURLEncoding.EncodeToString([]byte(principal)), exp, hex.EncodeToString(m.Sum(nil)))
}

func assertion(principal string) string {
	return mint(principalKey, "standin", principal, fixedNow.Unix()+60)
}

type rig struct {
	t       testing.TB
	data    *httptest.Server
	control *httptest.Server
	golden  []byte
}

func newRig(t testing.TB) *rig {
	t.Helper()
	golden, err := os.ReadFile("testdata/derive-golden.json")
	if err != nil {
		t.Fatal(err)
	}
	s, err := standin.New(standin.Options{BenchToken: benchToken, RuntimeToken: runtimeToken, PrincipalKey: principalKey, Now: func() time.Time { return fixedNow }})
	if err != nil {
		t.Fatal(err)
	}
	r := &rig{t: t, data: httptest.NewServer(s.DataHandler()), control: httptest.NewServer(s.ControlHandler()), golden: golden}
	t.Cleanup(r.data.Close)
	t.Cleanup(r.control.Close)
	return r
}

type reply struct {
	status int
	header http.Header
	body   string
}

func do(t testing.TB, method, url, token, principal string, body []byte) reply {
	t.Helper()
	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if principal != "" {
		req.Header.Set("X-Principal-Assertion", assertion(principal))
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	b, _ := io.ReadAll(res.Body)
	return reply{res.StatusCode, res.Header, string(b)}
}

func (r *rig) load(body []byte) reply {
	return do(r.t, "PUT", r.control.URL+"/control/v1/standin", benchToken, "", body)
}

func (r *rig) loadGolden() {
	r.t.Helper()
	if res := r.load(r.golden); res.status != 200 {
		r.t.Fatalf("golden load refused: %d %s", res.status, res.body)
	}
}

func (r *rig) read(principal, path string) reply {
	return do(r.t, "GET", r.data.URL+path, runtimeToken, principal, nil)
}

func decode(t testing.TB, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("not JSON: %v: %q", err, s)
	}
	return m
}

// edit returns the golden payload with one change made by fn.
func (r *rig) edit(fn func(m map[string]any)) []byte {
	var m map[string]any
	if err := json.Unmarshal(r.golden, &m); err != nil {
		r.t.Fatal(err)
	}
	fn(m)
	b, err := json.Marshal(m)
	if err != nil {
		r.t.Fatal(err)
	}
	return b
}

func TestControlNeedsTheBenchCredentialAndRefusesTheRuntimeOne(t *testing.T) { // REQ-ENV-002
	r := newRig(t)
	for name, token := range map[string]string{"none": "", "runtime": runtimeToken, "wrong": "nope"} {
		if res := do(t, "PUT", r.control.URL+"/control/v1/standin", token, "", r.golden); res.status != 401 {
			t.Fatalf("%s credential: want 401, got %d", name, res.status)
		}
	}
	if res := do(t, "GET", r.control.URL+"/control/v1/standin", runtimeToken, "", nil); res.status != 401 {
		t.Fatalf("the runtime credential must not read the control side, got %d", res.status)
	}
}

func TestDataNeedsTheRuntimeCredentialAndRefusesTheBenchOne(t *testing.T) { // REQ-ENV-002: the loader's key never reads, the reader's key never loads
	r := newRig(t)
	r.loadGolden()
	for name, token := range map[string]string{"none": "", "bench": benchToken, "wrong": "nope"} {
		if res := do(t, "GET", r.data.URL+"/standin/v1/principal", token, "usr_a", nil); res.status != 401 {
			t.Fatalf("%s credential: want 401, got %d", name, res.status)
		}
	}
}

func TestReadsBeforeALoadAreUnavailableNotEmpty(t *testing.T) { // AGENT-20: unknown stays unknown
	r := newRig(t)
	res := r.read("usr_a", "/standin/v1/transactions")
	if res.status != 503 || !strings.Contains(res.body, "STANDIN_NOT_LOADED") {
		t.Fatalf("want 503 STANDIN_NOT_LOADED, got %d %s", res.status, res.body)
	}
}

func TestLoadRefusesWhatIsNotAStandIn(t *testing.T) {
	r := newRig(t)
	cases := map[string]func(m map[string]any){
		"not labelled stand-in":      func(m map[string]any) { m["data_source"] = "tushe" },
		"not marked synthetic":       func(m map[string]any) { m["synthetic"] = false },
		"unknown top-level field":    func(m map[string]any) { m["extra"] = 1 },
		"no fixture id":              func(m map[string]any) { m["fixture_id"] = "" },
		"candidate owner not listed": func(m map[string]any) { m["candidates"].(map[string]any)["usr_zzz"] = []any{} },
		"a ledger entry for no transaction": func(m map[string]any) {
			m["ledger_entries"].(map[string]any)["fin.transaction:ORPHAN"] = map[string]any{"unavailable": false, "entries": []any{}}
		},
		"a transaction with no switch record": func(m map[string]any) { delete(m["switch_records"].(map[string]any), "fin.transaction:TX1") },
		"a transaction listed for two owners": func(m map[string]any) {
			c := m["candidates"].(map[string]any)
			c["usr_b"] = append(c["usr_b"].([]any), c["usr_a"].([]any)[0])
		},
	}
	for name, fn := range cases {
		if res := r.load(r.edit(fn)); res.status != 400 {
			t.Fatalf("%s: want 400, got %d %s", name, res.status, res.body)
		}
	}
	if res := r.load([]byte("not json")); res.status != 400 {
		t.Fatalf("not JSON: want 400, got %d", res.status)
	}
	if res := r.read("usr_a", "/standin/v1/principal"); res.status != 503 {
		t.Fatalf("a refused load must leave nothing loaded, got %d", res.status)
	}
}

func TestALoadTooLargeIsRefused(t *testing.T) {
	r := newRig(t)
	if res := r.load(bytes.Repeat([]byte(" "), 5<<20)); res.status != 413 {
		t.Fatalf("want 413, got %d", res.status)
	}
}

func TestCandidatesAreScopedToThePrincipalWithACompletenessMarker(t *testing.T) { // REQ-CTX-006, INV-12: never a global search
	r := newRig(t)
	r.loadGolden()
	refs := func(principal string) (string, bool) {
		res := r.read(principal, "/standin/v1/transactions")
		if res.status != 200 {
			t.Fatalf("%s: %d %s", principal, res.status, res.body)
		}
		doc := decode(t, res.body)
		var out []string
		for _, it := range doc["items"].([]any) {
			out = append(out, it.(map[string]any)["transaction_ref"].(string))
		}
		return strings.Join(out, ","), doc["complete"].(bool)
	}
	a, complete := refs("usr_a")
	if a != "fin.transaction:TX2,fin.transaction:TX1,fin.transaction:TX3" || !complete { // newest first
		t.Fatalf("usr_a sees %q complete=%t", a, complete)
	}
	if b, _ := refs("usr_b"); b != "fin.transaction:TX9" {
		t.Fatalf("usr_b sees %q", b)
	}
}

func TestAnotherPrincipalsTransactionLooksLikeOneThatDoesNotExist(t *testing.T) { // T8: not-owned looks like not-found
	r := newRig(t)
	r.loadGolden()
	for _, kind := range []string{"ledger-entries", "switch-status", "destination-status"} {
		foreign := r.read("usr_a", "/standin/v1/transactions/fin.transaction:TX9/"+kind)
		missing := r.read("usr_a", "/standin/v1/transactions/fin.transaction:NOPE/"+kind)
		if foreign.status != 404 || foreign.body != missing.body || missing.status != 404 {
			t.Fatalf("%s: foreign %d %q, missing %d %q", kind, foreign.status, foreign.body, missing.status, missing.body)
		}
	}
	if res := r.read("usr_a", "/standin/v1/transactions?account=acc_usr_b"); res.status != 404 {
		t.Fatalf("another principal's account must look absent, got %d", res.status)
	}
}

func TestAMissingOrUnknownPrincipalIsRefused(t *testing.T) { // INV-12
	r := newRig(t)
	r.loadGolden()
	for _, who := range []string{"", "usr_nobody"} {
		if res := r.read(who, "/standin/v1/transactions"); res.status != 401 {
			t.Fatalf("principal %q: want 401, got %d", who, res.status)
		}
	}
}

func TestFiltersNarrowTheCandidatesAndBadValuesAreRefused(t *testing.T) {
	r := newRig(t)
	r.loadGolden()
	count := func(q string) int {
		res := r.read("usr_a", "/standin/v1/transactions?"+q)
		if res.status != 200 {
			t.Fatalf("%s: %d %s", q, res.status, res.body)
		}
		return len(decode(t, res.body)["items"].([]any))
	}
	for q, want := range map[string]int{
		"account=acc_usr_a": 3, "amount_min=300000": 1, "amount_max=250000": 2, "amount_min=250000&amount_max=250000": 2,
		"from=2026-10-01T09:00:00%2B01:00": 1, "to=2026-10-01T08:00:00%2B01:00": 2, "counterparty=ben_0001": 3, "counterparty=ben_9999": 0,
	} {
		if got := count(q); got != want {
			t.Fatalf("%s: want %d, got %d", q, want, got)
		}
	}
	for _, q := range []string{"amount_min=2500.50", "amount_min=-1", "amount_max=abc", "from=yesterday", "to=2026-13-01"} {
		if res := r.read("usr_a", "/standin/v1/transactions?"+q); res.status != 400 {
			t.Fatalf("%s: want 400, got %d", q, res.status)
		}
	}
}

func TestThePrincipalAndBeneficiariesAreTheirOwn(t *testing.T) {
	r := newRig(t)
	r.loadGolden()
	p := decode(t, r.read("usr_a", "/standin/v1/principal").body)
	if p["principal_id"] != "usr_a" || p["display_name"] != "A" || len(p["accounts"].([]any)) != 1 || len(p["beneficiaries"].([]any)) != 1 {
		t.Fatalf("principal wrong: %v", p)
	}
	if b := decode(t, r.read("usr_b", "/standin/v1/beneficiaries").body); len(b["beneficiaries"].([]any)) != 0 {
		t.Fatalf("usr_b owns no beneficiary: %v", b)
	}
}

func TestLedgerSwitchAndDestinationAnswerAsDerived(t *testing.T) {
	r := newRig(t)
	r.loadGolden()
	ledger := decode(t, r.read("usr_a", "/standin/v1/transactions/fin.transaction:TX1/ledger-entries").body)
	entries := ledger["entries"].([]any)
	if len(entries) != 1 || entries[0].(map[string]any)["entry_type"] != "DEBIT" || entries[0].(map[string]any)["posting_status"] != "POSTED" || ledger["unavailable"] != false {
		t.Fatalf("TX1 ledger wrong: %v", ledger)
	}
	sw := decode(t, r.read("usr_a", "/standin/v1/transactions/fin.transaction:TX2/switch-status").body)
	if sw["semantic_status"] != "TIMED_OUT" || sw["is_terminal"] != false || sw["raw_code_label"] != "SYNTHETIC" { // a non-terminal answer is never read as credited
		t.Fatalf("TX2 switch wrong: %v", sw)
	}
	dest := decode(t, r.read("usr_a", "/standin/v1/transactions/fin.transaction:TX2/destination-status").body)
	if dest["credit_state"] != "PENDING" || dest["settlement_state"] != "PENDING" {
		t.Fatalf("TX2 destination wrong: %v", dest)
	}
	down := r.read("usr_a", "/standin/v1/transactions/fin.transaction:TX3/ledger-entries")
	d := decode(t, down.body)
	if down.status != 200 || d["unavailable"] != true || len(d["entries"].([]any)) != 0 { // an unavailable answer, not an empty one
		t.Fatalf("TX3 ledger must be an unavailable answer: %d %s", down.status, down.body)
	}
}

func TestEveryAnswerIsLabelledStandIn(t *testing.T) { // AGENT-25: evidence from a stand-in run is never mistaken for Tushe's
	r := newRig(t)
	r.loadGolden()
	for _, path := range []string{"/standin/v1/principal", "/standin/v1/beneficiaries", "/standin/v1/transactions",
		"/standin/v1/transactions/fin.transaction:TX1/ledger-entries", "/standin/v1/transactions/fin.transaction:TX1/switch-status", "/standin/v1/transactions/fin.transaction:TX1/destination-status"} {
		res := r.read("usr_a", path)
		doc := decode(t, res.body)
		if res.status != 200 || res.header.Get("X-Data-Source") != "stand-in" || doc["data_source"] != "stand-in" || doc["synthetic"] != true {
			t.Fatalf("%s is not labelled: %d %v %s", path, res.status, res.header, res.body)
		}
	}
	if res := r.read("usr_a", "/standin/v1/transactions/fin.transaction:NOPE/ledger-entries"); res.header.Get("X-Data-Source") != "stand-in" {
		t.Fatal("even a refusal says where it came from")
	}
}

func TestTheSameLoadGivesTheSameBytesAndDigest(t *testing.T) { // REQ-ENV-004
	paths := []string{"/standin/v1/principal", "/standin/v1/transactions", "/standin/v1/transactions/fin.transaction:TX2/switch-status"}
	snapshot := func(r *rig) (string, string) {
		var all strings.Builder
		for _, p := range paths {
			all.WriteString(r.read("usr_a", p).body)
		}
		return all.String(), decode(t, do(t, "GET", r.control.URL+"/control/v1/standin", benchToken, "", nil).body)["digest"].(string)
	}
	a, b := newRig(t), newRig(t)
	a.loadGolden()
	b.loadGolden()
	b.loadGolden() // loading twice changes nothing
	bytesA, digestA := snapshot(a)
	bytesB, digestB := snapshot(b)
	if bytesA != bytesB || digestA != digestB || !strings.HasPrefix(digestA, "sha256:") {
		t.Fatalf("not identical: %q vs %q", digestA, digestB)
	}
	other := a.edit(func(m map[string]any) { m["fixture_id"] = "fx_other" })
	if res := a.load(other); res.status != 200 {
		t.Fatalf("other load refused: %d %s", res.status, res.body)
	}
	if _, d := snapshot(a); d == digestA {
		t.Fatal("a different fixture must change the digest")
	}
}

func TestNoRouteOnTheDataSideChangesAnything(t *testing.T) { // INV-05, BD-08: no write, no money movement
	r := newRig(t)
	r.loadGolden()
	before := r.read("usr_a", "/standin/v1/transactions").body
	for _, m := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		for _, p := range []string{"/standin/v1/transactions", "/standin/v1/transactions/fin.transaction:TX1/ledger-entries", "/standin/v1/payouts", "/control/v1/standin"} {
			if res := do(t, m, r.data.URL+p, runtimeToken, "usr_a", []byte("{}")); res.status != 405 && res.status != 404 {
				t.Fatalf("%s %s: want 405 or 404, got %d", m, p, res.status)
			}
		}
	}
	if after := r.read("usr_a", "/standin/v1/transactions").body; after != before {
		t.Fatal("a refused write changed what is served")
	}
}

func TestDeletingTheLoadLeavesNothingServed(t *testing.T) {
	r := newRig(t)
	r.loadGolden()
	if res := do(t, "DELETE", r.control.URL+"/control/v1/standin", benchToken, "", nil); res.status != 200 {
		t.Fatalf("delete: %d", res.status)
	}
	if res := r.read("usr_a", "/standin/v1/principal"); res.status != 503 {
		t.Fatalf("want 503 after delete, got %d", res.status)
	}
	if st := decode(t, do(t, "GET", r.control.URL+"/control/v1/standin", benchToken, "", nil).body); st["loaded"] != false {
		t.Fatalf("status must say not loaded: %v", st)
	}
}

func TestOptionsNeedBothCredentialsAndKeepThemApart(t *testing.T) { // REQ-ENV-002
	for _, o := range []standin.Options{{BenchToken: benchToken}, {RuntimeToken: runtimeToken}, {BenchToken: "short", RuntimeToken: runtimeToken}, {BenchToken: benchToken, RuntimeToken: benchToken},
		{BenchToken: benchToken, RuntimeToken: runtimeToken}, {BenchToken: benchToken, RuntimeToken: runtimeToken, PrincipalKey: "short"},
		{BenchToken: benchToken, RuntimeToken: runtimeToken, PrincipalKey: runtimeToken}, {BenchToken: benchToken, RuntimeToken: runtimeToken, PrincipalKey: benchToken}} {
		if _, err := standin.New(o); err == nil {
			t.Fatalf("options %+v must be refused", o)
		}
	}
}

func (r *rig) withAssertion(a, path string, extra map[string]string) reply {
	r.t.Helper()
	req, err := http.NewRequest("GET", r.data.URL+path, nil)
	if err != nil {
		r.t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+runtimeToken)
	if a != "" {
		req.Header.Set("X-Principal-Assertion", a)
	}
	for k, v := range extra {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		r.t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	b, _ := io.ReadAll(res.Body)
	return reply{res.StatusCode, res.Header, string(b)}
}

func TestACallerCannotChangeThePrincipalWhileKeepingTheSameCredential(t *testing.T) { // INV-12: identity is bound, not asserted
	r := newRig(t)
	r.loadGolden()
	const path = "/standin/v1/transactions"
	good := assertion("usr_a")
	if res := r.withAssertion(good, path, nil); res.status != 200 {
		t.Fatalf("a valid assertion must be served, got %d %s", res.status, res.body)
	}
	parts := strings.Split(good, ".")
	swapped := strings.Join([]string{parts[0], base64.RawURLEncoding.EncodeToString([]byte("usr_b")), parts[2], parts[3]}, ".")
	for name, a := range map[string]string{
		"principal swapped, MAC kept": swapped,
		"expiry extended, MAC kept":   strings.Join([]string{parts[0], parts[1], "1790999999", parts[3]}, "."),
		"no assertion":                "",
		"malformed":                   "usr_a",
		"wrong version":               strings.Replace(good, "v1.", "v2.", 1),
		"MAC not hex":                 strings.Join([]string{parts[0], parts[1], parts[2], "zz"}, "."),
		"wrong key":                   mint("another-key-another-key-another-key-0009", "standin", "usr_a", fixedNow.Unix()+60),
		"wrong audience":              mint(principalKey, "mock-case", "usr_a", fixedNow.Unix()+60),
		"expired":                     mint(principalKey, "standin", "usr_a", fixedNow.Unix()-1),
		"lifetime too long":           mint(principalKey, "standin", "usr_a", fixedNow.Unix()+3600),
		"valid but not in the load":   mint(principalKey, "standin", "usr_nobody", fixedNow.Unix()+60),
	} {
		if res := r.withAssertion(a, path, nil); res.status != 401 {
			t.Fatalf("%s: want 401, got %d %s", name, res.status, res.body)
		}
	}
	// the old way no longer identifies anyone, and never overrides a valid assertion
	if res := r.withAssertion("", path, map[string]string{"X-Principal": "usr_a"}); res.status != 401 {
		t.Fatalf("a bare X-Principal must not identify the caller, got %d", res.status)
	}
	res := r.withAssertion(assertion("usr_b"), path, map[string]string{"X-Principal": "usr_a"})
	if res.status != 200 || strings.Contains(res.body, "TX1") || !strings.Contains(res.body, "TX9") {
		t.Fatalf("the assertion, not the header, must decide who is served: %d %s", res.status, res.body)
	}
}

func TestTheAssertionContractMatchesThePinnedVector(t *testing.T) { // the adapter implements this from the README; a second implementation (Python hmac) produced the vector
	const want = "v1.dXNyX2E.1790847660.aef356c16b785df13d4e4f17c28430155dc3de688179577663024029a99230ce"
	if got := mint(principalKey, "standin", "usr_a", 1790847660); got != want {
		t.Fatalf("test minter drifted from the vector: %s", got)
	}
	r := newRig(t)
	r.loadGolden()
	if res := r.withAssertion(want, "/standin/v1/principal", nil); res.status != 200 {
		t.Fatalf("the service must accept the pinned vector at the fixed clock, got %d %s", res.status, res.body)
	}
}

func TestAnAssertionIsRefusedAtTheInstantItExpires(t *testing.T) { // INV-12: an expiry is exclusive, so the five-minute window is not stretched by a second
	r := newRig(t)
	r.loadGolden()
	const path = "/standin/v1/principal"
	now := fixedNow.Unix()
	for name, c := range map[string]struct {
		exp  int64
		want int
	}{
		"one second left":        {now + 1, 200},
		"expires at this second": {now, 401},
		"expired a second ago":   {now - 1, 401},
		"exactly five minutes":   {now + 300, 200},
		"five minutes and one":   {now + 301, 401},
	} {
		if res := r.withAssertion(mint(principalKey, "standin", "usr_a", c.exp), path, nil); res.status != c.want {
			t.Fatalf("%s: want %d, got %d", name, c.want, res.status)
		}
	}
}
