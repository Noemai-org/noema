package synthetic_tushe

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/adapters"
)

// The client of the Noema-side stand-in (synthetic/standin), as the Evidence Layer needs it (noema#174 comment): read-only, a
// deadline on every call, *adapters.ReadError with a Failure and a RawCode the layer can tell apart, the SHA-256 of the wire
// bytes, the stand-in's label passed through, and an identity minted from the authenticated session only (INV-12).
// SYNTHETIC. Field names are the stand-in's reading of PERD 34.4, not Tushe's.

const (
	testRuntimeToken = "runtime-token-runtime-token-runtime-token-0001"
	testPrincipalKey = "principal-principal-principal-principal-0003"
	pinnedVector     = "v1.dXNyX2E.1790847660.aef356c16b785df13d4e4f17c28430155dc3de688179577663024029a99230ce" // synthetic/standin/README.md
)

var testNow = time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC) // SYNTHETIC

func TestTheAssertionMatchesThePinnedVectorOfTheStandinReadme(t *testing.T) {
	if got := standinAssertion([]byte(testPrincipalKey), "usr_a", 1790847660); got != pinnedVector {
		t.Fatalf("assertion = %s, want %s", got, pinnedVector)
	}
}

type fakeStandin struct {
	srv      *httptest.Server
	mu       sync.Mutex
	handlers map[string]func(w http.ResponseWriter, r *http.Request)
	seen     []seenRequest
}

type seenRequest struct {
	method, path, query, auth, assertion string
}

