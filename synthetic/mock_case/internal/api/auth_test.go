package api_test

import (
	"strings"
	"testing"
)

func concrete(route string) (method, path string) {
	parts := strings.SplitN(route, " ", 2)
	p := parts[1]
	for _, name := range []string{"{case_id}", "{complaint_ref}", "{attachment_id}"} {
		p = strings.ReplaceAll(p, name, "case_x")
	}
	return parts[0], p
}

// REQ-ENV-002 and REQ-ENV-008: the truth endpoint refuses every runtime identity.
// The identities that exist on the runtime side are the case adapter's credential, a caller with no credential
// (the model service and the UI hold none), a malformed one and a guessed one.
func TestBenchApiRefusesEveryRuntimeIdentity(t *testing.T) {
	e := newEnv(t)
	e.createCase("idk_1")
	identities := map[string]struct {
		token  string
		hdr    map[string]string
		suffix string
		status int
	}{
		"the core's case adapter":    {runtimeToken, nil, "", 403},
		"no credential (model, ui)":  {"", nil, "", 401},
		"a guessed credential":       {"guess-guess-guess-guess-guess-guess", nil, "", 401},
		"the bench token as a query": {"", nil, "?token=" + benchToken, 401},
		"the bench token, basic":     {"", map[string]string{"Authorization": "Basic " + benchToken}, "", 401},
		"the bench token, bare":      {"", map[string]string{"Authorization": benchToken}, "", 401},
	}
	routes := append(e.srv.BenchRoutes(), "GET /truth/v1/nothing-here", "DELETE /truth/v1/state", "POST /control/v1/not-a-route")
	for who, id := range identities {
		for _, route := range routes {
			method, path := concrete(route)
			r := e.do(method, e.benchURL+path+id.suffix, id.token, id.hdr, `{}`)
			if r.status != id.status {
				t.Errorf("%s -> %s: got %d, want %d (%s)", who, route, r.status, id.status, r.raw)
			}
			if r.json["cases"] != nil || r.json["commands"] != nil || r.json["plan"] != nil || r.json["state_hash"] != nil {
				t.Errorf("%s -> %s: a refusal leaked data: %s", who, route, r.raw)
			}
		}
	}
	if e.count("cases") != 1 {
		t.Fatal("refused calls changed state")
	}
	logs := e.logs.String()
	if !strings.Contains(logs, `"identity":"runtime"`) || !strings.Contains(logs, `"listener":"bench"`) {
		t.Fatalf("a runtime identity's attempt on the truth endpoint must be logged: %s", logs)
	}
	if strings.Contains(logs, runtimeToken) || strings.Contains(logs, benchToken) {
		t.Fatal("a credential reached the log")
	}
}

func TestCaseApiOpensOnlyForTheRuntimeCredential(t *testing.T) {
	e := newEnv(t)
	for _, route := range e.srv.CaseRoutes() {
		if route == "GET /healthz" {
			continue
		}
		method, path := concrete(route)
		for who, tc := range map[string]struct {
			token  string
			status int
		}{"none": {"", 401}, "guess": {"guess-guess-guess-guess", 401}, "the bench credential": {benchToken, 403}} {
			r := e.do(method, e.caseURL+path+"?tenant_id=ten_a", tc.token, key("idk_x"), `{}`)
			if r.status != tc.status {
				t.Errorf("%s -> %s: got %d want %d", who, route, r.status, tc.status)
			}
		}
	}
	// The truth paths do not exist on the case listener, even for the runtime credential.
	for _, path := range []string{"/truth/v1/state", "/truth/v1/digest", "/control/v1/fault-plan", "/control/v1/reset", "/bench/v1/openapi.json"} {
		if r := e.runtime("GET", path, nil, nil); r.status != 404 {
			t.Errorf("runtime GET %s on the case listener: %d", path, r.status)
		}
	}
	if r := e.runtime("PUT", "/control/v1/fault-plan", nil, plan("", "")); r.status != 404 {
		t.Errorf("the runtime cannot load a fault plan: %d", r.status)
	}
}

func TestBenchCredentialOpensTheBenchApi(t *testing.T) {
	e := newEnv(t)
	for _, path := range []string{"/truth/v1/state", "/truth/v1/commands", "/truth/v1/digest", "/control/v1/fault-plan", "/control/v1/clock", "/bench/v1/openapi.json"} {
		if r := e.bench("GET", path, nil); r.status != 200 {
			t.Errorf("GET %s: %d %s", path, r.status, r.raw)
		}
	}
}
