package opslistener_test

// EXE-06, the ops listener (REQ-EXE-008, REQ-OPS-002, REQ-SEC-006; INV-03, INV-11, INV-12; rulings A666 and A699, A710; ruling A71, interim, relayed, not yet committed): mTLS with
// operator client certificates, a mapping from the verified certificate to a controlplane.Caller with the admin role, and the set and clear endpoints. The kill switch path
// behind it is the merged controlplane.Switch; here it is a recording fake, because what is under test is who may call it and with what identity. All data is SYNTHETIC.

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/cmd/ops/opslistener"
	"github.com/Noemai-org/noema/services/core/execution/controlplane"
	"github.com/Noemai-org/noema/services/core/platform/clock"
)

type call struct {
	op, tenant, corr string
	caller           controlplane.Caller
}

type fakeSwitch struct {
	mu    sync.Mutex
	calls []call
	err   error
}

func (f *fakeSwitch) rec(op, tenant, corr string, c controlplane.Caller) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call{op, tenant, corr, c})
	return f.err
}
func (f *fakeSwitch) Set(_ context.Context, tenant, corr string, c controlplane.Caller) error {
	return f.rec("set", tenant, corr, c)
}
func (f *fakeSwitch) Clear(_ context.Context, tenant, corr string, c controlplane.Caller) error {
	return f.rec("clear", tenant, corr, c)
}
func (f *fakeSwitch) got() []call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]call(nil), f.calls...)
}

const (
	tenant   = "ten_aa"
	operator = "usr_oper1" // SYNTHETIC
	corrID   = "corr_ops0001"
	setPath  = "/v1/ops/tenants/" + tenant + "/kill-switch:set"
	clrPath  = "/v1/ops/tenants/" + tenant + "/kill-switch:clear"
)

type world struct {
	t        *testing.T
	opCA     *authority
	srvCA    *authority
	sw       *fakeSwitch
	cfg      opslistener.Config
	l        *opslistener.Listener
	srv      *httptest.Server
	srvPool  *x509.CertPool
	operator tls.Certificate
}

func fixedClock() clock.Clock { return clock.NewManual(synthNow) }

func newWorld(t *testing.T) *world {
	t.Helper()
	w := &world{t: t, opCA: newCA(t, "synthetic operator CA"), srvCA: newCA(t, "synthetic server CA"), sw: &fakeSwitch{}}
	dir := t.TempDir()
	sc, sk, _ := w.srvCA.leaf(t, leafOpts{cn: "ops", usages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, dns: []string{"localhost"}, ips: []net.IP{net.ParseIP("127.0.0.1")}})
	read := func(p string) ([]byte, error) { return osRead(p) }
	w.cfg = opslistener.Config{
		ServerCertFile: writeFile(t, dir, "server.crt", sc), ServerKeyFile: writeFile(t, dir, "server.key", sk), ClientCAFile: writeFile(t, dir, "operator-ca.crt", w.opCA.pem),
		ReadFile: read, ReadSecret: read, Clock: fixedClock(), Switch: w.sw, CorrelationIDs: func() (string, error) { return corrID, nil }, HeaderTimeout: 5 * time.Second,
		ReadTimeout: 5 * time.Second, IdleTimeout: 5 * time.Second, MaxHeaderBytes: 8192,
	}
	w.srvPool = x509.NewCertPool()
	w.srvPool.AddCert(w.srvCA.cert)
	_, _, w.operator = w.opCA.leaf(t, leafOpts{cn: operator, usages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}})
	return w
}

func (w *world) start() {
	w.t.Helper()
	l, err := opslistener.New(w.cfg)
	if err != nil {
		w.t.Fatalf("New: %v", err)
	}
	w.l = l
	w.srv = httptest.NewUnstartedServer(l.Handler())
	w.srv.TLS = l.TLSConfig()
	w.srv.StartTLS()
	w.t.Cleanup(w.srv.Close)
}

func (w *world) client(certs ...tls.Certificate) *http.Client {
	return &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: w.srvPool, Certificates: certs, Time: func() time.Time { return synthNow }}}}
}

func (w *world) do(c *http.Client, method, path, body string) (int, string, error) {
	req, err := http.NewRequest(method, w.srv.URL+path, strings.NewReader(body))
	if err != nil {
		w.t.Fatal(err)
	}
	resp, err := c.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b), nil
}

