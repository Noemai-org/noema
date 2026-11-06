// Package session verifies the synthetic IdP's session tokens at the core's edge (REQ-SEC-001, REQ-API-004, INV-12).
//
// A token is a JWT signed with ES256 (ECDSA P-256, SHA-256). The core checks the signature against a key it knows by `kid`, the audience,
// the tenant, the expiry, the issue time and a maximum lifetime, and takes the principal from `sub` and nothing else. Anything it cannot
// check is a rejection: there is no partial acceptance, and every failure is one error, ErrInvalid, so a caller learns nothing about which
// check failed. Time comes from platform/clock only (INV-21).
//
// The contract is the Architect's ruling A1 (D7 of DEC-2026-11-05, docs#115): issuer `noema-poc-idp`, exact match; claims `sub`, `aud`,
// `exp`, `iat`, `iss`, `tenant_id` and `roles`, all required; exactly one role from customer, ops, approver, audit; ES256 only, with a
// `kid` from the configured set and no other header member than alg, kid and typ; a lifetime ceiling of 15 minutes. There is no `jti`, so a
// stolen token works until `exp`: a named residual risk at that lifetime (A1 item 8). See status/EDG-01.md.
package session

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/Noemai-org/noema/services/core/platform/clock"
)

// ErrInvalid is returned for every token the core will not accept, whatever the reason.
var ErrInvalid = errors.New("session: invalid token")

// Role is a role a session carries.
type Role string

const (
	RoleCustomer Role = "customer"
	RoleOps      Role = "ops"
	RoleApprover Role = "approver"
	RoleAudit    Role = "audit"
)

func knownRole(r Role) bool {
	return r == RoleCustomer || r == RoleOps || r == RoleApprover || r == RoleAudit
}

// Issuer is the one issuer the core accepts (ruling A1). A token's `iss` must equal it exactly.
const Issuer = "noema-poc-idp"

// CeilingLifetime is the longest MaxLifetime a verifier may be configured with (ruling A1, item 4).
const CeilingLifetime = 15 * time.Minute

// maxSubLen bounds `sub` (ruling A1, item 6).
const maxSubLen = 128

// Principal is the authenticated caller. Nothing else in a request may name one (INV-12).
type Principal struct {
	ID        string
	Tenant    string
	Role      Role // exactly one (ruling A1)
	IssuedAt  time.Time
	ExpiresAt time.Time
}

// Config is what a Verifier needs. Every field is required.
type Config struct {
	Keys        map[string]*ecdsa.PublicKey // by kid
	Audience    string
	Tenants     []string      // the tenants this deployment serves
	MaxLifetime time.Duration // the longest a token may live (exp - iat); set, positive and at most CeilingLifetime
	Skew        time.Duration // how far in the future an iat may be; configuration with no default (zero allows none), below MaxLifetime
	Clock       clock.Clock
}

// Verifier checks tokens. It holds no mutable state.
type Verifier struct {
	keys     map[string]*ecdsa.PublicKey
	audience string
	tenants  map[string]bool
	maxLife  time.Duration
	skew     time.Duration
	clock    clock.Clock
}

// NewVerifier refuses to start without keys, an audience, tenants, a maximum lifetime and a clock, and accepts only P-256 keys.
func NewVerifier(c Config) (*Verifier, error) {
	if len(c.Keys) == 0 || c.Audience == "" || len(c.Tenants) == 0 || c.MaxLifetime <= 0 || c.Clock == nil {
		return nil, errors.New("session: keys, audience, tenants, a positive maximum lifetime and a clock are all required")
	}
	if c.MaxLifetime > CeilingLifetime {
		return nil, errors.New("session: the maximum lifetime is above the ceiling of 15 minutes")
	}
	if c.Skew < 0 || c.Skew >= c.MaxLifetime {
		return nil, errors.New("session: the skew must be zero or more and below the maximum lifetime")
	}
	v := &Verifier{keys: make(map[string]*ecdsa.PublicKey, len(c.Keys)), audience: c.Audience, tenants: make(map[string]bool, len(c.Tenants)), maxLife: c.MaxLifetime, skew: c.Skew, clock: c.Clock}
	for kid, k := range c.Keys {
		if kid == "" || k == nil || k.Curve != elliptic.P256() {
			return nil, fmt.Errorf("session: key %q is missing or not P-256", kid)
		}
		v.keys[kid] = k
	}
	for _, t := range c.Tenants {
		if t == "" {
			return nil, errors.New("session: an empty tenant")
		}
		v.tenants[t] = true
	}
	return v, nil
}

