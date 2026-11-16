package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/petruknisme/lanvory/internal/httpx"
	"github.com/petruknisme/lanvory/internal/secrets"
	"github.com/petruknisme/lanvory/internal/store"
)

// Implemented kinds vs. designed-but-not-built ones (honesty in the UI).
var implementedKinds = setOf("proxmox", "docker", "icmp", "dns", "cloudflare", "kubernetes", "adguard", "arp")
var knownKinds = setOf("proxmox", "docker", "icmp", "snmp", "ssh", "unifi", "opnsense", "pfsense", "dns", "cloudflare", "kubernetes", "adguard", "arp")

// Secret keys that must be encrypted at rest and never echoed back.
var secretKeysByKind = map[string][]string{
	"proxmox":    {"token_secret"},
	"docker":     {},
	"icmp":       {},
	"dns":        {},
	"cloudflare": {"api_token"},
	"kubernetes": {"token"},
	"adguard":    {"password"},
	"arp":        {},
}

// handleListIntegrations returns configured integrations with secrets redacted.
func (s *Server) handleListIntegrations(w http.ResponseWriter, r *http.Request) {
	items, err := s.integrations.List(r.Context())
	if err != nil {
		s.logger.Error("list integrations failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not load integrations.", nil)
		return
	}
	out := make([]map[string]any, 0, len(items))
	for _, integ := range items {
		out = append(out, s.redactIntegration(integ))
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"integrations":  out,
		"implemented":   []string{"proxmox", "docker", "icmp", "dns", "cloudflare", "kubernetes", "adguard", "arp"},
		"planned":       []string{"snmp", "ssh", "unifi", "opnsense", "pfsense"},
		"secrets_ready": s.box != nil,
	})
}

// redactIntegration strips encrypted secret material from the payload.
func (s *Server) redactIntegration(integ store.Integration) map[string]any {
	configured := map[string]bool{}
	var cfg map[string]any
	_ = json.Unmarshal(integ.Config, &cfg)
	if secretsRaw, ok := cfg["secrets"].(map[string]any); ok {
		for key := range secretsRaw {
			configured[key] = true
		}
	}
	delete(cfg, "secrets")
	if cfg == nil {
		cfg = map[string]any{}
	}
	cfg["secrets_configured"] = configured
	cfg["secrets_encrypted"] = s.box != nil

	return map[string]any{
		"id": integ.ID, "kind": integ.Kind, "name": integ.Name,
		"description": integ.Description, "enabled": integ.Enabled,
		"config": cfg, "last_sync_at": integ.LastSyncAt,
		"created_at": integ.CreatedAt, "updated_at": integ.UpdatedAt,
		"implemented": implementedKinds[integ.Kind],
	}
}

