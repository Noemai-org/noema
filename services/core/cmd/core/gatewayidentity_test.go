package main

// EDG-01 slice 3c: the identity the Input Gateway is given comes from the verified session and from nothing else (INV-12, AGENT-10). The route is not mounted here (what
// POST /v1/conversations answers is open: noema#601); this is the one function the mounting will use, tested through the real edge chain (a signed token, the session
// middleware, the role gate and the body guard). Tokens and keys are SYNTHETIC, made per test (AGENT-25).

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/edge"
	"github.com/Noemai-org/noema/services/core/edge/edgetest"
	"github.com/Noemai-org/noema/services/core/edge/session"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/ids"
)

// identityRoute answers with the identity the gateway would be given, so a test sees exactly what crosses.
func identityRoute(roles ...session.Role) edge.Route {
	return edge.Route{
		Pattern: "POST /v1/conversations",
		Roles:   roles,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id, ok := gatewayIdentity(r)
			if !ok {
				http.Error(w, "no identity", http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(id)
		}),
	}
}

func identityAPI(t *testing.T, iss *edgetest.Issuer, roles ...session.Role) http.Handler {
	t.Helper()
	clk := clock.NewManual(apiEpoch)
	h, err := edge.Load(sessionEnv(t, iss).get, os.ReadFile, clk, ids.New(clk, nil), []edge.Route{identityRoute(roles...)})
	if err != nil || h == nil {
		t.Fatalf("edge: %v, %v", h, err)
	}
	return h
}

func post(h http.Handler, authz, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/conversations", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if authz != "" {
		req.Header.Set("Authorization", authz)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// INV-12, AGENT-10: tenant, principal, kind and session reference are the verified session's, and the session reference is the session's AuthnRef, never the token.
func TestTheGatewayIdentityIsTheVerifiedSessionsAndNothingElse(t *testing.T) {
	iss := edgetest.NewIssuer(t)
	h := identityAPI(t, iss, session.RoleCustomer)
	auth := bearer(iss, session.RoleCustomer, edgetest.Tenant)
	rec := post(h, auth, `{"request_id":"3f2a9c1e-5b7d-4e8f-9a1c-2d4b6e8f0a1b","text":"x"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var got struct{ TenantID, PrincipalID, PrincipalKind, SessionID string }
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.TenantID != edgetest.Tenant || got.PrincipalID != "prn_1" || got.PrincipalKind != "customer" {
		t.Errorf("identity %+v, want the token's tenant, subject and the customer kind", got)
	}
	if !strings.HasPrefix(got.SessionID, "ses_") || strings.Contains(auth, got.SessionID) || len(got.SessionID) != 36 {
		t.Errorf("session reference %q: want the session's AuthnRef (ses_ and 32 hex characters), never part of the token", got.SessionID)
	}
}

// A second tenant's token gives that tenant's identity, so a tenant is never carried over from a configuration or a default.
func TestTheTenantOfTheIdentityIsTheTokensOwn(t *testing.T) {
	iss := edgetest.NewIssuer(t)
	h := identityAPI(t, iss, session.RoleCustomer)
	rec := post(h, bearer(iss, session.RoleCustomer, "tnt_other"), `{"request_id":"3f2a9c1e-5b7d-4e8f-9a1c-2d4b6e8f0a1b","text":"x"}`)
	var got struct{ TenantID string }
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.TenantID != "tnt_other" {
		t.Fatalf("status %d, body %s, err %v", rec.Code, rec.Body.String(), err)
	}
}

// A request that did not pass the session middleware has no identity: the function answers false, never an empty identity that a caller might accept.
func TestARequestWithNoVerifiedSessionHasNoIdentity(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/conversations", strings.NewReader("{}"))
	if id, ok := gatewayIdentity(req); ok {
		t.Fatalf("an identity %+v for a request with no session", id)
	}
}

// The identity is not read from the request: a body that names one is refused by the edge's body guard before the handler, and a header that names one is ignored.
func TestNoIdentityIsTakenFromTheBodyOrTheHeaders(t *testing.T) {
	iss := edgetest.NewIssuer(t)
	h := identityAPI(t, iss, session.RoleCustomer)
	auth := bearer(iss, session.RoleCustomer, edgetest.Tenant)
	if rec := post(h, auth, `{"request_id":"3f2a9c1e-5b7d-4e8f-9a1c-2d4b6e8f0a1b","text":"x","tenant_id":"tnt_other"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("a body naming a tenant: status %d, want 400", rec.Code)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/conversations", strings.NewReader(`{"request_id":"3f2a9c1e-5b7d-4e8f-9a1c-2d4b6e8f0a1b","text":"x"}`))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-Id", "tnt_other")
	req.Header.Set("X-Principal-Id", "prn_2")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var got struct{ TenantID, PrincipalID string }
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.TenantID != edgetest.Tenant || got.PrincipalID != "prn_1" {
		t.Errorf("status %d, body %s: the headers changed the identity", rec.Code, rec.Body.String())
	}
}

// Only a customer holds a conversation: with the route gated to customers, an ops, audit or approver session gets 404 and the handler never runs.
func TestOnlyACustomerSessionReachesTheConversationRoute(t *testing.T) {
	iss := edgetest.NewIssuer(t)
	h := identityAPI(t, iss, rolesCustomer...)
	for _, role := range []session.Role{session.RoleOps, session.RoleAudit, session.RoleApprover} {
		if rec := post(h, bearer(iss, role, edgetest.Tenant), `{"request_id":"3f2a9c1e-5b7d-4e8f-9a1c-2d4b6e8f0a1b","text":"x"}`); rec.Code != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", role, rec.Code)
		}
	}
	if rec := post(h, bearer(iss, session.RoleCustomer, edgetest.Tenant), `{"request_id":"3f2a9c1e-5b7d-4e8f-9a1c-2d4b6e8f0a1b","text":"x"}`); rec.Code != http.StatusOK {
		t.Errorf("customer: status %d, want 200", rec.Code)
	}
}
