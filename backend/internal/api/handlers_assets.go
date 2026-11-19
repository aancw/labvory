package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/aancw/labvory/internal/httpx"
	"github.com/aancw/labvory/internal/integrations"
	"github.com/aancw/labvory/internal/store"
)

var (
	uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	nameRe = regexp.MustCompile(`^[\w .\-()/@:+]{1,100}$`)
	macRe  = regexp.MustCompile(`^([0-9A-Fa-f]{2}[:-]){5}[0-9A-Fa-f]{2}$`)
)

var assetStatuses = setOf("online", "offline", "maintenance", "retired", "unknown")
var assetLifecycles = setOf("planned", "active", "staged", "decommissioned")
var environments = setOf("production", "development", "testing", "lab")
var interfaceKinds = setOf("ethernet", "wifi", "vlan", "bond", "loopback", "other")
var relationshipKinds = setOf("runs_on", "connected_to", "depends_on")

func setOf(values ...string) map[string]bool {
	m := make(map[string]bool, len(values))
	for _, v := range values {
		m[v] = true
	}
	return m
}

func isUUID(v string) bool { return uuidRe.MatchString(v) }

// ---------- payloads ----------

type assetPayload struct {
	Name            string          `json:"name"`
	Hostname        string          `json:"hostname"`
	TypeID          string          `json:"type_id"`
	Status          string          `json:"status"`
	Lifecycle       string          `json:"lifecycle"`
	Environment     string          `json:"environment"`
	LocationID      *string         `json:"location_id"`
	ParentID        *string         `json:"parent_id"`
	Manufacturer    string          `json:"manufacturer"`
	Model           string          `json:"model"`
	SerialNumber    string          `json:"serial_number"`
	OSName          string          `json:"os_name"`
	OSVersion       string          `json:"os_version"`
	CPUModel        string          `json:"cpu_model"`
	CPUCores        *int            `json:"cpu_cores"`
	RamGB           *float64        `json:"ram_gb"`
	StorageGB       *float64        `json:"storage_gb"`
	PurchaseDate    *string         `json:"purchase_date"`
	PurchaseCost    *float64        `json:"purchase_cost"`
	WarrantyExpires *string         `json:"warranty_expires"`
	Notes           string          `json:"notes"`
	Metadata        json.RawMessage `json:"metadata"`
	TagIDs          []string        `json:"tag_ids"`
}

// validateAssetPayload checks enum membership and referenced IDs exist; returns an HTTP-ready error.
func (s *Server) validateAssetPayload(ctx context.Context, p *assetPayload, currentID string) *httpxError {
	p.Name = strings.TrimSpace(p.Name)
	if !nameRe.MatchString(p.Name) {
		return errInvalid("invalid_name", "Name must be 1-100 characters without control characters.")
	}
	if p.Hostname != "" && len(p.Hostname) > 100 {
		return errInvalid("invalid_hostname", "Hostname must be at most 100 characters.")
	}
	if p.Status == "" {
		p.Status = "unknown"
	}
	if !assetStatuses[p.Status] {
		return errInvalid("invalid_status", "Status must be one of: online, offline, maintenance, retired, unknown.")
	}
	if p.Lifecycle == "" {
		p.Lifecycle = "active"
	}
	if !assetLifecycles[p.Lifecycle] {
		return errInvalid("invalid_lifecycle", "Lifecycle must be one of: planned, active, staged, decommissioned.")
	}
	if p.Environment == "" {
		p.Environment = "production"
	}
	if !environments[p.Environment] {
		return errInvalid("invalid_environment", "Environment must be one of: production, development, testing, lab.")
	}
	if !isUUID(p.TypeID) {
		return errInvalid("invalid_type", "A valid asset type is required.")
	}
	if p.LocationID != nil && *p.LocationID != "" {
		if !isUUID(*p.LocationID) {
			return errInvalid("invalid_location", "Invalid location reference.")
		}
		if _, err := s.locations.Get(ctx, *p.LocationID); err != nil {
			return errInvalid("unknown_location", "Referenced location does not exist.")
		}
	} else {
		p.LocationID = nil
	}
	if p.ParentID != nil && *p.ParentID != "" {
		if !isUUID(*p.ParentID) {
			return errInvalid("invalid_parent", "Invalid parent asset reference.")
		}
		if *p.ParentID == currentID {
			return errInvalid("invalid_parent", "An asset cannot be its own parent.")
		}
		if _, err := s.assets.GetByID(ctx, *p.ParentID); err != nil {
			return errInvalid("unknown_parent", "Referenced parent asset does not exist.")
		}
	} else {
		p.ParentID = nil
	}
	if p.PurchaseDate != nil && *p.PurchaseDate != "" && !dateRe.MatchString(*p.PurchaseDate) {
		return errInvalid("invalid_date", "Dates must use YYYY-MM-DD.")
	}
	if p.WarrantyExpires != nil && *p.WarrantyExpires != "" && !dateRe.MatchString(*p.WarrantyExpires) {
		return errInvalid("invalid_date", "Dates must use YYYY-MM-DD.")
	}
	if p.CPUCores != nil && (*p.CPUCores < 0 || *p.CPUCores > 4096) {
		return errInvalid("invalid_cpu_cores", "CPU cores must be between 0 and 4096.")
	}
	if p.RamGB != nil && (*p.RamGB < 0 || *p.RamGB > 1e6) {
		return errInvalid("invalid_ram", "RAM must be between 0 and 1,000,000 GB.")
	}
	if p.StorageGB != nil && (*p.StorageGB < 0 || *p.StorageGB > 1e7) {
		return errInvalid("invalid_storage", "Storage must be between 0 and 10,000,000 GB.")
	}
	for _, tagID := range p.TagIDs {
		if !isUUID(tagID) {
			return errInvalid("invalid_tag", "Invalid tag reference.")
		}
	}
	return nil
}

var dateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// reqCtx bundles request + authenticated user for handlers.
type reqCtx struct {
	w    http.ResponseWriter
	r    *http.Request
	user store.User
}

type httpxError struct {
	status int
	code   string
	msg    string
}

func errInvalid(code, msg string) *httpxError {
	return &httpxError{http.StatusUnprocessableEntity, code, msg}
}

func (e *httpxError) write(rc reqCtx) {
	httpx.Error(rc.w, rc.r, e.status, e.code, e.msg, nil)
}

// ---------- asset CRUD ----------

func (s *Server) handleCreateAsset(w http.ResponseWriter, r *http.Request) {
	var p assetPayload
	if err := httpx.DecodeJSON(r, &p); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	rc := reqCtx{w, r, currentUser(r)}
	if herr := s.validateAssetPayload(r.Context(), &p, ""); herr != nil {
		herr.write(rc)
		return
	}

	asset := payloadToAsset(p)
	if err := s.assets.Create(r.Context(), &asset, p.TagIDs); err != nil {
		s.logger.Error("create asset failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not create asset.", nil)
		return
	}

	s.auditChange(r, rc.user, "asset", asset.ID, asset.Name, "created", map[string]any{
		"name": asset.Name, "type": asset.TypeLabel, "status": asset.Status,
	})
	s.writeAssetDetail(w, r, asset.ID, http.StatusCreated)
}

func payloadToAsset(p assetPayload) store.Asset {
	return store.Asset{
		Name: p.Name, Hostname: p.Hostname, TypeID: p.TypeID,
		// Source stays "manual" only for new assets; updates preserve the original.

		Status: p.Status, Lifecycle: p.Lifecycle, Environment: p.Environment,
		LocationID: p.LocationID, ParentID: p.ParentID,
		Manufacturer: p.Manufacturer, Model: p.Model, SerialNumber: p.SerialNumber,
		OSName: p.OSName, OSVersion: p.OSVersion, CPUModel: p.CPUModel,
		CPUCores: p.CPUCores, RamGB: p.RamGB, StorageGB: p.StorageGB,
		PurchaseDate: emptyToNil(p.PurchaseDate), PurchaseCost: p.PurchaseCost,
		WarrantyExpires: emptyToNil(p.WarrantyExpires),
		Notes:           p.Notes, Source: "manual",
		Metadata: p.Metadata,
	}
}

func emptyToNil(s *string) *string {
	if s == nil || *s == "" {
		return nil
	}
	return s
}

// handleCheckAsset runs an on-demand ICMP reachability probe against the
// asset's hostname (preferred) or primary IP and records the observation
// under metadata.observed.icmp — the same shape the scheduled connector uses.
func (s *Server) handleCheckAsset(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "assetID")
	if !isUUID(id) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_id", "Invalid asset id.", nil)
		return
	}
	asset, err := s.assets.GetByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", "Asset not found.", nil)
		return
	}
	if err != nil {
		s.logger.Error("asset check: load failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not check asset.", nil)
		return
	}
	target := asset.Hostname
	if target == "" && asset.PrimaryIP != nil {
		target = *asset.PrimaryIP
	}
	if strings.TrimSpace(target) == "" {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "no_target",
			"Nothing to check: the asset has neither a hostname nor a tracked IP address.", nil)
		return
	}

	reachable := integrations.ProbeHost(r.Context(), target, 2)
	state := "offline"
	if reachable {
		state = "online"
	}
	if err := store.NewIntegrations(s.pool).SetObservedSub(r.Context(), asset.ID, "icmp", map[string]any{
		"status": state, "target": target, "checked_at": time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		s.logger.Error("asset check: record observation failed", "error", err)
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok": true, "reachable": reachable, "target": target, "status": state,
	})
}