type integrationPayload struct {
	Kind        string          `json:"kind"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Enabled     bool            `json:"enabled"`
	Config      json.RawMessage `json:"config"`
	Secrets     json.RawMessage `json:"secrets"` // plaintext in, encrypted at rest, never out
}

// handleCreateIntegration stores a new integration; secrets are encrypted.
func (s *Server) handleCreateIntegration(w http.ResponseWriter, r *http.Request) {
	var p integrationPayload
	if err := httpx.DecodeJSON(r, &p); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if !knownKinds[p.Kind] {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "invalid_kind",
			"Unknown integration kind.", nil)
		return
	}
	if p.Name == "" {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "invalid_name", "Name is required.", nil)
		return
	}

	merged, herr := s.mergeSecrets(p.Kind, p.Config, p.Secrets, map[string]any{})
	if herr != nil {
		herr.write(reqCtx{w, r, currentUser(r)})
		return
	}

	integ := store.Integration{
		Kind: p.Kind, Name: p.Name, Description: p.Description,
		Enabled: p.Enabled, Config: merged,
	}
	if err := s.integrations.Create(r.Context(), &integ); errors.Is(err, store.ErrConflict) {
		httpx.Error(w, r, http.StatusConflict, "duplicate_integration",
			"An integration with this name already exists.", nil)
		return
	} else if err != nil {
		s.logger.Error("create integration failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not create integration.", nil)
		return
	}
	s.auditChange(r, currentUser(r), "integration", integ.ID, integ.Name, "created",
		map[string]any{"kind": integ.Kind})
	httpx.WriteJSON(w, http.StatusCreated, s.redactIntegration(integ))
}

func (s *Server) handleUpdateIntegration(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "integrationID")
	if !isUUID(id) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_id", "Invalid integration id.", nil)
		return
	}
	var p integrationPayload
	if err := httpx.DecodeJSON(r, &p); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}

	existing, err := s.integrations.Get(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", "Integration not found.", nil)
		return
	}
	if err != nil {
		s.logger.Error("integration update: load failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not update integration.", nil)
		return
	}

	var existingCfg map[string]any
	_ = json.Unmarshal(existing.Config, &existingCfg)

	merged, herr := s.mergeSecrets(existing.Kind, p.Config, p.Secrets, existingCfg)
	if herr != nil {
		herr.write(reqCtx{w, r, currentUser(r)})
		return
	}

	integ := store.Integration{
		ID: id, Kind: existing.Kind, Name: p.Name, Description: p.Description,
		Enabled: p.Enabled, Config: merged,
	}
	if err := s.integrations.Update(r.Context(), &integ); err != nil {
		s.logger.Error("update integration failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not update integration.", nil)
		return
	}
	s.auditChange(r, currentUser(r), "integration", id, integ.Name, "updated", nil)
	httpx.WriteJSON(w, http.StatusOK, s.redactIntegration(integ))
}

// mergeSecrets combines the non-secret config with encrypted secret material.
func (s *Server) mergeSecrets(kind string, configJSON, secretsJSON json.RawMessage, previous map[string]any) (json.RawMessage, *httpxError) {
	cfg := map[string]any{}
	if len(configJSON) > 0 {
		if err := json.Unmarshal(configJSON, &cfg); err != nil {
			return nil, errInvalid("invalid_config", "config must be a JSON object.")
		}
	}
	delete(cfg, "secrets_configured")
	delete(cfg, "secrets_encrypted")

	newSecrets := map[string]string{}
	if len(secretsJSON) > 0 {
		if err := json.Unmarshal(secretsJSON, &newSecrets); err != nil {
			return nil, errInvalid("invalid_secrets", "secrets must be a JSON object of key→value strings.")
		}
	}

	// Carry forward previously-encrypted secrets not present in this request.
	prevSecrets, _ := previous["secrets"].(map[string]any)
	secretKeys := secretKeysByKind[kind]
	mergedSecrets := map[string]string{}
	for _, key := range secretKeys {
		if plain, ok := newSecrets[key]; ok && plain != "" {
			if s.box == nil {
				return nil, &httpxError{http.StatusUnprocessableEntity, "secrets_unavailable",
					"LV_SECRET_KEY is not configured on the server: integration secrets cannot be stored."}
			}
			enc, err := s.box.Encrypt(plain)
			if err != nil {
				return nil, &httpxError{http.StatusInternalServerError, "internal", "Could not encrypt secret."}
			}
			mergedSecrets[key] = enc
		} else if prev, ok := prevSecrets[key].(string); ok && prev != "" {
			mergedSecrets[key] = prev // keep existing encrypted blob
		}
	}
	if len(mergedSecrets) > 0 {
		cfg["secrets"] = mergedSecrets
	}

	merged, err := json.Marshal(cfg)
	if err != nil {
		return nil, &httpxError{http.StatusInternalServerError, "internal", "Could not serialize config."}
	}
	return merged, nil
}

func (s *Server) handleDeleteIntegration(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "integrationID")
	if !isUUID(id) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_id", "Invalid integration id.", nil)
		return
	}
	integ, err := s.integrations.Get(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", "Integration not found.", nil)
		return
	}
	if err != nil {
		s.logger.Error("integration delete: load failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not delete integration.", nil)
		return
	}
	if err := s.integrations.Delete(r.Context(), id); err != nil {
		s.logger.Error("delete integration failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not delete integration.", nil)
		return
	}
	s.auditChange(r, currentUser(r), "integration", id, integ.Name, "deleted",
		map[string]any{"kind": integ.Kind})
	httpx.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleSyncIntegration runs one sync synchronously (homelab-scale: seconds).
func (s *Server) handleSyncIntegration(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "integrationID")
	if !isUUID(id) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_id", "Invalid integration id.", nil)
		return
	}
	integ, err := s.integrations.Get(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", "Integration not found.", nil)
		return
	}
	if err != nil {
		s.logger.Error("sync: load failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Sync failed.", nil)
		return
	}
	if !implementedKinds[integ.Kind] {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "not_implemented",
			"This integration kind is planned but not implemented yet.", nil)
		return
	}
	if !integ.Enabled {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "integration_disabled",
			"This integration is disabled: enable it before running syncs.", nil)
		return
	}
	if s.runner == nil {
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Sync runner unavailable.", nil)
		return
	}

	summary, syncErr := s.runner.Run(r.Context(), integ)
	if syncErr != nil {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "sync_failed", syncErr.Error(), summary)
		return
	}
	s.auditChange(r, currentUser(r), "integration", id, integ.Name, "synced", summary)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "summary": summary})
}

func (s *Server) handleListSyncRuns(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "integrationID")
	if !isUUID(id) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_id", "Invalid integration id.", nil)
		return
	}
	runs, err := s.integrations.ListRuns(r.Context(), id, 20)
	if err != nil {
		s.logger.Error("list sync runs failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not load sync history.", nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"runs": runs})
}

// ensure box/runner are typed imports so the linter sees usage.
var _ = secrets.IsEncrypted
