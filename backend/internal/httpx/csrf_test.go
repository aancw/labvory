package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func newCSRFRequest(method string) *http.Request {
	r := httptest.NewRequest(method, "/api/v1/test", nil)
	return r
}

func TestCSRFAllowsSafeMethods(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := CSRF(next)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, newCSRFRequest(http.MethodGet))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET should pass without CSRF, got %d", rec.Code)
	}
}

func TestCSRFRejectsUnsafeWithoutToken(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler must not run")
	})
	h := CSRF(next)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, newCSRFRequest(http.MethodPost))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("POST without CSRF should be 403, got %d", rec.Code)
	}
}

func TestCSRFDoubleSubmitMatch(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := CSRF(next)

	token := newToken()
	r := newCSRFRequest(http.MethodPost)
	r.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: token})
	r.Header.Set(CSRFHeader, token)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("matching double-submit should pass, got %d", rec.Code)
	}
}

func TestCSRFDoubleSubmitMismatch(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler must not run on mismatch")
	})
	h := CSRF(next)

	cookieToken := newToken()
	headerToken := newToken()
	r := newCSRFRequest(http.MethodPut)
	r.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: cookieToken})
	r.Header.Set(CSRFHeader, headerToken)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("mismatched tokens should be 403, got %d", rec.Code)
	}
}

func TestEnsureCSRFCookieIssuesOnce(t *testing.T) {
	token := newToken()
	r := newCSRFRequest(http.MethodGet)
	r.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: token})
	rec := httptest.NewRecorder()

	got, issued := EnsureCSRFCookie(rec, r, false)
	if issued {
		t.Fatal("should reuse existing valid cookie")
	}
	if got != token {
		t.Fatalf("expected existing token %q, got %q", token, got)
	}

	r2 := newCSRFRequest(http.MethodGet)
	rec2 := httptest.NewRecorder()
	got2, issued2 := EnsureCSRFCookie(rec2, r2, false)
	if !issued2 {
		t.Fatal("should issue a cookie when none present")
	}
	if !isValidToken(got2) {
		t.Fatal("issued token must be 32-byte hex")
	}
	if len(rec2.Result().Cookies()) == 0 {
		t.Fatal("Set-Cookie header must be present")
	}
}

func TestIsValidToken(t *testing.T) {
	if isValidToken("not-a-token") {
		t.Fatal("short garbage must be invalid")
	}
	if isValidToken(newToken() + "x") {
		t.Fatal("wrong length must be invalid")
	}
	if !isValidToken(newToken()) {
		t.Fatal("generated token must be valid")
	}
}