// A valid operator certificate sets the switch: the listener calls the kill switch path with the admin role and the certificate's identity, and the generated correlation ID.
func TestOperatorCertificateSetsTheSwitchAsAdmin(t *testing.T) { // REQ-EXE-008, REQ-SEC-006, INV-12
	w := newWorld(t)
	w.start()
	code, _, err := w.do(w.client(w.operator), http.MethodPost, setPath, "")
	if err != nil || code/100 != 2 {
		t.Fatalf("set: status %d, err %v; want 2xx", code, err)
	}
	got := w.sw.got()
	want := call{"set", tenant, corrID, controlplane.Caller{Role: controlplane.RoleAdmin, PrincipalID: operator}}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("calls = %+v; want exactly %+v", got, want)
	}
}

func TestOperatorCertificateClearsTheSwitchAsAdmin(t *testing.T) { // REQ-EXE-008, REQ-OPS-002, INV-12
	w := newWorld(t)
	w.start()
	code, _, err := w.do(w.client(w.operator), http.MethodPost, clrPath, "")
	if err != nil || code/100 != 2 {
		t.Fatalf("clear: status %d, err %v; want 2xx", code, err)
	}
	got := w.sw.got()
	want := call{"clear", tenant, corrID, controlplane.Caller{Role: controlplane.RoleAdmin, PrincipalID: operator}}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("calls = %+v; want exactly %+v", got, want)
	}
}

// Nothing but a certificate that chains to the operator CA, valid now, for client authentication, gets past the handshake. Each case must fail before any handler runs.
func TestNoOtherClientReachesTheHandler(t *testing.T) { // REQ-SEC-006, INV-12, INV-03
	w := newWorld(t)
	other := newCA(t, "synthetic other CA")
	w.start()
	mk := func(a *authority, o leafOpts) tls.Certificate { _, _, p := a.leaf(t, o); return p }
	client := []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	cases := map[string][]tls.Certificate{
		"no client certificate":      nil,
		"signed by another CA":       {mk(other, leafOpts{cn: operator, usages: client})},
		"the server CA's own client": {mk(w.srvCA, leafOpts{cn: operator, usages: client})},
		"expired":                    {mk(w.opCA, leafOpts{cn: operator, usages: client, notBefore: synthNow.Add(-2 * time.Hour), notAfter: synthNow.Add(-time.Hour)})},
		"not yet valid":              {mk(w.opCA, leafOpts{cn: operator, usages: client, notBefore: synthNow.Add(time.Hour), notAfter: synthNow.Add(2 * time.Hour)})},
		"server authentication only": {mk(w.opCA, leafOpts{cn: operator, usages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}})},
	}
	for name, certs := range cases {
		t.Run(name, func(t *testing.T) {
			// A TLS 1.3 server rejects the client after the client's handshake call returns, so the error can show on the request or not at all: what must hold is that no 2xx
			// answer comes back and the kill switch path is never called.
			code, _, err := w.do(w.client(certs...), http.MethodPost, setPath, "")
			if err == nil && code/100 == 2 {
				t.Fatalf("status %d: the request was served", code)
			}
			if n := len(w.sw.got()); n != 0 {
				t.Fatalf("%d calls to the kill switch path; want none", n)
			}
		})
	}
}

// The listener does not depend on the TLS layer alone: a request that did not come through a verified client certificate is refused by the handler itself.
func TestHandlerRefusesARequestWithoutAVerifiedChain(t *testing.T) { // INV-12, REQ-SEC-006
	w := newWorld(t)
	l, err := opslistener.New(w.cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, _, pair := w.opCA.leaf(t, leafOpts{cn: operator, usages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}})
	leaf, _ := x509.ParseCertificate(pair.Certificate[0])
	_, _, srvOnly := w.opCA.leaf(t, leafOpts{cn: operator, usages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}})
	badUse, _ := x509.ParseCertificate(srvOnly.Certificate[0])
	cases := map[string]*tls.ConnectionState{
		"verified chain, but not for client authentication": {PeerCertificates: []*x509.Certificate{badUse}, VerifiedChains: [][]*x509.Certificate{{badUse}}},
		"no TLS at all":                      nil,
		"TLS with no peer certificate":       {},
		"a peer certificate, never verified": {PeerCertificates: []*x509.Certificate{leaf}},
	}
	for name, st := range cases {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, setPath, nil)
			req.TLS = st
			rec := httptest.NewRecorder()
			l.Handler().ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized && rec.Code != http.StatusForbidden {
				t.Fatalf("status %d; want 401 or 403", rec.Code)
			}
			if n := len(w.sw.got()); n != 0 {
				t.Fatalf("%d calls to the kill switch path; want none", n)
			}
		})
	}
}

