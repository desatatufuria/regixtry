package regixtryhttp

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// ParseTrustedProxies parses a comma-separated list of IP addresses and CIDR
// prefixes (the -trusted-proxies flag). An empty list is valid and means "no
// proxy is trusted": X-Forwarded-For is then never consulted.
func ParseTrustedProxies(value string) ([]netip.Prefix, error) {
	var prefixes []netip.Prefix
	for _, entry := range strings.Split(value, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if strings.Contains(entry, "/") {
			prefix, err := netip.ParsePrefix(entry)
			if err != nil {
				return nil, fmt.Errorf("invalid trusted proxy %q: %w", entry, err)
			}
			prefixes = append(prefixes, prefix)
			continue
		}
		addr, err := netip.ParseAddr(entry)
		if err != nil {
			return nil, fmt.Errorf("invalid trusted proxy %q: %w", entry, err)
		}
		addr = addr.Unmap()
		prefixes = append(prefixes, netip.PrefixFrom(addr, addr.BitLen()))
	}
	return prefixes, nil
}

// clientIP returns the address a request should be attributed to.
//
// Without trusted proxies it is the TCP peer (RemoteAddr) and every forwarding
// header is ignored, so a client cannot choose the address it is logged under.
// When the peer is a trusted proxy, X-Forwarded-For (all header lines merged)
// is walked right to left skipping trusted hops; the first untrusted hop is
// the client. Only the rightmost untrusted value is reliable: values to its
// left were supplied by the client or by an earlier, untrusted hop.
func clientIP(req *http.Request, trusted []netip.Prefix) string {
	peer, ok := parsePeer(req.RemoteAddr)
	if !ok {
		return "unknown"
	}
	if len(trusted) == 0 || !containsAddr(trusted, peer) {
		return peer.String()
	}

	var hops []string
	for _, line := range req.Header.Values("X-Forwarded-For") {
		for _, hop := range strings.Split(line, ",") {
			if hop = strings.TrimSpace(hop); hop != "" {
				hops = append(hops, hop)
			}
		}
	}
	if len(hops) == 0 {
		return peer.String()
	}

	var leftmost netip.Addr
	for i := len(hops) - 1; i >= 0; i-- {
		addr, ok := parseHop(hops[i])
		if !ok {
			// An unparseable hop cannot be attributed; stay with the last
			// address we can vouch for.
			return peer.String()
		}
		if !containsAddr(trusted, addr) {
			return addr.String()
		}
		leftmost = addr
	}
	return leftmost.String()
}

func containsAddr(prefixes []netip.Prefix, addr netip.Addr) bool {
	for _, prefix := range prefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func parsePeer(remote string) (netip.Addr, bool) {
	remote = strings.TrimSpace(remote)
	if host, _, err := net.SplitHostPort(remote); err == nil {
		remote = host
	}
	return parseAddr(remote)
}

func parseHop(hop string) (netip.Addr, bool) {
	if addrPort, err := netip.ParseAddrPort(hop); err == nil {
		return addrPort.Addr().Unmap().WithZone(""), true
	}
	return parseAddr(strings.Trim(hop, "[]"))
}

func parseAddr(value string) (netip.Addr, bool) {
	addr, err := netip.ParseAddr(value)
	if err != nil {
		return netip.Addr{}, false
	}
	return addr.Unmap().WithZone(""), true
}
