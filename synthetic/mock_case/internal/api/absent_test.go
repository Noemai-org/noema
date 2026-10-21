package api_test

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/synthetic/mock_case/internal/api"
)

// REQ-ENV-008: "It offers no operation to close or resolve a case, downgrade priority, change an SLA or clock,
// or edit or delete evidence. Those operations SHALL have no route and SHALL be absent from its API description."
// DEC-2026-10-12 D1 adds: no monetary operation exists.

type forbidden struct{ method, path string }

func forbiddenRequests(caseID, ref, att string) []forbidden {
	var out []forbidden
	for _, verb := range []string{"close", "resolve", "reopen", "downgrade", "downgrade-priority", "priority", "sla", "clock", "status", "escalate", "transfer", "refund", "reverse", "dispute", "delete"} {
		out = append(out, forbidden{"POST", "/v1/cases/" + caseID + "/" + verb})
	}
	for _, m := range []string{"PUT", "PATCH", "DELETE"} {
		out = append(out,
			forbidden{m, "/v1/cases/" + caseID}, forbidden{m, "/v1/cases/" + caseID + "/priority"}, forbidden{m, "/v1/cases/" + caseID + "/status"},
			forbidden{m, "/v1/cases/" + caseID + "/sla"}, forbidden{m, "/v1/cases/" + caseID + "/evidence"},
			forbidden{m, "/v1/evidence/" + att}, forbidden{m, "/v1/complaints/" + ref}, forbidden{m, "/v1/complaints"}, forbidden{m, "/v1/cases"})
	}
	out = append(out, forbidden{"HEAD", "/v1/cases/" + caseID}, forbidden{"HEAD", "/healthz"}, forbidden{"OPTIONS", "/v1/cases"},
		forbidden{"POST", "/v1/complaints/" + ref + "/close"}, forbidden{"POST", "/v1/complaints/" + ref + "/resolve"},
		forbidden{"POST", "/v1/evidence/" + att}, forbidden{"POST", "/v1/payments"}, forbidden{"POST", "/v1/transfers"},
		forbidden{"POST", "/v1/refunds"}, forbidden{"POST", "/v1/reversals"}, forbidden{"POST", "/v1/disputes"}, forbidden{"GET", "/v1/disputes"},
		forbidden{"POST", "/v1/cases/" + caseID + "/route/undo"})
	return out
}

func TestForbiddenOperationsHaveNoRouteAndChangeNothing(t *testing.T) {
	e := newEnv(t)
	c := e.createCase("idk_1")
	comp := e.runtime("POST", "/v1/cases/"+c.str("case_id")+"/complaints", key("idk_c"), map[string]any{"tenant_id": "ten_a"})
	att := e.runtime("POST", "/v1/cases/"+c.str("case_id")+"/evidence", key("idk_e"), map[string]any{"tenant_id": "ten_a", "payload": "x"})
	before, _ := e.digest()
	attempts := forbiddenRequests(c.str("case_id"), comp.str("complaint_ref"), att.str("attachment_id"))
	for _, f := range attempts {
		body := map[string]any{"tenant_id": "ten_a", "status": "CLOSED", "priority": "LOW", "payload": "edited"}
		r := e.runtime(f.method, f.path+"?tenant_id=ten_a", key("idk_x"), body)
		if f.method == "HEAD" { // a HEAD response has no body to inspect
			if r.status != 404 {
				t.Errorf("HEAD %s: %d", f.path, r.status)
			}
			continue
		}
		if r.status != 404 || r.errCode() != "NOT_FOUND" || r.str("error", "message") != "no such route" {
			t.Errorf("%s %s: want no route, got %d %s", f.method, f.path, r.status, r.raw)
		}
	}
	after, _ := e.digest()
	if before != after {
		t.Fatal("a refused operation changed state")
	}
	// The same on the bench listener: even the benchmark runner cannot edit what the case system never offers.
	for _, f := range []forbidden{{"DELETE", "/truth/v1/state"}, {"PUT", "/truth/v1/state"}, {"POST", "/truth/v1/state"}, {"DELETE", "/v1/cases/" + c.str("case_id")}, {"POST", "/control/v1/snapshot"}, {"POST", "/control/v1/restore"}} {
		if r := e.do(f.method, e.benchURL+f.path, benchToken, nil, `{}`); r.status != 404 {
			t.Errorf("bench %s %s: %d %s", f.method, f.path, r.status, r.raw)
		}
	}
	if s, _ := e.digest(); s != before {
		t.Fatal("state changed")
	}
}