// A certificate the CA signed but whose name is not a usable principal is refused: the audit trail must name a person or a service (grammar of the Record's actor).
func TestACertificateWithoutAUsablePrincipalIsRefused(t *testing.T) { // INV-12, REQ-AUD-001
	for name, cn := range map[string]string{"no common name": "", "a space": "bad name", "a colon": "admin:x", "a slash": "a/b", "leading dash": "-x", "65 characters": strings.Repeat("a", 65), "a newline": "a\nb"} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			w.start()
			_, _, pair := w.opCA.leaf(t, leafOpts{cn: cn, usages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}})
			code, _, err := w.do(w.client(pair), http.MethodPost, setPath, "")
			if err == nil && code != http.StatusForbidden && code != http.StatusUnauthorized {
				t.Fatalf("status %d; want 401 or 403", code)
			}
			if n := len(w.sw.got()); n != 0 {
				t.Fatalf("%d calls to the kill switch path; want none", n)
			}
		})
	}
}

// INV-12: identity is the certificate and nothing else. Headers, query, and bodies that claim another person or the verification role change nothing, and a body is refused.
func TestIdentityComesOnlyFromTheCertificate(t *testing.T) { // INV-12, AGENT-10
	w := newWorld(t)
	w.start()
	c := w.client(w.operator)
	req, _ := http.NewRequest(http.MethodPost, w.srv.URL+setPath+"?principal=usr_other&role=verify&actor=admin:usr_other", nil)
	for _, h := range []string{"X-Principal", "X-Actor", "X-Role", "X-Forwarded-User", "X-Forwarded-Client-Cert", "Authorization"} {
		req.Header.Set(h, "usr_other")
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	got := w.sw.got()
	if len(got) != 1 || got[0].caller != (controlplane.Caller{Role: controlplane.RoleAdmin, PrincipalID: operator}) {
		t.Fatalf("calls = %+v; want one with the certificate's identity and the admin role", got)
	}
	code, _, err := w.do(c, http.MethodPost, setPath, `{"principal":"usr_other","role":"verify"}`)
	if err != nil || code != http.StatusBadRequest {
		t.Fatalf("a request body: status %d, err %v; want 400", code, err)
	}
	if n := len(w.sw.got()); n != 1 {
		t.Fatalf("%d calls; a refused body must not reach the kill switch path", n)
	}
}

// The verification role is never produced here: it may set and never clear, and an operator listener is not its path.
func TestNoRequestEverCarriesTheVerifyRole(t *testing.T) { // INV-11, REQ-AUD-005
	w := newWorld(t)
	w.start()
	c := w.client(w.operator)
	for _, p := range []string{setPath, clrPath, setPath, clrPath} {
		if _, _, err := w.do(c, http.MethodPost, p, ""); err != nil {
			t.Fatal(err)
		}
	}
	for _, g := range w.sw.got() {
		if g.caller.Role != controlplane.RoleAdmin {
			t.Fatalf("call %+v has role %v; want admin only", g, g.caller.Role)
		}
	}
}

func TestOnlyTheTwoRoutesAndOnlyPost(t *testing.T) { // REQ-SEC-006
	w := newWorld(t)
	w.start()
	c := w.client(w.operator)
	for name, tc := range map[string]struct {
		method, path string
		want         int
	}{
		"GET set":        {http.MethodGet, setPath, http.StatusMethodNotAllowed},
		"PUT clear":      {http.MethodPut, clrPath, http.StatusMethodNotAllowed},
		"DELETE set":     {http.MethodDelete, setPath, http.StatusMethodNotAllowed},
		"unknown action": {http.MethodPost, "/v1/ops/tenants/" + tenant + "/kill-switch:toggle", http.StatusNotFound},
		"another route":  {http.MethodPost, "/v1/ops/keys:register", http.StatusNotFound},
		"the root":       {http.MethodPost, "/", http.StatusNotFound},
		"no tenant":      {http.MethodPost, "/v1/ops/tenants//kill-switch:set", http.StatusNotFound},
	} {
		t.Run(name, func(t *testing.T) {
			code, _, err := w.do(c, tc.method, tc.path, "")
			if err != nil || code != tc.want {
				t.Fatalf("status %d, err %v; want %d", code, err, tc.want)
			}
		})
	}
	if n := len(w.sw.got()); n != 0 {
		t.Fatalf("%d calls; want none", n)
	}
}

// The tenant is named in the path and checked against the ID grammar before anything is called; the kill switch path then reaches only that tenant's own login.
func TestATenantIDOutsideTheGrammarIsRefused(t *testing.T) { // REQ-SEC-004, INV-12
	w := newWorld(t)
	w.start()
	c := w.client(w.operator)
	for name, id := range map[string]string{"no prefix": "aa", "upper prefix": "TEN_aa", "a dot": "ten_a.b", "33 characters": "ten_" + strings.Repeat("a", 33), "one character": "ten_a", "encoded slash": "ten_a%2Fb", "encoded newline": "ten_aa%0A"} {
		t.Run(name, func(t *testing.T) {
			code, _, err := w.do(c, http.MethodPost, "/v1/ops/tenants/"+id+"/kill-switch:set", "")
			if err != nil || code != http.StatusBadRequest && code != http.StatusNotFound {
				t.Fatalf("status %d, err %v; want 400 or 404", code, err)
			}
			if n := len(w.sw.got()); n != 0 {
				t.Fatalf("%d calls; want none", n)
			}
		})
	}
}

// The answers say nothing about the tenant, the person or the cause (AGENT-13): fixed text per class, and the status says which class.
func TestErrorsMapToFixedAnswers(t *testing.T) { // REQ-AUD-001, AGENT-13
	for name, tc := range map[string]struct {
		err  error
		want int
	}{
		"forbidden":        {controlplane.ErrForbidden, http.StatusForbidden},
		"caller":           {controlplane.ErrCaller, http.StatusUnauthorized},
		"config":           {controlplane.ErrConfig, http.StatusInternalServerError},
		"database failure": {errors.New("pq: password for ten_aa on host db-secret.internal"), http.StatusInternalServerError},
		"joined":           {errors.Join(controlplane.ErrForbidden, errors.New("the Record could not take usr_oper1")), http.StatusForbidden},
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			w.sw.err = tc.err
			w.start()
			code, body, err := w.do(w.client(w.operator), http.MethodPost, clrPath, "")
			if err != nil || code != tc.want {
				t.Fatalf("status %d, err %v; want %d", code, err, tc.want)
			}
			for _, leak := range []string{tenant, operator, "password", "db-secret", "Record could not"} {
				if strings.Contains(body, leak) {
					t.Fatalf("the answer %q names %q", body, leak)
				}
			}
		})
	}
}

