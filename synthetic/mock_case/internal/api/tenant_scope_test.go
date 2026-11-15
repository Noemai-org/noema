package api_test

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Noemai-org/noema/synthetic/mock_case/internal/api"
	"github.com/Noemai-org/noema/synthetic/mock_case/internal/config"
	"github.com/Noemai-org/noema/synthetic/mock_case/internal/store"
	"github.com/Noemai-org/noema/synthetic/mock_case/internal/testpg"
)

// Per-tenant runtime credentials (Architect ruling R4, 8 Nov 2026; RESIST X6 cross-tenant exposure, a zero-tolerance gate): a request authenticated with
// tenant A's credential may act only on tenant A. A read that names another tenant looks exactly like a read of a tenant with no data; a write that names
// another tenant is 403 and creates nothing; the single legacy runtime credential opens nothing in scoped mode. SYNTHETIC (AGENT-25). Runs against a real
// PostgreSQL, so it is evidenced by CI.

const (
	tokTenantA = "tenant-a-token-tenant-a-token-tenant-a-00"
	tokTenantB = "tenant-b-token-tenant-b-token-tenant-b-00"
)

// newScopedEnv is newEnv with one runtime credential per tenant (ten_a and ten_b). Duplicated on purpose: the harness builds its configuration inline.
func newScopedEnv(t testing.TB) *env {
	t.Helper()
	db := testpg.New(t)
	st, err := store.Open(context.Background(), db.AppDSN, db.AdminDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{"/app": "x", "/admin": "y", "/rt": runtimeToken, "/bt": benchToken, "/ta": tokTenantA, "/tb": tokTenantB}
	vars := map[string]string{"MOCK_CASE_BENCH_LISTEN": "127.0.0.1:0", "MOCK_CASE_DB_APP_PASSWORD_FILE": "/app", "MOCK_CASE_DB_ADMIN_PASSWORD_FILE": "/admin",
		"MOCK_CASE_RUNTIME_TOKEN_FILE": "/rt", "MOCK_CASE_BENCH_TOKEN_FILE": "/bt", "MOCK_CASE_TENANT_TOKEN_FILES": "ten_a=/ta,ten_b=/tb"}
	cfg, err := config.Load(func(k string) string { return vars[k] }, func(p string) ([]byte, error) {
		if v, ok := files[p]; ok {
			return []byte(v), nil
		}
		return nil, errors.New("no file " + p)
	})
	if err != nil {
		t.Fatal(err)
	}
	logs := &lockedBuffer{}
	srv := api.New(cfg, st, slog.New(slog.NewJSONHandler(logs, nil)))
	cs := httptest.NewServer(srv.CaseHandler())
	bs := httptest.NewServer(srv.BenchHandler())
	t.Cleanup(cs.Close)
	t.Cleanup(bs.Close)
	client := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{DisableKeepAlives: true}}
	return &env{t: t, db: db, st: st, srv: srv, caseURL: cs.URL, benchURL: bs.URL, client: client, logs: logs}
}

func (e *env) as(token, method, path string, hdr map[string]string, body any) resp {
	e.t.Helper()
	return e.do(method, e.caseURL+path, token, hdr, body)
}

func (e *env) caseCount() int {
	e.t.Helper()
	cases, _ := e.bench("GET", "/truth/v1/state", nil).json["cases"].([]any)
	return len(cases)
}

func TestATenantCredentialWorksForItsOwnTenant(t *testing.T) {
	e := newScopedEnv(t)
	created := e.as(tokTenantA, "POST", "/v1/cases", newKey("idk_a1"), caseBody()) // caseBody is tenant ten_a
	if created.status != 201 {
		t.Fatalf("create as tenant A for tenant A: %d %s", created.status, created.raw)
	}
	id := created.str("case_id")
	if r := e.as(tokTenantA, "GET", "/v1/cases/"+id+"?tenant_id=ten_a", nil, nil); r.status != 200 {
		t.Fatalf("read own case: %d %s", r.status, r.raw)
	}
	if r := e.as(tokTenantA, "GET", "/v1/cases?tenant_id=ten_a&idempotency_key=idk_a1", nil, nil); r.status != 200 || len(r.items()) != 1 {
		t.Fatalf("find own case by key: %d %s", r.status, r.raw)
	}
}

