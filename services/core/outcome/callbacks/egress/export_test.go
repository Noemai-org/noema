package egress

import (
	"context"
	"net"
)

// SetDialForTest replaces the connection function; nil restores the real dialer. It exists only in test builds.
func (g *Guard) SetDialForTest(f func(ctx context.Context, network, addr string) (net.Conn, error)) {
	g.dial = f
}
