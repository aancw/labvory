package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/petruknisme/lanvory/internal/httpx"
	"github.com/petruknisme/lanvory/internal/store"
)

// ---------- API tokens (bearer credentials for machine clients) ----------

type apiTokenPayload struct {
	Name    string `json:"name"`
	Purpose string `json:"purpose"`
}

func validateAPITokenPayload(p *apiTokenPayload) *httpxError {
	if len([]rune(p.Name)) < 1 || len([]rune(p.Name)) > 100 {
		return errInvalid("invalid_name", "Token name must be 1-100 characters.")
	}
	// Empty purpose keeps the historical default (MCP).
	if p.Purpose == "" {
		p.Purpose = store.TokenPurposeMCP
	}
	if !store.ValidTokenPurpose(p.Purpose) {
		return errInvalid("invalid_purpose", "Purpose must be \"mcp\" or \"api\".")
	}
	return nil
}

// handleListTokens lists the caller's API tokens (never includes secrets).
func (s *Server) handleListTokens(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	tokens, err := s.tokens.List(r.Context(), user.ID)
	if err != nil {
		s.logger.Error("list api tokens failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not load tokens.", nil)
		return
	}
	if tokens == nil {
		tokens = []store.APIToken{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"tokens": tokens})
}

// handleCreateToken issues a new token; the plaintext is returned exactly once.
func (s *Server) handleCreateToken(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	var p apiTokenPayload
	if err := httpx.DecodeJSON(r, &p); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if herr := validateAPITokenPayload(&p); herr != nil {
		herr.write(reqCtx{w, r, user})
		return
	}
	created, err := s.tokens.Create(r.Context(), user.ID, p.Name, p.Purpose)
	if err != nil {
		s.logger.Error("create api token failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not create token.", nil)
		return
	}
	s.auditChange(r, user, "api_token", created.ID, created.Name, "created", map[string]any{"name": created.Name, "purpose": created.Purpose})
	httpx.WriteJSON(w, http.StatusCreated, created)
}

// handleRevokeToken revokes one of the caller's tokens.
func (s *Server) handleRevokeToken(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	id := chi.URLParam(r, "tokenID")
	if !isUUID(id) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_id", "Invalid token id.", nil)
		return
	}
	err := s.tokens.Revoke(r.Context(), id, user.ID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		httpx.Error(w, r, http.StatusNotFound, "not_found", "Token not found.", nil)
		return
	case err != nil:
		s.logger.Error("revoke api token failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not revoke token.", nil)
		return
	}
	s.auditChange(r, user, "api_token", id, "", "revoked", nil)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"revoked": true})
}

// ---------- bearer authentication (API tokens) ----------

// bearerAuth authenticates requests via `Authorization: Bearer lv_...` API
// tokens. Unlike cookie sessions it needs no CSRF check (tokens are not
// ambient credentials) and is intended for the MCP endpoint and scripts.
// Only MCP-purpose tokens are accepted here; API-purpose tokens are rejected
// so each token works on exactly the surface it was issued for.
func (s *Server) bearerAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		token, ok := strings.CutPrefix(header, "Bearer ")
		if !ok || !strings.HasPrefix(token, "lv_") {
			w.Header().Set("WWW-Authenticate", `Bearer realm="lanvory"`)
			httpx.Error(w, r, http.StatusUnauthorized, "unauthorized",
				"Authentication required: send an API token as `Authorization: Bearer lv_...`.", nil)
			return
		}
		tok, user, err := s.tokens.Resolve(r.Context(), token)
		if errors.Is(err, store.ErrNotFound) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="lanvory"`)
			httpx.Error(w, r, http.StatusUnauthorized, "unauthorized", "Invalid or revoked API token.", nil)
			return
		}
		if err != nil {
			s.logger.Error("resolve api token failed", "error", err)
			httpx.Error(w, r, http.StatusInternalServerError, "internal", "Token lookup failed.", nil)
			return
		}
		if tok.Purpose != store.TokenPurposeMCP {
			httpx.Error(w, r, http.StatusForbidden, "wrong_purpose",
				"This token is for REST API use only and cannot access the MCP endpoint.", nil)
			return
		}
		ctx := contextWithUser(r.Context(), user, tok.TokenHash)
		s.tokens.Touch(r.Context(), tok.ID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