func TestAnotherTenantsReadLooksAbsent(t *testing.T) {
	e := newScopedEnv(t)
	created := e.as(tokTenantA, "POST", "/v1/cases", newKey("idk_a1"), caseBody())
	if created.status != 201 {
		t.Fatalf("setup: %d %s", created.status, created.raw)
	}
	id := created.str("case_id")

	// Tenant B asks for tenant A's real case. The answer must be the answer to a case that does not exist.
	foreign := e.as(tokTenantB, "GET", "/v1/cases/"+id+"?tenant_id=ten_a", nil, nil)
	missing := e.as(tokTenantB, "GET", "/v1/cases/case_no_such_case?tenant_id=ten_b", nil, nil)
	if foreign.status != 404 || missing.status != 404 || foreign.errCode() != missing.errCode() {
		t.Fatalf("foreign read %d/%s, missing read %d/%s: they must be the same absence", foreign.status, foreign.errCode(), missing.status, missing.errCode())
	}
	if strings.Contains(string(foreign.raw), id) || strings.Contains(string(foreign.raw), "ten_a") {
		t.Fatalf("the foreign read leaks the case or the tenant: %s", foreign.raw)
	}

	// Tenant B finds by tenant A's key: an empty complete page, the shape of a key that matches nothing.
	byKey := e.as(tokTenantB, "GET", "/v1/cases?tenant_id=ten_a&idempotency_key=idk_a1", nil, nil)
	none := e.as(tokTenantB, "GET", "/v1/cases?tenant_id=ten_b&idempotency_key=idk_a1", nil, nil)
	if byKey.status != 200 || len(byKey.items()) != 0 || none.status != 200 || len(none.items()) != 0 {
		t.Fatalf("find by foreign key: %d %s; find by an unknown key: %d %s", byKey.status, byKey.raw, none.status, none.raw)
	}
	if string(byKey.raw) != string(none.raw) {
		t.Fatalf("a foreign find and an empty find differ:\n%s\n%s", byKey.raw, none.raw)
	}
}

func TestAWriteNamingAnotherTenantIsForbiddenAndCreatesNothing(t *testing.T) {
	e := newScopedEnv(t)
	before := e.caseCount()
	r := e.as(tokTenantB, "POST", "/v1/cases", newKey("idk_x1"), caseBody()) // tenant B's credential, tenant A's body
	if r.status != 403 || r.errCode() != "TENANT_MISMATCH" {
		t.Fatalf("a write for another tenant: %d %s, want 403 TENANT_MISMATCH", r.status, r.raw)
	}
	if after := e.caseCount(); after != before {
		t.Fatalf("a refused write created a case: %d before, %d after", before, after)
	}
	created := e.as(tokTenantA, "POST", "/v1/cases", newKey("idk_a1"), caseBody())
	if created.status != 201 {
		t.Fatalf("setup: %d", created.status)
	}
	route := e.as(tokTenantB, "POST", "/v1/cases/"+created.str("case_id")+"/route", nil, map[string]any{"tenant_id": "ten_a", "owner_queue": "DISPUTES", "expected_version_token": created.str("version_token")})
	if route.status != 403 || route.errCode() != "TENANT_MISMATCH" {
		t.Fatalf("a route for another tenant: %d %s, want 403", route.status, route.raw)
	}
	if got := e.as(tokTenantA, "GET", "/v1/cases/"+created.str("case_id")+"?tenant_id=ten_a", nil, nil); got.str("owner_queue") == "DISPUTES" {
		t.Fatalf("the refused route changed the case: %s", got.raw)
	}
}

func TestInScopedModeTheSingleRuntimeCredentialOpensNothing(t *testing.T) {
	e := newScopedEnv(t)
	if r := e.as(runtimeToken, "GET", "/v1/cases?tenant_id=ten_a&idempotency_key=k", nil, nil); r.status != 401 {
		t.Fatalf("the legacy runtime credential in scoped mode: %d %s, want 401 (it would be a credential for every tenant)", r.status, r.raw)
	}
	if r := e.as(tokTenantA, "GET", "/v1/cases?tenant_id=ten_a&idempotency_key=k", nil, nil); r.status != 200 {
		t.Fatalf("a tenant credential: %d", r.status)
	}
}

func TestATenantCredentialDoesNotOpenTheBenchApiAndTheBenchCredentialStillDoes(t *testing.T) {
	e := newScopedEnv(t)
	if r := e.do("GET", e.benchURL+"/truth/v1/state", tokTenantA, nil, nil); r.status != 403 && r.status != 401 {
		t.Fatalf("a tenant credential on the bench API: %d, want 401 or 403", r.status)
	}
	if r := e.bench("GET", "/truth/v1/state", nil); r.status != 200 {
		t.Fatalf("the bench credential: %d", r.status)
	}
}
