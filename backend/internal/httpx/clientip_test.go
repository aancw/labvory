package httpx

import (
	"net/http/httptest"
	"testing"
)

func TestClientIP(t *testing.T) {
	cases := []struct {
		name string
		peer string
		xff  string
		want string
	}{
		{"direct client, no proxy header", "203.0.113.9:5000", "", "203.0.113.9"},
		{"proxy peer: first XFF entry wins", "127.0.0.1:8443", "203.0.113.7, 10.0.0.9", "203.0.113.7"},
		{"private proxy peer: XFF honored", "192.168.1.5:8443", "203.0.113.7", "203.0.113.7"},
		{"public peer: XFF ignored (spoof)", "203.0.113.9:5000", "8.8.8.8", "203.0.113.9"},
		{"proxy peer with junk XFF: fall back to peer", "127.0.0.1:8443", "not-an-ip", "127.0.0.1"},
		{"IPv4-in-IPv6 XFF entry is unmapped", "127.0.0.1:8443", "::ffff:203.0.113.8", "203.0.113.8"},
		{"IPv4-in-IPv6 peer is unmapped", "[::ffff:203.0.113.6]:9000", "", "203.0.113.6"},
		{"remote addr without port", "10.1.2.3", "", "10.1.2.3"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = tc.peer
			if tc.xff != "" {
				r.Header.Set("X-Forwarded-For", tc.xff)
			}
			if got := ClientIP(r); got != tc.want {
				t.Fatalf("ClientIP() = %q, want %q", got, tc.want)
			}
		})
	}
}
