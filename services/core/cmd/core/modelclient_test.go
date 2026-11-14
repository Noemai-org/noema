package main

// UND-05 / noema#555 (the Founder's order of 14 Nov: the core side of mTLS now; A208 accepts IL's design, IL does Docker and Compose, EL the core client; relayed, not yet
// committed; PERD 36.1, 18.4 boundary 2; INV-29, AGENT-13, AGENT-16): the core calls the model service over mutual TLS, never over plain HTTP and never with a shared bearer.
// The client presents the core's certificate, trusts only the CA file it is given (not the system's roots), and the model service's certificate must verify. Every value comes
// from a file-mounted or environment setting with no default. Certificates are SYNTHETIC and made at run time with the standard library; no key is in the source.

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The validity is fixed and wide (these are SYNTHETIC certificates for a test server; the lint forbids reading the clock outside platform/clock).
var (
	validFrom = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	validTo   = time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)
	serial    int64
)

func nextSerial() *big.Int { serial++; return big.NewInt(serial + 1) }

type testCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pem  []byte
}

func newTestCA(t *testing.T, name string) testCA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{SerialNumber: nextSerial(), Subject: pkix.Name{CommonName: name}, NotBefore: validFrom, NotAfter: validTo,
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return testCA{cert: cert, key: key, pem: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})}
}

// issue makes a leaf certificate signed by the CA: for a server (with the loopback address as its name) or for a client (the core).
func (ca testCA) issue(t *testing.T, cn string, server bool) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{SerialNumber: nextSerial(), Subject: pkix.Name{CommonName: cn}, NotBefore: validFrom, NotAfter: validTo,
		KeyUsage: x509.KeyUsageDigitalSignature}
	if server {
		tpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
		tpl.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
	} else {
		tpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatal(err)
	}
	kder, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kder})
}

// modelServer is a stand-in for the model service's listener: it demands a client certificate signed by clientCA and presents a certificate signed by serverCA. The handler
// records the subject of the certificate it was called with.
func modelServer(t *testing.T, serverCA, clientCA testCA) (*httptest.Server, *string) {
	t.Helper()
	certPEM, keyPEM := serverCA.issue(t, "model", true)
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(clientCA.cert)
	var subject string
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(r.TLS.PeerCertificates) == 1 {
			subject = r.TLS.PeerCertificates[0].Subject.CommonName
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{pair}, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: pool, MinVersion: tls.VersionTLS12}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv, &subject
}

type mtlsFiles struct{ cert, key, ca string }

