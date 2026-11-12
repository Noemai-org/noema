package session_test

// A149 (DEC-2026-11-12-architect-rulings-a148-a160, relayed until docs#157 merges), INV-12, AGENT-10: the edge principal carries `principal_type`
// (from the single role), `asserted_by` (the verified issuer) and `authn_ref` (a hash of the token's kid, sub and iat, never the token), so the
// orchestrator copies them into the Intent Object from the session and from nothing else. Keys and tokens are SYNTHETIC, made per test.

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/edge/session"
	"github.com/Noemai-org/noema/services/core/platform/clock"
)

// authnShape is the SessionId of contracts/schemas/common.v1.json (the Intent Object's authn_ref): `ses_` and 2 to 32 letters or digits.
var authnShape = regexp.MustCompile(`^ses_[0-9A-Za-z]{2,32}$`)

func verifyWith(t *testing.T, k keys, header, claims map[string]any) session.Principal {
	t.Helper()
	v, _ := verifier(t, k, epoch)
	p, err := v.Verify(mint(t, k.priv, header, claims))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// independentAuthnRef recomputes the reference with no code of the package: SHA-256 of the JSON array [kid, sub, iat], hex, first 32 characters.
func independentAuthnRef(kid, sub string, iat int64) string {
	raw, _ := json.Marshal([]any{kid, sub, iat})
	sum := sha256.Sum256(raw)
	return "ses_" + hex.EncodeToString(sum[:])[:32]
}

// A149: principal_type is mapped from the single role: customer, ops to operator, audit to auditor, approver.
func TestPrincipalTypeIsMappedFromTheSingleRole(t *testing.T) {
	k := newKeys(t)
	for role, want := range map[string]string{"customer": "customer", "ops": "operator", "audit": "auditor", "approver": "approver"} {
		c := goodClaims(epoch)
		c["roles"] = []string{role}
		if got := verifyWith(t, k, goodHeader(), c).PrincipalType; got != want {
			t.Errorf("role %s: principal_type %q, want %q", role, got, want)
		}
	}
}

// A149, A1: asserted_by is the verified issuer, which the verifier has matched exactly.
func TestAssertedByIsTheVerifiedIssuer(t *testing.T) {
	k := newKeys(t)
	if got := verifyWith(t, k, goodHeader(), goodClaims(epoch)).AssertedBy; got != session.Issuer || got != "noema-poc-idp" {
		t.Fatalf("asserted_by %q, want %q", got, session.Issuer)
	}
}

// A149: authn_ref is a hash of kid, sub and iat; it has the shape of a session reference, equals an independent computation, and is the same for the
// same three values and different when any one changes.
func TestAuthnRefIsAHashOfKidSubAndIat(t *testing.T) {
	k := newKeys(t)
	c := goodClaims(epoch)
	p := verifyWith(t, k, goodHeader(), c)
	if !authnShape.MatchString(p.AuthnRef) {
		t.Fatalf("authn_ref %q is not a session reference", p.AuthnRef)
	}
	if want := independentAuthnRef(kid, "cust_1", epoch.Unix()); p.AuthnRef != want {
		t.Fatalf("authn_ref %q, independent computation %q", p.AuthnRef, want)
	}
	other := goodClaims(epoch)
	other["exp"] = epoch.Add(9 * time.Minute).Unix() // a different expiry is not part of the reference
	if got := verifyWith(t, k, goodHeader(), other).AuthnRef; got != p.AuthnRef {
		t.Errorf("exp changed the reference: %q != %q", got, p.AuthnRef)
	}
	changes := map[string]func(m map[string]any){
		"sub": func(m map[string]any) { m["sub"] = "cust_2" },
		"iat": func(m map[string]any) { m["iat"] = epoch.Add(-time.Second).Unix() },
	}
	for name, f := range changes {
		m := goodClaims(epoch)
		f(m)
		if got := verifyWith(t, k, goodHeader(), m).AuthnRef; got == p.AuthnRef {
			t.Errorf("a different %s gave the same reference", name)
		}
	}
}

// A149: the kid is bound too. A second key under another kid signs the same sub and iat and gets another reference.
func TestAuthnRefBindsTheKeyID(t *testing.T) {
	k := newKeys(t)
	v, err := session.NewVerifier(session.Config{Keys: map[string]*ecdsa.PublicKey{kid: &k.priv.PublicKey, "idp-k2": &k.priv.PublicKey}, Audience: aud,
		Tenants: []string{tenantA}, MaxLifetime: lifetime, Skew: skew, Clock: clock.NewManual(epoch)})
	if err != nil {
		t.Fatal(err)
	}
	h2 := goodHeader()
	h2["kid"] = "idp-k2"
	p1, err1 := v.Verify(mint(t, k.priv, goodHeader(), goodClaims(epoch)))
	p2, err2 := v.Verify(mint(t, k.priv, h2, goodClaims(epoch)))
	if err1 != nil || err2 != nil {
		t.Fatal(err1, err2)
	}
	if p1.AuthnRef == p2.AuthnRef || p2.AuthnRef != independentAuthnRef("idp-k2", "cust_1", epoch.Unix()) {
		t.Fatalf("kid not bound: %q %q", p1.AuthnRef, p2.AuthnRef)
	}
}

// A149: never the token. No part of the token appears in the reference.
func TestAuthnRefNeverContainsTheToken(t *testing.T) {
	k := newKeys(t)
	v, _ := verifier(t, k, epoch)
	tok := mint(t, k.priv, goodHeader(), goodClaims(epoch))
	p, err := v.Verify(tok)
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range strings.Split(tok, ".") {
		if len(part) >= 8 && strings.Contains(p.AuthnRef, part[:8]) {
			t.Errorf("authn_ref contains a part of the token: %q", p.AuthnRef)
		}
	}
}
