package session_test

// REQ-SEC-001 (PERD): the core validates a synthetic IdP's ES256 session token: signature, expiry, audience and tenant. Tokens that
// are forged, expired, issued for the wrong audience or for the wrong tenant are rejected 100% of the time. The token's `sub` is the
// only principal source (INV-12). REQ-API-004: every public call needs a valid session; the lifetime is bounded (REC 15 minutes, an
// input here, not a literal). Keys and tokens are SYNTHETIC, made in the test.

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	mrand "math/rand"
	"strings"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/edge/session"
	"github.com/Noemai-org/noema/services/core/platform/clock"
)

const (
	aud      = "noema-core"
	tenantA  = "tnt_a"
	tenantB  = "tnt_b"
	kid      = "idp-k1"
	lifetime = 15 * time.Minute
)

var epoch = time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)

type keys struct{ priv, other *ecdsa.PrivateKey }

func newKeys(t *testing.T) keys {
	t.Helper()
	a, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	b, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return keys{a, b}
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// mint signs claims with key under header; a nil claims value is omitted so a test can build a malformed token.
func mint(t *testing.T, key *ecdsa.PrivateKey, header, claims map[string]any) string {
	t.Helper()
	h, _ := json.Marshal(header)
	c, _ := json.Marshal(claims)
	signing := b64(h) + "." + b64(c)
	sum := sha256.Sum256([]byte(signing))
	r, s, err := ecdsa.Sign(rand.Reader, key, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return signing + "." + b64(sig)
}

func goodClaims(now time.Time) map[string]any {
	return map[string]any{"sub": "cust_1", "aud": aud, "tenant_id": tenantA, "roles": []string{"customer"},
		"iat": now.Unix(), "exp": now.Add(5 * time.Minute).Unix()}
}

func goodHeader() map[string]any { return map[string]any{"alg": "ES256", "typ": "JWT", "kid": kid} }

func verifier(t *testing.T, k keys, now time.Time) (*session.Verifier, *clock.Manual) {
	t.Helper()
	clk := clock.NewManual(now)
	v, err := session.NewVerifier(session.Config{Keys: map[string]*ecdsa.PublicKey{kid: &k.priv.PublicKey}, Audience: aud,
		Tenants: []string{tenantA}, MaxLifetime: lifetime, Clock: clk})
	if err != nil {
		t.Fatal(err)
	}
	return v, clk
}

func TestAValidTokenGivesThePrincipalFromSubAndNothingElse(t *testing.T) {
	k := newKeys(t)
	v, _ := verifier(t, k, epoch)
	p, err := v.Verify(mint(t, k.priv, goodHeader(), goodClaims(epoch)))
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != "cust_1" || p.Tenant != tenantA || len(p.Roles) != 1 || p.Roles[0] != session.RoleCustomer ||
		!p.IssuedAt.Equal(epoch) || !p.ExpiresAt.Equal(epoch.Add(5*time.Minute)) {
		t.Fatalf("%+v", p)
	}
}

func TestEveryKindOfBadTokenIsRejectedAsInvalid(t *testing.T) {
	k := newKeys(t)
	v, clk := verifier(t, k, epoch)
	mod := func(f func(map[string]any)) map[string]any {
		c := goodClaims(epoch)
		f(c)
		return c
	}
	good := mint(t, k.priv, goodHeader(), goodClaims(epoch))
	parts := strings.Split(good, ".")
	flipped := []byte(parts[2])
	flipped[3] ^= 1
	cases := map[string]string{
		"forged: signed by another key":          mint(t, k.other, goodHeader(), goodClaims(epoch)),
		"signature bit flipped":                  parts[0] + "." + parts[1] + "." + string(flipped),
		"payload swapped under a good signature": parts[0] + "." + b64([]byte(`{"sub":"op_9","aud":"noema-core","tenant_id":"tnt_a","roles":["operator"],"iat":1,"exp":9999999999}`)) + "." + parts[2],
		"alg none":                               parts[0][:0] + b64([]byte(`{"alg":"none","typ":"JWT","kid":"idp-k1"}`)) + "." + parts[1] + ".",
		"alg HS256":                              mint(t, k.priv, map[string]any{"alg": "HS256", "typ": "JWT", "kid": kid}, goodClaims(epoch)),
		"unknown kid":                            mint(t, k.priv, map[string]any{"alg": "ES256", "typ": "JWT", "kid": "other"}, goodClaims(epoch)),
		"no kid":                                 mint(t, k.priv, map[string]any{"alg": "ES256", "typ": "JWT"}, goodClaims(epoch)),
		"wrong audience":                         mint(t, k.priv, goodHeader(), mod(func(c map[string]any) { c["aud"] = "other" })),
		"wrong tenant":                           mint(t, k.priv, goodHeader(), mod(func(c map[string]any) { c["tenant_id"] = tenantB })),
		"no tenant":                              mint(t, k.priv, goodHeader(), mod(func(c map[string]any) { delete(c, "tenant_id") })),
		"no sub":                                 mint(t, k.priv, goodHeader(), mod(func(c map[string]any) { delete(c, "sub") })),
		"empty sub":                              mint(t, k.priv, goodHeader(), mod(func(c map[string]any) { c["sub"] = "" })),
		"no exp":                                 mint(t, k.priv, goodHeader(), mod(func(c map[string]any) { delete(c, "exp") })),
		"already expired": mint(t, k.priv, goodHeader(), mod(func(c map[string]any) {
			c["exp"] = epoch.Add(-time.Second).Unix()
			c["iat"] = epoch.Add(-time.Minute).Unix()
		})),
		"issued in the future": mint(t, k.priv, goodHeader(), mod(func(c map[string]any) {
			c["iat"] = epoch.Add(time.Hour).Unix()
			c["exp"] = epoch.Add(2 * time.Hour).Unix()
		})),
		"lifetime longer than the maximum": mint(t, k.priv, goodHeader(), mod(func(c map[string]any) { c["exp"] = epoch.Add(lifetime + time.Second).Unix() })),
		"unknown role":                     mint(t, k.priv, goodHeader(), mod(func(c map[string]any) { c["roles"] = []string{"root"} })),
		"no roles":                         mint(t, k.priv, goodHeader(), mod(func(c map[string]any) { c["roles"] = []string{} })),
		"not three parts":                  "abc.def",
		"empty":                            "",
		"bad base64":                       "!!!.???.***",
		"payload is not JSON":              parts[0] + "." + b64([]byte("nope")) + "." + parts[2],
		"signature of the wrong length":    parts[0] + "." + parts[1] + "." + b64([]byte("short")),
	}
	for name, tok := range cases {
		if _, err := v.Verify(tok); !errors.Is(err, session.ErrInvalid) {
			t.Errorf("%s: got %v, want ErrInvalid", name, err)
		}
	}
	// a good token stops being good once the clock passes its expiry
	if _, err := v.Verify(good); err != nil {
		t.Fatal(err)
	}
	clk.Advance(5*time.Minute + time.Second)
	if _, err := v.Verify(good); !errors.Is(err, session.ErrInvalid) {
		t.Fatalf("an expired token must be rejected: %v", err)
	}
}

// Token fuzzing (REQ-SEC-001): thousands of seeded random mutations of a valid token are all rejected; none is accepted as another principal.
// The boundaries on their own: a token that expires exactly now is expired, and one issued a second from now is not yet valid (each
// with a lifetime inside the maximum, so no other check can reject it).
func TestTimeBoundariesAreExact(t *testing.T) {
	k := newKeys(t)
	v, _ := verifier(t, k, epoch)
	exactlyNow := goodClaims(epoch)
	exactlyNow["iat"], exactlyNow["exp"] = epoch.Add(-time.Minute).Unix(), epoch.Unix()
	if _, err := v.Verify(mint(t, k.priv, goodHeader(), exactlyNow)); !errors.Is(err, session.ErrInvalid) {
		t.Errorf("a token that expires now must be rejected: %v", err)
	}
	oneSecondLeft := goodClaims(epoch)
	oneSecondLeft["iat"], oneSecondLeft["exp"] = epoch.Add(-time.Minute).Unix(), epoch.Add(time.Second).Unix()
	if _, err := v.Verify(mint(t, k.priv, goodHeader(), oneSecondLeft)); err != nil {
		t.Errorf("a token with a second left is valid: %v", err)
	}
	notYet := goodClaims(epoch)
	notYet["iat"], notYet["exp"] = epoch.Add(time.Second).Unix(), epoch.Add(time.Minute).Unix()
	if _, err := v.Verify(mint(t, k.priv, goodHeader(), notYet)); !errors.Is(err, session.ErrInvalid) {
		t.Errorf("a token issued in the future must be rejected: %v", err)
	}
	maxLife := goodClaims(epoch)
	maxLife["iat"], maxLife["exp"] = epoch.Unix(), epoch.Add(lifetime).Unix()
	if _, err := v.Verify(mint(t, k.priv, goodHeader(), maxLife)); err != nil {
		t.Errorf("a token living exactly the maximum is valid: %v", err)
	}
}

// Regression (found by the fuzz test in CI, 4 Nov 2026): the last character of a base64url segment carries unused bits that a lenient decoder
// ignores, so a token whose final signature character is respelled still decodes to the same bytes and verified. One token must have one
// spelling (a cache or a deny list keyed on the token string would otherwise be bypassed), so the decoder is strict.
func TestARespelledButEquivalentTokenIsRejected(t *testing.T) {
	k := newKeys(t)
	v, _ := verifier(t, k, epoch)
	good := mint(t, k.priv, goodHeader(), goodClaims(epoch))
	if _, err := v.Verify(good); err != nil {
		t.Fatal(err)
	}
	alphabet := "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	last := good[len(good)-1]
	respelled := 0
	for i := 0; i < len(alphabet); i++ {
		c := alphabet[i]
		if c == last {
			continue
		}
		tok := good[:len(good)-1] + string(c)
		sigBytes, err := base64.RawURLEncoding.DecodeString(strings.Split(tok, ".")[2])
		orig, _ := base64.RawURLEncoding.DecodeString(strings.Split(good, ".")[2])
		if err == nil && bytes.Equal(sigBytes, orig) { // a lenient decoder reads this as the same signature
			respelled++
			if _, err := v.Verify(tok); !errors.Is(err, session.ErrInvalid) {
				t.Errorf("a respelling of the signature (%q) was accepted: %v", string(c), err)
			}
		}
	}
	if respelled == 0 {
		t.Fatal("the test found no equivalent respelling; it proves nothing")
	}
}

func TestFuzzedTokensAreNeverAccepted(t *testing.T) {
	k := newKeys(t)
	v, _ := verifier(t, k, epoch)
	good := mint(t, k.priv, goodHeader(), goodClaims(epoch))
	rng := mrand.New(mrand.NewSource(20261104))
	const rounds = 20_000
	accepted := 0
	for n := 0; n < rounds; n++ {
		b := []byte(good)
		for flips := 1 + rng.Intn(3); flips > 0; flips-- {
			b[rng.Intn(len(b))] = byte(rng.Intn(256))
		}
		tok := string(b)
		if tok == good {
			continue
		}
		if p, err := v.Verify(tok); err == nil {
			accepted++
			if p.ID != "cust_1" {
				t.Fatalf("a mutated token was accepted as another principal: %+v", p)
			}
		}
	}
	if accepted != 0 {
		t.Fatalf("%d of %d mutated tokens were accepted", accepted, rounds)
	}
}

// ES256 signatures are 64 bytes R||S with S in the lower half or not; a zero R or S, or a value at or above the curve order, never verifies.
func TestDegenerateSignaturesAreRejected(t *testing.T) {
	k := newKeys(t)
	v, _ := verifier(t, k, epoch)
	good := mint(t, k.priv, goodHeader(), goodClaims(epoch))
	parts := strings.Split(good, ".")
	order := elliptic.P256().Params().N
	for name, rs := range map[string][2]*big.Int{"zero": {big.NewInt(0), big.NewInt(0)}, "order": {order, order}, "one": {big.NewInt(1), big.NewInt(1)}} {
		sig := make([]byte, 64)
		rs[0].FillBytes(sig[:32])
		rs[1].FillBytes(sig[32:])
		if _, err := v.Verify(parts[0] + "." + parts[1] + "." + b64(sig)); !errors.Is(err, session.ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestAVerifierWithoutWhatItNeedsDoesNotStart(t *testing.T) {
	k := newKeys(t)
	pub := map[string]*ecdsa.PublicKey{kid: &k.priv.PublicKey}
	clk := clock.NewManual(epoch)
	for name, cfg := range map[string]session.Config{
		"no keys":         {Audience: aud, Tenants: []string{tenantA}, MaxLifetime: lifetime, Clock: clk},
		"no audience":     {Keys: pub, Tenants: []string{tenantA}, MaxLifetime: lifetime, Clock: clk},
		"no tenants":      {Keys: pub, Audience: aud, MaxLifetime: lifetime, Clock: clk},
		"no max lifetime": {Keys: pub, Audience: aud, Tenants: []string{tenantA}, Clock: clk},
		"no clock":        {Keys: pub, Audience: aud, Tenants: []string{tenantA}, MaxLifetime: lifetime},
		"a nil key":       {Keys: map[string]*ecdsa.PublicKey{kid: nil}, Audience: aud, Tenants: []string{tenantA}, MaxLifetime: lifetime, Clock: clk},
	} {
		if _, err := session.NewVerifier(cfg); err == nil {
			t.Errorf("%s: a verifier must not start without it", name)
		}
	}
	other, _ := ecdsa.GenerateKey(elliptic.P224(), rand.Reader)
	if _, err := session.NewVerifier(session.Config{Keys: map[string]*ecdsa.PublicKey{kid: &other.PublicKey}, Audience: aud, Tenants: []string{tenantA}, MaxLifetime: lifetime, Clock: clk}); err == nil {
		t.Error("only P-256 keys are accepted for ES256")
	}
}

// The core reads the IdP's public keys from PEM files (PKIX); only a P-256 public key is accepted (REQ-SEC-001).
func TestParsePublicKeyPEMAcceptsOnlyAP256PublicKey(t *testing.T) {
	k := newKeys(t)
	der, err := x509.MarshalPKIXPublicKey(&k.priv.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	good := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
	got, err := session.ParsePublicKeyPEM(good)
	if err != nil || !got.Equal(&k.priv.PublicKey) {
		t.Fatalf("a P-256 public key must parse: %v", err)
	}
	p384, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	der384, _ := x509.MarshalPKIXPublicKey(&p384.PublicKey)
	priv, _ := x509.MarshalPKCS8PrivateKey(k.priv)
	for name, in := range map[string][]byte{
		"P-384 key":     pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der384}),
		"private key":   pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: priv}),
		"not PEM":       []byte("nope"),
		"empty":         nil,
		"bad DER":       pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: []byte("x")}),
		"two blocks":    append(append([]byte{}, good...), good...),
		"trailing junk": append(append([]byte{}, good...), []byte("junk")...),
	} {
		if _, err := session.ParsePublicKeyPEM(in); err == nil {
			t.Errorf("%s must be refused", name)
		}
	}
}
