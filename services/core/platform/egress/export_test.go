package egress

import (
	"context"
	"crypto/x509"
	"net"
)

// SetDialForTest replaces the connection function; nil restores the real dialer. It exists only in test builds.
func (g *Guard) SetDialForTest(f func(ctx context.Context, network, addr string) (net.Conn, error)) {
	g.dial = f
}

// SetRootCAsForTest makes the transport trust a test server's certificate. It exists only in test builds.
func (g *Guard) SetRootCAsForTest(p *x509.CertPool) { g.rootCAs = p }
