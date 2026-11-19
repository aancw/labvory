package api

import (
	"errors"
	"net/http"

	"github.com/aancw/labvory/internal/httpx"
	"github.com/aancw/labvory/internal/update"
)

// handleGetUpdate reports the last observed update-check result. The
// background scheduler refreshes it; this handler only reads the cache.
func (s *Server) handleGetUpdate(w http.ResponseWriter, r *http.Request) {
	if s.updates == nil {
		// Update checks disabled (or a source build): report the running
		// version with nothing available.
		httpx.WriteJSON(w, http.StatusOK, update.State{Current: Version})
		return
	}
	state := s.updates.State()
	if state.Current == "" {
		state.Current = Version
	}
	httpx.WriteJSON(w, http.StatusOK, state)
}

// handleInstallUpdate downloads, verifies, and swaps in the newest release
// binary, then schedules a self-restart. The response is written before the
// restart fires so the browser learns the outcome.
func (s *Server) handleInstallUpdate(w http.ResponseWriter, r *http.Request) {
	if s.installer == nil {
		httpx.Error(w, r, http.StatusConflict, "update_disabled",
			"Update checks are disabled in the configuration.", nil)
		return
	}
	res, err := s.installer.Install(r.Context())
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, update.ErrNotUpdatable):
			status = http.StatusConflict
		case errors.Is(err, update.ErrChecksum):
			status = http.StatusBadGateway
		}
		s.logger.Warn("update install failed", "error", err)
		httpx.Error(w, r, status, "update_failed", err.Error(), nil)
		return
	}

	user := currentUser(r)
	s.auditChange(r, user, "instance", "", "LabVory", "update_installed",
		map[string]any{"from": res.From, "to": res.To, "path": res.Path})

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"from":    res.From,
		"to":      res.To,
		"message": "Update applied — the server is restarting. Reload in a few seconds.",
	})
	s.installer.TriggerRestart()
}
