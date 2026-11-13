package edge_test

import (
	"net/http"
	"testing"

	"github.com/Noemai-org/noema/services/core/edge"
	"github.com/Noemai-org/noema/services/core/edge/edgetest"
	"github.com/Noemai-org/noema/services/core/edge/session"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/ids"
)

// SEC-04 (noema#505), REQ-SEC-001, REQ-API-004, INV-12; OD-P-79, ruling A54, the control-plane authorization matrix rows 1 to 7 and 13 and rule 7 (D3a): the public edge serves
// no admin route, no benchmark route and no route that names a key or a rotation, whatever roles the registration names. A configuration that tries is an error and no handler
// is built. (The Security Reviewer wrote the matching red test in noema#504, which failed at ec5b44a for the same reason: checkRoute required only a /v1/ prefix.)
func TestThePublicEdgeRefusesToServeTheAdminBenchmarkAndKeyRoutes(t *testing.T) {
	iss := edgetest.NewIssuer(t)
	clk := clock.NewManual(epoch)
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	for _, p := range []string{
		"POST /v1/admin/tenants/{t}/kill-switch:set",
		"POST /v1/admin/tenants/{t}/kill-switch:clear",
		"GET /v1/admin/tenants/{t}/kill-switch",
		"POST /v1/admin/policies/{id}/versions",
		"POST /v1/admin/policies/{id}/versions/{v}/publish",
		"POST /v1/admin/packs:load",
		"PUT /v1/admin/tenants/{t}/callback",
		"POST /v1/benchmark/runs",
		"GET /v1/benchmark/runs/{id}",
		"POST /v1/keys/rotate",
		"POST /v1/ops/keys:register",
		"POST /v1/tenants/{t}/rotation",
		"POST /v1/Admin/tenants/{t}/kill-switch:set", // the mux is case-sensitive; the refusal is not
		"POST /v1/admin:reload",                      // a ":verb" on the first segment is still the admin segment
		"POST /v1/benchmark:run",
	} {
		h, err := edge.Protected(edge.Options{Verifier: newVerifier(t, iss, clk), IDs: ids.New(clk, nil), MaxBody: maxBody, Routes: []edge.Route{
			{Pattern: p, Roles: []session.Role{session.RoleOps}, Handler: ok},
		}})
		if err == nil || h != nil {
			t.Errorf("the public edge accepted the route %q, which belongs to another listener or does not exist in the POC", p)
		}
	}
}

// The refusal is of the control plane only: the routes the public API serves (PERD 39.2, and the reconstruction read of REQ-AUD-003) are still accepted, and a word that merely
// begins like a refused one is not refused.
func TestTheRefusalOfTheControlPlaneLeavesThePublicRoutesAlone(t *testing.T) {
	iss := edgetest.NewIssuer(t)
	clk := clock.NewManual(epoch)
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	for _, p := range []string{
		"POST /v1/conversations",
		"POST /v1/conversations/{conversation_ref}/messages",
		"GET /v1/conversations/{conversation_ref}/events",
		"POST /v1/confirmations/{confirmation_request_id}",
		"GET /v1/investigations/{investigation_id}",
		"GET /v1/reconstructions/{correlation_id}",
		"GET /v1/administrators/{id}", // "administrators" is not "admin": only the segment itself is the control plane's
	} {
		if _, err := edge.Protected(edge.Options{Verifier: newVerifier(t, iss, clk), IDs: ids.New(clk, nil), MaxBody: maxBody, Routes: []edge.Route{
			{Pattern: p, Roles: []session.Role{session.RoleOps}, Handler: ok},
		}}); err != nil {
			t.Errorf("a public route was refused: %q: %v", p, err)
		}
	}
}
