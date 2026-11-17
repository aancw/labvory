package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/aancw/lanvory/internal/httpx"
	"github.com/aancw/lanvory/internal/notify"
	"github.com/aancw/lanvory/internal/store"
)

// ---------- Settings ----------

// settingsSchema defines the writable settings whitelist and validation per key.
var settingsSchema = map[string]func(v json.RawMessage) (json.RawMessage, string){
	"instance.name":             validateStringSetting(1, 64),
	"theme.default":             validateStringSetting(0, 16),
	"notifications.webhook_url": validateWebhookURL,
	"audit.retention_days":      validateIntSetting(0, 3650),
}

// validateIntSetting accepts an integer inside [min, max].
func validateIntSetting(min, max int) func(json.RawMessage) (json.RawMessage, string) {
	return func(v json.RawMessage) (json.RawMessage, string) {
		var n int
		if err := json.Unmarshal(v, &n); err != nil {
			return nil, "must be an integer"
		}
		if n < min || n > max {
			return nil, "out of range"
		}
		return json.RawMessage(mustMarshal(n)), ""
	}
}

// validateWebhookURL accepts an empty string (notifications off) or an
// http(s) URL of at most 500 characters.
func validateWebhookURL(v json.RawMessage) (json.RawMessage, string) {
	var s string
	if err := json.Unmarshal(v, &s); err != nil {
		return nil, "must be a string"
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return json.RawMessage(`""`), ""
	}
	if len(s) > 500 {
		return nil, "URL too long"
	}
	if !strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://") {
		return nil, "must be an http(s) URL"
	}
	return json.RawMessage(mustMarshal(s)), ""
}

// handleTestNotification sends a sample payload to the configured webhook so
// the operator can confirm delivery before relying on it.
func (s *Server) handleTestNotification(w http.ResponseWriter, r *http.Request) {
	all, err := s.settings.All(r.Context())
	if err != nil {
		s.logger.Error("notify test: load settings failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not load settings.", nil)
		return
	}
	raw := all["notifications.webhook_url"]
	var url string
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &url)
	}
	if strings.TrimSpace(url) == "" {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "no_webhook",
			"No webhook URL configured — set it first.", nil)
		return
	}
	if err := notify.Post(r.Context(), url, "Lanvory test notification",
		"If you can read this, webhook notifications from Lanvory are working."); err != nil {
		httpx.Error(w, r, http.StatusBadGateway, "webhook_failed",
			"Webhook delivery failed: "+err.Error(), nil)
		return
	}
	s.auditChange(r, currentUser(r), "settings", "", "notifications", "notified_test", nil)
	httpx.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func validateStringSetting(minLen, maxLen int) func(json.RawMessage) (json.RawMessage, string) {
	return func(v json.RawMessage) (json.RawMessage, string) {
		var s string
		if err := json.Unmarshal(v, &s); err != nil {
			return nil, "must be a string"
		}
		if len(s) < minLen || len(s) > maxLen {
			return nil, "length out of range"
		}
		return json.RawMessage(mustMarshal(s)), ""
	}
}

func mustMarshal(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// handleGetSettings returns all instance settings.
func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	all, err := s.settings.All(r.Context())
	if err != nil {
		s.logger.Error("load settings failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not load settings.", nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"settings": all})
}

type updateSettingsRequest struct {
	Settings map[string]json.RawMessage `json:"settings"`
}

// handleUpdateSettings writes whitelisted settings with validation and audits the change.
func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	var req updateSettingsRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if len(req.Settings) == 0 {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "invalid_request",
			"No settings provided.", nil)
		return
	}

	user := currentUser(r)
	updated := map[string]json.RawMessage{}
	for key, value := range req.Settings {
		validate, ok := settingsSchema[key]
		if !ok {
			httpx.Error(w, r, http.StatusUnprocessableEntity, "unknown_setting",
				"Unknown setting: "+key, nil)
			return
		}
		valid, msg := validate(value)
		if msg != "" {
			httpx.Error(w, r, http.StatusUnprocessableEntity, "invalid_value",
				"Invalid value for "+key+": "+msg, nil)
			return
		}
		if err := s.settings.Upsert(r.Context(), key, valid, user.ID); err != nil {
			s.logger.Error("save setting failed", "error", err, "key", key)
			httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not save settings.", nil)
			return
		}
		updated[key] = valid
	}

	s.auditChange(r, user, "settings", "", "instance", "updated", updated)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"settings": updated})
}

// ---------- Dashboard ----------

// handleDashboard returns aggregate counts for the operational dashboard.
func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	stats, err := s.stats.Summary(r.Context())
	if err != nil {
		s.logger.Error("dashboard stats failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not load dashboard.", nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, stats)
}

// ---------- Activity ----------

// handleActivity lists audit events with pagination and optional filters.
func (s *Server) handleActivity(w http.ResponseWriter, r *http.Request) {
	limit := clampInt(parseQueryInt(r, "limit", 50), 1, 200)
	offset := clampInt(parseQueryInt(r, "offset", 0), 0, 100000)

	events, total, err := s.audit.List(r.Context(), limit, offset,
		r.URL.Query().Get("entity_type"), r.URL.Query().Get("action"))
	if err != nil {
		s.logger.Error("list activity failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not load activity.", nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"events": events,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// ---------- Search ----------

// handleSearch runs the global search backing the command palette.
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if len([]rune(q)) < 2 {
		httpx.WriteJSON(w, http.StatusOK, store.SearchResults{})
		return
	}
	results, err := s.search.Query(r.Context(), q)
	if err != nil {
		s.logger.Error("search failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Search failed.", nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, results)
}

// ---------- shared helpers ----------

func (s *Server) auditChange(r *http.Request, actor store.User, entityType, entityID, entityLabel, action string, changes any) {
	payload := []byte("{}")
	if changes != nil {
		payload, _ = json.Marshal(changes)
	}
	err := s.audit.Record(r.Context(), store.AuditEvent{
		ActorUserID: actor.ID,
		ActorLabel:  actor.Username,
		EntityType:  entityType,
		EntityID:    entityID,
		EntityLabel: entityLabel,
		Action:      action,
		Changes:     payload,
		SourceIP:    clientIP(r),
	})
	if err != nil {
		s.logger.Error("audit write failed", "error", err, "action", action)
	}
}

func parseQueryInt(r *http.Request, key string, fallback int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func clampInt(n, min, max int) int {
	if n < min {
		return min
	}
	if n > max {
		return max
	}
	return n
}
