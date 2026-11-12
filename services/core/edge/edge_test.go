package edge_test

// EDG-01 slice 3b. REQ-API-004: every public call needs a valid session (401 in the 39.1 envelope, handler never runs); REQ-API-002: the
// refusals are safe text and a hidden route looks like a forbidden one; REQ-SEC-001: forged, expired, wrong-audience, wrong-issuer,
// wrong-tenant and two-role tokens are refused; INV-12 and AGENT-10: the principal comes from the session only, never from a body; ruling A1:
// the role gate is deny by default and the body guard runs after the session and role checks. Keys and tokens are SYNTHETIC, made per test.

import (
	"crypto/ecdsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/edge"
	"github.com/Noemai-org/noema/services/core/edge/edgetest"
	"github.com/Noemai-org/noema/services/core/edge/session"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/ids"
)

var epoch = time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC)

const maxBody = 1024

type rig struct {
	h     http.Handler
	iss   *edgetest.Issuer
	clk   *clock.Manual
	calls *int
	seen  *session.Principal
	body  *string
}

func newVerifier(t *testing.T, iss *edgetest.Issuer, clk clock.Clock) *session.Verifier {
	t.Helper()
	v, err := session.NewVerifier(session.Config{Keys: map[string]*ecdsa.PublicKey{edgetest.Kid: iss.Public()}, Audience: edgetest.Audience, Tenants: []string{edgetest.Tenant},
		MaxLifetime: session.CeilingLifetime, Skew: time.Minute, Clock: clk})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func newRig(t *testing.T) rig {
	t.Helper()
	iss := edgetest.NewIssuer(t)
	clk := clock.NewManual(epoch)
	calls, seen, body := new(int), new(session.Principal), new(string)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*calls++
		if p, ok := session.FromContext(r.Context()); ok {
			*seen = p
		}
		b := new(strings.Builder)
		buf := make([]byte, 64)
		for {
			n, err := r.Body.Read(buf)
			b.Write(buf[:n])
			if err != nil {
				break
			}
		}
		*body = b.String()
		w.WriteHeader(http.StatusOK)
	})
	h, err := edge.Protected(edge.Options{Verifier: newVerifier(t, iss, clk), IDs: ids.New(clk, nil), MaxBody: maxBody, Routes: []edge.Route{
		{Pattern: "POST /v1/cases", Roles: []session.Role{session.RoleCustomer}, Handler: handler},
		{Pattern: "GET /v1/cases", Roles: []session.Role{session.RoleCustomer, session.RoleOps}, Handler: handler},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return rig{h: h, iss: iss, clk: clk, calls: calls, seen: seen, body: body}
}

func (r rig) token(c edgetest.Claims) string { return r.iss.Token(edgetest.Kid, c) }

func (r rig) do(method, path, bearer, body string) *httptest.ResponseRecorder {
	var rd *strings.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	var req *http.Request
	if rd != nil {
		req = httptest.NewRequest(method, path, rd)
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	r.h.ServeHTTP(rec, req)
	return rec
}

func code(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var env struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("body %q is not the error envelope: %v", rec.Body.String(), err)
	}
	if rec.Header().Get(session.CorrelationHeader) == "" {
		t.Fatal("no correlation header")
	}
	return env.Error.Code
}

// REQ-API-004: no credential is 401 UNAUTHENTICATED and the handler does not run.
func TestNoSessionIs401AndTheHandlerDoesNotRun(t *testing.T) {
	r := newRig(t)
	rec := r.do(http.MethodGet, "/v1/cases", "", "")
	if rec.Code != http.StatusUnauthorized || code(t, rec) != "UNAUTHENTICATED" || *r.calls != 0 {
		t.Fatalf("got %d %s, handler calls %d", rec.Code, rec.Body.String(), *r.calls)
	}
}

// REQ-SEC-001, REQ-API-002: every bad token is the same 401 SESSION_INVALID, and the handler does not run.
func TestBadTokensAre401SessionInvalid(t *testing.T) {
	r := newRig(t)
	good := edgetest.Good(epoch, session.RoleCustomer)
	mut := func(f func(*edgetest.Claims)) string { c := good; f(&c); return r.token(c) }
	cases := map[string]string{
		"garbage": "not-a-token",
		"expired": mut(func(c *edgetest.Claims) {
			c.Iat, c.Exp = epoch.Add(-20*time.Minute).Unix(), epoch.Add(-10*time.Minute).Unix()
		}),
		"wrong audience": mut(func(c *edgetest.Claims) { c.Aud = "other" }),
		"wrong issuer":   mut(func(c *edgetest.Claims) { c.Iss = "someone-else" }),
		"wrong tenant":   mut(func(c *edgetest.Claims) { c.Tenant = "tnt_unknown" }),
		"two roles":      mut(func(c *edgetest.Claims) { c.Roles = []string{"customer", "ops"} }),
		"no role":        mut(func(c *edgetest.Claims) { c.Roles = nil }),
		"unknown role":   mut(func(c *edgetest.Claims) { c.Roles = []string{"root"} }),
		"unknown kid":    r.iss.Token("idp-other", good),
		"forged key":     edgetest.NewIssuer(t).Token(edgetest.Kid, good),
	}
	for name, tok := range cases {
		rec := r.do(http.MethodGet, "/v1/cases", tok, "")
		if rec.Code != http.StatusUnauthorized || code(t, rec) != "SESSION_INVALID" {
			t.Errorf("%s: got %d %s", name, rec.Code, rec.Body.String())
		}
	}
	if *r.calls != 0 {
		t.Fatalf("the handler ran %d times for refused sessions", *r.calls)
	}
}

// INV-12: a valid session reaches the handler with the principal of the token.
func TestAValidSessionReachesTheHandlerWithItsPrincipal(t *testing.T) {
	r := newRig(t)
	rec := r.do(http.MethodGet, "/v1/cases", r.token(edgetest.Good(epoch, session.RoleOps)), "")
	if rec.Code != http.StatusOK || *r.calls != 1 {
		t.Fatalf("got %d, calls %d", rec.Code, *r.calls)
	}
	if r.seen.ID != "prn_1" || r.seen.Tenant != edgetest.Tenant || r.seen.Role != session.RoleOps {
		t.Fatalf("principal %+v", *r.seen)
	}
}

// Ruling A1: a role the route does not list gets the answer for "not found", and the handler does not run.
func TestAWrongRoleIs404AndTheHandlerDoesNotRun(t *testing.T) {
	r := newRig(t)
	for _, role := range []session.Role{session.RoleApprover, session.RoleAudit} {
		rec := r.do(http.MethodGet, "/v1/cases", r.token(edgetest.Good(epoch, role)), "")
		if rec.Code != http.StatusNotFound || code(t, rec) != "NOT_FOUND_OR_NOT_AUTHORIZED" {
			t.Errorf("%s: got %d %s", role, rec.Code, rec.Body.String())
		}
	}
	rec := r.do(http.MethodPost, "/v1/cases", r.token(edgetest.Good(epoch, session.RoleOps)), `{"a":1}`)
	if rec.Code != http.StatusNotFound || *r.calls != 0 {
		t.Fatalf("ops on a customer-only route: %d, calls %d", rec.Code, *r.calls)
	}
}

// Ruling A1 (deny by default): a path or method no route registers is 404 for a session and 401 without one, so a route's existence is not revealed.
func TestAnUnregisteredRouteIsDeniedByDefault(t *testing.T) {
	r := newRig(t)
	tok := r.token(edgetest.Good(epoch, session.RoleCustomer))
	for _, c := range []struct{ method, path string }{{http.MethodGet, "/v1/nothing"}, {http.MethodDelete, "/v1/cases"}, {http.MethodGet, "/v1/"}} {
		rec := r.do(c.method, c.path, tok, "")
		if rec.Code != http.StatusNotFound || code(t, rec) != "NOT_FOUND_OR_NOT_AUTHORIZED" {
			t.Errorf("%s %s with a session: %d %s", c.method, c.path, rec.Code, rec.Body.String())
		}
		rec = r.do(c.method, c.path, "", "")
		if rec.Code != http.StatusUnauthorized || code(t, rec) != "UNAUTHENTICATED" {
			t.Errorf("%s %s without a session: %d %s", c.method, c.path, rec.Code, rec.Body.String())
		}
	}
	if *r.calls != 0 {
		t.Fatalf("handler ran for an unregistered route")
	}
}

// INV-12, AGENT-10: a body that names an identity is 400 INPUT_INVALID and the handler does not run.
func TestABodyNamingAnIdentityIs400(t *testing.T) {
	r := newRig(t)
	tok := r.token(edgetest.Good(epoch, session.RoleCustomer))
	for _, b := range []string{`{"principal_id":"prn_2"}`, `{"a":{"Tenant_ID":"tnt_b"}}`, `[{"role":"ops"}]`, `{"a":1} {"b":2}`, `not json`} {
		rec := r.do(http.MethodPost, "/v1/cases", tok, b)
		if rec.Code != http.StatusBadRequest || code(t, rec) != "INPUT_INVALID" {
			t.Errorf("body %q: %d %s", b, rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "prn_2") || strings.Contains(rec.Body.String(), "tnt_b") {
			t.Errorf("the refusal repeats the body: %s", rec.Body.String())
		}
	}
	if *r.calls != 0 {
		t.Fatalf("handler ran for a refused body")
	}
}

// REQ-API-002: the body is read after the session and the role are known, so an unauthenticated caller gets 401 and a wrong-role caller 404
// whatever the body says, and neither learns what the guard refuses.
func TestTheSessionAndRoleAreCheckedBeforeTheBody(t *testing.T) {
	r := newRig(t)
	bad := `{"principal_id":"prn_2"}`
	if rec := r.do(http.MethodPost, "/v1/cases", "", bad); rec.Code != http.StatusUnauthorized {
		t.Errorf("no session, identity body: %d", rec.Code)
	}
	if rec := r.do(http.MethodPost, "/v1/cases", r.token(edgetest.Good(epoch, session.RoleAudit)), bad); rec.Code != http.StatusNotFound {
		t.Errorf("wrong role, identity body: %d", rec.Code)
	}
}

// An accepted body reaches the handler unchanged; one over the limit is refused.
func TestAnAcceptedBodyIsUnchangedAndAnOversizeOneIsRefused(t *testing.T) {
	r := newRig(t)
	tok := r.token(edgetest.Good(epoch, session.RoleCustomer))
	ok := `{"case_ref":"c1","amount":{"minor":100,"currency":"NGN"}}`
	if rec := r.do(http.MethodPost, "/v1/cases", tok, ok); rec.Code != http.StatusOK || *r.body != ok {
		t.Fatalf("got %d, body seen %q", rec.Code, *r.body)
	}
	big := `{"x":"` + strings.Repeat("a", maxBody) + `"}`
	if rec := r.do(http.MethodPost, "/v1/cases", tok, big); rec.Code != http.StatusBadRequest {
		t.Fatalf("oversize body: %d", rec.Code)
	}
}

// Time comes from the injected clock (INV-21): a session that has expired on that clock is refused.
func TestExpiryFollowsTheInjectedClock(t *testing.T) {
	r := newRig(t)
	tok := r.token(edgetest.Good(epoch, session.RoleCustomer))
	if rec := r.do(http.MethodGet, "/v1/cases", tok, ""); rec.Code != http.StatusOK {
		t.Fatalf("fresh session: %d", rec.Code)
	}
	r.clk.Advance(7 * time.Minute)
	if rec := r.do(http.MethodGet, "/v1/cases", tok, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("expired session: %d", rec.Code)
	}
}

// Ruling A1: a misconfigured edge does not start. Fail closed, at construction.
func TestProtectedRefusesAnUnsafeConfiguration(t *testing.T) {
	iss := edgetest.NewIssuer(t)
	clk := clock.NewManual(epoch)
	v := newVerifier(t, iss, clk)
	idg := ids.New(clk, nil)
	ok := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	route := edge.Route{Pattern: "GET /v1/x", Roles: []session.Role{session.RoleOps}, Handler: ok}
	cases := map[string]edge.Options{
		"no verifier":     {IDs: idg, MaxBody: 10, Routes: []edge.Route{route}},
		"no ids":          {Verifier: v, MaxBody: 10, Routes: []edge.Route{route}},
		"zero body limit": {Verifier: v, IDs: idg, MaxBody: 0, Routes: []edge.Route{route}},
		"no roles":        {Verifier: v, IDs: idg, MaxBody: 10, Routes: []edge.Route{{Pattern: "GET /v1/x", Handler: ok}}},
		"unknown role":    {Verifier: v, IDs: idg, MaxBody: 10, Routes: []edge.Route{{Pattern: "GET /v1/x", Roles: []session.Role{"root"}, Handler: ok}}},
		"no handler":      {Verifier: v, IDs: idg, MaxBody: 10, Routes: []edge.Route{{Pattern: "GET /v1/x", Roles: []session.Role{session.RoleOps}}}},
		"no pattern":      {Verifier: v, IDs: idg, MaxBody: 10, Routes: []edge.Route{{Roles: []session.Role{session.RoleOps}, Handler: ok}}},
		"duplicate":       {Verifier: v, IDs: idg, MaxBody: 10, Routes: []edge.Route{route, route}},
		"catch-all":       {Verifier: v, IDs: idg, MaxBody: 10, Routes: []edge.Route{{Pattern: "/", Roles: []session.Role{session.RoleOps}, Handler: ok}}},
		"outside the API": {Verifier: v, IDs: idg, MaxBody: 10, Routes: []edge.Route{{Pattern: "GET /healthz", Roles: []session.Role{session.RoleOps}, Handler: ok}}},
		"invalid pattern": {Verifier: v, IDs: idg, MaxBody: 10, Routes: []edge.Route{{Pattern: "GET", Roles: []session.Role{session.RoleOps}, Handler: ok}}},
	}
	for name, o := range cases {
		if h, err := edge.Protected(o); err == nil || h != nil {
			t.Errorf("%s: accepted (err %v)", name, err)
		}
	}
	if _, err := edge.Protected(edge.Options{Verifier: v, IDs: idg, MaxBody: 10, Routes: []edge.Route{route}}); err != nil {
		t.Fatalf("a valid configuration was refused: %v", err)
	}
}
