package routes

import (
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
// assertion. The proxy does not hold the key and does not verify the MAC (the stand-in does); this is diagnostic attribution, not authentication.
func TestAnAssertionAttributeReadsThePrincipalOfAV2Assertion(t *testing.T) {
	tab, err := Parse([]byte(`{"schema_version":"1.0","synthetic":true,"routes":[{"method":"GET","path":"/standin/v1/principal","upstream":"standin","target":"mock-customer","operation":"get_principal","attrs":{"principal_id":"assertion:X-Principal-Assertion"}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	const v2 = "v2.dXNyX2E.dGVuX2FscGhh.1790847660.36f86867759aea52907e4c76ed41bd1608534527a1f3ecbe5d7f8f57c8d9ed42"
	for name, c := range map[string]struct{ header, want string }{
		"a v2 assertion":            {v2, "usr_a"},
		"no header":                 {"", ""},
		"the old v1 form":           {"v1.dXNyX2E.1790847660.aef356c16b785df13d4e4f17c28430155dc3de688179577663024029a99230ce", ""},
		"not base64":                {"v2.!!!.dGVuX2FscGhh.1.00", ""},
		"too few parts":             {"v2.dXNyX2E", ""},
		"an empty principal":        {"v2..dGVuX2FscGhh.1.00", ""},
		"a non-printable principal": {"v2.AQID.dGVuX2FscGhh.1.00", ""},
	} {
		r := httptest.NewRequest("GET", "/standin/v1/principal", nil)
		if c.header != "" {
			r.Header.Set("X-Principal-Assertion", c.header)
		}
		rt := tab.Match(r)
		if rt == nil {
			t.Fatalf("%s: no route", name)
		}
		if got := rt.Extract(r)["principal_id"]; got != c.want {
			t.Errorf("%s: principal_id %q, want %q", name, got, c.want)
		}
	}
}
