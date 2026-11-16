package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/aancw/lanvory/internal/httpx"
	"github.com/aancw/lanvory/internal/store"
)

// diffMap builds a {field: {from, to}} audit payload for changed scalar fields.
func diffMap(before, after store.Asset) map[string]any {
	type pair struct {
		from any
		to   any
	}
	fields := map[string]pair{
		"name":          {before.Name, after.Name},
		"hostname":      {before.Hostname, after.Hostname},
		"status":        {before.Status, after.Status},
		"lifecycle":     {before.Lifecycle, after.Lifecycle},
		"environment":   {before.Environment, after.Environment},
		"location":      {before.LocationName, after.LocationName},
		"parent":        {before.ParentName, after.ParentName},
		"manufacturer":  {before.Manufacturer, after.Manufacturer},
		"model":         {before.Model, after.Model},
		"serial_number": {before.SerialNumber, after.SerialNumber},
		"os_name":       {before.OSName, after.OSName},
		"os_version":    {before.OSVersion, after.OSVersion},
		"cpu_model":     {before.CPUModel, after.CPUModel},
		"notes":         {before.Notes, after.Notes},
	}
	num := func(a, b *float64) (any, any) {
		if a == nil && b == nil {
			return nil, nil
		}
		if a == nil {
			return nil, *b
		}
		if b == nil {
			return *a, nil
		}
		return *a, *b
	}
	ints := func(a, b *int) (any, any) {
		if a == nil && b == nil {
			return nil, nil
		}
		if a == nil {
			return nil, *b
		}
		if b == nil {
			return *a, nil
		}
		return *a, *b
	}

	out := map[string]any{}
	for name, p := range fields {
		if !scalarEqual(p.from, p.to) {
			out[name] = map[string]any{"from": p.from, "to": p.to}
		}
	}
	if fa, ta := num(before.RamGB, after.RamGB); !scalarEqual(fa, ta) {
		out["ram_gb"] = map[string]any{"from": fa, "to": ta}
	}
	if fa, ta := num(before.StorageGB, after.StorageGB); !scalarEqual(fa, ta) {
		out["storage_gb"] = map[string]any{"from": fa, "to": ta}
	}
	if fa, ta := ints(before.CPUCores, after.CPUCores); !scalarEqual(fa, ta) {
		out["cpu_cores"] = map[string]any{"from": fa, "to": ta}
	}
	if !scalarEqual(before.SerialNumber, after.SerialNumber) {
		// already covered above; kept for clarity of intent
	}
	return out
}

func scalarEqual(a, b any) bool {
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return string(ab) == string(bb)
}

// ---------- network interfaces ----------

type interfacePayload struct {
	Name       string  `json:"name"`
	Kind       string  `json:"kind"`
	MACAddress *string `json:"mac_address"`
	MTU        *int    `json:"mtu"`
	SpeedMbps  *int    `json:"speed_mbps"`
	Notes      string  `json:"notes"`
}

func (s *Server) validateInterfacePayload(p *interfacePayload) *httpxError {
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" || len(p.Name) > 64 {
		return errInvalid("invalid_name", "Interface name is required (max 64 characters).")
	}
	if p.Kind == "" {
		p.Kind = "ethernet"
	}
	if !interfaceKinds[p.Kind] {
		return errInvalid("invalid_kind", "Interface kind must be: ethernet, wifi, vlan, bond, loopback or other.")
	}
	if p.MACAddress != nil && *p.MACAddress != "" && !macRe.MatchString(*p.MACAddress) {
		return errInvalid("invalid_mac", "MAC address must use AA:BB:CC:DD:EE:FF (or dashes).")
	} else if p.MACAddress != nil && *p.MACAddress == "" {
		p.MACAddress = nil
	}
	if p.MTU != nil && (*p.MTU < 68 || *p.MTU > 65535) {
		return errInvalid("invalid_mtu", "MTU must be between 68 and 65535.")
	}
	if p.SpeedMbps != nil && (*p.SpeedMbps < 0 || *p.SpeedMbps > 1_000_000) {
		return errInvalid("invalid_speed", "Speed must be between 0 and 1,000,000 Mbps.")
	}
	return nil
}

