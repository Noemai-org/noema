// Package auth tells the two credentials of mock-case apart. The runtime credential belongs to the core's case
// adapter and opens the case API only. The bench credential belongs to the benchmark runner and opens the truth
// and control API only. No other identity opens anything (REQ-ENV-002, REQ-ENV-008).
package auth

import (
	"crypto/sha256"
	"crypto/subtle"
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

// Authenticator compares presented credentials against the two configured ones.
type Authenticator struct {
	runtime [32]byte
	bench   [32]byte
}

// New builds an Authenticator. Only digests are kept, and compared in constant time.
func New(runtimeToken, benchToken string) *Authenticator {
	return &Authenticator{runtime: sha256.Sum256([]byte(runtimeToken)), bench: sha256.Sum256([]byte(benchToken))}
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
	switch {
	case isRuntime == 1:
		return Runtime
	case isBench == 1:
		return Bench
	}
	return None
}
