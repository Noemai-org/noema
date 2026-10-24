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
