package edge_test

// SEC-04, ADV-14: the authorization matrix of docs/security/control-plane-authz-matrix.md (docs#134, merged), as a test over the real edge router.
// Written by the Security Reviewer, failing first where the edge does not yet hold a rule (E1: the edge is EL's and C1's to change).
//
// What this file proves today (all SYNTHETIC tokens, one key per test):
//   - rows 8 to 12, the routes the public edge serves: for every session role, an allowed cell is a 200 that runs the handler once with the
//     session's principal, and a refused cell is the not-found shape of a route that does not exist (same status, code and body apart from the
//     correlation id) and never runs the handler (matrix rules 1 to 3);
//   - matrix rule 9: a database login or any non-token string presented as a bearer is the same single 401, handler not run.
//
// What it requires of the edge and does not yet get (red by design, for C1):
//   - matrix rows 1 to 7 and 13 are not on the public listener: OD-P-79 and ruling A54 put the admin routes on a separate listener and the
//     benchmark route on the benchmark listener, so the public edge must refuse to register a /v1/admin/ or /v1/benchmark/ pattern;
//   - matrix rule 7: key rotation has no route in the POC (D3a), so a pattern naming a key or a rotation is refused too.
// C-ADMIN and C-VERIFY (rows 1 to 7, columns C7 and C8) are tested when the admin listener exists; the Architect ruled the classes on noema#305.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/edge"
	"github.com/Noemai-org/noema/services/core/edge/edgetest"
	"github.com/Noemai-org/noema/services/core/edge/session"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/ids"
)

// mxRow is one public route of the matrix and the session roles the matrix lets call it (PERD 39.4; ruling A110 keeps rows 10 and 11 audit-only).
type mxRow struct {
	pattern, path string
	allowed       []session.Role
}

var mxRows = []mxRow{
	{"GET /v1/ops/debt", "/v1/ops/debt", []session.Role{session.RoleOps}},
	{"GET /v1/ops/alerts", "/v1/ops/alerts", []session.Role{session.RoleOps}},
	{"GET /v1/investigations/{id}/panel", "/v1/investigations/inv_1/panel", []session.Role{session.RoleOps, session.RoleAudit}},
	{"GET /v1/reconstructions/{cid}", "/v1/reconstructions/corr_1", []session.Role{session.RoleAudit}},
	{"GET /v1/record/verify", "/v1/record/verify", []session.Role{session.RoleAudit}},
	{"GET /v1/outcomes/{verdict_id}", "/v1/outcomes/vrd_1", []session.Role{session.RoleCustomer, session.RoleOps, session.RoleAudit}},
}

var mxRoles = []session.Role{session.RoleCustomer, session.RoleApprover, session.RoleOps, session.RoleAudit}

func mxAllows(allowed []session.Role, r session.Role) bool {
	for _, a := range allowed {
		if a == r {
			return true
		}
	}
	return false
}

type mxRig struct {
	h     http.Handler
	iss   *edgetest.Issuer
	calls map[string]int
	who   map[string]session.Principal
}

func newMxRig(t *testing.T) mxRig {
	t.Helper()
	iss := edgetest.NewIssuer(t)
	clk := clock.NewManual(epoch)
	rig := mxRig{iss: iss, calls: map[string]int{}, who: map[string]session.Principal{}}
	var routes []edge.Route
	for _, row := range mxRows {
		row := row
		routes = append(routes, edge.Route{Pattern: row.pattern, Roles: row.allowed, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rig.calls[row.pattern]++
			if p, ok := session.FromContext(r.Context()); ok {
				rig.who[row.pattern] = p
			}
			w.WriteHeader(http.StatusOK)
		})})
	}
	h, err := edge.Protected(edge.Options{Verifier: newVerifier(t, iss, clk), IDs: ids.New(clk, nil), MaxBody: maxBody, Routes: routes})
	if err != nil {
		t.Fatal(err)
	}
	rig.h = h
	return rig
}

func (r mxRig) get(path, bearer string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	r.h.ServeHTTP(rec, req)
	return rec
}

// mxShape is a response with the per-request correlation id removed: what a caller can observe about a refusal.
func mxShape(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var env map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("body %q is not the error envelope: %v", rec.Body.String(), err)
	}
	if e, ok := env["error"].(map[string]any); ok {
		delete(e, "correlation_id")
	}
	delete(env, "correlation_id")
	b, _ := json.Marshal(env)
	return http.StatusText(rec.Code) + " " + string(b)
}

