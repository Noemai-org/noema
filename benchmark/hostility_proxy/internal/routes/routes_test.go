package routes

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func sample(t *testing.T) *Table {
	t.Helper()
	b, err := os.ReadFile("../../config/routes.json")
	if err != nil {
		t.Fatal(err)
	}
	tab, err := Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	return tab
}

func TestMatchAndExtract(t *testing.T) {
	tab := sample(t)
	r := httptest.NewRequest("GET", "/v1/status/TX88213", nil)
	rt := tab.Match(r)
	if rt == nil || rt.Target != "mock-switch" || rt.Operation != "get_status" {
		t.Fatalf("%+v", rt)
	}
	if got := rt.Extract(r)["transaction_ref"]; got != "TX88213" {
		t.Fatalf("path attribute: %q", got)
	}
	q := httptest.NewRequest("GET", "/v1/transactions?account=0123456789", nil)
	if got := tab.Match(q).Extract(q)["account"]; got != "0123456789" {
		t.Fatalf("query attribute: %q", got)
	}
	h := httptest.NewRequest("GET", "/v1/beneficiaries", nil)
	h.Header.Set("X-Principal", "usr_1")
	if got := tab.Match(h).Extract(h)["principal_id"]; got != "usr_1" {
		t.Fatalf("header attribute: %q", got)
	}
	if rt := tab.Lookup("mock-ledger", "get_entries"); rt == nil || rt.Path != "/v1/accounts/{account}/entries" {
		t.Fatalf("lookup: %+v", rt)
	}
	if tab.Lookup("mock-ledger", "nope") != nil {
		t.Fatal("unknown operation")
	}
	if got := strings.Join(tab.UpstreamNames(), ","); got != "relay,tushe" {
		t.Fatal(got)
	}
}

// Anything not in the table is not forwarded: the proxy is not an open relay.
func TestUnlistedRequestsDoNotMatch(t *testing.T) {
	tab := sample(t)
	for _, c := range []struct{ method, path string }{
		{"DELETE", "/v1/transactions/TX1"}, {"GET", "/v1/admin"}, {"GET", "/partner/oracle"}, {"POST", "/v1/status/TX1"},
		{"HEAD", "/v1/status/TX1"}, {"GET", "/"}, {"PATCH", "/v1/lookups"},
	} {
		if rt := tab.Match(httptest.NewRequest(c.method, c.path, nil)); rt != nil {
			t.Errorf("%s %s matched %+v", c.method, c.path, rt)
		}
	}
}

