package main

// EDG-01 / noema#601, ruling A243 (interim, relayed, not yet committed): GET /v1/investigations/{id} answers the current public stage of the caller's own investigation:
// {schema_version, state}, state one of the six public stages (A187), never an internal state name. The reader (a read-only store method, A243) is a port here; it returns the
// internal state of an investigation that belongs to this tenant AND this principal, and found=false otherwise. Absent, another principal's, another tenant's and a malformed
// ID are all the one not-found shape of errors.v3 (the same bytes as a route that does not exist). REQ-API-001, REQ-UI-001, INV-12. Fakes below are SYNTHETIC.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/edge"
	"github.com/Noemai-org/noema/services/core/edge/edgetest"
	"github.com/Noemai-org/noema/services/core/edge/session"
	"github.com/Noemai-org/noema/services/core/orchestrator"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/ids"
)

const invID = "inv_01ARZ3NDEKTSV4RRFFQ69G5FAV"

type fakeReader struct {
	state             orchestrator.State
	found             bool
	err               error
	calls             int
	tenant, principal string
	id                string
}

func (f *fakeReader) Read(_ context.Context, tenant, principal, id string) (orchestrator.State, bool, error) {
	f.calls++
	f.tenant, f.principal, f.id = tenant, principal, id
	return f.state, f.found, f.err
}

func invAPI(t *testing.T, iss *edgetest.Issuer, r InvestigationReader) http.Handler {
	t.Helper()
	clk := clock.NewManual(apiEpoch)
	e := sessionEnv(t, iss)
	e[edge.EnvTenants] = convTenant
	h, err := edge.Load(e.get, os.ReadFile, clk, ids.New(clk, nil), []edge.Route{investigationRoute(r)})
	if err != nil || h == nil {
		t.Fatalf("edge: %v, %v", h, err)
	}
	return h
}