type openAPI struct {
	Paths map[string]map[string]struct {
		OperationID string `json:"operationId"`
		Summary     string `json:"summary"`
	} `json:"paths"`
}

func described(t *testing.T, raw []byte) (routes []string, doc openAPI) {
	t.Helper()
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	for path, ops := range doc.Paths {
		for method := range ops {
			routes = append(routes, strings.ToUpper(method)+" "+path)
		}
	}
	sort.Strings(routes)
	return routes, doc
}

func sorted(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

// The description lists exactly the routes the server serves: no more, no fewer, for either listener.
func TestApiDescriptionsMatchTheRoutes(t *testing.T) {
	e := newEnv(t)
	for name, tc := range map[string]struct {
		raw    []byte
		routes []string
	}{"case": {api.CaseDescription(), e.srv.CaseRoutes()}, "bench": {api.BenchDescription(), e.srv.BenchRoutes()}} {
		got, _ := described(t, tc.raw)
		want := sorted(tc.routes)
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("%s: description and routes differ\ndescribed: %v\nserved:    %v", name, got, want)
		}
	}
}

var word = regexp.MustCompile(`[a-z]+`)

func TestCaseApiDescriptionOffersNothingProhibited(t *testing.T) {
	banned := map[string]bool{"close": true, "closes": true, "resolve": true, "resolves": true, "downgrade": true, "lower": true, "delete": true, "edit": true,
		"update": true, "patch": true, "sla": true, "clock": true, "money": true, "payment": true, "payments": true, "transfer": true, "transfers": true,
		"refund": true, "refunds": true, "reversal": true, "reversals": true, "dispute": true, "disputes": true, "reopen": true, "status": true}
	_, doc := described(t, api.CaseDescription())
	for path, ops := range doc.Paths {
		for method, op := range ops {
			if method != "get" && method != "post" {
				t.Errorf("%s %s: the case API offers only GET and POST", method, path)
			}
			for _, w := range word.FindAllString(strings.ToLower(path+" "+op.OperationID+" "+op.Summary), -1) {
				if banned[w] {
					t.Errorf("%s %s mentions %q", method, path, w)
				}
			}
		}
	}
	// And the server table: no verb other than GET and POST is registered.
	e := newEnv(t)
	for _, r := range append(e.srv.CaseRoutes(), e.srv.BenchRoutes()...) {
		if strings.HasPrefix(r, "PATCH") || (strings.HasPrefix(r, "DELETE") && !strings.Contains(r, "/control/v1/fault-plan")) {
			t.Errorf("unexpected route %s", r)
		}
	}
}

// The scan must be able to fail: a description that adds a prohibited operation is caught.
func TestProhibitedOperationInADescriptionWouldBeCaught(t *testing.T) {
	bad := []byte(`{"paths":{"/v1/cases/{case_id}/close":{"post":{"operationId":"closeCase","summary":"Close a case"}}}}`)
	_, doc := described(t, bad)
	hits := 0
	for path, ops := range doc.Paths {
		for _, op := range ops {
			for _, w := range word.FindAllString(strings.ToLower(path+" "+op.OperationID+" "+op.Summary), -1) {
				if w == "close" {
					hits++
				}
			}
		}
	}
	if hits == 0 {
		t.Fatal("the scan did not notice a prohibited operation")
	}
}
