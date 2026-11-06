// Command synthetic-idp is the synthetic enterprise identity provider of the POC (PERD REQ-SEC-001; EDG-01): a development and benchmark
// tool, not part of the core image (the image builds ./cmd/core only). `keygen` makes an ES256 key pair; `issue` signs a session token
// the core's verifier accepts. The private key stays in a file only its owner can read and is never printed. Every key and token is
// SYNTHETIC. Time comes from platform/clock (INV-21).
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Noemai-org/noema/services/core/edge/session"
	"github.com/Noemai-org/noema/services/core/platform/clock"
)

const (
	privateFile = "private.pem"
	publicFile  = "public.pem"
	sigHalf     = 32
)

func main() {
	if err := run(os.Args[1:], os.Stdout, clock.System{}); err != nil {
		fmt.Fprintln(os.Stderr, "synthetic-idp:", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer, clk clock.Clock) error {
	if len(args) == 0 {
		return errors.New("usage: synthetic-idp keygen -dir DIR | issue -key FILE -kid KID -aud AUD -tenant T -principal P -role R [-role R] -ttl D")
	}
	switch args[0] {
	case "keygen":
		return keygen(args[1:], out)
	case "issue":
		return issue(args[1:], out, clk)
	}
	return fmt.Errorf("unknown subcommand %q", args[0])
}

func keygen(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("keygen", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dir := fs.String("dir", "", "directory to write private.pem and public.pem into")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dir == "" {
		return errors.New("keygen: -dir is required")
	}
	if err := os.MkdirAll(*dir, 0o700); err != nil {
		return fmt.Errorf("keygen: %w", err)
	}
	priv, pub := filepath.Join(*dir, privateFile), filepath.Join(*dir, publicFile)
	for _, p := range []string{priv, pub} {
		if _, err := os.Stat(p); err == nil {
			return fmt.Errorf("keygen: %s exists; a key is never replaced in place", p)
		}
	}
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("keygen: %w", err)
	}
	pk8, err := x509.MarshalPKCS8PrivateKey(k)
	if err != nil {
		return fmt.Errorf("keygen: %w", err)
	}
	pkix, err := x509.MarshalPKIXPublicKey(&k.PublicKey)
	if err != nil {
		return fmt.Errorf("keygen: %w", err)
	}
	if err := writeNew(priv, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pk8}), 0o600); err != nil {
		return err
	}
	if err := writeNew(pub, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pkix}), 0o644); err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, "wrote", pub, "and the private key beside it")
	return err
}

// writeNew creates a file that must not exist, with the given mode.
func writeNew(path string, data []byte, mode os.FileMode) (err error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return fmt.Errorf("keygen: %w", err)
	}
	defer func() {
		if cerr := f.Close(); err == nil && cerr != nil {
			err = fmt.Errorf("keygen: %w", cerr)
		}
	}()
	if _, err = f.Write(data); err != nil {
		return fmt.Errorf("keygen: %w", err)
	}
	return nil
}

type roles []string

func (r *roles) String() string     { return strings.Join(*r, ",") }
func (r *roles) Set(v string) error { *r = append(*r, v); return nil }

func issue(args []string, out io.Writer, clk clock.Clock) error {
	fs := flag.NewFlagSet("issue", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	key := fs.String("key", "", "private key file (PKCS#8 PEM)")
	kid := fs.String("kid", "", "key identifier the core knows the public key by")
	aud := fs.String("aud", "", "audience (the core)")
	tenant := fs.String("tenant", "", "tenant")
	principal := fs.String("principal", "", "the principal (becomes sub)")
	ttl := fs.Duration("ttl", 0, "token lifetime")
	var rs roles
	fs.Var(&rs, "role", "the one role of the token: customer, ops, approver or audit (ruling A1: exactly one)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *key == "" || *kid == "" || *aud == "" || *tenant == "" || *principal == "" || len(*principal) > session.MaxSubjectLen || len(rs) != 1 || *ttl < time.Second || *ttl > session.CeilingLifetime {
		return errors.New("issue: -key, -kid, -aud, -tenant, -principal, exactly one -role, a -principal of at most 128 bytes and a -ttl of at least one second and at most 15 minutes are all required")
	}
	for _, r := range rs {
		if _, ok := session.ParseRole(r); !ok {
			return fmt.Errorf("issue: %q is not a role", r)
		}
	}
	raw, err := os.ReadFile(*key)
	if err != nil {
		return fmt.Errorf("issue: cannot read the key: %w", err)
	}
	blk, _ := pem.Decode(raw)
	if blk == nil || blk.Type != "PRIVATE KEY" {
		return errors.New("issue: the key file is not a PKCS#8 PEM private key")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(blk.Bytes)
	if err != nil {
		return errors.New("issue: the key file does not hold a private key")
	}
	priv, ok := parsed.(*ecdsa.PrivateKey)
	if !ok || priv.Curve != elliptic.P256() {
		return errors.New("issue: the key is not an ECDSA P-256 private key")
	}
	now := clk.Now()
	h, err := json.Marshal(map[string]string{"alg": "ES256", "typ": "JWT", "kid": *kid})
	if err != nil {
		return fmt.Errorf("issue: %w", err)
	}
	c, err := json.Marshal(map[string]any{"sub": *principal, "aud": *aud, "iss": session.Issuer, "tenant_id": *tenant, "roles": []string(rs), "iat": now.Unix(), "exp": now.Add(*ttl).Unix()})
	if err != nil {
		return fmt.Errorf("issue: %w", err)
	}
	enc := base64.RawURLEncoding
	signing := enc.EncodeToString(h) + "." + enc.EncodeToString(c)
	sum := sha256.Sum256([]byte(signing))
	r, s, err := ecdsa.Sign(rand.Reader, priv, sum[:])
	if err != nil {
		return fmt.Errorf("issue: %w", err)
	}
	sig := make([]byte, 2*sigHalf)
	r.FillBytes(sig[:sigHalf])
	s.FillBytes(sig[sigHalf:])
	_, err = fmt.Fprintln(out, signing+"."+enc.EncodeToString(sig))
	return err
}
