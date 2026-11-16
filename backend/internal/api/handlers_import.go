package api

import (
	"encoding/json"
	"net/http"

	"github.com/petruknisme/lanvory/internal/httpx"
)

// ---------- export ----------

// handleExportInventory streams the full inventory as JSON (DoD: export & restore).
func (s *Server) handleExportInventory(w http.ResponseWriter, r *http.Request) {
	doc, err := s.importer.ExportInventory(r.Context())
	if err != nil {
		s.logger.Error("export failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not export inventory.", nil)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="lanvory-inventory.json"`)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(doc)
	s.auditChange(r, currentUser(r), "export", "", "inventory", "exported", nil)
}

// ---------- import preview ----------

type importPreviewRequest struct {
	Entity string `json:"entity"` // assets | subnets | ip_addresses
	// Scope for subnet/IP imports.
	ContextID string           `json:"context_id"`
	SubnetID  string           `json:"subnet_id"`
	Rows      []map[string]any `json:"rows"`
}

// handleImportPreview validates rows without writing anything.
func (s *Server) handleImportPreview(w http.ResponseWriter, r *http.Request) {
	var req importPreviewRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if len(req.Rows) == 0 || len(req.Rows) > 5000 {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "invalid_request",
			"Provide between 1 and 5000 rows.", nil)
		return
	}

	var results any
	var err error
	switch req.Entity {
	case "assets":
		results, err = s.importer.PreviewAssets(r.Context(), req.Rows)
	case "subnets":
		if !isUUID(req.ContextID) {
			httpx.Error(w, r, http.StatusUnprocessableEntity, "invalid_context",
				"Subnet imports require a valid context_id.", nil)
			return
		}
		results, err = s.importer.PreviewSubnets(r.Context(), req.Rows, req.ContextID)
	case "ip_addresses":
		if !isUUID(req.SubnetID) {
			httpx.Error(w, r, http.StatusUnprocessableEntity, "invalid_subnet",
				"IP imports require a valid subnet_id.", nil)
			return
		}
		results, err = s.importer.PreviewIPs(r.Context(), req.Rows, req.SubnetID)
	default:
		httpx.Error(w, r, http.StatusUnprocessableEntity, "invalid_entity",
			"Entity must be assets, subnets or ip_addresses.", nil)
		return
	}
	if err != nil {
		s.logger.Error("import preview failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Preview failed.", nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"rows": results})
}

// ---------- import commit ----------

type importCommitRequest struct {
	importPreviewRequest
	SkipDuplicates bool `json:"skip_duplicates"`
}

// handleImportCommit writes valid rows in one transaction; duplicates are
// skipped (never overwritten) unless explicitly rejected.
func (s *Server) handleImportCommit(w http.ResponseWriter, r *http.Request) {
	var req importCommitRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if len(req.Rows) == 0 || len(req.Rows) > 5000 {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "invalid_request",
			"Provide between 1 and 5000 rows.", nil)
		return
	}

	var summary any
	var err error
	switch req.Entity {
	case "assets":
		summary, err = s.importer.CommitAssets(r.Context(), req.Rows, req.SkipDuplicates)
	case "subnets":
		if !isUUID(req.ContextID) {
			httpx.Error(w, r, http.StatusUnprocessableEntity, "invalid_context",
				"Subnet imports require a valid context_id.", nil)
			return
		}
		summary, err = s.importer.CommitSubnets(r.Context(), req.Rows, req.ContextID, req.SkipDuplicates)
	case "ip_addresses":
		if !isUUID(req.SubnetID) {
			httpx.Error(w, r, http.StatusUnprocessableEntity, "invalid_subnet",
				"IP imports require a valid subnet_id.", nil)
			return
		}
		summary, err = s.importer.CommitIPs(r.Context(), req.Rows, req.SubnetID, req.SkipDuplicates)
	default:
		httpx.Error(w, r, http.StatusUnprocessableEntity, "invalid_entity",
			"Entity must be assets, subnets or ip_addresses.", nil)
		return
	}
	if err != nil {
		// Validation failure: nothing was written (transaction rolled back).
		httpx.Error(w, r, http.StatusUnprocessableEntity, "import_failed", err.Error(), summary)
		return
	}

	s.auditChange(r, currentUser(r), "import", "", req.Entity, "imported", summary)
	httpx.WriteJSON(w, http.StatusOK, summary)
}

// ---------- JSON restore ----------

type restoreRequest struct {
	Inventory map[string]any `json:"inventory"`
}

// handleImportRestore re-imports a previously exported inventory idempotently.
func (s *Server) handleImportRestore(w http.ResponseWriter, r *http.Request) {
	var req restoreRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if req.Inventory == nil || len(req.Inventory) == 0 {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "invalid_request",
			"Provide an exported inventory document.", nil)
		return
	}

	summary, err := s.importer.RestoreInventory(r.Context(), req.Inventory, currentUser(r).ID)
	if err != nil {
		s.logger.Error("restore failed", "error", err)
		httpx.Error(w, r, http.StatusUnprocessableEntity, "restore_failed", err.Error(), nil)
		return
	}
	s.auditChange(r, currentUser(r), "import", "", "inventory", "restored", summary)
	httpx.WriteJSON(w, http.StatusOK, summary)
}
