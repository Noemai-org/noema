package main

// The synthetic enterprise IdP (PERD REQ-SEC-001; EDG-01 slice 2): a development and benchmark tool that makes an ES256 key pair and
// issues session tokens the core's verifier accepts. It is not part of the core image. Every key and token here is SYNTHETIC.

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/edge/session"
	"github.com/Noemai-org/noema/services/core/platform/clock"
)

var epoch = time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)

func newKeyDir(t *testing.T) (dir string) {
	t.Helper()
	dir = t.TempDir()
	var out bytes.Buffer
	if err := run([]string{"keygen", "-dir", dir}, &out, clock.NewManual(epoch)); err != nil {
		t.Fatal(err)
	}
	return dir
}

func verifierFor(t *testing.T, dir, kid string, clk clock.Clock) *session.Verifier {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "public.pem"))
	if err != nil {
		t.Fatal(err)
	}
	pub, err := session.ParsePublicKeyPEM(b)
	if err != nil {
		t.Fatal(err)
	}
	v, err := session.NewVerifier(session.Config{Keys: map[string]*ecdsa.PublicKey{kid: pub}, Audience: "noema-core", Tenants: []string{"tnt_a"}, MaxLifetime: 15 * time.Minute, Clock: clk})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func hasRole(args []string) bool {
	for _, a := range args {
		if a == "-role" {
			return true
		}
	}
	return false
}

func mintToken(t *testing.T, dir string, clk clock.Clock, extra ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	args := append([]string{"issue", "-key", filepath.Join(dir, "private.pem"), "-kid", "idp-k1", "-aud", "noema-core", "-tenant", "tnt_a", "-principal", "cust_1", "-ttl", "10m"}, extra...)
	if !hasRole(extra) {
		args = append(args, "-role", "customer")
	}
	err := run(args, &out, clk)
	return strings.TrimSpace(out.String()), err
}

func TestKeygenWritesAPrivateKeyOnlyTheOwnerCanReadAndAPublicKeyTheCoreParses(t *testing.T) {
	dir := newKeyDir(t)
	st, err := os.Stat(filepath.Join(dir, "private.pem"))
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("private key: %v %v", err, st)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "private.pem"))
	blk, _ := pem.Decode(raw)
	if blk == nil || blk.Type != "PRIVATE KEY" {
		t.Fatalf("not a PKCS#8 private key: %q", raw)
	}
	if _, err := x509.ParsePKCS8PrivateKey(blk.Bytes); err != nil {
		t.Fatal(err)
	}
	pub, _ := os.ReadFile(filepath.Join(dir, "public.pem"))
	if strings.Contains(string(pub), "PRIVATE") {
		t.Fatal("the public file holds a private key")
	}
	// it never overwrites a key that exists: replacing a key silently would invalidate every token
	var out bytes.Buffer
	if err := run([]string{"keygen", "-dir", dir}, &out, clock.NewManual(epoch)); err == nil {
		t.Fatal("keygen overwrote an existing key")
	}
}

func TestATokenTheIssuerMakesIsAcceptedByTheCoresVerifier(t *testing.T) {
	dir := newKeyDir(t)
	clk := clock.NewManual(epoch)
	tok, err := mintToken(t, dir, clk, "-role", "approver")
	if err != nil {
		t.Fatal(err)
	}
	p, err := verifierFor(t, dir, "idp-k1", clk).Verify(tok)
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != "cust_1" || p.Tenant != "tnt_a" || p.Role != session.RoleApprover ||
		!p.IssuedAt.Equal(epoch) || !p.ExpiresAt.Equal(epoch.Add(10*time.Minute)) {
		t.Fatalf("%+v", p)
	}
	clk.Advance(10*time.Minute + time.Second)
	if _, err := verifierFor(t, dir, "idp-k1", clk).Verify(tok); !errors.Is(err, session.ErrInvalid) {
		t.Fatalf("the token must expire at its ttl: %v", err)
	}
}

func TestATokenFromAnotherKeyOrKidIsRejectedByTheCore(t *testing.T) {
	a, b := newKeyDir(t), newKeyDir(t)
	clk := clock.NewManual(epoch)
	tok, err := mintToken(t, b, clk)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verifierFor(t, a, "idp-k1", clk).Verify(tok); !errors.Is(err, session.ErrInvalid) {
		t.Fatalf("a token signed by another key must be rejected: %v", err)
	}
	other, err := mintToken(t, a, clk, "-kid", "idp-k2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verifierFor(t, a, "idp-k1", clk).Verify(other); !errors.Is(err, session.ErrInvalid) {
		t.Fatalf("a token naming an unknown kid must be rejected: %v", err)
	}
}

func TestTheIssuerRefusesWhatTheCoreWouldRefuse(t *testing.T) {
	dir := newKeyDir(t)
	clk := clock.NewManual(epoch)
	for name, extra := range map[string][]string{
		"zero ttl":                           {"-ttl", "0s"},
		"sub-second ttl":                     {"-ttl", "500ms"},
		"principal above the verifier bound": {"-principal", strings.Repeat("p", session.MaxSubjectLen+1)},
		"ttl above the ceiling":              {"-ttl", "15m1s"},
		"ttl far above the ceiling":          {"-ttl", "20m"},
		"negative ttl":                       {"-ttl", "-5m"},
		"unknown role":                       {"-role", "root"},
		"old role name":                      {"-role", "operator"},
		"two roles":                          {"-role", "ops", "-role", "approver"},
		"the same twice":                     {"-role", "ops", "-role", "ops"},
		"empty tenant":                       {"-tenant", ""},
		"empty princ.":                       {"-principal", ""},
		"empty audience":                     {"-aud", ""},
		"empty kid":                          {"-kid", ""},
	} {
		if tok, err := mintToken(t, dir, clk, extra...); err == nil || tok != "" {
			t.Errorf("%s: issued %q, err %v", name, tok, err)
		}
	}
	var out bytes.Buffer
	if err := run([]string{"issue", "-key", filepath.Join(dir, "missing.pem")}, &out, clk); err == nil {
		t.Error("issuing without a readable key must fail")
	}
	if err := run([]string{"nope"}, &out, clk); err == nil {
		t.Error("an unknown subcommand must fail")
	}
	if err := run(nil, &out, clk); err == nil {
		t.Error("no subcommand must fail")
	}
}

// The issuer never prints a private key, and an error never carries one.
func TestNothingSecretIsPrinted(t *testing.T) {
	dir := newKeyDir(t)
	raw, _ := os.ReadFile(filepath.Join(dir, "private.pem"))
	var out bytes.Buffer
	_ = run([]string{"issue", "-key", filepath.Join(dir, "private.pem")}, &out, clock.NewManual(epoch))
	if strings.Contains(out.String(), "PRIVATE") || strings.Contains(out.String(), strings.TrimSpace(strings.Split(string(raw), "\n")[1])) {
		t.Fatalf("secret material in the output: %q", out.String())
	}
}
