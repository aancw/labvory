package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/petruknisme/lanvory/internal/httpx"
	"github.com/petruknisme/lanvory/internal/store"
)

type ctxKey string

const userKey ctxKey = "user"
const sessionTokenKey ctxKey = "session_token"

const sessionCookieName = "lv_session"

// clientIP extracts the client address, preferring the proxy-provided value.
func clientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		if host, _, err := net.SplitHostPort(fwd); err == nil {
			return host
		}
		return fwd
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// requireAuth authenticates /api/v1 requests via session cookie OR an
// "api"-purpose bearer token (Authorization: Bearer lv_...). MCP-purpose
// tokens are rejected here — they only work on /mcp.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Bearer path: explicit credentials, no CSRF concerns.
		if plaintext, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
			if !strings.HasPrefix(plaintext, "lv_") {
				httpx.Error(w, r, http.StatusUnauthorized, "unauthorized", "Invalid API token.", nil)
				return
			}
			tok, user, err := s.tokens.Resolve(r.Context(), plaintext)
			if errors.Is(err, store.ErrNotFound) {
				httpx.Error(w, r, http.StatusUnauthorized, "unauthorized", "Invalid or revoked API token.", nil)
				return
			}
			if err != nil {
				s.logger.Error("resolve api token failed", "error", err)
				httpx.Error(w, r, http.StatusInternalServerError, "internal", "Token lookup failed.", nil)
				return
			}
			if tok.Purpose != store.TokenPurposeAPI {
				httpx.Error(w, r, http.StatusForbidden, "wrong_purpose",
					"This token is for MCP use only and cannot access the REST API.", nil)
				return
			}
			ctx := contextWithUser(r.Context(), user, tok.TokenHash)
			s.tokens.Touch(r.Context(), tok.ID)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		// Session path.
		c, err := r.Cookie(sessionCookieName)
		if err != nil || c.Value == "" {
			httpx.Error(w, r, http.StatusUnauthorized, "unauthorized", "Authentication required.", nil)
			return
		}
		user, ok, err := s.auth.ResolveSession(r.Context(), c.Value)
		if err != nil {
			s.logger.Error("resolve session failed", "error", err)
			httpx.Error(w, r, http.StatusInternalServerError, "internal", "Session lookup failed.", nil)
			return
		}
		if !ok {
			httpx.Error(w, r, http.StatusUnauthorized, "unauthorized", "Authentication required.", nil)
			return
		}
		ctx := contextWithUser(r.Context(), user, c.Value)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// contextWithUser attaches the authenticated user (and the credential used,
// session token or token hash) to the request context.
func contextWithUser(ctx context.Context, user store.User, credential string) context.Context {
	ctx = context.WithValue(ctx, userKey, user)
	return context.WithValue(ctx, sessionTokenKey, credential)
}

// currentUser returns the authenticated user from the request context.
func currentUser(r *http.Request) store.User {
	user, _ := r.Context().Value(userKey).(store.User)
	return user
}

// currentSessionToken returns the plaintext session token from the request context.
func currentSessionToken(r *http.Request) string {
	token, _ := r.Context().Value(sessionTokenKey).(string)
	return token
}

// setSessionCookie issues the session cookie.
func (s *Server) setSessionCookie(w http.ResponseWriter, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		Secure:   s.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

// clearSessionCookie expires the session cookie.
func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func contextWithTimeout(r *http.Request, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), d)
}
