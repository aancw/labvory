package api

import (
	"errors"
	"net/http"

	"github.com/aancw/lanvory/internal/auth"
	"github.com/aancw/lanvory/internal/httpx"
	"github.com/aancw/lanvory/internal/store"
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
	// TOTPCode is required as the second step when the user has 2FA enabled.
	TOTPCode string `json:"totp_code"`
}

type userResponse struct {
	User store.User `json:"user"`
}

// handleLogin authenticates and issues the session cookie. When the account
// has two-factor enabled and no code was supplied, the response is 403 with
// code "totp_required" and no session is issued.
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

	user, sess, err := s.auth.Login(r.Context(), req.Username, req.Password, req.TOTPCode, clientIP(r), r.UserAgent())
	switch {
	case errors.Is(err, auth.ErrTOTPRequired):
		httpx.Error(w, r, http.StatusForbidden, "totp_required",
			"Two-factor is enabled — enter the 6-digit code from your authenticator.", nil)
		return
	case errors.Is(err, auth.ErrInvalidTOTP):
		httpx.Error(w, r, http.StatusForbidden, "invalid_totp",
			"Invalid or expired verification code.", nil)
		return
	case errors.Is(err, auth.ErrInvalidCredentials):
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

// ---------- two-factor (TOTP) ----------

// handleTOTPSetup starts enrollment: it returns the plaintext secret and the
// otpauth:// URI once; confirming requires a valid code via /2fa/enable.
func (s *Server) handleTOTPSetup(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	secret, uri, err := s.auth.SetupTOTP(r.Context(), user)
	switch {
	case errors.Is(err, auth.ErrNoSecretBox):
		httpx.Error(w, r, http.StatusServiceUnavailable, "secret_key_unavailable",
			"Two-factor secrets require LV_SECRET_KEY to be configured.", nil)
		return
	case err != nil:
		s.logger.Error("totp setup failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not start enrollment.", nil)
		return
	}
	s.auditChange(r, user, "user", user.ID, user.Username, "totp.setup_started", nil)
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"secret": secret, "otpauth_uri": uri})
}

type totpEnableRequest struct {
	Code string `json:"code"`
}

// handleTOTPEnable confirms the pending enrollment with one valid code.
func (s *Server) handleTOTPEnable(w http.ResponseWriter, r *http.Request) {
	var req totpEnableRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	err := s.auth.EnableTOTP(r.Context(), currentUser(r), req.Code, clientIP(r))
	switch {
	case errors.Is(err, auth.ErrInvalidTOTP):
		httpx.Error(w, r, http.StatusUnprocessableEntity, "invalid_code",
			"That code did not match — check the code and try again.", nil)
		return
	case errors.Is(err, auth.ErrNoTOTPEnroll):
		httpx.Error(w, r, http.StatusUnprocessableEntity, "no_enrollment",
			"Start enrollment first.", nil)
		return
	case err != nil:
		s.logger.Error("totp enable failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not enable two-factor.", nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type totpDisableRequest struct {
	Password string `json:"password"`
	Code     string `json:"code"`
}

// handleTOTPDisable removes the authenticator after re-verifying the password
// and a valid code.
func (s *Server) handleTOTPDisable(w http.ResponseWriter, r *http.Request) {
	var req totpDisableRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	err := s.auth.DisableTOTP(r.Context(), currentUser(r), req.Password, req.Code, clientIP(r))
	switch {
	case errors.Is(err, auth.ErrWrongPassword):
		httpx.Error(w, r, http.StatusForbidden, "wrong_password",
			"Password is incorrect.", nil)
		return
	case errors.Is(err, auth.ErrInvalidTOTP):
		httpx.Error(w, r, http.StatusUnprocessableEntity, "invalid_code",
			"That code did not match — check the code and try again.", nil)
		return
	case errors.Is(err, auth.ErrTOTPDisabled):
		httpx.Error(w, r, http.StatusUnprocessableEntity, "not_enabled",
			"Two-factor is not enabled.", nil)
		return
	case err != nil:
		s.logger.Error("totp disable failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not disable two-factor.", nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---------- sessions ----------

// handleListSessions returns the operator's active sessions with the current
// one marked.
func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	views, err := s.sessions.ListForUser(r.Context(), user.ID)
	if err != nil {
		s.logger.Error("list sessions failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not load sessions.", nil)
		return
	}
	currentID := ""
	if token := currentSessionToken(r); token != "" {
		if v, err := s.sessions.ByTokenHash(r.Context(), auth.TokenHash(token)); err == nil {
			currentID = v.ID
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"sessions": views, "current_id": currentID})
}

// handleRevokeOtherSessions signs out every other session of the operator.
func (s *Server) handleRevokeOtherSessions(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	n, err := s.sessions.DeleteOthersForUser(r.Context(), user.ID, auth.TokenHash(currentSessionToken(r)))
	if err != nil {
		s.logger.Error("revoke sessions failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not revoke sessions.", nil)
		return
	}
	s.auditChange(r, user, "user", user.ID, user.Username, "sessions.revoked_others",
		map[string]any{"revoked": n})
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "revoked": n})
}