func (s *Server) handleCreateInterface(w http.ResponseWriter, r *http.Request) {
	assetID := chi.URLParam(r, "assetID")
	if !isUUID(assetID) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_id", "Invalid asset id.", nil)
		return
	}
	var p interfacePayload
	if err := httpx.DecodeJSON(r, &p); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if herr := s.validateInterfacePayload(&p); herr != nil {
		herr.write(reqCtx{w, r, currentUser(r)})
		return
	}

	// Asset existence check.
	if _, err := s.assets.GetByID(r.Context(), assetID); errors.Is(err, store.ErrNotFound) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", "Asset not found.", nil)
		return
	} else if err != nil {
		s.logger.Error("interface create: asset load failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not create interface.", nil)
		return
	}

	ni := store.NetworkInterface{
		AssetID: assetID, Name: p.Name, Kind: p.Kind,
		MACAddress: p.MACAddress, MTU: p.MTU, SpeedMbps: p.SpeedMbps, Notes: p.Notes,
	}
	if err := s.interfaces.Create(r.Context(), &ni); err != nil {
		if errors.Is(err, store.ErrConflict) {
			httpx.Error(w, r, http.StatusConflict, "duplicate_interface",
				"An interface with this name already exists on the asset.", nil)
			return
		}
		s.logger.Error("create interface failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not create interface.", nil)
		return
	}
	s.auditChange(r, currentUser(r), "interface", ni.ID, ni.Name, "created",
		map[string]any{"asset_id": assetID, "name": ni.Name, "kind": ni.Kind})
	httpx.WriteJSON(w, http.StatusCreated, ni)
}

func (s *Server) handleUpdateInterface(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "interfaceID")
	if !isUUID(id) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_id", "Invalid interface id.", nil)
		return
	}
	var p interfacePayload
	if err := httpx.DecodeJSON(r, &p); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if herr := s.validateInterfacePayload(&p); herr != nil {
		herr.write(reqCtx{w, r, currentUser(r)})
		return
	}

	ni := store.NetworkInterface{
		ID: id, Name: p.Name, Kind: p.Kind,
		MACAddress: p.MACAddress, MTU: p.MTU, SpeedMbps: p.SpeedMbps, Notes: p.Notes,
	}
	if err := s.interfaces.Update(r.Context(), &ni); errors.Is(err, store.ErrNotFound) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", "Interface not found.", nil)
		return
	} else if err != nil {
		if errors.Is(err, store.ErrConflict) {
			httpx.Error(w, r, http.StatusConflict, "duplicate_interface",
				"An interface with this name already exists on the asset.", nil)
			return
		}
		s.logger.Error("update interface failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not update interface.", nil)
		return
	}
	s.auditChange(r, currentUser(r), "interface", id, ni.Name, "updated",
		map[string]any{"name": ni.Name, "kind": ni.Kind})
	httpx.WriteJSON(w, http.StatusOK, ni)
}

func (s *Server) handleDeleteInterface(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "interfaceID")
	if !isUUID(id) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_id", "Invalid interface id.", nil)
		return
	}
	if err := s.interfaces.Delete(r.Context(), id); err != nil {
		s.logger.Error("delete interface failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not delete interface.", nil)
		return
	}
	s.auditChange(r, currentUser(r), "interface", id, "", "deleted", nil)
	httpx.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleListInterfaces(w http.ResponseWriter, r *http.Request) {
	limit := clampInt(parseQueryInt(r, "limit", 100), 1, 300)
	offset := clampInt(parseQueryInt(r, "offset", 0), 0, 100000)
	items, total, err := s.interfaces.ListAll(r.Context(), limit, offset)
	if err != nil {
		s.logger.Error("list interfaces failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not load interfaces.", nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"interfaces": items, "total": total, "limit": limit, "offset": offset,
	})
}

// ---------- relationships ----------

type relationshipPayload struct {
	TargetID          string `json:"target_asset_id"`
	Kind              string `json:"kind"`
	Note              string `json:"note"`
	SourceInterfaceID string `json:"source_interface_id"`
	TargetInterfaceID string `json:"target_interface_id"`
	SourcePort        string `json:"source_port"`
	TargetPort        string `json:"target_port"`
}

