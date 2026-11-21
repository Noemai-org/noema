// Package edgetest makes SYNTHETIC session tokens for tests of the edge and of the core process. Its key is generated per test and
// signs nothing outside the test that made it (AGENT-25). It is a test helper; no production package imports it.
package edgetest

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/edge/session"
)

// Kid, Audience and Tenant are the values the helper's verifier settings use.
const (
	Kid      = "idp-test-k1"
	Audience = "noema-core"
	Tenant   = "tnt_a"
)

// Issuer signs SYNTHETIC ES256 tokens with a key made for one test.
type Issuer struct {
	t    testing.TB
	priv *ecdsa.PrivateKey
}

// NewIssuer makes a key pair for the test.
func NewIssuer(t testing.TB) *Issuer {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return &Issuer{t: t, priv: k}
}

// Public returns the verification key.
func (i *Issuer) Public() *ecdsa.PublicKey { return &i.priv.PublicKey }

// PublicPEM returns the verification key as a PKIX PEM, the form the core reads from a file.
func (i *Issuer) PublicPEM() []byte {
	i.t.Helper()
	der, err := x509.MarshalPKIXPublicKey(&i.priv.PublicKey)
	if err != nil {
		i.t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
}

// Claims are the claims of a token; a test changes the field it is about.
type Claims struct {
	Sub    string   `json:"sub"`
	Aud    string   `json:"aud"`
	Iss    string   `json:"iss"`
	Tenant string   `json:"tenant_id"`
	Roles  []string `json:"roles"`
	Iat    int64    `json:"iat"`
	Exp    int64    `json:"exp"`
}

// Good returns the claims of a valid five-minute session for role at now.
func Good(now time.Time, role session.Role) Claims {
	return Claims{Sub: "prn_1", Aud: Audience, Iss: session.Issuer, Tenant: Tenant, Roles: []string{string(role)}, Iat: now.Unix(), Exp: now.Add(5 * time.Minute).Unix()}
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// Token signs c under kid.
func (i *Issuer) Token(kid string, c Claims) string {
	i.t.Helper()
	h, err := json.Marshal(map[string]string{"alg": "ES256", "typ": "JWT", "kid": kid})
	if err != nil {
		i.t.Fatal(err)
	}
	cb, err := json.Marshal(c)
	if err != nil {
		i.t.Fatal(err)
	}
	signing := b64(h) + "." + b64(cb)
	sum := sha256.Sum256([]byte(signing))
	r, s, err := ecdsa.Sign(rand.Reader, i.priv, sum[:])
	if err != nil {
		i.t.Fatal(err)
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	session.LowS(s).FillBytes(sig[32:])
	return signing + "." + b64(sig)
}
