package listen

import (
	"net"
	"testing"
)

func cidr(s string) net.Addr {
	ip, n, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	n.IP = ip
	return n
}

func TestPickChoosesTheBenchSideOnly(t *testing.T) {
	addrs := []net.Addr{cidr("127.0.0.1/8"), cidr("172.20.0.5/16"), cidr("172.21.0.5/16"), cidr("fd00::5/64")}
	got, err := Pick([]net.IP{net.ParseIP("172.21.0.2")}, addrs)
	if err != nil || got.String() != "172.21.0.5" {
		t.Fatalf("%v %v", got, err)
	}
	// The tushe-side address is never chosen for a bench-side host, in either order.
	got, err = Pick([]net.IP{net.ParseIP("172.20.0.9")}, addrs)
	if err != nil || got.String() != "172.20.0.5" {
		t.Fatalf("%v %v", got, err)
	}
}

func TestPickFailsClosed(t *testing.T) {
	addrs := []net.Addr{cidr("172.20.0.5/16"), cidr("172.21.0.5/16")}
	if _, err := Pick([]net.IP{net.ParseIP("10.9.9.9")}, addrs); err == nil {
		t.Fatal("a host on no local network must not yield an address")
	}
	if _, err := Pick([]net.IP{net.ParseIP("172.20.0.9"), net.ParseIP("172.21.0.9")}, addrs); err == nil {
		t.Fatal("a host on both networks is ambiguous and must be refused")
	}
	if _, err := Pick([]net.IP{net.ParseIP("fd00::9")}, []net.Addr{cidr("fd00::5/64")}); err == nil {
		t.Fatal("only IPv4 is considered")
	}
}

func TestViaLoopbackOpensAListenerOnThatAddressOnly(t *testing.T) {
	l, err := Via("localhost", "0")
	if err != nil {
		t.Fatalf("localhost must resolve to an IPv4 loopback address: %v", err)
	}
	defer l.Close()
	if host, _, _ := net.SplitHostPort(l.Addr().String()); host != "127.0.0.1" {
		t.Fatalf("bound to %s", l.Addr())
	}
}