func (s *Server) handleListAssets(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.AssetFilter{
		Q:           q.Get("q"),
		Category:    q.Get("category"),
		TypeID:      q.Get("type_id"),
		Status:      q.Get("status"),
		Environment: q.Get("environment"),
		LocationID:  q.Get("location_id"),
		ParentID:    q.Get("parent_id"),
		Sort:        q.Get("sort"),
		Order:       q.Get("order"),
		Limit:       parseQueryInt(r, "limit", 50),
		Offset:      parseQueryInt(r, "offset", 0),
	}
	if slugs := q.Get("type_slugs"); slugs != "" {
		f.TypeSlugs = strings.Split(slugs, ",")
	}

	// Whitelist filter values defensively.
	if f.Category != "" && f.Category != "physical" && f.Category != "virtual" {
		f.Category = ""
	}
	if f.Status != "" && !assetStatuses[f.Status] {
		f.Status = ""
	}
	if f.Environment != "" && !environments[f.Environment] {
		f.Environment = ""
	}

	assets, total, err := s.assets.List(r.Context(), f)
	if err != nil {
		s.logger.Error("list assets failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not load assets.", nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"assets": assets, "total": total,
		"limit": clampInt(f.Limit, 1, 100), "offset": f.Offset,
	})
}

func (s *Server) handleGetAsset(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "assetID")
	if !isUUID(id) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_id", "Invalid asset id.", nil)
		return
	}
	s.writeAssetDetail(w, r, id, http.StatusOK)
}

// writeAssetDetail assembles the full detail payload: asset + interfaces + relationships +
// children + recent entity activity.
func (s *Server) writeAssetDetail(w http.ResponseWriter, r *http.Request, id string, status int) {
	ctx := r.Context()
	asset, err := s.assets.GetByID(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", "Asset not found.", nil)
		return
	}
	if err != nil {
		s.logger.Error("get asset failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not load asset.", nil)
		return
	}

	interfaces, err := s.interfaces.ListByAsset(ctx, id)
	if err != nil {
		s.logger.Error("list interfaces failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not load interfaces.", nil)
		return
	}
	relationships, err := s.relationships.ListForAsset(ctx, id)
	if err != nil {
		s.logger.Error("list relationships failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not load relationships.", nil)
		return
	}
	children, _, err := s.assets.List(ctx, store.AssetFilter{ParentID: id, Limit: 100, Sort: "name", Order: "asc"})
	if err != nil {
		s.logger.Error("list children failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not load hosted resources.", nil)
		return
	}
	activity, err := s.audit.ListForEntity(ctx, "asset", id, 15)
	if err != nil {
		s.logger.Error("asset activity failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not load activity.", nil)
		return
	}

	httpx.WriteJSON(w, status, map[string]any{
		"asset":         asset,
		"interfaces":    interfaces,
		"relationships": relationships,
		"children":      children,
		"activity":      activity,
	})
}

func (s *Server) handleUpdateAsset(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "assetID")
	if !isUUID(id) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_id", "Invalid asset id.", nil)
		return
	}
	var p assetPayload
	if err := httpx.DecodeJSON(r, &p); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}

	ctx := r.Context()
	before, err := s.assets.GetByID(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", "Asset not found.", nil)
		return
	}
	if err != nil {
		s.logger.Error("load asset for update failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not update asset.", nil)
		return
	}

	rc := reqCtx{w, r, currentUser(r)}
	if herr := s.validateAssetPayload(r.Context(), &p, id); herr != nil {
		herr.write(rc)
		return
	}

	after := payloadToAsset(p)
	after.ID = id
	// Source attributions (metadata, source) are managed by integrations and the
	// create path: a manual edit without a metadata field must never wipe them.
	if len(p.Metadata) == 0 {
		after.Metadata = before.Metadata
	}
	if err := s.assets.Update(ctx, &after, p.TagIDs); err != nil {
		s.logger.Error("update asset failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not update asset.", nil)
		return
	}

	s.auditChange(r, rc.user, "asset", id, after.Name, "updated", diffMap(before, after))
	s.writeAssetDetail(w, r, id, http.StatusOK)
}

