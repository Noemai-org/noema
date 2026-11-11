package session_test

// REQ-API-004: every public endpoint requires a valid session (401 otherwise, in the §39.1 error envelope); REQ-API-002: the error is
// safe text and does not tell a forged token from an expired one; INV-12: the principal comes from the session only.

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/edge/session"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/ids"
)

func serve(t *testing.T, k keys) (http.Handler, *int, *session.Principal) {
	t.Helper()
	v, clk := verifier(t, k, epoch)
	calls := 0
	var seen session.Principal
	h := session.Middleware(v, ids.New(clk, nil))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		p, ok := session.FromContext(r.Context())
		if !ok {
			t.Error("no principal in the context of an authenticated request")
		}
		seen = p
		w.WriteHeader(http.StatusNoContent)
	}))
	return h, &calls, &seen
}

func do(h http.Handler, authz string, extra map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/conversations", strings.NewReader("{}"))
	if authz != "" {
		req.Header.Set("Authorization", authz)
	}
	for k, v := range extra {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func envelope(t *testing.T, rec *httptest.ResponseRecorder) contracts.ErrorsV2ErrorBody {
	t.Helper()
	var e contracts.ErrorsV2ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
		t.Fatalf("not the error envelope: %v: %s", err, rec.Body.String())
	}
	return e.Error
}

func TestNoCredentialIsUnauthenticatedAndABadOneIsAnInvalidSession(t *testing.T) {
	k := newKeys(t)
	h, calls, _ := serve(t, k)
	rec := do(h, "", nil)
	if rec.Code != http.StatusUnauthorized || envelope(t, rec).Code != contracts.ErrorsV2ErrorCodeUNAUTHENTICATED || envelope(t, rec).Retryable {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	forged := do(h, "Bearer "+mint(t, k.other, goodHeader(), goodClaims(epoch)), nil)
	expired := goodClaims(epoch)
	expired["iat"], expired["exp"] = epoch.Add(-time.Hour).Unix(), epoch.Add(-skew-time.Minute).Unix()
	old := do(h, "Bearer "+mint(t, k.priv, goodHeader(), expired), nil)
	for name, r := range map[string]*httptest.ResponseRecorder{"forged": forged, "expired": old, "wrong scheme": do(h, "Basic abc", nil), "bare token": do(h, mint(t, k.priv, goodHeader(), goodClaims(epoch)), nil)} {
		b := envelope(t, r)
		if r.Code != http.StatusUnauthorized || b.Retryable || (name != "bare token" && name != "wrong scheme" && b.Code != contracts.ErrorsV2ErrorCodeSESSIONINVALID) {
			t.Errorf("%s: %d %+v", name, r.Code, b)
		}
	}
	// a forged token and an expired one are told apart by nothing: same status, code and message
	fb, ob := envelope(t, forged), envelope(t, old)
	if fb.Code != ob.Code || fb.Message != ob.Message {
		t.Fatalf("the error tells a forged token from an expired one: %+v vs %+v", fb, ob)
	}
	if *calls != 0 {
		t.Fatalf("the handler ran %d times for unauthenticated requests", *calls)
	}
}

func TestEveryErrorCarriesACorrelationIDAndSafeText(t *testing.T) {
	k := newKeys(t)
	h, _, _ := serve(t, k)
	digits := regexp.MustCompile(`[0-9]{5,}`)
	rec := do(h, "Bearer junk", nil)
	b := envelope(t, rec)
	if !regexp.MustCompile(`^corr_[0-9A-Za-z]{2,32}$`).MatchString(string(b.CorrelationId)) || rec.Header().Get("Noema-Correlation-Id") != string(b.CorrelationId) {
		t.Fatalf("correlation: %+v header %q", b, rec.Header().Get("Noema-Correlation-Id"))
	}
	if digits.MatchString(b.Message) || strings.Contains(strings.ToLower(rec.Body.String()), "goroutine") || b.Message == "" {
		t.Fatalf("unsafe message: %q", b.Message)
	}
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("content type %q", rec.Header().Get("Content-Type"))
	}
	// an incoming correlation ID in the right shape is kept, so a call can be traced end to end
	kept := do(h, "Bearer junk", map[string]string{"Noema-Correlation-Id": "corr_ABC123"})
	if envelope(t, kept).CorrelationId != "corr_ABC123" {
		t.Fatalf("incoming correlation ID not kept: %+v", envelope(t, kept))
	}
	hostile := do(h, "Bearer junk", map[string]string{"Noema-Correlation-Id": "x\r\nSet-Cookie: a=b"})
	if hostile.Header().Get("Set-Cookie") != "" || !strings.HasPrefix(string(envelope(t, hostile).CorrelationId), "corr_") {
		t.Fatalf("a hostile correlation ID was echoed: %q", hostile.Header())
	}
}

func TestTheHandlerSeesThePrincipalOfTheTokenAndNoOneElse(t *testing.T) {
	k := newKeys(t)
	h, calls, seen := serve(t, k)
	rec := do(h, "Bearer "+mint(t, k.priv, goodHeader(), goodClaims(epoch)),
		map[string]string{"X-Principal-Id": "op_9", "X-Tenant-Id": tenantB, "Noema-Principal": "op_9"})
	if rec.Code != http.StatusNoContent || *calls != 1 {
		t.Fatalf("%d calls %d", rec.Code, *calls)
	}
	if seen.ID != "cust_1" || seen.Tenant != tenantA {
		t.Fatalf("principal from anywhere but the token: %+v", seen)
	}
}

// Ruling A1 (D7): routes are gated by the one role of the session. A route for ops and audit refuses customers and approvers, and the
// refusal is the same answer as "not found" (REQ-API-002), so a caller cannot tell a hidden route from a forbidden one.
func TestARouteForOpsAndAuditRefusesEveryOtherRole(t *testing.T) {
	k := newKeys(t)
	v, clk := verifier(t, k, epoch)
	calls := 0
	idm := ids.New(clk, nil)
	h := session.Middleware(v, idm)(session.RequireRole(idm, session.RoleOps, session.RoleAudit)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusNoContent)
	})))
	for _, r := range []session.Role{session.RoleOps, session.RoleAudit, session.RoleCustomer, session.RoleApprover} {
		c := goodClaims(epoch)
		c["roles"] = []string{string(r)}
		rec := do(h, "Bearer "+mint(t, k.priv, goodHeader(), c), nil)
		allowed := r == session.RoleOps || r == session.RoleAudit
		switch {
		case allowed && rec.Code != http.StatusNoContent:
			t.Errorf("%s: refused: %d", r, rec.Code)
		case !allowed && (rec.Code != http.StatusNotFound || envelope(t, rec).Code != contracts.ErrorsV2ErrorCodeNOTFOUNDORNOTAUTHORIZED || envelope(t, rec).Retryable):
			t.Errorf("%s: %d %s", r, rec.Code, rec.Body.String())
		}
	}
	if calls != 2 {
		t.Fatalf("the handler ran %d times, want 2 (ops and audit)", calls)
	}
}