func writeMTLS(t *testing.T, coreCert, coreKey, caPEM []byte) mtlsFiles {
	t.Helper()
	d := t.TempDir()
	f := mtlsFiles{cert: filepath.Join(d, "core.crt"), key: filepath.Join(d, "core.key"), ca: filepath.Join(d, "ca.crt")}
	for p, b := range map[string][]byte{f.cert: coreCert, f.key: coreKey, f.ca: caPEM} {
		if err := os.WriteFile(p, b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func modelEnv(url string, f mtlsFiles, maxBytes string) Env {
	e := envMap{EnvModelURL: url, EnvModelClientCertFile: f.cert, EnvModelClientKeyFile: f.key, EnvModelCAFile: f.ca, EnvModelMaxResponseBytes: maxBytes}
	return Env{Getenv: e.get, ReadFile: os.ReadFile, ReadDir: os.ReadDir}
}

// The core presents its certificate, and the model service sees it: mutual TLS, not a bearer. The call is made through the client's own HTTP client (the compile call itself is
// the understanding client's, tested there).
func TestTheCoreCallsTheModelServiceOverMutualTLSWithItsOwnCertificate(t *testing.T) {
	ca := newTestCA(t, "noema-poc-ca")
	srv, subject := modelServer(t, ca, ca)
	cc, ck := ca.issue(t, "core-understanding", false)
	client, err := buildModelHTTPClient(modelEnv(srv.URL, writeMTLS(t, cc, ck, ca.pem), "65536"))
	if err != nil || client == nil {
		t.Fatalf("got %v, %v", client, err)
	}
	resp, err := client.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("the call failed: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent || *subject != "core-understanding" {
		t.Errorf("status %d, the service saw the client subject %q; want 204 and core-understanding", resp.StatusCode, *subject)
	}
}

// The client trusts only the CA file it is given: a model service whose certificate another CA signed is refused at the handshake, and so is one the system's roots would trust.
func TestAModelServiceWhoseCertificateIsFromAnotherCAIsRefused(t *testing.T) {
	ours, theirs := newTestCA(t, "noema-poc-ca"), newTestCA(t, "another-ca")
	srv, _ := modelServer(t, theirs, ours) // the server's certificate is from the other CA; it would accept our client certificate
	cc, ck := ours.issue(t, "core-understanding", false)
	client, err := buildModelHTTPClient(modelEnv(srv.URL, writeMTLS(t, cc, ck, ours.pem), "65536"))
	if err != nil {
		t.Fatal(err)
	}
	if resp, err := client.Get(srv.URL + "/healthz"); err == nil {
		_ = resp.Body.Close()
		t.Fatal("a server certificate from another CA must be refused at the handshake")
	}
}

// A model service that demands a client certificate from our CA refuses a core whose certificate another CA signed (the server's side of the same rule, so the test is of the
// client's identity and not only of its trust).
func TestACoreCertificateFromAnotherCAIsRefusedByTheModelService(t *testing.T) {
	ours, other := newTestCA(t, "noema-poc-ca"), newTestCA(t, "another-ca")
	srv, _ := modelServer(t, ours, ours)
	cc, ck := other.issue(t, "core-understanding", false)
	client, err := buildModelHTTPClient(modelEnv(srv.URL, writeMTLS(t, cc, ck, ours.pem), "65536"))
	if err != nil {
		t.Fatal(err)
	}
	if resp, err := client.Get(srv.URL + "/healthz"); err == nil {
		_ = resp.Body.Close()
		t.Fatal("a client certificate from another CA must be refused by the model service")
	}
}

// No default for any setting, no plain HTTP, and nothing but a certificate pair and one CA: each of these refuses to build, naming the setting and never a key.
func TestABadModelConfigurationRefusesToBuildAndNeverEchoesAKey(t *testing.T) {
	ca := newTestCA(t, "noema-poc-ca")
	srv, _ := modelServer(t, ca, ca)
	cc, ck := ca.issue(t, "core-understanding", false)
	good := writeMTLS(t, cc, ck, ca.pem)
	notPEM := writeMTLS(t, []byte("not a certificate"), []byte("not a key"), []byte("not a CA"))
	otherKey := func() mtlsFiles { // a certificate with a key that is not its own
		_, k2 := ca.issue(t, "someone else", false)
		return writeMTLS(t, cc, k2, ca.pem)
	}()
	cases := map[string]Env{
		"plain http":             modelEnv(strings.Replace(srv.URL, "https://", "http://", 1), good, "65536"),
		"no URL":                 modelEnv("", good, "65536"),
		"no certificate file":    modelEnv(srv.URL, mtlsFiles{key: good.key, ca: good.ca}, "65536"),
		"no key file":            modelEnv(srv.URL, mtlsFiles{cert: good.cert, ca: good.ca}, "65536"),
		"no CA file":             modelEnv(srv.URL, mtlsFiles{cert: good.cert, key: good.key}, "65536"),
		"no response bound":      modelEnv(srv.URL, good, ""),
		"a bound of zero":        modelEnv(srv.URL, good, "0"),
		"a bound that is no int": modelEnv(srv.URL, good, "big"),
		"files that are not PEM": modelEnv(srv.URL, notPEM, "65536"),
		"a key of another cert":  modelEnv(srv.URL, otherKey, "65536"),
		"a file that is missing": modelEnv(srv.URL, mtlsFiles{cert: good.cert + ".absent", key: good.key, ca: good.ca}, "65536"),
	}
	for name, env := range cases {
		c, err := buildModelHTTPClient(env)
		if err == nil || c != nil {
			t.Errorf("%s: got %v, %v; want an error and no client", name, c, err)
			continue
		}
		if msg := err.Error(); strings.Contains(msg, "PRIVATE KEY") || strings.Contains(msg, "not a key") || strings.Contains(msg, string(ck)) {
			t.Errorf("%s: the error echoes key material: %s", name, msg)
		}
	}
}

// The understanding client is built over that HTTP client with the configured response bound, and refuses an unbuilt configuration like the HTTP client does.
func TestTheUnderstandingClientIsBuiltOverTheMutualTLSClient(t *testing.T) {
	ca := newTestCA(t, "noema-poc-ca")
	srv, _ := modelServer(t, ca, ca)
	cc, ck := ca.issue(t, "core-understanding", false)
	c, err := buildModelClient(modelEnv(srv.URL, writeMTLS(t, cc, ck, ca.pem), "65536"))
	if err != nil || c == nil {
		t.Fatalf("got %v, %v", c, err)
	}
	if c, err := buildModelClient(Env{Getenv: envMap{}.get, ReadFile: os.ReadFile, ReadDir: os.ReadDir}); err != nil || c != nil {
		t.Errorf("with no model setting at all: got %v, %v; want no client and no error (the interpreter is then not configured)", c, err)
	}
}
