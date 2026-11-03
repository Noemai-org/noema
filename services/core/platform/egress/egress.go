package egress

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ErrEgressDenied is the one error of every refusal. It maps to the audit reason EGRESS_DENIED.
var ErrEgressDenied = errors.New("EGRESS_DENIED")

type deniedError struct{ why string }

func (e *deniedError) Error() string        { return "EGRESS_DENIED: " + e.why }
func (e *deniedError) Is(target error) bool { return target == ErrEgressDenied }

func denied(format string, args ...any) error { return &deniedError{fmt.Sprintf(format, args...)} }

// Protocol limits, not tunable thresholds: the HTTPS port (RFC 9110), the DNS name and label lengths (RFC 1035) and the header bound.
const (
	callbackPort   = "443"
	maxHostLen     = 253
	maxLabelLen    = 63
	maxHeaderBytes = 16 << 10
)

// Options carries the timeouts. They are required and have no default: a safety timeout is configuration with a change record
// (REQ-OPS-003, AGENT-16), so the caller passes the configured value.
type Options struct {
	LookupTimeout time.Duration // DNS resolution, at registration and at every delivery
	DialTimeout   time.Duration // the TCP connect, the TLS handshake and the wait for response headers
}

// Resolver is what the guard needs from DNS; *net.Resolver satisfies it.
type Resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

var nonPublicV4 = mustPrefixes(
	"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24",
	"192.88.99.0/24", "192.168.0.0/16", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4",
)

// IPv6: only the allocated global unicast space 2000::/3 is public, minus the ranges inside it that are not routable to a public host.
var (
	globalV6    = netip.MustParsePrefix("2000::/3")
	nonPublicV6 = mustPrefixes("2001::/23", "2001:db8::/32", "2002::/16", "3fff::/20")
)

func mustPrefixes(ss ...string) []netip.Prefix {
	out := make([]netip.Prefix, len(ss))
	for i, s := range ss {
		out[i] = netip.MustParsePrefix(s)
	}
	return out
}

// IsPublic reports whether a is a public unicast address. An IPv4-mapped IPv6 address is refused outright, whatever it carries, as are NAT64,
// 6to4, Teredo, unique-local, link-local, documentation, loopback, multicast and unspecified addresses.
func IsPublic(a netip.Addr) bool {
	if !a.IsValid() || a.Zone() != "" {
		return false
	}
	if a.Is4() {
		if !a.IsGlobalUnicast() {
			return false
		}
		for _, p := range nonPublicV4 {
			if p.Contains(a) {
				return false
			}
		}
		return true
	}
	if !globalV6.Contains(a) {
		return false
	}
	for _, p := range nonPublicV6 {
		if p.Contains(a) {
			return false
		}
	}
	return a.IsGlobalUnicast()
}

var (
	hostLabel   = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	tldLabel    = regexp.MustCompile(`^([a-z]{2,63}|xn--[a-z0-9-]{1,59})$`)
	encodedCtrl = regexp.MustCompile(`(?i)%(0[0-9a-f]|1[0-9a-f]|7f)`)
)

// ParseCallbackURL applies the structural rules of PERD 39.4 and returns the URL with its host lower-cased and the default port removed.
// It does no DNS. Every refusal is ErrEgressDenied.
func ParseCallbackURL(raw string) (*url.URL, error) {
	if raw == "" {
		return nil, denied("empty URL")
	}
	for i := 0; i < len(raw); i++ {
		if c := raw[i]; c <= 0x20 || c >= 0x7f || c == '\\' || c == '#' {
			return nil, denied("the URL contains a space, control, non-ASCII, backslash or fragment character")
		}
	}
	if encodedCtrl.MatchString(raw) {
		return nil, denied("the URL contains an encoded control character")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, denied("the URL does not parse")
	}
	if !strings.EqualFold(u.Scheme, "https") || u.Opaque != "" {
		return nil, denied("the scheme must be https")
	}
	if u.User != nil {
		return nil, denied("the URL carries credentials")
	}
	if p := u.Port(); p != "" && p != callbackPort {
		return nil, denied("the port must be %s", callbackPort)
	}
	host := strings.ToLower(u.Hostname())
	if err := checkHostName(host); err != nil {
		return nil, err
	}
	u.Scheme, u.Host = "https", host
	return u, nil
}

