package httpx

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
)

// CSRF cookie: readable by JS (double-submit pattern), Strict same-site.
const (
	CSRFCookieName = "lv_csrf"
	CSRFHeader     = "X-CSRF-Token"
)

// EnsureCSRFCookie guarantees a CSRF token cookie exists and returns its value.
// The token is an opaque random value; the double-submit comparison happens on unsafe methods.
func EnsureCSRFCookie(w http.ResponseWriter, r *http.Request, secure bool) (string, bool) {
	if c, err := r.Cookie(CSRFCookieName); err == nil && isValidToken(c.Value) {
		return c.Value, false
	}
	token := newToken()
	http.SetCookie(w, &http.Cookie{
		Name:     CSRFCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   30 * 24 * 3600,
		HttpOnly: false,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	})
	return token, true
}

// CSRFFetch returns the current (or freshly issued) CSRF token to the SPA.
func CSRFFetch(secure bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, _ := EnsureCSRFCookie(w, r, secure)
		WriteJSON(w, http.StatusOK, map[string]string{"token": token})
	}
}

// CSRF enforces the double-submit check on state-changing methods. Requests
// carrying an Authorization header are exempt: bearer tokens are explicit
// per-request credentials, not ambient cookies, so they cannot be CSRF'd.
func CSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		if r.Header.Get("Authorization") != "" {
			next.ServeHTTP(w, r)
			return
		}
		cookie, err := r.Cookie(CSRFCookieName)
		if err != nil || !isValidToken(cookie.Value) {
			Error(w, r, http.StatusForbidden, "csrf",
				"Missing CSRF token. Reload the page to obtain a fresh token.", nil)
			return
		}
		header := r.Header.Get(CSRFHeader)
		if !isValidToken(header) || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(header)) != 1 {
			Error(w, r, http.StatusForbidden, "csrf",
				"Invalid CSRF token.", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isValidToken(v string) bool {
	if len(v) != 64 { // 32 bytes hex
		return false
	}
	_, err := hex.DecodeString(v)
	return err == nil
}

func newToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failing is unrecoverable; crash early rather than issue a weak token.
		panic(err)
	}
	return hex.EncodeToString(b)
}
