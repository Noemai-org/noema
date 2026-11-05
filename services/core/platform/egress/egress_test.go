package egress_test

import (
	"context"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/platform/egress"
)

// SEC-05, REQ-SEC-009, REQ-API-006, TM-25, PERD 39.4 "Callback registration safety". Acceptance (REQ-SEC-009): registering a callback to a
// private, loopback or link-local address fails; a delivery to a host that resolves to a private address after registration is refused.
// Written before the implementation. No test touches the network: the resolver and the dial function are injected.

type fakeResolver map[string][]string

func (f fakeResolver) LookupNetIP(_ context.Context, _ string, host string) ([]netip.Addr, error) {
	var out []netip.Addr
	for _, s := range f[host] {
		out = append(out, netip.MustParseAddr(s))
	}
	if len(out) == 0 {
		return nil, errors.New("no such host")
	}
	return out, nil
}

const publicIP = "93.184.216.34"

// Every address that is not a public unicast address is refused, in every spelling the standard library can produce.
func TestNonPublicAddressesAreRefused(t *testing.T) {
	for _, s := range []string{
		"127.0.0.1", "127.255.255.254", "::1", "0.0.0.0", "::", "10.0.0.1", "10.255.255.255", "172.16.0.1", "172.31.255.255", "192.168.1.1",
		"169.254.169.254" /* cloud metadata */, "169.254.0.1", "fe80::1", "fc00::1", "fd12:3456::1", "100.64.0.1" /* CGNAT */, "100.127.255.255",
		"192.0.0.1", "192.0.2.1", "198.18.0.1", "198.19.255.255", "198.51.100.1", "203.0.113.1", "224.0.0.1", "239.255.255.255", "ff02::1",
		"240.0.0.1", "255.255.255.255", "2001:db8::1",
		"::ffff:127.0.0.1", "::ffff:10.0.0.1", "::ffff:169.254.169.254", // IPv4-mapped
		"64:ff9b::7f00:1", "64:ff9b::a00:1", // NAT64 with an embedded private v4
		"2002:7f00:1::", "2002:a00:1::", // 6to4 with an embedded private v4
		"2001:0:4136:e378:8000:63bf:3fff:fdd2", // Teredo
	} {
		if egress.IsPublic(netip.MustParseAddr(s)) {
			t.Errorf("%s was accepted as a public address", s)
		}
	}
	if egress.IsPublic(netip.MustParseAddr("::ffff:" + publicIP)) {
		t.Error("an IPv4-mapped address was accepted, even a mapped public one; mapped addresses are refused outright")
	}
	for _, s := range []string{publicIP, "8.8.8.8", "1.1.1.1", "2606:4700:4700::1111", "2a00:1450:4009:81f::200e"} {
		if !egress.IsPublic(netip.MustParseAddr(s)) {
			t.Errorf("%s was refused; a public address must be accepted", s)
		}
	}
}

// The structural rules of PERD 39.4: HTTPS, a registered-style DNS name, port 443, no credentials, no numeric host in any spelling.
func TestRegistrationRefusesMalformedAndSmugglingURLs(t *testing.T) {
	bad := []string{
		"", "http://cb.example.com/hook", "ftp://cb.example.com/", "//cb.example.com/", "cb.example.com/hook", "https:///hook",
		"https://user@cb.example.com/", "https://user:pw@cb.example.com/", "https://evil.example.com@127.0.0.1/", "https://cb.example.com:8443/", "https://cb.example.com:80/",
		"https://127.0.0.1/", "https://[::1]/", "https://[::ffff:127.0.0.1]/", "https://169.254.169.254/latest/meta-data/", "https://93.184.216.34/",
		"https://2130706433/", "https://0x7f.1/", "https://0x7f000001/", "https://017700000001/", "https://127.1/", "https://127.0.0.1.rebind.example.com.",
		"https://localhost/", "https://intranet/", "https://cb/", "https://cb.example.com./hook", "https://.example.com/", "https://cb..example.com/",
		"https://cb.example.com#frag", "https://cb_x.example.com/", "https://-cb.example.com/", "https://cb.example.com\t/", "https://cb.example.com\\@127.0.0.1/",
		"https://bücher.example.com/", "https://cb.example.com/\x00", "https://cb.example.com/%0d%0aHost:x", "https://1.2.3.4.5/", "https://example.123/",
	}
	for _, raw := range bad {
		if _, err := egress.ParseCallbackURL(raw); err == nil {
			t.Errorf("ParseCallbackURL(%q) accepted a URL that must be refused", raw)
		} else if !errors.Is(err, egress.ErrEgressDenied) {
			t.Errorf("ParseCallbackURL(%q) error %v is not ErrEgressDenied (audit reason EGRESS_DENIED)", raw, err)
		}
	}
	for _, raw := range []string{"https://cb.example.com/hook", "https://CB.Example.COM:443/a/b?x=1", "https://partner-1.api.example.co.uk/v1/outcomes"} {
		u, err := egress.ParseCallbackURL(raw)
		if err != nil {
			t.Errorf("ParseCallbackURL(%q) refused a valid URL: %v", raw, err)
		} else if u.Host != "cb.example.com" && u.Host != "partner-1.api.example.co.uk" {
			t.Errorf("host not normalised to lower case: %q", u.Host)
		}
	}
}

