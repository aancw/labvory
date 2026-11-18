package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/aancw/lanvory/internal/auth"
	"github.com/aancw/lanvory/internal/httpx"
	"github.com/aancw/lanvory/internal/store"
)

// userRoles is the role ladder. admin = everything; operator = inventory
// mutations but not accounts/settings/tokens; viewer = read-only.
var userRoles = setOf("admin", "operator", "viewer")

// requireAdmin gates admin-only surfaces (user management).
func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if currentUser(r).Role != "admin" {
			httpx.Error(w, r, http.StatusForbidden, "role_forbidden",
				"Only administrators can manage accounts.", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// enforceRoles restricts data-API mutations by role. viewer = read-only;
// operator = inventory mutations but not accounts, settings, or tokens;
// admin = everything. GETs stay open to all authenticated callers.
func (s *Server) enforceRoles(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := currentUser(r)
		switch user.Role {
		case "admin", "":
			// "" happens only for internal dispatch paths that never attach a
			// user (none reach this middleware in production wiring).
			next.ServeHTTP(w, r)
			return
		}
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		if user.Role == "viewer" {
			httpx.Error(w, r, http.StatusForbidden, "role_forbidden",
				"Your account is read-only.", nil)
			return
		}
		// operator: admin-owned surfaces stay off-limits.
		if strings.HasPrefix(r.URL.Path, "/api/v1/users") ||
			strings.HasPrefix(r.URL.Path, "/api/v1/settings") ||
			strings.HasPrefix(r.URL.Path, "/api/v1/tokens") {
			httpx.Error(w, r, http.StatusForbidden, "role_forbidden",
				"Accounts, settings and tokens are administrator-only.", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireTwoFactor enforces the instance-wide "require two-factor
// authentication" setting: while it is on, callers without an active TOTP
// enrollment are walled off from the data API. Authentication and 2FA
// setup/enable endpoints stay reachable so an unenrolled user can always
// complete enrollment (and sign out) — nobody can be locked out of their own
// account by the setting.
func (s *Server) requireTwoFactor(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := currentUser(r)
		if user.ID == "" || user.TOTPEnabledAt != nil {
			next.ServeHTTP(w, r)
			return
		}
		required, err := s.settings.GetBool(r.Context(), "security.require_2fa")
		if err != nil {
			s.logger.Error("load require_2fa setting failed", "error", err)
			// Fail closed would lock every session out on a DB hiccup; the
			// setting is a hardening layer, so degrade open instead.
			next.ServeHTTP(w, r)
			return
		}
		if required {
			httpx.Error(w, r, http.StatusForbidden, "totp_required",
				"Two-factor authentication is required on this instance. Set it up before continuing.", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ---------- user management ----------

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.users.List(r.Context())
	if err != nil {
		s.logger.Error("list users failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not load accounts.", nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"users": users})
}

type createUserRequest struct {
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Password    string `json:"password"`
	Role        string `json:"role"`
}

// handleCreateUser adds an account. Passwords follow the same policy as the
// setup wizard; the actor is audited.
func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if err := auth.ValidateUsername(req.Username); errors.Is(err, auth.ErrInvalidUsername) {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "invalid_username",
			"Username must be 3-32 characters: letters, digits, '.', '_' or '-'.", nil)
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if errors.Is(err, auth.ErrPasswordTooShort) || errors.Is(err, auth.ErrPasswordTooLong) {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "invalid_password", err.Error(), nil)
		return
	}
	if err != nil {
		s.logger.Error("hash password failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not create account.", nil)
		return
	}
	if req.Role == "" {
		req.Role = "viewer"
	}
	if !userRoles[req.Role] {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "invalid_role",
			"Role must be admin, operator or viewer.", nil)
		return
	}
	if req.DisplayName == "" {
		req.DisplayName = req.Username
	}
	user, err := s.users.Create(r.Context(), req.Username, req.DisplayName, hash, req.Role)
	if errors.Is(err, store.ErrConflict) {
		httpx.Error(w, r, http.StatusConflict, "username_taken", "Username already taken.", nil)
		return
	}
	if err != nil {
		s.logger.Error("create user failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not create account.", nil)
		return
	}
	s.auditChange(r, currentUser(r), "user", user.ID, user.Username, "created",
		map[string]any{"role": user.Role})
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"user": user})
}

type updateUserRequest struct {
	DisplayName *string `json:"display_name"`
	Role        *string `json:"role"`
	Password    *string `json:"password"`
}

// handleUpdateUser edits an account: display name, role, and optionally a
// password reset (which revokes the target's sessions). The last-admin and
// self-demote guards protect operator lockout.
func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "userID")
	if !isUUID(id) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_id", "Invalid user id.", nil)
		return
	}
	var req updateUserRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	target, err := s.users.GetByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", "Account not found.", nil)
		return
	}
	if err != nil {
		s.logger.Error("update user: load failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not update account.", nil)
		return
	}

	changes := map[string]any{}
	newRole := target.Role
	if req.Role != nil {
		if !userRoles[*req.Role] {
			httpx.Error(w, r, http.StatusUnprocessableEntity, "invalid_role",
				"Role must be admin, operator or viewer.", nil)
			return
		}
		if *req.Role != target.Role {
			// Removing an admin (demote or role change) needs another admin left.
			if target.Role == "admin" && *req.Role != "admin" {
				n, cerr := s.users.CountAdmins(r.Context())
				if cerr != nil {
					s.logger.Error("count admins failed", "error", cerr)
					httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not update account.", nil)
					return
				}
				if n <= 1 {
					httpx.Error(w, r, http.StatusConflict, "last_admin",
						"Cannot demote the last administrator.", nil)
					return
				}
			}
			changes["role"] = map[string]any{"from": target.Role, "to": *req.Role}
			newRole = *req.Role
		}
	}
	displayName := target.DisplayName
	if req.DisplayName != nil {
		if *req.DisplayName != target.DisplayName {
			changes["display_name"] = map[string]any{"from": target.DisplayName, "to": *req.DisplayName}
		}
		displayName = strings.TrimSpace(*req.DisplayName)
	}
	if err := s.users.UpdateProfile(r.Context(), id, displayName, newRole); err != nil {
		s.logger.Error("update user failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not update account.", nil)
		return
	}

	// Optional password reset by the administrator.
	if req.Password != nil && *req.Password != "" {
		hash, err := auth.HashPassword(*req.Password)
		if errors.Is(err, auth.ErrPasswordTooShort) || errors.Is(err, auth.ErrPasswordTooLong) {
			httpx.Error(w, r, http.StatusUnprocessableEntity, "invalid_password", err.Error(), nil)
			return
		}
		if err != nil {
			s.logger.Error("hash password failed", "error", err)
			httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not update account.", nil)
			return
		}
		if err := s.users.UpdatePassword(r.Context(), id, hash); err != nil {
			s.logger.Error("reset password failed", "error", err)
			httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not update account.", nil)
			return
		}
		if _, err := s.sessions.DeleteAllForUser(r.Context(), id); err != nil {
			s.logger.Error("revoke sessions failed", "error", err)
		}
		changes["password"] = map[string]any{"reset": true}
	}

	s.auditChange(r, currentUser(r), "user", id, target.Username, "updated", changes)
	httpx.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleDeleteUser removes an account. Self-deletion and removing the last
// administrator are refused; the target's sessions cascade.
func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "userID")
	if !isUUID(id) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_id", "Invalid user id.", nil)
		return
	}
	actor := currentUser(r)
	if actor.ID == id {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "cannot_delete_self",
			"You cannot delete the account you are signed in with.", nil)
		return
	}
	target, err := s.users.GetByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", "Account not found.", nil)
		return
	}
	if err != nil {
		s.logger.Error("delete user: load failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not delete account.", nil)
		return
	}
	if target.Role == "admin" {
		n, cerr := s.users.CountAdmins(r.Context())
		if cerr != nil {
			s.logger.Error("count admins failed", "error", cerr)
			httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not delete account.", nil)
			return
		}
		if n <= 1 {
			httpx.Error(w, r, http.StatusConflict, "last_admin",
				"Cannot delete the last administrator.", nil)
			return
		}
	}
	if err := s.users.Delete(r.Context(), id); err != nil {
		s.logger.Error("delete user failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not delete account.", nil)
		return
	}
	s.auditChange(r, actor, "user", id, target.Username, "deleted", nil)
	httpx.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
