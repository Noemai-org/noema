package main

// STM-04 slice 2: the client's TLS configuration (REQ-EXE-006 is not at stake here; INV-29 and PERD 36.1, mTLS between services). Keys and certificates are generated per test and are SYNTHETIC
// (AGENT-25). Errors name the setting and never a path or file content.

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writePair(t *testing.T, dir string) (certFile, keyFile string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "SYNTHETIC core"}, NotBefore: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), NotAfter: time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC), // SYNTHETIC fixed dates: no wall clock in a test (INV-21)
		IsCA: true, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	kb, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certFile, keyFile = filepath.Join(dir, "c.crt"), filepath.Join(dir, "c.key")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb}), 0o600); err != nil {
		t.Fatal(err)
	}
	return certFile, keyFile
}

func TestTheTemporalTLSConfigIsMutualAndTLS13(t *testing.T) {
	dir := t.TempDir()
	cert, key := writePair(t, dir)
	cfg := &WorkerConfig{Address: "temporal.internal:7233", TLS: &WorkerTLS{CertFile: cert, KeyFile: key, CAFile: cert}}
	tc, err := temporalTLS(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(tc.Certificates) != 1 || tc.RootCAs == nil || tc.ServerName != "temporal.internal" || tc.MinVersion != tls.VersionTLS13 || tc.InsecureSkipVerify {
		t.Errorf("%+v", tc)
	}
}

func TestABrokenTLSInputIsRefusedByNameWithNoPathInTheError(t *testing.T) {
	dir := t.TempDir()
	cert, key := writePair(t, dir)
	empty := filepath.Join(dir, "empty.pem")
	if err := os.WriteFile(empty, []byte("not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "does-not-exist")
	cases := map[string]struct {
		tls  WorkerTLS
		addr string
		want string
	}{
		"missing certificate": {WorkerTLS{CertFile: missing, KeyFile: key, CAFile: cert}, "h:7233", EnvTemporalTLSCert},
		"missing key":         {WorkerTLS{CertFile: cert, KeyFile: missing, CAFile: cert}, "h:7233", EnvTemporalTLSKey},
		"missing CA":          {WorkerTLS{CertFile: cert, KeyFile: key, CAFile: missing}, "h:7233", EnvTemporalTLSCA},
		"CA with no cert":     {WorkerTLS{CertFile: cert, KeyFile: key, CAFile: empty}, "h:7233", EnvTemporalTLSCA},
		"no host":             {WorkerTLS{CertFile: cert, KeyFile: key, CAFile: cert}, ":7233", EnvTemporalAddress},
	}
	for name, c := range cases {
		tc := c.tls
		tcfg, err := temporalTLS(&WorkerConfig{Address: c.addr, TLS: &tc})
		if tcfg != nil || err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, %v", name, tcfg, err)
			continue
		}
		if strings.Contains(err.Error(), dir) || strings.Contains(err.Error(), "not a certificate") {
			t.Errorf("%s: the error carries a path or file content: %v", name, err)
		}
	}
}