func newGuard(r fakeResolver) *egress.Guard {
	g, err := egress.NewGuard(r, egress.Options{LookupTimeout: 2 * time.Second, DialTimeout: 2 * time.Second})
	if err != nil {
		panic(err)
	}
	return g
}

func TestTimeoutsAreRequired(t *testing.T) {
	for _, o := range []egress.Options{{}, {LookupTimeout: time.Second}, {DialTimeout: time.Second}, {LookupTimeout: -1, DialTimeout: time.Second}} {
		if _, err := egress.NewGuard(nil, o); err == nil {
			t.Errorf("NewGuard accepted %+v; the timeouts have no default", o)
		}
	}
}

// REQ-SEC-009 acceptance, first half: registering a callback whose host resolves to a private, loopback or link-local address fails,
// and so does a host with ANY non-public answer among public ones.
func TestRegistrationRefusesAHostThatResolvesToANonPublicAddress(t *testing.T) {
	for name, answers := range map[string][]string{
		"loopback":            {"127.0.0.1"},
		"private":             {"10.1.2.3"},
		"link-local metadata": {"169.254.169.254"},
		"ipv6 loopback":       {"::1"},
		"mapped loopback":     {"::ffff:127.0.0.1"},
		"public then private": {publicIP, "192.168.0.10"},
		"private then public": {"10.0.0.1", publicIP},
	} {
		g := newGuard(fakeResolver{"cb.example.com": answers})
		if err := g.Register(context.Background(), "https://cb.example.com/hook"); !errors.Is(err, egress.ErrEgressDenied) {
			t.Errorf("%s: Register error = %v, want ErrEgressDenied", name, err)
		}
	}
	g := newGuard(fakeResolver{"nx.example.com": nil})
	if err := g.Register(context.Background(), "https://nx.example.com/"); !errors.Is(err, egress.ErrEgressDenied) {
		t.Errorf("an unresolvable host must be refused (fail closed), got %v", err)
	}
	g = newGuard(fakeResolver{"cb.example.com": {publicIP}})
	if err := g.Register(context.Background(), "https://cb.example.com/hook"); err != nil {
		t.Fatalf("a public host was refused: %v", err)
	}
}

// REQ-SEC-009 acceptance, second half, and PERD 39.4 "re-resolved and re-checked on every delivery": a host that was public at
// registration and resolves to a private address at delivery is refused (DNS rebinding), and so is an unregistered host.
func TestDeliveryReChecksTheHostEveryTime(t *testing.T) {
	r := fakeResolver{"cb.example.com": {publicIP}, "other.example.com": {publicIP}}
	g := newGuard(r)
	if err := g.Register(context.Background(), "https://cb.example.com/hook"); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Resolve(context.Background(), "https://cb.example.com/hook"); err != nil {
		t.Fatalf("a registered public host was refused at delivery: %v", err)
	}
	if _, err := g.Resolve(context.Background(), "https://other.example.com/hook"); !errors.Is(err, egress.ErrEgressDenied) {
		t.Errorf("an unregistered host reached delivery: %v", err)
	}
	r["cb.example.com"] = []string{"127.0.0.1"} // the rebinding
	if _, err := g.Resolve(context.Background(), "https://cb.example.com/hook"); !errors.Is(err, egress.ErrEgressDenied) {
		t.Errorf("a host that now resolves to loopback was accepted: %v", err)
	}
	r["cb.example.com"] = []string{publicIP, "10.0.0.5"} // mixed answers
	if _, err := g.Resolve(context.Background(), "https://cb.example.com/hook"); !errors.Is(err, egress.ErrEgressDenied) {
		t.Errorf("a host with one private answer was accepted: %v", err)
	}
}

