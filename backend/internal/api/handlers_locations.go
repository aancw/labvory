package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/aancw/labvory/internal/httpx"
	"github.com/aancw/labvory/internal/store"
)

// ---------- locations ----------

type locationPayload struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	ParentID    *string `json:"parent_id"`
}

func validateLocationPayload(p *locationPayload) *httpxError {
	if len([]rune(p.Name)) < 1 || len([]rune(p.Name)) > 100 {
		return errInvalid("invalid_name", "Location name must be 1-100 characters.")
	}
	if p.ParentID != nil && *p.ParentID != "" && !isUUID(*p.ParentID) {
		return errInvalid("invalid_parent", "Invalid parent location reference.")
	}
	return nil
}

func (s *Server) handleListLocations(w http.ResponseWriter, r *http.Request) {
	items, err := s.locations.List(r.Context())
	if err != nil {
		s.logger.Error("list locations failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not load locations.", nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"locations": items})
}

func (s *Server) handleCreateLocation(w http.ResponseWriter, r *http.Request) {
	var p locationPayload
	if err := httpx.DecodeJSON(r, &p); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if herr := validateLocationPayload(&p); herr != nil {
		herr.write(reqCtx{w, r, currentUser(r)})
		return
	}

	l, err := s.locations.Create(r.Context(), p.Name, p.Description, p.ParentID)
	switch {
	case errors.Is(err, store.ErrConflict):
		httpx.Error(w, r, http.StatusConflict, "duplicate_location",
			"A location with this name already exists.", nil)
		return
	case err != nil:
		s.logger.Error("create location failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not create location.", nil)
		return
	}
	s.auditChange(r, currentUser(r), "location", l.ID, l.Name, "created", map[string]any{"name": l.Name})
	httpx.WriteJSON(w, http.StatusCreated, l)
}

func (s *Server) handleUpdateLocation(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "locationID")
	if !isUUID(id) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_id", "Invalid location id.", nil)
		return
	}
	var p locationPayload
	if err := httpx.DecodeJSON(r, &p); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if herr := validateLocationPayload(&p); herr != nil {
		herr.write(reqCtx{w, r, currentUser(r)})
		return
	}
	if p.ParentID != nil && *p.ParentID == id {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "invalid_parent",
			"A location cannot be its own parent.", nil)
		return
	}

	l, err := s.locations.Update(r.Context(), id, p.Name, p.Description, p.ParentID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		httpx.Error(w, r, http.StatusNotFound, "not_found", "Location not found.", nil)
		return
	case errors.Is(err, store.ErrCycle):
		httpx.Error(w, r, http.StatusConflict, "location_cycle",
			"This parent assignment would create a circular hierarchy.", nil)
		return
	case errors.Is(err, store.ErrConflict):
		httpx.Error(w, r, http.StatusConflict, "duplicate_location",
			"A location with this name already exists.", nil)
		return
	case err != nil:
		s.logger.Error("update location failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not update location.", nil)
		return
	}
	s.auditChange(r, currentUser(r), "location", id, l.Name, "updated", map[string]any{"name": l.Name})
	httpx.WriteJSON(w, http.StatusOK, l)
}

func (s *Server) handleDeleteLocation(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "locationID")
	if !isUUID(id) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_id", "Invalid location id.", nil)
		return
	}
	l, err := s.locations.Get(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", "Location not found.", nil)
		return
	}
	if err != nil {
		s.logger.Error("location delete: load failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not delete location.", nil)
		return
	}

	err = s.locations.Delete(r.Context(), id)
	switch {
	case errors.Is(err, store.ErrHasChildren):
		httpx.Error(w, r, http.StatusConflict, "has_dependents",
			"This location still has child locations or assigned assets.", nil)
		return
	case err != nil:
		s.logger.Error("delete location failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not delete location.", nil)
		return
	}
	s.auditChange(r, currentUser(r), "location", id, l.Name, "deleted", map[string]any{"name": l.Name})
	httpx.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---------- asset types ----------

// handleListAssetTypes returns the reference catalogue used by forms and filters.
func (s *Server) handleListAssetTypes(w http.ResponseWriter, r *http.Request) {
	items, err := s.locations.ListAssetTypes(r.Context())
	if err != nil {
		s.logger.Error("list asset types failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not load asset types.", nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"types": items})
}

// ---------- tags ----------

type tagPayload struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

func validateTagPayload(p *tagPayload) *httpxError {
	p.Name = strings.TrimSpace(p.Name)
	if len(p.Name) < 1 || len(p.Name) > 40 {
		return errInvalid("invalid_name", "Tag name must be 1-40 characters.")
	}
	if p.Color == "" {
		p.Color = "zinc"
	}
	if !store.TagColors[p.Color] {
		return errInvalid("invalid_color", "Unsupported tag color.")
	}
	return nil
}

func (s *Server) handleListTags(w http.ResponseWriter, r *http.Request) {
	items, err := s.tags.List(r.Context())
	if err != nil {
		s.logger.Error("list tags failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not load tags.", nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"tags": items})
}

func (s *Server) handleCreateTag(w http.ResponseWriter, r *http.Request) {
	var p tagPayload
	if err := httpx.DecodeJSON(r, &p); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if herr := validateTagPayload(&p); herr != nil {
		herr.write(reqCtx{w, r, currentUser(r)})
		return
	}
	t, err := s.tags.Create(r.Context(), p.Name, p.Color)
	if errors.Is(err, store.ErrConflict) {
		httpx.Error(w, r, http.StatusConflict, "duplicate_tag", "A tag with this name already exists.", nil)
		return
	}
	if err != nil {
		s.logger.Error("create tag failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not create tag.", nil)
		return
	}
	s.auditChange(r, currentUser(r), "tag", t.ID, t.Name, "created", map[string]any{"name": t.Name})
	httpx.WriteJSON(w, http.StatusCreated, t)
}

func (s *Server) handleUpdateTag(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "tagID")
	if !isUUID(id) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_id", "Invalid tag id.", nil)
		return
	}
	var p tagPayload
	if err := httpx.DecodeJSON(r, &p); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if herr := validateTagPayload(&p); herr != nil {
		herr.write(reqCtx{w, r, currentUser(r)})
		return
	}
	t, err := s.tags.Update(r.Context(), id, p.Name, p.Color)
	switch {
	case errors.Is(err, store.ErrNotFound):
		httpx.Error(w, r, http.StatusNotFound, "not_found", "Tag not found.", nil)
		return
	case errors.Is(err, store.ErrConflict):
		httpx.Error(w, r, http.StatusConflict, "duplicate_tag", "A tag with this name already exists.", nil)
		return
	case err != nil:
		s.logger.Error("update tag failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not update tag.", nil)
		return
	}
	s.auditChange(r, currentUser(r), "tag", id, t.Name, "updated", map[string]any{"name": t.Name})
	httpx.WriteJSON(w, http.StatusOK, t)
}

func (s *Server) handleDeleteTag(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "tagID")
	if !isUUID(id) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_id", "Invalid tag id.", nil)
		return
	}
	err := s.tags.Delete(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", "Tag not found.", nil)
		return
	}
	if err != nil {
		s.logger.Error("delete tag failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not delete tag.", nil)
		return
	}
	s.auditChange(r, currentUser(r), "tag", id, "", "deleted", nil)
	httpx.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