func newFakeStandin(t *testing.T) *fakeStandin {
	f := &fakeStandin{handlers: map[string]func(http.ResponseWriter, *http.Request){}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.seen = append(f.seen, seenRequest{r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Get("Authorization"), r.Header.Get("X-Principal-Assertion")})
		h := f.handlers[r.URL.Path]
		f.mu.Unlock()
		if h == nil {
			http.NotFound(w, r)
			return
		}
		h(w, r)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeStandin) on(path string, h func(http.ResponseWriter, *http.Request)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.handlers[path] = h
}

func (f *fakeStandin) body(path string, status int, body string) {
	f.on(path, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
}

func newStandinClient(t *testing.T, f *fakeStandin) *StandinClient {
	t.Helper()
	dir := t.TempDir()
	tok, key := filepath.Join(dir, "token"), filepath.Join(dir, "key")
	if err := os.WriteFile(tok, []byte(testRuntimeToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(key, []byte(testPrincipalKey), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := NewStandinClient(StandinOptions{BaseURL: f.srv.URL, TokenFile: tok, PrincipalKeyFile: key, Now: func() time.Time { return testNow }})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func sdl(t *testing.T, d time.Duration) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	t.Cleanup(cancel)
	return ctx
}

var sScope = adapters.Scope{TenantID: "ten_aa", PrincipalID: "usr_a"}

const (
	ledgerBody = `{"data_source":"stand-in","synthetic":true,"unavailable":false,"entries":[{"entry_id":"le_1_1","account_ref":"acc_usr_a","transaction_ref":"tx:1","entry_type":"DEBIT","amount_minor":500000,"currency":"NGN","posting_status":"POSTED","posted_at":"2026-10-01T09:30:00+01:00","version":1}]}` + "\n"
	switchBody = `{"data_source":"stand-in","synthetic":true,"unavailable":false,"switch_ref":"sw_1","transaction_ref":"tx:1","raw_code":"SYN-91","raw_code_label":"SYNTHETIC","semantic_status":"TIMED_OUT","is_terminal":false,"destination_ack":false,"received_at":"2026-10-01T09:30:00+01:00","raw_payload_hash":"sha256:` + zeros + `"}`
	destBody   = `{"data_source":"stand-in","synthetic":true,"unavailable":false,"transaction_ref":"tx:1","credit_state":"PENDING","settlement_state":"PENDING","effective_at":"2026-10-01T09:30:00+01:00","observed_at":"2026-10-01T09:40:00+01:00"}`
	candBody   = `{"data_source":"stand-in","synthetic":true,"complete":true,"items":[{"transaction_ref":"tx:1","account_ref":"acc_usr_a","direction":"OUTGOING","amount_minor":500000,"currency":"NGN","counterparty":"ben_1","occurred_at":"2026-10-01T09:30:00+01:00"}]}`
	zeros      = "0000000000000000000000000000000000000000000000000000000000000000"
)

func sha(b string) string { s := sha256.Sum256([]byte(b)); return "sha256:" + hex.EncodeToString(s[:]) }

func TestEachReadReturnsTheDecodedAnswerWithTheHashOfTheWireBytesAndTheLabel(t *testing.T) {
	f := newFakeStandin(t)
	f.body("/standin/v1/transactions/tx:1/ledger-entries", 200, ledgerBody)
	f.body("/standin/v1/transactions/tx:1/switch-status", 200, switchBody)
	f.body("/standin/v1/transactions/tx:1/destination-status", 200, destBody)
	f.body("/standin/v1/transactions", 200, candBody)
	c := newStandinClient(t, f)
	ctx := sdl(t, 5*time.Second)

	l, err := c.LedgerEntries(ctx, sScope, "tx:1")
	if err != nil || len(l.Entries) != 1 || l.Entries[0].AmountMinor != 500000 || l.Unavailable {
		t.Fatalf("ledger: %+v %v", l, err)
	}
	if l.Provenance != (adapters.Provenance{RawHash: sha(ledgerBody), DataSource: "stand-in", Synthetic: true}) {
		t.Fatalf("ledger provenance: %+v", l.Provenance)
	}
	s, err := c.SwitchStatus(ctx, sScope, "tx:1")
	if err != nil || s.Record.SemanticStatus != adapters.SwitchTimedOut || s.Record.IsTerminal || s.Provenance.RawHash != sha(switchBody) {
		t.Fatalf("switch: %+v %v", s, err)
	}
	d, err := c.DestinationStatus(ctx, sScope, "tx:1")
	if err != nil || d.Status.CreditState != adapters.CreditPending || d.Provenance.RawHash != sha(destBody) {
		t.Fatalf("destination: %+v %v", d, err)
	}
	p, err := c.FindCandidates(ctx, sScope, adapters.TransactionQuery{AccountRef: "acc_usr_a", Limit: 10})
	if err != nil || len(p.Items) != 1 || !p.Complete || p.Provenance.RawHash != sha(candBody) || p.Provenance.DataSource != "stand-in" {
		t.Fatalf("candidates: %+v %v", p, err)
	}
}

func TestAnUnavailableBodyIsUnavailableNeverEmpty(t *testing.T) {
	f := newFakeStandin(t)
	f.body("/standin/v1/transactions/tx:1/ledger-entries", 200, `{"data_source":"stand-in","synthetic":true,"unavailable":true,"entries":[]}`)
	f.body("/standin/v1/transactions/tx:1/switch-status", 200, `{"data_source":"stand-in","synthetic":true,"unavailable":true}`)
	f.body("/standin/v1/transactions/tx:1/destination-status", 200, `{"data_source":"stand-in","synthetic":true,"unavailable":true,"transaction_ref":"tx:1","credit_state":"UNAVAILABLE","settlement_state":"PENDING","effective_at":"2026-10-01T09:30:00+01:00","observed_at":"2026-10-01T09:40:00+01:00"}`)
	c := newStandinClient(t, f)
	ctx := sdl(t, 5*time.Second)
	if l, err := c.LedgerEntries(ctx, sScope, "tx:1"); err != nil || !l.IsUnavailable() || len(l.Entries) != 0 || l.Provenance.RawHash == "" {
		t.Fatalf("ledger: %+v %v", l, err)
	}
	if s, err := c.SwitchStatus(ctx, sScope, "tx:1"); err != nil || !s.Unavailable {
		t.Fatalf("switch: %+v %v", s, err)
	}
	if d, err := c.DestinationStatus(ctx, sScope, "tx:1"); err != nil || !d.Unavailable {
		t.Fatalf("destination: %+v %v", d, err)
	}
}

func sFail(t *testing.T, err error) *adapters.ReadError {
	t.Helper()
	var re *adapters.ReadError
	if !errors.As(err, &re) {
		t.Fatalf("want *adapters.ReadError, got %v", err)
	}
	return re
}

func TestEveryStatusMapsToAFailureTheEvidenceLayerCanTellApart(t *testing.T) {
	cases := []struct {
		status  int
		code    string
		retry   string
		failure adapters.ReadFailure
		raw     string
	}{
		{503, "STANDIN_NOT_LOADED", "", adapters.ReadUnavailable, "503:STANDIN_NOT_LOADED"},
		{500, "INTERNAL", "", adapters.ReadUnavailable, "500:INTERNAL"},
		{429, "RATE_LIMITED", "7", adapters.ReadUnavailable, "429:RATE_LIMITED"},
		{404, "NOT_FOUND", "", adapters.ReadNotFound, "404:NOT_FOUND"},
		{400, "BAD_REQUEST", "", adapters.ReadInvalid, "400:BAD_REQUEST"},
		{401, "UNAUTHORIZED", "", adapters.ReadUnauthorized, "401:UNAUTHORIZED"},
		{405, "", "", adapters.ReadMalformed, "405"},
		{302, "", "", adapters.ReadMalformed, "302"},
	}
	for _, tc := range cases {
		t.Run(strconv.Itoa(tc.status), func(t *testing.T) {
			f := newFakeStandin(t)
			f.on("/standin/v1/transactions/tx:1/ledger-entries", func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if tc.retry != "" {
					w.Header().Set("Retry-After", tc.retry)
				}
				if tc.status == 302 {
					w.Header().Set("Location", "http://127.0.0.1:1/elsewhere")
				}
				w.WriteHeader(tc.status)
				if tc.code != "" {
					fmt.Fprintf(w, `{"data_source":"stand-in","synthetic":true,"error":{"code":%q,"message":"x"}}`, tc.code)
				}
			})
			re := sFail(t, func() error {
				_, err := newStandinClient(t, f).LedgerEntries(sdl(t, 5*time.Second), sScope, "tx:1")
				return err
			}())
			if re.Failure != tc.failure || re.RawCode != tc.raw {
				t.Fatalf("got %s/%s, want %s/%s", re.Failure, re.RawCode, tc.failure, tc.raw)
			}
			if tc.retry != "" && re.RetryAfter != 7*time.Second {
				t.Fatalf("RetryAfter = %v", re.RetryAfter)
			}
		})
	}
}

func TestACallWithoutADeadlineIsRefusedAndSendsNothing(t *testing.T) {
	f := newFakeStandin(t)
	c := newStandinClient(t, f)
	_, err := c.LedgerEntries(context.Background(), sScope, "tx:1")
	if re := sFail(t, err); re.Failure != adapters.ReadInvalid || re.RawCode != "ADAPTER:NO_DEADLINE" {
		t.Fatalf("got %+v", re)
	}
	if len(f.seen) != 0 {
		t.Fatal("a refused call must not reach the stand-in")
	}
}

func TestATimeoutIsUnavailableAndSaysTimeout(t *testing.T) {
	f := newFakeStandin(t)
	f.on("/standin/v1/transactions/tx:1/ledger-entries", func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	_, err := newStandinClient(t, f).LedgerEntries(sdl(t, 200*time.Millisecond), sScope, "tx:1")
	if re := sFail(t, err); re.Failure != adapters.ReadUnavailable || re.RawCode != "TIMEOUT" {
		t.Fatalf("got %+v", re)
	}
}

func TestAnAnswerTheClientCannotTrustIsMalformedNeverKnown(t *testing.T) {
	bad := map[string]struct {
		ctype, body string
	}{
		"unknown field":      {"application/json", strings.Replace(ledgerBody, `"unavailable":false`, `"unavailable":false,"surprise":1`, 1)},
		"not json":           {"text/html", ledgerBody},
		"truncated":          {"application/json", ledgerBody[:60]},
		"empty body":         {"application/json", ""},
		"another source":     {"application/json", strings.Replace(ledgerBody, `"stand-in"`, `"tushe"`, 1)},
		"not synthetic":      {"application/json", strings.Replace(ledgerBody, `"synthetic":true`, `"synthetic":false`, 1)},
		"unknown entry type": {"application/json", strings.Replace(ledgerBody, `"DEBIT"`, `"REFUND"`, 1)},
		"entries missing":    {"application/json", `{"data_source":"stand-in","synthetic":true,"unavailable":false}`},
		"trailing data":      {"application/json", ledgerBody + `{}`},
	}
	for name, tc := range bad {
		t.Run(name, func(t *testing.T) {
			f := newFakeStandin(t)
			f.on("/standin/v1/transactions/tx:1/ledger-entries", func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", tc.ctype)
				_, _ = w.Write([]byte(tc.body))
			})
			_, err := newStandinClient(t, f).LedgerEntries(sdl(t, 5*time.Second), sScope, "tx:1")
			if re := sFail(t, err); re.Failure != adapters.ReadMalformed {
				t.Fatalf("got %+v, want MALFORMED", re)
			}
		})
	}
}

func TestTheRequestIsAGetWithTheRuntimeTokenAndAnAssertionForTheSessionPrincipal(t *testing.T) {
	f := newFakeStandin(t)
	f.body("/standin/v1/transactions/tx:1/ledger-entries", 200, ledgerBody)
	c := newStandinClient(t, f)
	if _, err := c.LedgerEntries(sdl(t, 5*time.Second), sScope, "tx:1"); err != nil {
		t.Fatal(err)
	}
	r := f.seen[0]
	if r.method != http.MethodGet || r.auth != "Bearer "+testRuntimeToken {
		t.Fatalf("method %s auth %q", r.method, r.auth)
	}
	parts := strings.Split(r.assertion, ".")
	if len(parts) != 4 || parts[0] != "v1" {
		t.Fatalf("assertion %q", r.assertion)
	}
	who, _ := base64.RawURLEncoding.DecodeString(parts[1])
	exp, _ := strconv.ParseInt(parts[2], 10, 64)
	if string(who) != "usr_a" || exp <= testNow.Unix() || exp-testNow.Unix() > 300 {
		t.Fatalf("principal %q expiry %d (now %d): must be the session principal and live at most 5 minutes", who, exp, testNow.Unix())
	}
	m := hmac.New(sha256.New, []byte(testPrincipalKey))
	fmt.Fprintf(m, "noema-standin-principal-v1\nstandin\nusr_a\n%d", exp)
	if parts[3] != hex.EncodeToString(m.Sum(nil)) {
		t.Fatal("the MAC does not verify with the principal key")
	}
	if strings.Contains(r.path+r.query, testRuntimeToken) || strings.Contains(r.path+r.query, testPrincipalKey) {
		t.Fatal("a credential reached the URL")
	}
}

func TestAnIncompleteScopeSendsNothing(t *testing.T) {
	f := newFakeStandin(t)
	c := newStandinClient(t, f)
	ctx := sdl(t, 5*time.Second)
	if _, err := c.LedgerEntries(ctx, adapters.Scope{TenantID: "ten_aa"}, "tx:1"); sFail(t, err).Failure != adapters.ReadInvalid {
		t.Fatal("no principal must be refused")
	}
	if _, err := c.SwitchStatus(ctx, sScope, ""); sFail(t, err).Failure != adapters.ReadInvalid {
		t.Fatal("no transaction must be refused")
	}
	if _, err := c.FindCandidates(ctx, sScope, adapters.TransactionQuery{}); sFail(t, err).Failure != adapters.ReadInvalid {
		t.Fatal("an unbounded query must be refused")
	}
	if len(f.seen) != 0 {
		t.Fatal("nothing may be sent")
	}
}

func TestCandidateFiltersAreSentAndAnAnswerOverTheLimitIsNotTruncated(t *testing.T) {
	f := newFakeStandin(t)
	f.body("/standin/v1/transactions", 200, candBody)
	c := newStandinClient(t, f)
	q := adapters.TransactionQuery{AccountRef: "acc_usr_a", From: "2026-10-01T00:00:00Z", To: "2026-10-02T00:00:00Z", AmountMinMinor: 100, AmountMaxMinor: 900000, Counterparty: "ben_1", Limit: 10}
	if _, err := c.FindCandidates(sdl(t, 5*time.Second), sScope, q); err != nil {
		t.Fatal(err)
	}
	want := "account=acc_usr_a&amount_max=900000&amount_min=100&counterparty=ben_1&from=2026-10-01T00%3A00%3A00Z&to=2026-10-02T00%3A00%3A00Z"
	if got := f.seen[0].query; got != want {
		t.Fatalf("query = %s, want %s", got, want)
	}
	// The stand-in has no pagination: more items than the caller's limit cannot be cut without hiding some, so it is not an answer.
	two := strings.Replace(candBody, `"items":[`, `"items":[{"transaction_ref":"tx:2","account_ref":"acc_usr_a","direction":"OUTGOING","amount_minor":1,"currency":"NGN","counterparty":"ben_1","occurred_at":"2026-10-01T09:30:00+01:00"},`, 1)
	f.body("/standin/v1/transactions", 200, two)
	q.Limit = 1
	_, err := c.FindCandidates(sdl(t, 5*time.Second), sScope, q)
	if re := sFail(t, err); re.Failure != adapters.ReadMalformed {
		t.Fatalf("got %+v", re)
	}
}

func TestTheClientHasNoWritePathAndNoControlPath(t *testing.T) {
	src, err := os.ReadFile("standin_client.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"MethodPost", "MethodPut", "MethodDelete", "MethodPatch", `"POST"`, `"PUT"`, `"DELETE"`, "/control/", "bench"} {
		if strings.Contains(string(src), bad) {
			t.Errorf("standin_client.go mentions %q: the client is read-only and never holds the bench credential (BD-08, REQ-ENV-002)", bad)
		}
	}
}

func TestACredentialFileThatIsMissingOrEmptyIsRefusedAtConstruction(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty")
	_ = os.WriteFile(empty, []byte("\n"), 0o600)
	good := filepath.Join(dir, "good")
	_ = os.WriteFile(good, []byte(testPrincipalKey), 0o600)
	now := func() time.Time { return testNow }
	for name, o := range map[string]StandinOptions{
		"missing token":  {BaseURL: "http://127.0.0.1:1", TokenFile: filepath.Join(dir, "none"), PrincipalKeyFile: good, Now: now},
		"empty token":    {BaseURL: "http://127.0.0.1:1", TokenFile: empty, PrincipalKeyFile: good, Now: now},
		"empty key":      {BaseURL: "http://127.0.0.1:1", TokenFile: good, PrincipalKeyFile: empty, Now: now},
		"no clock":       {BaseURL: "http://127.0.0.1:1", TokenFile: good, PrincipalKeyFile: good},
		"bad url":        {BaseURL: "ftp://x", TokenFile: good, PrincipalKeyFile: good, Now: now},
		"same token/key": {BaseURL: "http://127.0.0.1:1", TokenFile: good, PrincipalKeyFile: good, Now: now},
	} {
		if _, err := NewStandinClient(o); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

// A list that is absent is not an empty list: a missing field must never read as "no entries" or "no candidates" (found by the
// recorded missing_required_field exchange of the real proxy, ENV-06).
func TestAMissingListIsMalformedNotEmpty(t *testing.T) {
	f := newFakeStandin(t)
	f.body("/standin/v1/transactions", 200, `{"data_source":"stand-in","synthetic":true,"complete":true}`)
	_, err := newStandinClient(t, f).FindCandidates(sdl(t, 5*time.Second), sScope, adapters.TransactionQuery{AccountRef: "acc_usr_a", Limit: 10})
	if re := sFail(t, err); re.Failure != adapters.ReadMalformed {
		t.Fatalf("got %+v, want MALFORMED", re)
	}
	f.body("/standin/v1/transactions", 200, `{"data_source":"stand-in","synthetic":true,"complete":true,"items":[]}`)
	page, err := newStandinClient(t, f).FindCandidates(sdl(t, 5*time.Second), sScope, adapters.TransactionQuery{AccountRef: "acc_usr_a", Limit: 10})
	if err != nil || !page.ProvesAbsence() {
		t.Fatalf("a present empty list on a complete page is a real absence: %+v %v", page, err)
	}
}

// Codex review of noema#205 (P1): a requested cursor must be refused, not dropped. The stand-in has no pagination, so a caller asking for
// "the next page" would get the first page again, and a complete first page would then look like the whole answer.
func TestAnUnsupportedCursorIsRefusedAndNothingIsSent(t *testing.T) {
	f := newFakeStandin(t)
	f.body("/standin/v1/transactions", 200, candBody)
	_, err := newStandinClient(t, f).FindCandidates(sdl(t, 5*time.Second), sScope, adapters.TransactionQuery{AccountRef: "acc_usr_a", Limit: 10, Cursor: "c2"})
	if re := sFail(t, err); re.Failure != adapters.ReadInvalid || re.RawCode != "FIN:CURSOR_UNSUPPORTED" {
		t.Fatalf("got %+v, want INVALID FIN:CURSOR_UNSUPPORTED", re)
	}
	if len(f.seen) != 0 {
		t.Fatal("a refused cursor must not reach the stand-in")
	}
}

func TestACandidateWithAnUnknownDirectionOrABadTimeInAnAnswerIsMalformed(t *testing.T) {
	for name, body := range map[string]string{
		"unknown direction": strings.Replace(candBody, `"OUTGOING"`, `"SIDEWAYS"`, 1),
		"no direction":      strings.Replace(candBody, `"direction":"OUTGOING",`, ``, 1),
		"bad time":          strings.Replace(candBody, `"2026-10-01T09:30:00+01:00"`, `"yesterday"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			f := newFakeStandin(t)
			f.body("/standin/v1/transactions", 200, body)
			_, err := newStandinClient(t, f).FindCandidates(sdl(t, 5*time.Second), sScope, adapters.TransactionQuery{AccountRef: "acc_usr_a", Limit: 10})
			if re := sFail(t, err); re.Failure != adapters.ReadMalformed {
				t.Fatalf("got %+v, want MALFORMED", re)
			}
		})
	}
}

// Evidence Layer request: every error that received a response carries the SHA-256 of its bytes (non-2xx bodies and malformed 2xx bodies);
// only a failure with no bytes (a timeout) leaves it empty.
func TestEveryErrorThatReceivedBytesCarriesTheirHash(t *testing.T) {
	cases := map[string]struct {
		status int
		ctype  string
		body   string
	}{
		"404":                {404, "application/json", `{"data_source":"stand-in","synthetic":true,"error":{"code":"NOT_FOUND","message":"x"}}`},
		"503 not loaded":     {503, "application/json", `{"data_source":"stand-in","synthetic":true,"error":{"code":"STANDIN_NOT_LOADED","message":"x"}}`},
		"429":                {429, "application/json", `{"error":{"code":"RATE_LIMITED"}}`},
		"500 without a body": {500, "application/json", ``},
		"malformed 2xx":      {200, "application/json", `{"data_source":"stand-in",`},
		"wrong content type": {200, "text/html", ledgerBody},
		"unknown field":      {200, "application/json", strings.Replace(ledgerBody, `"unavailable":false`, `"unavailable":false,"x":1`, 1)},
		"another label":      {200, "application/json", strings.Replace(ledgerBody, `"stand-in"`, `"tushe"`, 1)},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFakeStandin(t)
			f.body("/standin/v1/transactions/tx:1/ledger-entries", tc.status, tc.body)
			f.on("/standin/v1/transactions/tx:1/ledger-entries", func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", tc.ctype)
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})
			_, err := newStandinClient(t, f).LedgerEntries(sdl(t, 5*time.Second), sScope, "tx:1")
			if re := sFail(t, err); re.RawHash != sha(tc.body) {
				t.Fatalf("RawHash = %q, want %q", re.RawHash, sha(tc.body))
			}
		})
	}
	t.Run("a timeout has no bytes and no hash", func(t *testing.T) {
		f := newFakeStandin(t)
		f.on("/standin/v1/transactions/tx:1/ledger-entries", func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
		_, err := newStandinClient(t, f).LedgerEntries(sdl(t, 200*time.Millisecond), sScope, "tx:1")
		if re := sFail(t, err); re.RawHash != "" {
			t.Fatalf("a timeout carries RawHash %q", re.RawHash)
		}
	})
}