type header struct {
	Alg string `json:"alg"`
	Kid string `json:"kid"`
}

type claims struct {
	Sub    string   `json:"sub"`
	Aud    string   `json:"aud"`
	Iss    string   `json:"iss"`
	Tenant string   `json:"tenant_id"`
	Roles  []string `json:"roles"`
	Iat    *int64   `json:"iat"`
	Exp    *int64   `json:"exp"`
}

func invalid(why string) error { return fmt.Errorf("%w: %s", ErrInvalid, why) }

const sigLen = 64

// b64 is strict: the unused trailing bits of a segment must be zero, so one token has exactly one spelling. A lenient decoder ignores them,
// and a token with a respelled last character would verify as the same bytes (found by the fuzz test in CI, 4 Nov 2026).
var b64 = base64.RawURLEncoding.Strict()

// Verify returns the principal of a valid token, or an error that satisfies errors.Is(err, ErrInvalid).
func (v *Verifier) Verify(token string) (Principal, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Principal{}, invalid("not three parts")
	}
	hb, err := b64.DecodeString(parts[0])
	if err != nil {
		return Principal{}, invalid("header encoding")
	}
	var h header
	if json.Unmarshal(hb, &h) != nil {
		return Principal{}, invalid("header")
	}
	if !headerMembersAllowed(hb) {
		return Principal{}, invalid("header member") // jwk, jku, x5u, x5c, crit and anything else that could name or change the key
	}
	if h.Alg != "ES256" {
		return Principal{}, invalid("algorithm")
	}
	key, ok := v.keys[h.Kid]
	if !ok {
		return Principal{}, invalid("key")
	}
	sig, err := b64.DecodeString(parts[2])
	if err != nil || len(sig) != sigLen {
		return Principal{}, invalid("signature")
	}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	r, s := new(big.Int).SetBytes(sig[:sigLen/2]), new(big.Int).SetBytes(sig[sigLen/2:])
	if !ecdsa.Verify(key, sum[:], r, s) {
		return Principal{}, invalid("signature")
	}
	cb, err := b64.DecodeString(parts[1])
	if err != nil {
		return Principal{}, invalid("claims encoding")
	}
	var c claims
	if json.Unmarshal(cb, &c) != nil {
		return Principal{}, invalid("claims")
	}
	if c.Sub == "" || c.Iat == nil || c.Exp == nil || c.Aud != v.audience || c.Iss != Issuer || len(c.Sub) > maxSubLen || !v.tenants[c.Tenant] || len(c.Roles) != 1 {
		return Principal{}, invalid("claims")
	}
	iat, exp := time.Unix(*c.Iat, 0).UTC(), time.Unix(*c.Exp, 0).UTC()
	now := v.clock.Now()
	if iat.After(now.Add(v.skew)) || !exp.After(now) || exp.Sub(iat) > v.maxLife || !exp.After(iat) {
		return Principal{}, invalid("time")
	}
	role := Role(c.Roles[0])
	if !knownRole(role) {
		return Principal{}, invalid("role")
	}
	return Principal{ID: c.Sub, Tenant: c.Tenant, Role: role, IssuedAt: iat, ExpiresAt: exp}, nil
}

// headerMembersAllowed accepts only the members alg, kid and typ. A token that names its own key (jwk, jku, x5u, x5c) or carries critical
// extensions (crit) is refused, as is any member the core does not understand.
func headerMembersAllowed(hb []byte) bool {
	var m map[string]json.RawMessage
	if json.Unmarshal(hb, &m) != nil {
		return false
	}
	for name := range m {
		if name != "alg" && name != "kid" && name != "typ" {
			return false
		}
	}
	return true
}
