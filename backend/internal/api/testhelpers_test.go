package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/aancw/labvory/internal/store"
)

var testDBURLFromEnv = os.Getenv("TEST_DATABASE_URL")

// newTestServerWithSecrets boots the API with LV_SECRET_KEY configured so
// integration secrets can be stored (encrypted at rest).
func newTestServerWithSecrets(t *testing.T, databaseURL string) (*Server, string, *http.Client, func(string, string, any, int) map[string]any) {
	t.Helper()
	return newTestServerCfg(t, databaseURL, "0123456789abcdef0123456789abcdef")
}

// newHTTPTestServer starts the API on an ephemeral port.
func newHTTPTestServer(t *testing.T, h http.Handler) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	return ts
}

// newCookieJar enables cookie-based sessions for the test client.
func newCookieJar() (*cookiejar.Jar, error) {
	return cookiejar.New(nil)
}

// performJSON issues a request with automatic CSRF token handling and asserts
// the status code, returning the decoded body.
func performJSON(t *testing.T, client *http.Client, baseURL, method, path string, payload any, wantStatus int) map[string]any {
	t.Helper()

	fetchCSRF := func() string {
		resp, err := client.Get(baseURL + "/api/v1/csrf")
		if err != nil {
			t.Fatalf("csrf fetch: %v", err)
		}
		defer resp.Body.Close()
		var body struct {
			Token string `json:"token"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		return body.Token
	}

	var body []byte
	if payload != nil {
		body, _ = json.Marshal(payload)
	}
	req, _ := http.NewRequest(method, baseURL+path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if method != http.MethodGet {
		req.Header.Set("X-CSRF-Token", fetchCSRF())
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != wantStatus {
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(resp.Body)
		t.Fatalf("%s %s: want %d, got %d: %s", method, path, wantStatus, resp.StatusCode, buf.String())
	}
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return out
}

var _ = store.ErrNotFound

// activeTestServerURL tracks the most recent test server address
// (tests within a package run sequentially).
var activeTestServerURL string

func tsURL(t *testing.T) string {
	t.Helper()
	if activeTestServerURL == "" {
		t.Fatal("no active test server")
	}
	return activeTestServerURL
}

// performJSONExpectError issues a request and returns the error envelope body
// without failing on a non-2xx status (for negative-path assertions).
func performJSONExpectError(t *testing.T, client *http.Client, baseURL, method, path string, payload any) map[string]any {
	t.Helper()
	var body []byte
	if payload != nil {
		body, _ = json.Marshal(payload)
	}
	req, _ := http.NewRequest(method, baseURL+path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if method != http.MethodGet {
		req.Header.Set("X-CSRF-Token", fetchCSRFToken(t, client, baseURL))
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return out
}

func fetchCSRFToken(t *testing.T, client *http.Client, baseURL string) string {
	t.Helper()
	resp, err := client.Get(baseURL + "/api/v1/csrf")
	if err != nil {
		t.Fatalf("csrf fetch: %v", err)
	}
	defer resp.Body.Close()
	var body struct {
		Token string `json:"token"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	return body.Token
}
