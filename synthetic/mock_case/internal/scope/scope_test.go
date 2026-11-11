package scope_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/synthetic/mock_case/internal/scope"
)

// Per-tenant runtime credentials (Architect ruling R4, 8 Nov 2026; RESIST X6 cross-tenant exposure, a zero-tolerance gate). A request authenticated with
// tenant A's credential may act only on tenant A: a read of another tenant's data looks exactly like a read of a tenant with no data, and a write that
// names another tenant is refused with 403. SYNTHETIC (AGENT-25).

// seen records what the wrapped handler received.
type seen struct {
	called bool
	query  string
	body   string
}

func stub(s *seen) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.called = true
		s.query = r.URL.Query().Get("tenant_id")
		b, _ := io.ReadAll(r.Body)
		s.body = string(b)
		w.WriteHeader(http.StatusOK)
	})
}

func wrap(s *seen, tenant string, bound bool) http.Handler {
	forbid := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) }
	return scope.Wrap(stub(s), func(*http.Request) (string, bool) { return tenant, bound }, forbid)
}

func do(h http.Handler, method, target, body string) int {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

func TestARequestForTheCredentialsOwnTenantPassesUntouched(t *testing.T) {
	var s seen
	h := wrap(&s, "ten_a", true)
	if code := do(h, "GET", "/v1/cases?tenant_id=ten_a&idempotency_key=k", ""); code != 200 || !s.called || s.query != "ten_a" {
		t.Fatalf("read: %d %+v", code, s)
	}
	s = seen{}
	body := `{"tenant_id":"ten_a","principal_id":"usr_1"}`
	if code := do(h, "POST", "/v1/cases", body); code != 200 || !s.called || s.body != body {
		t.Fatalf("write: %d %+v (the body must reach the handler byte for byte)", code, s)
	}
}

func TestAReadOfAnotherTenantIsServedAsAReadOfATenantWithNoData(t *testing.T) {
	var s seen
	h := wrap(&s, "ten_a", true)
	if code := do(h, "GET", "/v1/cases/case_1?tenant_id=ten_b", ""); code != 200 || !s.called {
		t.Fatalf("%d %+v", code, s)
	}
	if s.query != scope.AbsentTenant {
		t.Fatalf("the handler saw tenant %q; a wrong-tenant read must run as a read of the empty tenant %q so it looks absent", s.query, scope.AbsentTenant)
	}
}

func TestAWriteNamingAnotherTenantIsRefusedAndNeverReachesTheHandler(t *testing.T) {
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		var s seen
		h := wrap(&s, "ten_a", true)
		if code := do(h, method, "/v1/cases/case_1/route", `{"tenant_id":"ten_b","owner_queue":"DISPUTES"}`); code != 403 || s.called {
			t.Errorf("%s: %d called=%v, want 403 and the handler never called", method, code, s.called)
		}
	}
}

func TestARequestThatNamesNoTenantIsLeftToTheHandler(t *testing.T) {
	var s seen
	h := wrap(&s, "ten_a", true)
	if code := do(h, "GET", "/v1/cases?idempotency_key=k", ""); code != 200 || !s.called || s.query != "" {
		t.Fatalf("read with no tenant: %d %+v (the handler answers its own 400)", code, s)
	}
	s = seen{}
	if code := do(h, "POST", "/v1/cases", `{"principal_id":"usr_1"}`); code != 200 || !s.called {
		t.Fatalf("write with no tenant: %d %+v", code, s)
	}
}

func TestABodyThatIsNotJSONOrIsHugeIsLeftToTheHandlerWithoutBeingLost(t *testing.T) {
	for name, body := range map[string]string{"not json": `{"tenant_id":`, "array": `[1,2]`, "huge": `{"x":"` + strings.Repeat("a", 2<<20) + `"}`} {
		var s seen
		h := wrap(&s, "ten_a", true)
		code := do(h, "POST", "/v1/cases", body)
		if code != 200 || !s.called || s.body != body {
			t.Errorf("%s: %d called=%v bodyIntact=%v", name, code, s.called, s.body == body)
		}
	}
}

func TestACredentialNotBoundToATenantIsNotScoped(t *testing.T) {
	// The legacy single runtime credential (no per-tenant credentials configured) keeps working as before.
	var s seen
	h := wrap(&s, "", false)
	if code := do(h, "GET", "/v1/cases?tenant_id=ten_b", ""); code != 200 || s.query != "ten_b" {
		t.Fatalf("%d %+v", code, s)
	}
}

func TestTheAbsentTenantCannotBeASessionsOwnTenant(t *testing.T) {
	if !strings.HasPrefix(scope.AbsentTenant, "ten_") || strings.ToLower(scope.AbsentTenant) != scope.AbsentTenant {
		t.Fatalf("the reserved empty tenant %q must look like a tenant id", scope.AbsentTenant)
	}
}
