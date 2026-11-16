package integrations

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

// TestIsLocalAnswer covers the "relevant for local" rewrite filter: private,
// loopback, link-local, IPv6 ULA and CGNAT (Tailscale) pass; public IPs don't.
func TestIsLocalAnswer(t *testing.T) {
	cases := map[string]bool{
		"192.168.1.11":   true,  // RFC1918
		"10.0.0.5":       true,  // RFC1918
		"172.16.5.4":     true,  // RFC1918
		"100.105.33.75":  true,  // CGNAT / Tailscale
		"100.64.0.1":     true,  // CGNAT edge
		"100.127.255.25": true,  // CGNAT edge
		"127.0.0.1":      true,  // loopback
		"169.254.1.1":    true,  // IPv4 link-local
		"fe80::1":        true,  // IPv6 link-local
		"fd00::5":        true,  // IPv6 ULA
		"8.8.8.8":        false, // public
		"1.1.1.1":        false, // public
		"100.63.255.255": false, // just below CGNAT range
		"100.128.0.1":    false, // just above CGNAT range
		"2606:4700::1":   false, // public IPv6
	}
	for in, want := range cases {
		addr, err := netip.ParseAddr(in)
		if err != nil {
			t.Fatalf("parse %s: %v", in, err)
		}
		if got := isLocalAnswer(addr); got != want {
			t.Errorf("isLocalAnswer(%s) = %v, want %v", in, got, want)
		}
	}
}

// TestSplitRewriteDomain covers the flat apex/label split AdGuard rewrites need.
func TestSplitRewriteDomain(t *testing.T) {
	cases := []struct{ in, apex, label string }{
		{"kali.lab", "lab", "kali"},
		{"proxmox.lab", "lab", "proxmox"},
		{"*.dokploy.lab", "lab", "*.dokploy"},
		{"dokploy.lab", "lab", "dokploy"},
		{"*.lolab", "lolab", "*"},
		{"*.lokal", "lokal", "*"},
		{"homelab", "homelab", ""},   // single label → apex record
		{"Kali.Lab.", "lab", "kali"}, // case + trailing dot normalization
		{"", "", ""},
		{"*", "", ""}, // bare wildcard has no apex
	}
	for _, c := range cases {
		apex, label := splitRewriteDomain(c.in)
		if apex != c.apex || label != c.label {
			t.Errorf("splitRewriteDomain(%q) = (%q, %q), want (%q, %q)", c.in, apex, label, c.apex, c.label)
		}
	}
}

// TestAdguardClientFetch verifies basic auth, path handling and payload parsing
// against a fake AdGuard Home (DB-free: the client layer only).
func TestAdguardClientFetch(t *testing.T) {
	var sawAuth bool
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		sawAuth = ok && user == "admin" && pass == "secret"
		switch r.URL.Path {
		case "/control/clients/list":
			_, _ = w.Write([]byte(`[
				{"name":"server","ids":["192.168.1.11","aa:bb:cc:dd:ee:ff"]},
				{"name":"laptop","ids":["192.168.1.50","192.168.2.0/24"]}
			]`))
		case "/control/rewrite/list":
			_, _ = w.Write([]byte(`[
				{"domain":"kali.lab","answer":"192.168.1.13","enabled":true},
				{"domain":"*.exlab","answer":"100.105.33.75","enabled":true},
				{"domain":"stale.lab","answer":"192.168.1.99","enabled":false},
				{"domain":"www.example.com","answer":"93.184.216.34","enabled":true}
			]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer fake.Close()

	ag := &adguardClient{base: fake.URL, username: "admin", password: "secret", hc: fake.Client()}
	clients, err := ag.clients(context.Background())
	if err != nil {
		t.Fatalf("clients: %v", err)
	}
	rewrites, err := ag.rewrites(context.Background())
	if err != nil {
		t.Fatalf("rewrites: %v", err)
	}
	if !sawAuth {
		t.Fatal("basic auth credentials were not sent correctly")
	}
	if len(clients) != 2 || len(clients[0].IDs) != 2 || clients[1].IDs[1] != "192.168.2.0/24" {
		t.Fatalf("unexpected clients: %+v", clients)
	}
	if len(rewrites) != 4 || rewrites[2].Enabled || rewrites[3].Answer != "93.184.216.34" {
		t.Fatalf("unexpected rewrites: %+v", rewrites)
	}
}

// TestAdguardClientAuthError verifies a wrong-password response surfaces as a
// sync error with the status code, not a parse failure.
func TestAdguardClientAuthError(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"forbidden"}`))
	}))
	defer fake.Close()

	ag := &adguardClient{base: fake.URL, username: "admin", password: "wrong", hc: fake.Client()}
	if _, err := ag.rewrites(context.Background()); err == nil {
		t.Fatal("expected error on 403 response")
	}
}