// The connection goes to the address that was checked, never to a second lookup of the name (which could differ), and a name that
// resolves to loopback is refused before any connection is made.
func TestTheTransportDialsTheCheckedAddressAndNothingElse(t *testing.T) {
	r := fakeResolver{"cb.example.com": {publicIP}}
	g := newGuard(r)
	if err := g.Register(context.Background(), "https://cb.example.com/hook"); err != nil {
		t.Fatal(err)
	}
	var dialed []string
	g.SetDialForTest(func(_ context.Context, network, addr string) (net.Conn, error) {
		dialed = append(dialed, addr)
		return nil, errors.New("test dial: no network")
	})
	tr := g.Transport()
	_, _ = tr.DialTLSContext(context.Background(), "tcp", "cb.example.com:443")
	if len(dialed) != 1 || dialed[0] != publicIP+":443" {
		t.Fatalf("dialed %v, want exactly [%s:443]", dialed, publicIP)
	}

	// Rebinding between registration and the dial: the loopback listener must see no connection at all.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	accepted := make(chan struct{}, 1)
	go func() {
		if c, err := ln.Accept(); err == nil {
			accepted <- struct{}{}
			_ = c.Close()
		}
	}()
	r["cb.example.com"] = []string{"127.0.0.1"}
	dialed = nil
	g.SetDialForTest(nil) // the real dialer
	if _, err := tr.DialTLSContext(context.Background(), "tcp", "cb.example.com:443"); !errors.Is(err, egress.ErrEgressDenied) {
		t.Fatalf("dial to a rebound host = %v, want ErrEgressDenied", err)
	}
	select {
	case <-accepted:
		t.Fatal("a connection reached the loopback listener")
	case <-time.After(150 * time.Millisecond):
	}
	r["unregistered.example.com"] = []string{publicIP} // resolves to a public address, but was never registered
	if _, err := tr.DialTLSContext(context.Background(), "tcp", "unregistered.example.com:443"); !errors.Is(err, egress.ErrEgressDenied) {
		t.Fatalf("dial to an unregistered host = %v, want ErrEgressDenied", err)
	}
	if _, err := tr.DialTLSContext(context.Background(), "tcp", "cb.example.com:8080"); !errors.Is(err, egress.ErrEgressDenied) {
		t.Fatalf("dial to port 8080 = %v, want ErrEgressDenied", err)
	}
}

// No redirect is followed, the environment's proxy settings are ignored, and the body that is read is bounded.
func TestTheClientNeverFollowsRedirectsOrProxies(t *testing.T) {
	g := newGuard(fakeResolver{"cb.example.com": {publicIP}})
	c := g.Client(5 * time.Second)
	if c.CheckRedirect == nil {
		t.Fatal("no CheckRedirect: redirects would be followed")
	}
	if err := c.CheckRedirect(nil, nil); err == nil {
		t.Fatal("CheckRedirect allows a redirect")
	}
	tr := g.Transport()
	if tr.Proxy != nil {
		t.Fatal("the transport uses a proxy function: HTTP_PROXY could redirect deliveries")
	}
	if tr.TLSClientConfig == nil || tr.TLSClientConfig.MinVersion < 0x0303 || tr.TLSClientConfig.InsecureSkipVerify {
		t.Fatalf("TLS must verify certificates and require TLS 1.2 or later: %+v", tr.TLSClientConfig)
	}
	if c.Timeout <= 0 {
		t.Fatal("no timeout")
	}
}

