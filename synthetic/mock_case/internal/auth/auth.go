// Package auth tells the two credentials of mock-case apart. The runtime credential belongs to the core's case
// adapter and opens the case API only. The bench credential belongs to the benchmark runner and opens the truth
// and control API only. No other identity opens anything (REQ-ENV-002, REQ-ENV-008).
package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// Identity is who a request claims to be, as far as mock-case can tell.
type Identity int

const (
	None    Identity = iota // no credential, a malformed one, or one mock-case does not know
	Runtime                 // the core's case adapter
	Bench                   // the benchmark runner
)

func (i Identity) String() string { return [...]string{"none", "runtime", "bench"}[i] }

// Authenticator compares presented credentials against the configured ones. In the legacy mode there are two: the runtime credential and the bench
// credential. In scoped mode (Architect ruling R4) each tenant has its own runtime credential, the credential names the tenant, and the single runtime
// credential no longer opens anything, because it would be a credential for every tenant.
type Authenticator struct {
	runtime [32]byte
	bench   [32]byte
	tenants []tenantCredential // empty in the legacy mode
}

type tenantCredential struct {
	digest [32]byte
	tenant string
}

// New builds an Authenticator. Only digests are kept, and compared in constant time.
func New(runtimeToken, benchToken string) *Authenticator {
	return &Authenticator{runtime: sha256.Sum256([]byte(runtimeToken)), bench: sha256.Sum256([]byte(benchToken))}
}

// NewScoped builds an Authenticator in scoped mode. Every credential must be non-empty and different from every other: a tenant credential that equals the
// runtime or the bench credential, or another tenant's, would defeat the scoping.
func NewScoped(runtimeToken, benchToken string, tenantTokens map[string]string) (*Authenticator, error) {
	if len(tenantTokens) == 0 {
		return nil, errors.New("scoped mode needs at least one tenant credential")
	}
	seen := map[string]string{runtimeToken: "the runtime credential", benchToken: "the bench credential"}
	a := &Authenticator{runtime: sha256.Sum256([]byte(runtimeToken)), bench: sha256.Sum256([]byte(benchToken))}
	for tenant, token := range tenantTokens {
		if tenant == "" || token == "" {
			return nil, errors.New("a tenant credential needs a tenant id and a token")
		}
		if other, dup := seen[token]; dup {
			return nil, fmt.Errorf("the credential of tenant %q is the same as %s: credentials must all differ", tenant, other)
		}
		seen[token] = "tenant " + tenant
		a.tenants = append(a.tenants, tenantCredential{digest: sha256.Sum256([]byte(token)), tenant: tenant})
	}
	return a, nil
}

func bearerOf(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(h) <= len(prefix) || !strings.EqualFold(h[:len(prefix)], prefix) {
		return "", false
	}
	return h[len(prefix):], true
}

// Tenant reports the tenant bound to the presented credential, and false when the credential is not bound to one (the legacy runtime credential, the bench
// credential, or none).
func (a *Authenticator) Tenant(r *http.Request) (string, bool) {
	tok, ok := bearerOf(r)
	if !ok {
		return "", false
	}
	got := sha256.Sum256([]byte(tok))
	found, tenant := 0, ""
	for _, c := range a.tenants { // every credential is compared, in constant time each
		if subtle.ConstantTimeCompare(got[:], c.digest[:]) == 1 {
			found, tenant = 1, c.tenant
		}
	}
	return tenant, found == 1
}

// Identify reads "Authorization: Bearer <token>". A token in a query string or any other scheme is not read.
func (a *Authenticator) Identify(r *http.Request) Identity {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(h) <= len(prefix) || !strings.EqualFold(h[:len(prefix)], prefix) {
		return None
	}
	got := sha256.Sum256([]byte(h[len(prefix):]))
	isRuntime := subtle.ConstantTimeCompare(got[:], a.runtime[:])
	isBench := subtle.ConstantTimeCompare(got[:], a.bench[:])
	if _, scoped := a.Tenant(r); scoped {
		return Runtime
	}
	switch {
	case isRuntime == 1 && len(a.tenants) == 0:
		return Runtime // legacy mode only; in scoped mode the single credential opens nothing
	case isBench == 1:
		return Bench
	}
	return None
}
