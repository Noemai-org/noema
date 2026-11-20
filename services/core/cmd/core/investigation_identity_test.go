package main

// STM-01 (the batch of 19 Nov, item 10): the investigation route takes the tenant and the principal from the verified session and from nothing else the request carries.
// INV-12, AGENT-10, REQ-SEC-004, REQ-API-001. Fakes are SYNTHETIC.

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Noemai-org/noema/services/core/edge/edgetest"
	"github.com/Noemai-org/noema/services/core/edge/session"
)

// The route asks the reader as the verified session's tenant and principal, and never as any other a request names: not in the query, not in a header (INV-12, AGENT-10,
// REQ-SEC-004). A mutant that lets the query's tenant through is killed here.
func TestTheInvestigationRouteIgnoresATenantOrPrincipalTheRequestNames(t *testing.T) {
	iss := edgetest.NewIssuer(t)
	fr := &fakeReader{state: "EVALUATING", found: true}
	h := invAPI(t, iss, fr)
	req := httptest.NewRequest(http.MethodGet, "/v1/investigations/"+invID+"?tenant=ten_zz&tenant_id=ten_zz&principal=prn_other&principal_id=prn_other", nil)
	req.Header.Set("Authorization", bearer(iss, session.RoleCustomer, invTenant))
	req.Header.Set("X-Tenant-ID", "ten_zz")
	req.Header.Set("X-Principal-ID", "prn_other")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if fr.calls != 1 || fr.tenant != invTenant || fr.principal != "prn_1" {
		t.Fatalf("status %d; reader asked %d times as %q %q; want once, as the session's %q and prn_1", rec.Code, fr.calls, fr.tenant, fr.principal, invTenant)
	}
}
