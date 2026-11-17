package httpx

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// ClientIP resolves the originating client address for logging, audit trails,
// and rate limiting.
//
// X-Forwarded-For is honored only when the direct peer is a loopback, private,
// or link-local address — i.e. we sit behind our own reverse proxy. When the
// peer is public the header would be client-controlled and is ignored. The
// first (client) entry is taken, must parse as an IP, and IPv4-in-IPv6 forms
// are unmapped so INET columns always receive a clean address.
func ClientIP(r *http.Request) string {
	peer := remoteHost(r)
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" && peerIsProxy(peer) {
		first := strings.TrimSpace(strings.Split(fwd, ",")[0])
		if addr, err := netip.ParseAddr(first); err == nil {
			return addr.Unmap().String()
		}
	}
	return peer
}

// remoteHost extracts the host part of the TCP peer address, unmapping
// IPv4-in-IPv6 forms so INET columns receive a clean address.
func remoteHost(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		return addr.Unmap().String()
	}
	return host
}

// peerIsProxy reports whether the direct peer looks like a reverse proxy on
// the local network (loopback, RFC1918/ULA, or link-local).
func peerIsProxy(peer string) bool {
	addr, err := netip.ParseAddr(peer)
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	return addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast()
}
