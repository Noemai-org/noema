// Package listen opens the bench listener on one network only. mock-case sits on two Docker networks (PERD §65.1):
// tushe_net, where the core's case adapter reaches the case API, and bench_net, where the benchmark runner reaches
// the truth and control API. A listener on every interface would be reachable from both, so the bench listener is
// bound to the one local address that shares a subnet with a host known to be on bench_net only (the benchmark
// database). If that address cannot be found, or is not unique, nothing is opened: the service refuses to start.
package listen

import (
	"fmt"
	"net"
)

// Via opens a TCP listener on the local IPv4 address in the same subnet as host.
func Via(host, port string) (net.Listener, error) {
	ips, err := net.LookupIP(host)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", host, err)
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err
	}
	local, err := Pick(ips, addrs)
	if err != nil {
		return nil, fmt.Errorf("bind via %s: %w", host, err)
	}
	return net.Listen("tcp", net.JoinHostPort(local.String(), port))
}

// Pick returns the single local IPv4 address whose subnet contains one of ips.
func Pick(ips []net.IP, addrs []net.Addr) (net.IP, error) {
	found := map[string]net.IP{}
	for _, a := range addrs {
		n, ok := a.(*net.IPNet)
		if !ok || n.IP.To4() == nil {
			continue
		}
		for _, ip := range ips {
			if ip.To4() != nil && n.Contains(ip) {
				found[n.IP.String()] = n.IP
			}
		}
	}
	switch len(found) {
	case 0:
		return nil, fmt.Errorf("no local address shares a subnet with %v", ips)
	case 1:
		for _, ip := range found {
			return ip, nil
		}
	}
	return nil, fmt.Errorf("%d local addresses share a subnet with %v; the network is not unambiguous", len(found), ips)
}