// EC-E16 in spirit: with no correlation ID there is no Record trail, so nothing is called.
func TestNoCorrelationIDNoCall(t *testing.T) { // REQ-AUD-001, EC-E16
	w := newWorld(t)
	w.cfg.CorrelationIDs = func() (string, error) { return "", errors.New("synthetic: no entropy") }
	w.start()
	code, _, err := w.do(w.client(w.operator), http.MethodPost, setPath, "")
	if err != nil || code != http.StatusInternalServerError {
		t.Fatalf("status %d, err %v; want 500", code, err)
	}
	if n := len(w.sw.got()); n != 0 {
		t.Fatalf("%d calls; want none", n)
	}
}

// The TLS configuration: client certificates are required and verified against the operator CA only (not the system roots), the floor is TLS 1.3, and the time is the clock's.
func TestTLSConfigIsStrict(t *testing.T) { // REQ-SEC-006, INV-12
	w := newWorld(t)
	l, err := opslistener.New(w.cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c := l.TLSConfig()
	if c == nil {
		t.Fatal("no TLS configuration")
	}
	if c.ClientAuth != tls.RequireAndVerifyClientCert {
		t.Fatalf("ClientAuth = %v; want RequireAndVerifyClientCert", c.ClientAuth)
	}
	if c.MinVersion < tls.VersionTLS13 {
		t.Fatalf("MinVersion = %#x; want TLS 1.3 or later", c.MinVersion)
	}
	if c.ClientCAs == nil || len(c.ClientCAs.Subjects()) != 1 { // Subjects counts a pool this process built itself
		t.Fatal("ClientCAs must hold exactly the operator CA")
	}
	if c.Time == nil || !c.Time().Equal(synthNow) {
		t.Fatal("the verification time must come from the injected clock")
	}
	if len(c.Certificates) != 1 {
		t.Fatalf("%d server certificates; want 1", len(c.Certificates))
	}
}

// Every part is required and nothing has a default (AGENT-16, A330). Each error names the setting and never a path's content or a key.
func TestNewRefusesAnIncompleteOrBadConfiguration(t *testing.T) { // AGENT-16, REQ-SEC-009
	w0 := newWorld(t)
	dir := t.TempDir()
	badCA := writeFile(t, dir, "ca.txt", []byte("not a certificate"))
	otherSC, _, _ := w0.srvCA.leaf(t, leafOpts{cn: "x", usages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}})
	_, otherK, _ := w0.srvCA.leaf(t, leafOpts{cn: "y", usages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}})
	mismatchC := writeFile(t, dir, "m.crt", otherSC)
	mismatchK := writeFile(t, dir, "m.key", otherK)
	for name, tc := range map[string]struct {
		mod  func(*opslistener.Config)
		need string
	}{
		"no client CA file":                   {func(c *opslistener.Config) { c.ClientCAFile = "" }, opslistener.EnvClientCAFile},
		"no server cert file":                 {func(c *opslistener.Config) { c.ServerCertFile = "" }, opslistener.EnvServerCertFile},
		"no server key file":                  {func(c *opslistener.Config) { c.ServerKeyFile = "" }, opslistener.EnvServerKeyFile},
		"a CA file with no PEM":               {func(c *opslistener.Config) { c.ClientCAFile = badCA }, opslistener.EnvClientCAFile},
		"a CA file that is gone":              {func(c *opslistener.Config) { c.ClientCAFile = dir + "/gone" }, opslistener.EnvClientCAFile},
		"a key that is not the certificate's": {func(c *opslistener.Config) { c.ServerCertFile, c.ServerKeyFile = mismatchC, mismatchK }, opslistener.EnvServerKeyFile},
		"no reader":                           {func(c *opslistener.Config) { c.ReadFile = nil }, "ReadFile"},
		"no secret reader":                    {func(c *opslistener.Config) { c.ReadSecret = nil }, "ReadSecret"},
		"no clock":                            {func(c *opslistener.Config) { c.Clock = nil }, "Clock"},
		"no kill switch path":                 {func(c *opslistener.Config) { c.Switch = nil }, "Switch"},
		"no correlation source":               {func(c *opslistener.Config) { c.CorrelationIDs = nil }, "CorrelationIDs"},
		"no header timeout":                   {func(c *opslistener.Config) { c.HeaderTimeout = 0 }, "HeaderTimeout"},
		"no read timeout":                     {func(c *opslistener.Config) { c.ReadTimeout = 0 }, "ReadTimeout"},
		"no idle timeout":                     {func(c *opslistener.Config) { c.IdleTimeout = 0 }, "IdleTimeout"},
		"no header size bound":                {func(c *opslistener.Config) { c.MaxHeaderBytes = 0 }, "MaxHeaderBytes"},
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			tc.mod(&w.cfg)
			l, err := opslistener.New(w.cfg)
			if err == nil || l != nil {
				t.Fatalf("New = %v, %v; want an error and no listener", l, err)
			}
			if !strings.Contains(err.Error(), tc.need) {
				t.Fatalf("error %q does not name %s", err, tc.need)
			}
		})
	}
}