func checkHostName(host string) error {
	if host == "" || len(host) > maxHostLen || strings.HasSuffix(host, ".") {
		return denied("the host is empty, too long or ends with a dot")
	}
	labels := strings.Split(host, ".")
	if len(labels) < 2 {
		return denied("the host must be a DNS name with at least two labels")
	}
	for _, l := range labels {
		if len(l) > maxLabelLen || !hostLabel.MatchString(l) {
			return denied("the host has an invalid label")
		}
	}
	if !tldLabel.MatchString(labels[len(labels)-1]) {
		return denied("the top-level label must be alphabetic: a numeric host in any spelling is refused")
	}
	return nil
}

// Guard holds the registered callback hosts and is the only thing that opens a callback connection.
type Guard struct {
	resolver Resolver
	opts     Options
	mu       sync.RWMutex
	hosts    map[string]struct{}
	dial     func(ctx context.Context, network, addr string) (net.Conn, error)
}

// NewGuard returns a guard with no registered host. A nil resolver means the system resolver. Both timeouts must be positive.
func NewGuard(r Resolver, o Options) (*Guard, error) {
	if o.LookupTimeout <= 0 || o.DialTimeout <= 0 {
		return nil, errors.New("egress: both timeouts must be positive; they come from configuration and have no default")
	}
	if r == nil {
		r = net.DefaultResolver
	}
	return &Guard{resolver: r, opts: o, hosts: map[string]struct{}{}}, nil
}

// checked resolves host and requires at least one answer and every answer public. It returns the first address, which is the one dialled.
func (g *Guard) checked(ctx context.Context, host string) (netip.Addr, error) {
	ctx, cancel := context.WithTimeout(ctx, g.opts.LookupTimeout)
	defer cancel()
	addrs, err := g.resolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(addrs) == 0 {
		return netip.Addr{}, denied("the host does not resolve")
	}
	for _, a := range addrs {
		if !IsPublic(a) {
			return netip.Addr{}, denied("the host resolves to a non-public address")
		}
	}
	return addrs[0], nil
}

// Register validates raw (PERD 39.4), resolves its host and refuses it unless every answer is public. On success the host is registered.
func (g *Guard) Register(ctx context.Context, raw string) error {
	u, err := ParseCallbackURL(raw)
	if err != nil {
		return err
	}
	if _, err := g.checked(ctx, u.Host); err != nil {
		return err
	}
	g.mu.Lock()
	g.hosts[u.Host] = struct{}{}
	g.mu.Unlock()
	return nil
}

func (g *Guard) registered(host string) bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	_, ok := g.hosts[host]
	return ok
}

// Resolve is the check made before every delivery: the URL is well formed, its host is registered, and it resolves now to public
// addresses only. It returns the address to connect to.
func (g *Guard) Resolve(ctx context.Context, raw string) (netip.Addr, error) {
	u, err := ParseCallbackURL(raw)
	if err != nil {
		return netip.Addr{}, err
	}
	if !g.registered(u.Host) {
		return netip.Addr{}, denied("the host is not a registered callback host")
	}
	return g.checked(ctx, u.Host)
}

// Transport returns a transport that can reach a registered host on port 443 and nothing else. It ignores the environment's proxy
// settings, verifies certificates, requires TLS 1.2, and opens a new connection per delivery so that every delivery is re-checked.
func (g *Guard) Transport() *http.Transport {
	d := &net.Dialer{Timeout: g.opts.DialTimeout, Control: func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return denied("the dial address is malformed")
		}
		if a, err := netip.ParseAddr(host); err != nil || !IsPublic(a) {
			return denied("the connection would reach a non-public address")
		}
		return nil
	}}
	return &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if network != "tcp" && network != "tcp4" && network != "tcp6" {
				return nil, denied("only TCP is allowed")
			}
			host, port, err := net.SplitHostPort(addr)
			if err != nil || port != callbackPort {
				return nil, denied("the port must be %s", callbackPort)
			}
			host = strings.ToLower(host)
			if !g.registered(host) {
				return nil, denied("the host is not a registered callback host")
			}
			ip, err := g.checked(ctx, host)
			if err != nil {
				return nil, err
			}
			target := net.JoinHostPort(ip.String(), callbackPort)
			if g.dial != nil {
				return g.dial(ctx, network, target)
			}
			return d.DialContext(ctx, network, target)
		},
		TLSClientConfig:        &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout:    g.opts.DialTimeout,
		ResponseHeaderTimeout:  g.opts.DialTimeout,
		MaxResponseHeaderBytes: maxHeaderBytes,
		DisableKeepAlives:      true,
	}
}

// Client returns an HTTP client over Transport that never follows a redirect: a 3xx is returned to the caller, which treats it as a failed
// delivery.
func (g *Guard) Client(timeout time.Duration) *http.Client {
	return &http.Client{
		Transport:     g.Transport(),
		Timeout:       timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}