func TestInvalidTables(t *testing.T) {
	good := func(route string) string {
		return `{"schema_version":"1.0","synthetic":true,"routes":[` + route + `]}`
	}
	r := `{"method":"GET","path":"/a","upstream":"tushe","target":"mock-ledger","operation":"op"}`
	cases := map[string]string{
		"not json":                 `{`,
		"unknown field":            `{"schema_version":"1.0","synthetic":true,"routes":[],"x":1}`,
		"wrong version":            `{"schema_version":"2.0","synthetic":true,"routes":[` + r + `]}`,
		"not synthetic":            `{"schema_version":"1.0","synthetic":false,"routes":[` + r + `]}`,
		"empty":                    good(""),
		"bad method":               good(strings.Replace(r, "GET", "PATCH", 1)),
		"path without slash":       good(strings.Replace(r, "/a", "a", 1)),
		"bad upstream":             good(strings.Replace(r, "tushe", "Tu she", 1)),
		"mock-case is not proxied": good(strings.Replace(r, "mock-ledger", "mock-case", 1)),
		"unknown target":           good(strings.Replace(r, "mock-ledger", "ledger", 1)),
		"bad operation":            good(strings.Replace(r, `"op"`, `"Op"`, 1)),
		"duplicate route":          good(r + "," + strings.Replace(r, `"op"`, `"op2"`, 1)),
		"duplicate operation":      good(r + "," + strings.Replace(r, "/a", "/b", 1)),
		"attr source":              good(strings.Replace(r, `"operation":"op"`, `"operation":"op","attrs":{"x":"body:y"}`, 1)),
		"attr path var missing":    good(strings.Replace(r, `"operation":"op"`, `"operation":"op","attrs":{"x":"path:ref"}`, 1)),
		"attr nth_call":            good(strings.Replace(r, `"operation":"op"`, `"operation":"op","attrs":{"nth_call":"query:n"}`, 1)),
		"feed on post":             good(strings.Replace(strings.Replace(r, "GET", "POST", 1), `"operation":"op"`, `"operation":"op","event_feed":{"events":"e"}`, 1)),
		"conflicting pattern":      good(`{"method":"GET","path":"/x/{a}","upstream":"t","target":"mock-ledger","operation":"o1"},{"method":"GET","path":"/x/{b}","upstream":"t","target":"mock-ledger","operation":"o2"}`),
	}
	for name, in := range cases {
		if _, err := Parse([]byte(in)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	_ = http.StatusOK
}

// INV-12 (noema ENV-05): the stand-in adapter sends only the signed identity assertion, never a bare principal header, so the request log must attribute from the
// assertion. The proxy does not hold the key and does not verify the MAC or the other fields (the stand-in does): only the principal field is read, any caller can label
// a request with any printable principal, and a plan that matches on principal_id can therefore be steered by a forged assertion. Diagnostic labelling, not authentication.
func assertionTable(t *testing.T) *Table {
	t.Helper()
	tab, err := Parse([]byte(`{"schema_version":"1.0","synthetic":true,"routes":[{"method":"GET","path":"/standin/v1/principal","upstream":"standin","target":"mock-customer","operation":"get_principal","attrs":{"principal_id":"assertion:X-Principal-Assertion"}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	return tab
}

func principalOf(t *testing.T, tab *Table, headers ...string) string {
	t.Helper()
	r := httptest.NewRequest("GET", "/standin/v1/principal", nil)
	for _, h := range headers {
		r.Header.Add("X-Principal-Assertion", h)
	}
	rt := tab.Match(r)
	if rt == nil {
		t.Fatal("no route")
	}
	return rt.Extract(r)["principal_id"]
}

func b64(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }

// v2 builds a five-part assertion around the given principal bytes; the tenant, expiry and MAC are placeholders because the proxy reads none of them.
func v2(principal string) string { return "v2." + b64(principal) + "." + b64("ten_alpha") + ".1.00" }

func TestAnAssertionAttributeReadsThePrincipalOfAV2Assertion(t *testing.T) {
	tab := assertionTable(t)
	const real = "v2.dXNyX2E.dGVuX2FscGhh.1790847660.36f86867759aea52907e4c76ed41bd1608534527a1f3ecbe5d7f8f57c8d9ed42"
	if got := principalOf(t, tab, real); got != "usr_a" {
		t.Fatalf("the README's pinned v2 vector: %q", got)
	}
	if got := principalOf(t, tab); got != "" {
		t.Fatalf("no header: %q", got)
	}
}

// Each bound below was mutated in assertionPrincipal and made a case here go red (recorded in the PR): the version, the part count, the 128-byte cap, both ends of the
// printable range, and strict base64url.
func TestAnAssertionPrincipalBounds(t *testing.T) {
	tab := assertionTable(t)
	p128, p129 := strings.Repeat("a", 128), strings.Repeat("a", 129)
	for name, c := range map[string]struct{ header, want string }{
		"128 bytes is the longest accepted":             {v2(p128), p128},
		"129 bytes gives nothing":                       {v2(p129), ""},
		"0x20 (space) is below the printable range":     {v2("a b"), ""},
		"0x21 is the lowest accepted byte":              {v2("!"), "!"},
		"0x7e is the highest accepted byte":             {v2("~"), "~"},
		"0x7f (DEL) is above the printable range":       {v2("a\x7f"), ""},
		"a byte of 0x80 or more":                        {v2("a\x80"), ""},
		"0x01 to 0x03 control bytes":                    {v2("\x01\x02\x03"), ""},
		"an empty principal":                            {"v2.." + b64("ten_alpha") + ".1.00", ""},
		"five parts starting v1":                        {"v1." + b64("usr_a") + "." + b64("ten_alpha") + ".1.00", ""},
		"five parts starting v3":                        {"v3." + b64("usr_a") + "." + b64("ten_alpha") + ".1.00", ""},
		"five parts starting V2 (case matters)":         {"V2." + b64("usr_a") + "." + b64("ten_alpha") + ".1.00", ""},
		"the old four-part v1 form":                     {"v1.dXNyX2E.1790847660.aef356c16b785df13d4e4f17c28430155dc3de688179577663024029a99230ce", ""},
		"too few parts":                                 {"v2.dXNyX2E", ""},
		"six parts":                                     {v2("usr_a") + ".extra", ""},
		"non-canonical trailing bits (dXNyX2F)":         {"v2.dXNyX2F." + b64("ten_alpha") + ".1.00", ""},
		"an embedded newline":                           {"v2.dXNy\nX2E." + b64("ten_alpha") + ".1.00", ""},
		"an embedded carriage return":                   {"v2.dXNy\rX2E." + b64("ten_alpha") + ".1.00", ""},
		"padding":                                       {"v2.dXNyX2E=." + b64("ten_alpha") + ".1.00", ""},
		"standard-alphabet characters (+ and /)":        {"v2.+/+/." + b64("ten_alpha") + ".1.00", ""},
		"not base64 at all":                             {"v2.!!!." + b64("ten_alpha") + ".1.00", ""},
		"only the principal field is read (documented)": {"v2." + b64("usr_a") + "...", "usr_a"},
	} {
		if got := principalOf(t, tab, c.header); got != c.want {
			t.Errorf("%s: principal_id %q, want %q", name, got, c.want)
		}
	}
}

func TestTwoAssertionHeadersGiveNothing(t *testing.T) { // the first of two is not the one the stand-in verifies; fail closed
	tab := assertionTable(t)
	if got := principalOf(t, tab, v2("usr_a"), v2("usr_b")); got != "" {
		t.Fatalf("two headers: %q", got)
	}
}

func TestTheStandinRouteTableAttributesFromTheAssertionNotABareHeader(t *testing.T) { // reverting the table to header:X-Principal must fail
	b, err := os.ReadFile("../../config/routes.standin.json")
	if err != nil {
		t.Fatal(err)
	}
	tab, err := Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, rt := range tab.routes {
		if src, ok := rt.Attrs["principal_id"]; ok {
			n++
			if src != "assertion:X-Principal-Assertion" {
				t.Errorf("%s %s: principal_id source %q", rt.Method, rt.Path, src)
			}
		}
	}
	if n == 0 {
		t.Fatal("no route of the stand-in table has a principal_id attribute")
	}
	r := httptest.NewRequest("GET", "/standin/v1/principal", nil)
	r.Header.Set("X-Principal", "usr_bare")
	if got := tab.Match(r).Extract(r)["principal_id"]; got != "" {
		t.Fatalf("a bare X-Principal header must not label the request (INV-12): %q", got)
	}
	r.Header.Set("X-Principal-Assertion", v2("usr_a"))
	if got := tab.Match(r).Extract(r)["principal_id"]; got != "usr_a" {
		t.Fatalf("the assertion labels the request: %q", got)
	}
}
