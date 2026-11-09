package auth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Noemai-org/noema/synthetic/mock_case/internal/auth"
)

// Per-tenant runtime credentials (Architect ruling R4, 8 Nov 2026). In scoped mode each tenant has its own credential and the credential names the tenant;
// the single legacy runtime credential no longer opens the case API, because it would be a credential for every tenant. SYNTHETIC (AGENT-25).

const (
	rt    = "runtime-token-runtime-token-runtime-token"
	bench = "bench-token-bench-token-bench-token-xx"
	tokA  = "tenant-a-token-tenant-a-token-tenant-a-00"
	tokB  = "tenant-b-token-tenant-b-token-tenant-b-00"
)

func req(token string) *http.Request {
	r := httptest.NewRequest("GET", "/v1/cases", nil)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	return r
}

func scoped(t *testing.T) *auth.Authenticator {
	t.Helper()
	a, err := auth.NewScoped(rt, bench, map[string]string{"ten_a": tokA, "ten_b": tokB})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestATenantCredentialOpensTheCaseApiAndNamesItsTenant(t *testing.T) {
	a := scoped(t)
	for tok, want := range map[string]string{tokA: "ten_a", tokB: "ten_b"} {
		if got := a.Identify(req(tok)); got != auth.Runtime {
			t.Errorf("identity of a tenant credential = %v, want runtime", got)
		}
		if tenant, ok := a.Tenant(req(tok)); !ok || tenant != want {
			t.Errorf("tenant = %q, %v; want %q", tenant, ok, want)
		}
	}
}

func TestInScopedModeTheLegacyRuntimeCredentialOpensNothing(t *testing.T) {
	a := scoped(t)
	if got := a.Identify(req(rt)); got != auth.None {
		t.Fatalf("the legacy runtime credential gave %v in scoped mode, want none: it would be a credential for every tenant", got)
	}
	if _, ok := a.Tenant(req(rt)); ok {
		t.Fatal("the legacy credential must not be bound to a tenant")
	}
}

func TestTheBenchCredentialStaysTheBenchAndIsNeverBoundToATenant(t *testing.T) {
	a := scoped(t)
	if got := a.Identify(req(bench)); got != auth.Bench {
		t.Fatalf("bench = %v", got)
	}
	if _, ok := a.Tenant(req(bench)); ok {
		t.Fatal("the bench credential must not be bound to a tenant")
	}
}

func TestAnUnknownOrMissingCredentialIsNoOneInScopedMode(t *testing.T) {
	a := scoped(t)
	for _, tok := range []string{"", "nope", tokA + "x", tokA[:len(tokA)-1]} {
		if got := a.Identify(req(tok)); got != auth.None {
			t.Errorf("token %q: %v, want none", tok, got)
		}
		if _, ok := a.Tenant(req(tok)); ok {
			t.Errorf("token %q is bound to a tenant", tok)
		}
	}
}

func TestWithoutTenantCredentialsTheLegacyModeIsUnchanged(t *testing.T) {
	a := auth.New(rt, bench)
	if got := a.Identify(req(rt)); got != auth.Runtime {
		t.Fatalf("legacy runtime = %v", got)
	}
	if _, ok := a.Tenant(req(rt)); ok {
		t.Fatal("the legacy credential is not bound to a tenant")
	}
}

func TestScopedModeRefusesCredentialsThatAreNotDistinct(t *testing.T) {
	cases := map[string]map[string]string{
		"two tenants share a token":             {"ten_a": tokA, "ten_b": tokA},
		"a tenant token equals the runtime one": {"ten_a": rt},
		"a tenant token equals the bench one":   {"ten_a": bench},
		"an empty tenant id":                    {"": tokA},
		"an empty token":                        {"ten_a": ""},
		"no tenants at all":                     {},
	}
	for name, m := range cases {
		if _, err := auth.NewScoped(rt, bench, m); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestScopedModeRefusesARuntimeCredentialEqualToTheBenchOne(t *testing.T) {
	// Codex finding 2 on noema#365: with equal runtime and bench values the map in NewScoped collapsed them and the shared credential was identified as the bench.
	// The constructor must refuse it itself, not rely on the config loader.
	for name, c := range map[string][2]string{"equal": {bench, bench}, "both empty": {"", ""}, "runtime empty": {"", bench}, "bench empty": {rt, ""}} {
		if _, err := auth.NewScoped(c[0], c[1], map[string]string{"ten_a": tokA}); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}
