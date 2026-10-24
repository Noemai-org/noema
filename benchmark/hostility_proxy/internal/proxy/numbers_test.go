package proxy_test

import (
	"fmt"
	"math/rand"
	"regexp"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/benchmark/hostility_proxy/internal/numbers"
)

var testNumber = regexp.MustCompile(`(^|[^0-9])9999[0-9]{6}([^0-9]|$)`)

// REQ-ENV-005: no `9999` test-number pattern in any response delivered to a system under test.
func TestNoTestNumberReachesTheSystemUnderTest(t *testing.T) {
	r := newRig(t)
	for _, p := range []string{"/v1/transactions/TX1", "/v1/status/TX1", "/v1/transactions?account=x", "/v1/beneficiaries", "/v1/events", "/relay/deliveries"} {
		res := r.get(p)
		if testNumber.MatchString(res.body) {
			t.Errorf("%s: %s", p, res.body)
		}
		for k, vs := range res.header {
			for _, v := range vs {
				if testNumber.MatchString(v) {
					t.Errorf("%s: header %s: %s", p, k, v)
				}
			}
		}
	}
	// under every fault that delivers a body too
	r.load(fault("mock-ledger", "get_transaction", "CLOCK_SKEW", `{}`, `{"skew_seconds":3}`)+","+fault("mock-switch", "get_status", "IMPOSSIBLE_STATE", `{}`, `{"fixture":"credited_before_debited"}`)+","+
		fault("mock-destination", "events_feed", "DUPLICATE_WEBHOOK", `{}`, `{}`)+","+fault("mock-customer", "get_beneficiaries", "SCHEMA_DRIFT", `{}`, `{"change":"add"}`), "")
	for _, p := range []string{"/v1/transactions/TX1", "/v1/status/TX1", "/v1/events", "/v1/beneficiaries"} {
		if res := r.get(p); testNumber.MatchString(res.body) {
			t.Errorf("faulted %s: %s", p, res.body)
		}
	}
	if !strings.Contains(r.ctl("GET", "/control/v1/stats", "").body, `"responses_refused_for_a_leaked_test_number":0`) {
		t.Fatal("no response may have been refused for a leak")
	}
}

func TestAccountNumbersAreRewrittenButAmountsAreNot(t *testing.T) {
	r := newRig(t)
	res := decode(t, r.get("/v1/transactions/TX1").body)
	if res["account"] == "9999041234" || res["counterparty"] == "9999100200" {
		t.Fatalf("not rewritten: %v", res)
	}
	if res["amount_minor"].(interface{ String() string }).String() != "500000" || res["amount_text"] != "999904123400" {
		t.Fatalf("an amount must never be touched, even one written to look like a number: %v", res)
	}
	for _, k := range []string{"account", "counterparty"} {
		s := res[k].(string)
		if len(s) != 10 || strings.HasPrefix(s, "9999") {
			t.Errorf("%s = %q", k, s)
		}
	}
}

// The same Tushe number always maps to the same synthetic number, across requests, proxies and runs.
func TestRewrittenNumbersAreStable(t *testing.T) {
	a, b := newRig(t), newRig(t)
	first := decode(t, a.get("/v1/transactions/TX1").body)["account"]
	for i := 0; i < 3; i++ {
		if got := decode(t, a.get("/v1/transactions/TX2").body)["account"]; got != first {
			t.Fatalf("%v then %v", first, got)
		}
	}
	if got := decode(t, b.get("/v1/transactions/TX1").body)["account"]; got != first {
		t.Fatalf("a second proxy with the same key: %v then %v", first, got)
	}
	c := newRigWithKey(t, strings.Replace(numberKey, "0001", "0002", 1))
	if got := decode(t, c.get("/v1/transactions/TX1").body)["account"]; got == first {
		t.Fatal("another key must give another number")
	}
	m, _ := numbers.New([]byte(numberKey))
	if want, _ := m.Synthetic("9999041234"); want != first {
		t.Fatalf("the proxy and the mapper disagree: %v vs %v", first, want)
	}
}

// What the system under test sends back goes to Tushe as the number Tushe knows.
func TestSyntheticNumbersAreMappedBackOnTheWayIn(t *testing.T) {
	r := newRig(t)
	syn := decode(t, r.get("/v1/transactions/TX1").body)["account"].(string)
	// path
	res := decode(t, r.get("/v1/accounts/"+syn+"/entries").body)
	if r.tushe.last.path != "/v1/accounts/9999041234/entries" {
		t.Fatalf("path: tushe saw %q", r.tushe.last.path)
	}
	if res["account_seen_by_tushe"] != syn {
		t.Fatalf("what Tushe echoes goes out rewritten again, as the number the system under test used: %v", res)
	}
	// query
	r.get("/v1/transactions?account=" + syn)
	if r.tushe.last.query != "account=9999041234" {
		t.Fatalf("query: tushe saw %q", r.tushe.last.query)
	}
	// JSON body: keys and values
	got := decode(t, r.post("/v1/lookups", `{"account":"`+syn+`","by":{"`+syn+`":["`+syn+`"]},"amount_minor":12}`).body)
	if !strings.Contains(r.tushe.last.body, `"account":"9999041234"`) || !strings.Contains(r.tushe.last.body, `"9999041234":["9999041234"]`) || !strings.Contains(r.tushe.last.body, `"amount_minor":12`) {
		t.Fatalf("body: tushe saw %s", r.tushe.last.body)
	}
	if testNumber.MatchString(fmt.Sprint(got)) {
		t.Fatalf("the echo reached the system under test unrewritten: %v", got)
	}
	// a number nobody issued is left alone
	r.post("/v1/lookups", `{"phone":"0803123456","account":"1234567890"}`)
	if !strings.Contains(r.tushe.last.body, `"phone":"0803123456"`) {
		t.Fatalf("an ordinary number changed: %s", r.tushe.last.body)
	}
}

func TestLocationHeaderIsRewritten(t *testing.T) {
	r := newRig(t)
	res := r.get("/v1/transactions?account=x")
	loc := res.header.Get("Location")
	if loc == "" || testNumber.MatchString(loc) || strings.Contains(loc, "9999041234") {
		t.Fatalf("Location: %q", loc)
	}
}

// Property: for many arbitrary test numbers, a response never shows one and what the system under test sends back
// reaches Tushe as the number that was rewritten.
func TestRoundTripOverManyNumbers(t *testing.T) {
	r := newRig(t)
	rng := rand.New(rand.NewSource(11))
	m, _ := numbers.New([]byte(numberKey))
	for i := 0; i < 300; i++ {
		real := fmt.Sprintf("9999%06d", rng.Intn(1000000))
		syn, ok := m.Synthetic(real)
		if !ok {
			t.Fatal(real)
		}
		r.get("/v1/accounts/" + syn + "/entries")
		if r.tushe.last.path != "/v1/accounts/"+real+"/entries" {
			t.Fatalf("%s -> %s -> tushe saw %s", real, syn, r.tushe.last.path)
		}
	}
}