// The secret reader is the one that reads the server's private key, and the plain reader never does (SEC-09, REQ-SEC-009, INV-29).
func TestTheServerKeyIsReadOnlyBySecretReader(t *testing.T) { // REQ-SEC-009
	w := newWorld(t)
	var plain, secret []string
	inner := w.cfg.ReadFile
	w.cfg.ReadFile = func(p string) ([]byte, error) { plain = append(plain, p); return inner(p) }
	w.cfg.ReadSecret = func(p string) ([]byte, error) { secret = append(secret, p); return inner(p) }
	if _, err := opslistener.New(w.cfg); err != nil {
		t.Fatalf("New: %v", err)
	}
	if len(secret) != 1 || secret[0] != w.cfg.ServerKeyFile {
		t.Fatalf("secret reader read %v; want only the key file", secret)
	}
	for _, p := range plain {
		if p == w.cfg.ServerKeyFile {
			t.Fatal("the plain reader read the private key")
		}
	}
}

// Serve runs the TLS listener until the context ends, then returns nil; a request in the meantime is served over mTLS.
func TestServeRunsUntilTheContextEnds(t *testing.T) { // REQ-OPS-003
	w := newWorld(t)
	l, err := opslistener.New(w.cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- l.Serve(ctx, ln) }()
	w.srv = &httptest.Server{URL: "https://" + ln.Addr().String()}
	code, _, err := w.do(w.client(w.operator), http.MethodPost, setPath, "")
	if err != nil || code/100 != 2 {
		t.Fatalf("status %d, err %v; want 2xx", code, err)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve returned %v after its context ended; want nil", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Serve did not return after its context ended")
	}
}