// A route gated by role, mounted without Middleware, has no principal: it is refused, never allowed (deny by default).
func TestARoleGateWithoutASessionDeniesEverything(t *testing.T) {
	clk := clock.NewManual(epoch)
	idm := ids.New(clk, nil)
	called := false
	h := session.RequireRole(idm, session.RoleOps)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	rec := do(h, "", nil)
	if called || rec.Code != http.StatusUnauthorized || envelope(t, rec).Code != contracts.ErrorsV2ErrorCodeUNAUTHENTICATED {
		t.Fatalf("called %v, %d %s", called, rec.Code, rec.Body.String())
	}
	if session.RequireRole(idm) == nil {
		t.Fatal("a gate with no roles must still be a gate")
	}
	empty := session.RequireRole(idm)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	if empty.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil)); called {
		t.Fatal("a gate with no allowed roles let a request through")
	}
}

// Ruling A1, items 5 and 7: the token's tenant must equal the tenant of the database login the request runs on, and an ops token acts in
// its own tenant only. Across tenants the answer is a refusal, not a read.
func TestATokenOfOneTenantNeverRunsOnTheLoginOfAnother(t *testing.T) {
	p := session.Principal{ID: "op_1", Tenant: tenantA, Role: session.RoleOps}
	if err := session.CheckTenant(p, tenantA); err != nil {
		t.Fatalf("own tenant: %v", err)
	}
	for name, login := range map[string]string{"another tenant": tenantB, "empty login tenant": ""} {
		if err := session.CheckTenant(p, login); !errors.Is(err, session.ErrTenantMismatch) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := session.CheckTenant(session.Principal{Role: session.RoleOps}, ""); !errors.Is(err, session.ErrTenantMismatch) {
		t.Errorf("two empty tenants matched: %v", err)
	}
}

// RFC 9110 section 11.1: the authentication scheme name is case-insensitive (Codex review of noema#198). The credential is not.
func TestTheBearerSchemeNameIsCaseInsensitive(t *testing.T) {
	k := newKeys(t)
	h, calls, _ := serve(t, k)
	tok := mint(t, k.priv, goodHeader(), goodClaims(epoch))
	for _, scheme := range []string{"Bearer", "bearer", "BEARER", "bEaReR"} {
		if rec := do(h, scheme+" "+tok, nil); rec.Code != http.StatusNoContent {
			t.Errorf("%q: %d %s", scheme, rec.Code, rec.Body.String())
		}
	}
	if *calls != 4 {
		t.Fatalf("handler ran %d times, want 4", *calls)
	}
	for name, authz := range map[string]string{"no space": "Bearer" + tok, "two spaces": "Bearer  " + tok, "tab": "Bearer\t" + tok, "other scheme": "Basic " + tok, "empty credential": "Bearer ", "scheme only": "bearer"} {
		if rec := do(h, authz, nil); rec.Code == http.StatusNoContent {
			t.Errorf("%s: accepted", name)
		}
	}
}