func (s *Server) handleCreateRelationship(w http.ResponseWriter, r *http.Request) {
	sourceID := chi.URLParam(r, "assetID")
	if !isUUID(sourceID) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_id", "Invalid asset id.", nil)
		return
	}
	var p relationshipPayload
	if err := httpx.DecodeJSON(r, &p); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if !relationshipKinds[p.Kind] {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "invalid_kind",
			"Relationship kind must be: runs_on, connected_to or depends_on.", nil)
		return
	}
	if !isUUID(p.TargetID) {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "invalid_target",
			"A valid target asset is required.", nil)
		return
	}
	if p.TargetID == sourceID {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "invalid_target",
			"An asset cannot relate to itself.", nil)
		return
	}

	ctx := r.Context()
	target, err := s.assets.GetByID(ctx, p.TargetID)
	if errors.Is(err, store.ErrNotFound) {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "unknown_target",
			"Target asset does not exist.", nil)
		return
	}
	if err != nil {
		s.logger.Error("relationship create: target load failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not create relationship.", nil)
		return
	}

	rel, err := s.relationships.Create(ctx, sourceID, p.TargetID, p.Kind, store.RelationshipMeta{
		Note:              p.Note,
		SourceInterfaceID: p.SourceInterfaceID,
		TargetInterfaceID: p.TargetInterfaceID,
		SourcePort:        p.SourcePort,
		TargetPort:        p.TargetPort,
	})
	switch {
	case errors.Is(err, store.ErrCycle):
		httpx.Error(w, r, http.StatusConflict, "relationship_cycle",
			"This "+p.Kind+" edge would create a circular dependency.", nil)
		return
	case errors.Is(err, store.ErrConflict):
		httpx.Error(w, r, http.StatusConflict, "duplicate_relationship",
			"An identical relationship already exists.", nil)
		return
	case err != nil:
		s.logger.Error("create relationship failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not create relationship.", nil)
		return
	}

	rel.Other = store.AssetStub{
		ID: target.ID, Name: target.Name, Hostname: target.Hostname,
		TypeLabel: target.TypeLabel, Category: target.Category, Status: target.Status,
	}
	s.auditChange(r, currentUser(r), "relationship", rel.ID, p.Kind, "created",
		map[string]any{"source": sourceID, "target": p.TargetID, "kind": p.Kind})
	httpx.WriteJSON(w, http.StatusCreated, rel)
}

func (s *Server) handleDeleteRelationship(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "relationshipID")
	if !isUUID(id) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_id", "Invalid relationship id.", nil)
		return
	}
	rel, err := s.relationships.GetByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", "Relationship not found.", nil)
		return
	}
	if err != nil {
		s.logger.Error("relationship delete: load failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not delete relationship.", nil)
		return
	}
	if err := s.relationships.Delete(r.Context(), id); err != nil {
		s.logger.Error("delete relationship failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not delete relationship.", nil)
		return
	}
	s.auditChange(r, currentUser(r), "relationship", id, rel.Kind, "deleted",
		map[string]any{"source": rel.SourceID, "target": rel.TargetID, "kind": rel.Kind})
	httpx.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleUpdateRelationship edits note and connection metadata (topology editing).
func (s *Server) handleUpdateRelationship(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "relationshipID")
	if !isUUID(id) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_id", "Invalid relationship id.", nil)
		return
	}
	var p relationshipPayload
	if err := httpx.DecodeJSON(r, &p); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	rel, err := s.relationships.GetByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", "Relationship not found.", nil)
		return
	}
	if err != nil {
		s.logger.Error("relationship update: load failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not update relationship.", nil)
		return
	}

	meta := store.RelationshipMeta{
		Note:              p.Note,
		SourceInterfaceID: p.SourceInterfaceID,
		TargetInterfaceID: p.TargetInterfaceID,
		SourcePort:        p.SourcePort,
		TargetPort:        p.TargetPort,
	}
	if err := s.relationships.Update(r.Context(), id, meta); err != nil {
		s.logger.Error("update relationship failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not update relationship.", nil)
		return
	}
	updated, _ := s.relationships.GetByID(r.Context(), id)
	updated.Direction = rel.Direction
	updated.Other = rel.Other
	s.auditChange(r, currentUser(r), "relationship", id, rel.Kind, "updated",
		map[string]any{"source_port": meta.SourcePort, "target_port": meta.TargetPort})
	httpx.WriteJSON(w, http.StatusOK, updated)
}
