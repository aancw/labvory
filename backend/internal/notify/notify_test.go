package notify

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPost(t *testing.T) {
	var gotTitle, gotBody, gotContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTitle = r.Header.Get("X-Title")
		gotContentType = r.Header.Get("Content-Type")
		b := make([]byte, 4096)
		n, _ := r.Body.Read(b)
		gotBody = string(b[:n])
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if err := Post(context.Background(), srv.URL, "Service down", "jellyfin.lab unreachable"); err != nil {
		t.Fatalf("Post returned error: %v", err)
	}
	if gotTitle != "Service down" {
		t.Errorf("X-Title = %q, want %q", gotTitle, "Service down")
	}
	if gotBody != "jellyfin.lab unreachable" {
		t.Errorf("body = %q, want %q", gotBody, "jellyfin.lab unreachable")
	}
	if gotContentType == "" {
		t.Error("Content-Type header missing")
	}
}

func TestPostHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	}))
	defer srv.Close()

	err := Post(context.Background(), srv.URL, "t", "m")
	if err == nil {
		t.Fatal("expected error for non-2xx webhook response, got nil")
	}
}

func TestPostBadURL(t *testing.T) {
	if err := Post(context.Background(), "http://127.0.0.1:1/nope", "t", "m"); err == nil {
		t.Fatal("expected transport error for unreachable URL, got nil")
	}
}