func getInv(h http.Handler, authz, id string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/v1/investigations/"+id, nil)
	if authz != "" {
		req.Header.Set("Authorization", authz)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func errBody(t *testing.T, rec *httptest.ResponseRecorder) (code, corr string) {
	t.Helper()
	var env struct {
		Error struct {
			Code          string `json:"code"`
			CorrelationID string `json:"correlation_id"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("body %q: %v", rec.Body.String(), err)
	}
	return env.Error.Code, env.Error.CorrelationID
}

// The answer is exactly {schema_version, state}, and the reader is asked as the SESSION's tenant and principal and the path's ID (INV-12).
func TestTheStageIsAnsweredToTheOwnersSession(t *testing.T) {
	iss := edgetest.NewIssuer(t)
	fr := &fakeReader{state: "EVALUATING", found: true}
	h := invAPI(t, iss, fr)
	rec := getInv(h, bearer(iss, session.RoleCustomer, convTenant), invID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["schema_version"] != "1.0" || got["state"] != "working" {
		t.Errorf("body %v; want exactly {schema_version: 1.0, state: working}", got)
	}
	if fr.tenant != convTenant || fr.principal != "prn_1" || fr.id != invID || fr.calls != 1 {
		t.Errorf("reader asked %q %q %q (%d calls); want the session's tenant and principal and the path's ID, once", fr.tenant, fr.principal, fr.id, fr.calls)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("content type %q", ct)
	}
}

// Every internal state is answered as a public stage and its own name never appears in the body (A187).
func TestNoInternalStateNameLeavesTheRoute(t *testing.T) {
	iss := edgetest.NewIssuer(t)
	tbl, err := orchestrator.LoadTable()
	if err != nil {
		t.Fatal(err)
	}
	six := map[string]bool{"understanding": true, "needs_input": true, "needs_confirmation": true, "working": true, "answered": true, "unavailable": true}
	for _, s := range tbl.States() {
		h := invAPI(t, iss, &fakeReader{state: s, found: true})
		rec := getInv(h, bearer(iss, session.RoleCustomer, convTenant), invID)
		var got struct {
			State string `json:"state"`
		}
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &got) != nil || !six[got.State] || strings.Contains(rec.Body.String(), string(s)) {
			t.Errorf("%s: %d %s", s, rec.Code, rec.Body.String())
		}
	}
}

// Absent, another principal's, another tenant's (all found=false) and malformed IDs are the one not-found shape, the same bytes as a route that does not exist (no oracle).
func TestNotFoundIsOneShapeWhateverTheReason(t *testing.T) {
	iss := edgetest.NewIssuer(t)
	fr := &fakeReader{found: false}
	h := invAPI(t, iss, fr)
	auth := bearer(iss, session.RoleCustomer, convTenant)
	shape := func(rec *httptest.ResponseRecorder) string {
		var env map[string]map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
			t.Fatalf("body %q: %v", rec.Body.String(), err)
		}
		delete(env["error"], "correlation_id")
		b, _ := json.Marshal(env)
		return http.StatusText(rec.Code) + string(b)
	}
	unknown := shape(getInv(h, auth, "nope"))
	for name, id := range map[string]string{"absent": invID, "malformed": "inv_!!", "empty prefix": "x_01ARZ3NDEKTSV4RRFFQ69G5FAV", "too long": "inv_" + strings.Repeat("A", 40)} {
		rec := getInv(h, auth, id)
		code, _ := errBody(t, rec)
		if rec.Code != http.StatusNotFound || code != "NOT_FOUND_OR_NOT_AUTHORIZED" || shape(rec) != unknown {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body.String())
		}
	}
	// the malformed IDs never reached the store
	if fr.calls != 1 {
		t.Errorf("the reader was called %d times; want 1 (the well-formed absent ID only)", fr.calls)
	}
}

// Only a customer: ops, audit and approver get the not-found shape and the reader is never called; no session is 401.
func TestOnlyACustomerReadsAnInvestigation(t *testing.T) {
	iss := edgetest.NewIssuer(t)
	fr := &fakeReader{state: "RECEIVED", found: true}
	h := invAPI(t, iss, fr)
	for _, role := range []session.Role{session.RoleOps, session.RoleAudit, session.RoleApprover} {
		if rec := getInv(h, bearer(iss, role, convTenant), invID); rec.Code != http.StatusNotFound {
			t.Errorf("%s: %d, want 404", role, rec.Code)
		}
	}
	if rec := getInv(h, "", invID); rec.Code != http.StatusUnauthorized {
		t.Errorf("no session: %d", rec.Code)
	}
	if fr.calls != 0 {
		t.Errorf("a refused request called the reader %d times", fr.calls)
	}
}

// A store that fails is 503 DEPENDENCY_UNAVAILABLE with the request's correlation ID in body and header and none of the error's text; a state outside the map is 500
// INTERNAL_SAFETY_BLOCK and its name is not shown (fail closed, never a guess).
func TestFailuresAreSafeAndCarryTheCorrelationID(t *testing.T) {
	iss := edgetest.NewIssuer(t)
	auth := bearer(iss, session.RoleCustomer, convTenant)
	down := getInv(invAPI(t, iss, &fakeReader{err: errors.New("pgx: connection refused to postgres://trust_rw:secret@db")}), auth, invID)
	code, corr := errBody(t, down)
	if down.Code != http.StatusServiceUnavailable || code != "DEPENDENCY_UNAVAILABLE" || corr == "corr_NONE" || corr != down.Header().Get(session.CorrelationHeader) ||
		strings.Contains(down.Body.String(), "pgx") || strings.Contains(down.Body.String(), "secret") {
		t.Errorf("store failure: %d %s (header %q)", down.Code, down.Body.String(), down.Header().Get(session.CorrelationHeader))
	}
	odd := getInv(invAPI(t, iss, &fakeReader{state: "FROZEN", found: true}), auth, invID)
	code, corr = errBody(t, odd)
	if odd.Code != http.StatusInternalServerError || code != "INTERNAL_SAFETY_BLOCK" || corr != odd.Header().Get(session.CorrelationHeader) || strings.Contains(odd.Body.String(), "FROZEN") {
		t.Errorf("unknown state: %d %s", odd.Code, odd.Body.String())
	}
}

// With no reader the route refuses (503), it never answers a default stage.
func TestWithNoReaderTheRouteRefuses(t *testing.T) {
	iss := edgetest.NewIssuer(t)
	rec := getInv(invAPI(t, iss, nil), bearer(iss, session.RoleCustomer, convTenant), invID)
	if code, _ := errBody(t, rec); rec.Code != http.StatusServiceUnavailable || code != "DEPENDENCY_UNAVAILABLE" {
		t.Errorf("%d %s", rec.Code, rec.Body.String())
	}
}