// A client that opens a connection and never finishes its headers is cut off after the header timeout (REQ-OPS-003).
func TestASlowClientIsCutOff(t *testing.T) { // REQ-OPS-003
	w := newWorld(t)
	w.cfg.HeaderTimeout = 300 * time.Millisecond
	l, err := opslistener.New(w.cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- l.Serve(ctx, ln) }()
	t.Cleanup(func() { cancel(); <-done })
	conn, err := tls.Dial("tcp", ln.Addr().String(), &tls.Config{RootCAs: w.srvPool, Certificates: []tls.Certificate{w.operator}, Time: func() time.Time { return synthNow }})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("POST /v1/ops/tenants/")); err != nil {
		t.Fatal(err)
	}
	closed := make(chan struct{})
	go func() { _, _ = io.ReadAll(conn); close(closed) }()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("the connection was not closed by the server after the header timeout")
	}
}

// A certificate holder that sends the headers and then stalls on a body is cut off by the read timeout, not held for ever (REQ-OPS-003; SR condition 3 on #1013).
func TestAStalledBodyIsCutOffByTheReadTimeout(t *testing.T) { // REQ-OPS-003
	w := newWorld(t)
	w.cfg.ReadTimeout = 300 * time.Millisecond
	l, err := opslistener.New(w.cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- l.Serve(ctx, ln) }()
	t.Cleanup(func() { cancel(); <-done })
	conn, err := tls.Dial("tcp", ln.Addr().String(), &tls.Config{RootCAs: w.srvPool, Certificates: []tls.Certificate{w.operator}, Time: func() time.Time { return synthNow }})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	req := "POST /v1/ops/tenants/ten_ABC/kill-switch:set HTTP/1.1\r\nHost: localhost\r\nTransfer-Encoding: chunked\r\n\r\n5\r\nab"
	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatal(err)
	}
	closed := make(chan struct{})
	go func() { _, _ = io.ReadAll(conn); close(closed) }()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("the connection was held open past the read timeout")
	}
}

// A request whose headers exceed the bound is refused with 431 (REQ-OPS-003).
func TestOversizedHeadersAreRefused(t *testing.T) { // REQ-OPS-003
	w := newWorld(t)
	w.cfg.MaxHeaderBytes = 1024
	l, err := opslistener.New(w.cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- l.Serve(ctx, ln) }()
	t.Cleanup(func() { cancel(); <-done })
	conn, err := tls.Dial("tcp", ln.Addr().String(), &tls.Config{RootCAs: w.srvPool, Certificates: []tls.Certificate{w.operator}, Time: func() time.Time { return synthNow }})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	req := "POST /v1/ops/tenants/ten_ABC/kill-switch:set HTTP/1.1\r\nHost: localhost\r\nX-Pad: " + strings.Repeat("a", 16384) + "\r\n\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	b, _ := io.ReadAll(conn)
	if !strings.HasPrefix(string(b), "HTTP/1.1 431") {
		t.Fatalf("response %q; want 431", string(b))
	}
}
