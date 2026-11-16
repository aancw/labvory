package api

import (
	"errors"
	"net/http"

	"github.com/petruknisme/lanvory/internal/auth"
	"github.com/petruknisme/lanvory/internal/httpx"
	"github.com/petruknisme/lanvory/internal/store"
)

type setupStatusResponse struct {
	NeedsSetup bool `json:"needs_setup"`
}

// handleSetupStatus reports whether the first-run wizard should be shown.
func (s *Server) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	needs, err := s.auth.SetupStatus(r.Context())
	if err != nil {
		s.logger.Error("setup status failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not check setup status.", nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, setupStatusResponse{NeedsSetup: needs})
}

type createAdminRequest struct {
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Password    string `json:"password"`
}

// handleSetupAdmin creates the first administrator and signs the operator in.
func (s *Server) handleSetupAdmin(w http.ResponseWriter, r *http.Request) {
	var req createAdminRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}

	user, sess, err := s.auth.CreateAdmin(r.Context(), req.Username, req.DisplayName, req.Password,
		clientIP(r), r.UserAgent())
	switch {
	case errors.Is(err, auth.ErrNoUsersAllowed):
		httpx.Error(w, r, http.StatusForbidden, "setup_closed",
			"Setup is only available before the first administrator exists.", nil)
		return
	case errors.Is(err, auth.ErrInvalidUsername):
		httpx.Error(w, r, http.StatusUnprocessableEntity, "invalid_username",
			"Username must be 3-32 characters: letters, digits, '.', '_' or '-'.", nil)
		return
	case errors.Is(err, auth.ErrPasswordTooShort), errors.Is(err, auth.ErrPasswordTooLong):
		httpx.Error(w, r, http.StatusUnprocessableEntity, "invalid_password", err.Error(), nil)
		return
	case errors.Is(err, auth.ErrTakenUsername):
		httpx.Error(w, r, http.StatusConflict, "username_taken", "Username already taken.", nil)
		return
	case err != nil:
		s.logger.Error("create admin failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not create administrator.", nil)
		return
	}

	if sess.Token != "" {
		s.setSessionCookie(w, sess.Token, sess.ExpiresAt)
	}
	httpx.WriteJSON(w, http.StatusCreated, userResponse{User: user})
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type userResponse struct {
	User store.User `json:"user"`
}

// handleLogin authenticates and issues the session cookie.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if req.Username == "" || req.Password == "" {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "invalid_request",
			"Username and password are required.", nil)
		return
	}

	user, sess, err := s.auth.Login(r.Context(), req.Username, req.Password, clientIP(r), r.UserAgent())
	if errors.Is(err, auth.ErrInvalidCredentials) {
		httpx.Error(w, r, http.StatusUnauthorized, "invalid_credentials",
			"Invalid username or password.", nil)
		return
	}
	if err != nil {
		s.logger.Error("login failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Login failed.", nil)
		return
	}

	s.setSessionCookie(w, sess.Token, sess.ExpiresAt)
	httpx.WriteJSON(w, http.StatusOK, userResponse{User: user})
}

// handleLogout revokes the session.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	token := currentSessionToken(r)
	if token != "" {
		if err := s.auth.Logout(r.Context(), auth.TokenHash(token), clientIP(r)); err != nil {
			s.logger.Error("logout failed", "error", err)
		}
	}
	s.clearSessionCookie(w)
	httpx.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleMe returns the authenticated user.
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, userResponse{User: currentUser(r)})
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// handleChangePassword verifies the current password and rotates credentials.
func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	var req changePasswordRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}

	user := currentUser(r)
	err := s.auth.ChangePassword(r.Context(), user, req.CurrentPassword, req.NewPassword,
		currentSessionToken(r), clientIP(r))
	switch {
	case errors.Is(err, auth.ErrWrongPassword):
		httpx.Error(w, r, http.StatusForbidden, "wrong_password",
			"Current password is incorrect.", nil)
		return
	case errors.Is(err, auth.ErrPasswordTooShort), errors.Is(err, auth.ErrPasswordTooLong):
		httpx.Error(w, r, http.StatusUnprocessableEntity, "invalid_password", err.Error(), nil)
		return
	case err != nil:
		s.logger.Error("change password failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not change password.", nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
