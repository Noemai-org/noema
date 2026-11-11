package config

import (
	"strings"
	"testing"
)

// Per-tenant runtime credentials (Architect ruling R4, 8 Nov 2026): MOCK_CASE_TENANT_TOKEN_FILES = "tenant=path,tenant=path". Each token comes from a file,
// is long enough, and differs from every other credential; the reserved empty tenant is never issued a credential. SYNTHETIC (AGENT-25).

const tokC = "cccccccccccccccccccccccccccccccccccccccc"

func withTenants(spec string, extra map[string]string) (*Config, error) {
	fs := map[string]string{"/ta": tokC + "\n", "/tb": "dddddddddddddddddddddddddddddddddddddddd\n"}
	for k, v := range good {
		fs[k] = v
	}
	for k, v := range extra {
		fs[k] = v
	}
	return Load(env(map[string]string{"MOCK_CASE_TENANT_TOKEN_FILES": spec}), files(fs))
}

func TestTenantCredentialsAreReadFromFiles(t *testing.T) {
	c, err := withTenants("ten_a=/ta, ten_b=/tb", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.TenantTokens) != 2 || c.TenantTokens["ten_a"] != tokC || !strings.HasPrefix(c.TenantTokens["ten_b"], "dddd") {
		t.Fatalf("%+v", c.TenantTokens)
	}
}

func TestWithoutTheVariableThereAreNoTenantCredentialsAndTheLegacyModeStands(t *testing.T) {
	c, err := Load(env(nil), files(good))
	if err != nil || len(c.TenantTokens) != 0 {
		t.Fatalf("%v %+v", err, c.TenantTokens)
	}
}

func TestBadTenantCredentialSpecsAreRefused(t *testing.T) {
	for name, tc := range map[string]struct {
		spec  string
		extra map[string]string
	}{
		"no equals sign":                          {"ten_a", nil},
		"empty tenant":                            {"=/ta", nil},
		"empty path":                              {"ten_a=", nil},
		"a tenant listed twice":                   {"ten_a=/ta,ten_a=/tb", nil},
		"a file that does not exist":              {"ten_a=/nope", nil},
		"a short token":                           {"ten_a=/ta", map[string]string{"/ta": "short\n"}},
		"an empty token file":                     {"ten_a=/ta", map[string]string{"/ta": "\n"}},
		"two tenants share a token":               {"ten_a=/ta,ten_b=/tb", map[string]string{"/tb": tokC + "\n"}},
		"a tenant token equals the runtime token": {"ten_a=/ta", map[string]string{"/ta": tokA + "\n"}},
		"a tenant token equals the bench token":   {"ten_a=/ta", map[string]string{"/ta": tokB + "\n"}},
		"a tenant id that is not an id":           {"te nant=/ta", nil},
		"the reserved empty tenant":               {"ten_scope_absent=/ta", nil},
	} {
		if _, err := withTenants(tc.spec, tc.extra); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestACredentialValueIsNeverTakenFromTheEnvironment(t *testing.T) {
	// The variable holds paths only. A value that looks like a token in place of a path is read as a path and fails.
	if _, err := withTenants("ten_a="+tokC, nil); err == nil {
		t.Fatal("a token given in place of a path must fail")
	}
}
