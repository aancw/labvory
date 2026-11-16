package integrations

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestCrtNameHostsParsing covers the crt.name response shape (newline-separated
// names) and the suffix/wildcard filtering.
func TestCrtNameHostsParsing(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/search" || r.URL.Query().Get("apex") != "example.com" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("example.com\nwww.example.com\n*.api.example.com\nother.com\nmail.other.com\n"))
	}))
	defer fake.Close()

	old := crtNameBaseURL
	crtNameBaseURL = fake.URL
	defer func() { crtNameBaseURL = old }()

	names, err := crtNameHosts(context.Background(), "example.com")
	if err != nil {
		t.Fatalf("crtNameHosts: %v", err)
	}
	if len(names) != 2 {
		t.Fatalf("expected 2 in-zone names, got %v", names)
	}
	want := map[string]bool{"www.example.com": true, "api.example.com": true}
	for _, n := range names {
		if !want[n] {
			t.Fatalf("unexpected name %q", n)
		}
	}
}