// Matrix rules 1 to 3, rows 8 to 12: every (role, route) cell is as the matrix says, a refusal is the shape of a route that does not exist, and the
// handler runs exactly when the cell is allowed, with the session's principal (INV-12).
func TestTheMatrixCellOfEveryRoleAndPublicRoute(t *testing.T) {
	rig := newMxRig(t)
	for _, role := range mxRoles {
		tok := rig.iss.Token(edgetest.Kid, edgetest.Good(epoch, role))
		unknown := mxShape(t, rig.get("/v1/does-not-exist", tok))
		for _, row := range mxRows {
			before := rig.calls[row.pattern]
			rec := rig.get(row.path, tok)
			if mxAllows(row.allowed, role) {
				if rec.Code != http.StatusOK || rig.calls[row.pattern] != before+1 {
					t.Errorf("%s as %s: want 200 and one handler call, got %d and %d calls", row.pattern, role, rec.Code, rig.calls[row.pattern]-before)
				}
				if rig.who[row.pattern].Tenant != edgetest.Tenant {
					t.Errorf("%s as %s: the handler's principal tenant is %q, want the token's", row.pattern, role, rig.who[row.pattern].Tenant)
				}
				continue
			}
			if rig.calls[row.pattern] != before {
				t.Errorf("%s as %s: a refused cell ran the handler", row.pattern, role)
			}
			if got := mxShape(t, rec); rec.Code != http.StatusNotFound || got != unknown {
				t.Errorf("%s as %s: refusal %d %s differs from the not-found shape of a route that does not exist (%s)", row.pattern, role, rec.Code, got, unknown)
			}
		}
	}
}

// Matrix rule 9 and A1 (one rejection): a database login, a password, a URL with a password or a raw key presented as a bearer is the same single 401
// and runs no handler. A service login is not an identity on the HTTP edge.
func TestAServiceLoginOrASecretIsNotACredentialOnTheEdge(t *testing.T) {
	rig := newMxRig(t)
	base := mxShape(t, rig.get(mxRows[0].path, "not-a-token"))
	for name, bearer := range map[string]string{
		"a database URL": "postgres://trust_rw:synthetic-pw@db:5432/noema",
		"a login name":   "execution_rw",
		"a hex secret":   strings.Repeat("ab", 24),
		"a basic pair":   "trust_rw:synthetic-pw",
	} {
		rec := rig.get(mxRows[0].path, bearer)
		if rec.Code != http.StatusUnauthorized || mxShape(t, rec) != base {
			t.Errorf("%s: got %d %s, want the one 401 of any bad token (%s)", name, rec.Code, mxShape(t, rec), base)
		}
	}
	for _, row := range mxRows {
		if rig.calls[row.pattern] != 0 {
			t.Errorf("%s: a handler ran for a non-token bearer", row.pattern)
		}
	}
}

// OD-P-79, ruling A54, matrix rows 1 to 7 and 13: the admin routes and the benchmark route are not served by the public edge, whatever roles the
// registration names; a configuration that tries is an error, never a handler. Matrix rule 7: nor is a route that names a key or a rotation (D3a).
// RED until the edge refuses these patterns (C1, EDG-01).
func TestThePublicEdgeRefusesToServeTheAdminBenchmarkAndKeyRoutes(t *testing.T) {
	iss := edgetest.NewIssuer(t)
	clk := clock.NewManual(epoch)
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	patterns := []string{
		"POST /v1/admin/tenants/{t}/kill-switch:set",
		"POST /v1/admin/tenants/{t}/kill-switch:clear",
		"GET /v1/admin/tenants/{t}/kill-switch",
		"POST /v1/admin/policies/{id}/versions",
		"POST /v1/admin/policies/{id}/versions/{v}/publish", // the D14 name is {v}:publish; Go 1.22 mux wildcards must be whole segments, so that spelling cannot be mounted at all (see the PR)
		"POST /v1/admin/packs:load",
		"PUT /v1/admin/tenants/{t}/callback",
		"POST /v1/benchmark/runs",
		"POST /v1/keys/rotate",
		"POST /v1/ops/keys:register",
	}
	for _, p := range patterns {
		_, err := edge.Protected(edge.Options{Verifier: newVerifier(t, iss, clk), IDs: ids.New(clk, nil), MaxBody: maxBody, Routes: []edge.Route{
			{Pattern: p, Roles: []session.Role{session.RoleOps}, Handler: ok},
		}})
		if err == nil {
			t.Errorf("the public edge accepted the route %q, which belongs to another listener or does not exist in the POC", p)
		}
	}
}