func (s *Server) handleDeleteAsset(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "assetID")
	if !isUUID(id) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_id", "Invalid asset id.", nil)
		return
	}

	ctx := r.Context()
	asset, err := s.assets.GetByID(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", "Asset not found.", nil)
		return
	}
	if err != nil {
		s.logger.Error("load asset for delete failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not delete asset.", nil)
		return
	}

	if err := s.assets.Delete(ctx, id); errors.Is(err, store.ErrHasChildren) {
		httpx.Error(w, r, http.StatusConflict, "has_children",
			"This asset hosts other assets. Move or delete them first.", nil)
		return
	} else if err != nil {
		s.logger.Error("delete asset failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not delete asset.", nil)
		return
	}

	s.auditChange(r, currentUser(r), "asset", id, asset.Name, "deleted", map[string]any{
		"name": asset.Name, "type": asset.TypeLabel,
	})
	httpx.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---------- bulk operations ----------

type bulkRequest struct {
	Action string   `json:"action"`
	IDs    []string `json:"ids"`
	Status string   `json:"status,omitempty"`
	Env    string   `json:"environment,omitempty"`
}

func (s *Server) handleBulkAssets(w http.ResponseWriter, r *http.Request) {
	var req bulkRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if len(req.IDs) == 0 || len(req.IDs) > 500 {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "invalid_request",
			"Provide between 1 and 500 asset ids.", nil)
		return
	}
	for _, id := range req.IDs {
		if !isUUID(id) {
			httpx.Error(w, r, http.StatusUnprocessableEntity, "invalid_request",
				"Invalid asset id in selection.", nil)
			return
		}
	}

	ctx := r.Context()
	user := currentUser(r)

	switch req.Action {
	case "delete":
		// Refuse the whole batch when any selected asset hosts children (atomic semantics).
		children, err := s.assets.CountChildren(ctx, req.IDs)
		if err != nil {
			s.logger.Error("bulk children check failed", "error", err)
			httpx.Error(w, r, http.StatusInternalServerError, "internal", "Bulk delete failed.", nil)
			return
		}
		var blocked []string
		for id, n := range children {
			if n > 0 {
				blocked = append(blocked, id)
			}
		}
		if len(blocked) > 0 {
			httpx.Error(w, r, http.StatusConflict, "has_children",
				"Some selected assets host other assets and cannot be deleted.", map[string]any{"asset_ids": blocked})
			return
		}
		if err := s.assets.DeleteMany(ctx, req.IDs); err != nil {
			s.logger.Error("bulk delete failed", "error", err)
			httpx.Error(w, r, http.StatusInternalServerError, "internal", "Bulk delete failed.", nil)
			return
		}
		s.auditChange(r, user, "asset", "", "", "bulk_deleted",
			map[string]any{"count": len(req.IDs), "ids": req.IDs})
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "affected": len(req.IDs)})

	case "set_status":
		if !assetStatuses[req.Status] {
			httpx.Error(w, r, http.StatusUnprocessableEntity, "invalid_status",
				"Status must be one of: online, offline, maintenance, retired, unknown.", nil)
			return
		}
		n, err := s.assets.BulkUpdateStatus(ctx, req.Status, req.IDs)
		if err != nil {
			s.logger.Error("bulk status failed", "error", err)
			httpx.Error(w, r, http.StatusInternalServerError, "internal", "Bulk update failed.", nil)
			return
		}
		s.auditChange(r, user, "asset", "", "", "bulk_status_changed",
			map[string]any{"status": req.Status, "count": n, "ids": req.IDs})
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "affected": n})

	case "set_environment":
		if !environments[req.Env] {
			httpx.Error(w, r, http.StatusUnprocessableEntity, "invalid_environment",
				"Environment must be one of: production, development, testing, lab.", nil)
			return
		}
		n, err := s.assets.BulkUpdateEnvironment(ctx, req.Env, req.IDs)
		if err != nil {
			s.logger.Error("bulk environment failed", "error", err)
			httpx.Error(w, r, http.StatusInternalServerError, "internal", "Bulk update failed.", nil)
			return
		}
		s.auditChange(r, user, "asset", "", "", "bulk_environment_changed",
			map[string]any{"environment": req.Env, "count": n, "ids": req.IDs})
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "affected": n})

	default:
		httpx.Error(w, r, http.StatusUnprocessableEntity, "invalid_action",
			"Supported actions: delete, set_status, set_environment.", nil)
	}
}