// Plaintext is structurally impossible (Architect, 3 Nov, Codex finding on #127): a registered host on port 443 must never be sent an
// http:// request. ParseCallbackURL refuses the scheme at registration, but the client and the transport are reachable with any request,
// so the refusal has to hold before any dial.
func TestAPlainHTTPRequestToARegisteredHostNeverDials(t *testing.T) {
	g := newGuard(fakeResolver{"cb.example.com": {publicIP}})
	if err := g.Register(context.Background(), "https://cb.example.com/hook"); err != nil {
		t.Fatal(err)
	}
	dials := 0
	g.SetDialForTest(func(context.Context, string, string) (net.Conn, error) {
		dials++
		return nil, errors.New("test dial: no network")
	})
	c := g.Client(2 * time.Second)
	for _, raw := range []string{"http://cb.example.com:443/hook", "http://cb.example.com/hook", "HTTP://CB.EXAMPLE.COM:443/hook", "ws://cb.example.com:443/", "ftp://cb.example.com:443/"} {
		req, err := http.NewRequest(http.MethodPost, raw, strings.NewReader("{}"))
		if err != nil {
			t.Fatal(err)
		}
		resp, err := c.Do(req)
		if err == nil {
			resp.Body.Close()
			t.Errorf("%s: the request was sent", raw)
		} else if !errors.Is(err, egress.ErrEgressDenied) {
			t.Errorf("%s: error %v is not ErrEgressDenied", raw, err)
		}
	}
	if dials != 0 {
		t.Fatalf("%d connection(s) were attempted for plaintext requests; none may be", dials)
	}
	// The transport on its own, with no client in front of it: the plaintext dial path is closed, and the TLS path is the only one.
	tr := g.Transport()
	if _, err := tr.DialContext(context.Background(), "tcp", "cb.example.com:443"); !errors.Is(err, egress.ErrEgressDenied) {
		t.Fatalf("the plaintext dial function = %v, want ErrEgressDenied", err)
	}
	if dials != 0 {
		t.Fatalf("the plaintext dial function connected (%d)", dials)
	}
	if tr.DialTLSContext == nil {
		t.Fatal("no TLS dial function: https requests would use the plaintext path")
	}
	// A request URL that is not callback-shaped is refused before any dial, even over https.
	for _, raw := range []string{"https://cb.example.com:8443/hook", "https://127.0.0.1/hook", "https://user@cb.example.com/hook", "https://unregistered.example.com/hook"} {
		req, _ := http.NewRequest(http.MethodPost, raw, strings.NewReader("{}"))
		if resp, err := c.Do(req); err == nil {
			resp.Body.Close()
			t.Errorf("%s: the request was sent", raw)
		} else if !errors.Is(err, egress.ErrEgressDenied) {
			t.Errorf("%s: error %v is not ErrEgressDenied", raw, err)
		}
	}
	if dials != 0 {
		t.Fatalf("%d connection(s) were attempted for refused requests", dials)
	}
}

// The https path still reaches the pinned, checked address (and only it) through the TLS dial function.
func TestTheTLSDialFunctionDialsTheCheckedAddress(t *testing.T) {
	g := newGuard(fakeResolver{"cb.example.com": {publicIP}})
	if err := g.Register(context.Background(), "https://cb.example.com/hook"); err != nil {
		t.Fatal(err)
	}
	var dialed []string
	g.SetDialForTest(func(_ context.Context, _, addr string) (net.Conn, error) {
		dialed = append(dialed, addr)
		return nil, errors.New("test dial: no network")
	})
	_, _ = g.Transport().DialTLSContext(context.Background(), "tcp", "cb.example.com:443")
	if len(dialed) != 1 || dialed[0] != publicIP+":443" {
		t.Fatalf("TLS dial went to %v, want exactly [%s:443]", dialed, publicIP)
	}
}

// A real TLS delivery through the guard: the connection goes to the checked address (here redirected to a local server by the test dial),
// the handshake verifies the certificate for the registered name, and a certificate for another name is refused.
func TestAHTTPSDeliveryVerifiesTheCertificate(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_, _ = w.Write([]byte("got " + string(b)))
	}))
	defer ts.Close()
	pool := x509.NewCertPool()
	pool.AddCert(ts.Certificate())
	r := fakeResolver{"example.com": {publicIP}, "mismatch.example.org": {publicIP}}
	g := newGuard(r)
	g.SetRootCAsForTest(pool)
	var dialed []string
	g.SetDialForTest(func(ctx context.Context, network, addr string) (net.Conn, error) {
		dialed = append(dialed, addr)
		var d net.Dialer
		return d.DialContext(ctx, network, ts.Listener.Addr().String())
	})
	for _, h := range []string{"example.com", "mismatch.example.org"} {
		if err := g.Register(context.Background(), "https://"+h+"/hook"); err != nil {
			t.Fatal(err)
		}
	}
	c := g.Client(5 * time.Second)
	resp, err := c.Post("https://example.com/hook", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("a valid https delivery failed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != "got {}" || len(dialed) != 1 || dialed[0] != publicIP+":443" {
		t.Fatalf("body %q, dialed %v; want the response and exactly [%s:443]", body, dialed, publicIP)
	}
	if resp, err := c.Post("https://mismatch.example.org/hook", "application/json", strings.NewReader("{}")); err == nil {
		resp.Body.Close()
		t.Fatal("a certificate that is not valid for the registered name (the test certificate covers example.com and *.example.com) was accepted")
	}
}
