package api

import (
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/aancw/labvory/internal/httpx"
	"github.com/aancw/labvory/internal/store"
)

var serviceSlugRe = regexp.MustCompile(`[^a-z0-9]+`)

func serviceSlugify(name string) string {
	s := serviceSlugRe.ReplaceAllString(strings.ToLower(strings.TrimSpace(name)), "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "service"
	}
	if len(s) > 60 {
		s = s[:60]
	}
	return s
}

var serviceCategories = setOf(
	"media", "network", "monitoring", "home-automation", "storage", "development",
	"security", "productivity", "database", "proxy", "other",
)
var deploymentTypes = setOf("native", "docker", "vm", "lxc", "kubernetes")

// ---------- service CRUD ----------

type servicePayload struct {
	Name           string `json:"name"`
	Description    string `json:"description"`
	Category       string `json:"category"`
	DeploymentType string `json:"deployment_type"`
	Environment    string `json:"environment"`
	Status         string `json:"status"`
	RepoURL        string `json:"repo_url"`
	DocsURL        string `json:"docs_url"`
	Notes          string `json:"notes"`
}

func validateServicePayload(p *servicePayload) *httpxError {
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" || len(p.Name) > 80 {
		return errInvalid("invalid_name", "Service name must be 1-80 characters.")
	}
	if p.Category == "" {
		p.Category = "other"
	}
	if !serviceCategories[p.Category] {
		return errInvalid("invalid_category", "Unknown service category.")
	}
	if p.DeploymentType == "" {
		p.DeploymentType = "native"
	}
	if !deploymentTypes[p.DeploymentType] {
		return errInvalid("invalid_deployment_type", "Deployment type must be native, docker, vm, lxc or kubernetes.")
	}
	if p.Environment == "" {
		p.Environment = "production"
	}
	if !environments[p.Environment] {
		return errInvalid("invalid_environment", "Environment must be production, development, testing or lab.")
	}
	if p.Status == "" {
		p.Status = "unknown"
	}
	if !assetStatuses[p.Status] {
		return errInvalid("invalid_status", "Status must be online, offline, maintenance, retired or unknown.")
	}
	return nil
}

func payloadToService(p servicePayload) store.Service {
	return store.Service{
		Name: p.Name, Slug: serviceSlugify(p.Name), Description: p.Description,
		Category: p.Category, DeploymentType: p.DeploymentType, Environment: p.Environment,
		Status: p.Status, RepoURL: p.RepoURL, DocsURL: p.DocsURL, Notes: p.Notes,
	}
}

func (s *Server) handleListServices(w http.ResponseWriter, r *http.Request) {
	items, err := s.services.List(r.Context(), r.URL.Query().Get("q"), r.URL.Query().Get("category"),
		r.URL.Query().Get("environment"), r.URL.Query().Get("deployment_type"))
	if err != nil {
		s.logger.Error("list services failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not load services.", nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"services": items})
}

func (s *Server) handleCreateService(w http.ResponseWriter, r *http.Request) {
	var p servicePayload
	if err := httpx.DecodeJSON(r, &p); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if herr := validateServicePayload(&p); herr != nil {
		herr.write(reqCtx{w, r, currentUser(r)})
		return
	}
	v := payloadToService(p)
	if err := s.services.Create(r.Context(), &v); errors.Is(err, store.ErrConflict) {
		httpx.Error(w, r, http.StatusConflict, "duplicate_service", "A service with this name already exists.", nil)
		return
	} else if err != nil {
		s.logger.Error("create service failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not create service.", nil)
		return
	}
	s.auditChange(r, currentUser(r), "service", v.ID, v.Name, "created", map[string]any{"name": v.Name})
	s.writeServiceDetail(w, r, v.ID, http.StatusCreated)
}

func (s *Server) handleGetService(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "serviceID")
	if !isUUID(id) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_id", "Invalid service id.", nil)
		return
	}
	s.writeServiceDetail(w, r, id, http.StatusOK)
}

func (s *Server) writeServiceDetail(w http.ResponseWriter, r *http.Request, id string, status int) {
	ctx := r.Context()
	v, err := s.services.Get(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", "Service not found.", nil)
		return
	}
	if err != nil {
		s.logger.Error("get service failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not load service.", nil)
		return
	}
	endpoints, err := s.services.ListEndpoints(ctx, id)
	if err != nil {
		s.logger.Error("list endpoints failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not load endpoints.", nil)
		return
	}
	deployments, err := s.services.ListDeployments(ctx, id)
	if err != nil {
		s.logger.Error("list deployments failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not load deployments.", nil)
		return
	}
	activity, err := s.audit.ListForEntity(ctx, "service", id, 15)
	if err != nil {
		s.logger.Error("service activity failed", "error", err)
	}
	uptime, err := s.services.ServiceUptime(ctx, id)
	if err != nil {
		s.logger.Error("service uptime failed", "error", err)
		uptime = map[string]store.EndpointUptime{}
	}
	httpx.WriteJSON(w, status, map[string]any{
		"service": v, "endpoints": endpoints, "deployments": deployments, "activity": activity,
		"uptime": uptime,
	})
}

func (s *Server) handleUpdateService(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "serviceID")
	if !isUUID(id) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_id", "Invalid service id.", nil)
		return
	}
	var p servicePayload
	if err := httpx.DecodeJSON(r, &p); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if herr := validateServicePayload(&p); herr != nil {
		herr.write(reqCtx{w, r, currentUser(r)})
		return
	}
	before, err := s.services.Get(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", "Service not found.", nil)
		return
	}
	if err != nil {
		s.logger.Error("service update: load failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not update service.", nil)
		return
	}
	v := payloadToService(p)
	v.ID = id
	if err := s.services.Update(r.Context(), &v); err != nil {
		s.logger.Error("update service failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not update service.", nil)
		return
	}
	changes := map[string]any{}
	if before.Status != v.Status {
		changes["status"] = map[string]any{"from": before.Status, "to": v.Status}
	}
	if before.DeploymentType != v.DeploymentType {
		changes["deployment_type"] = map[string]any{"from": before.DeploymentType, "to": v.DeploymentType}
	}
	s.auditChange(r, currentUser(r), "service", id, v.Name, "updated", changes)
	s.writeServiceDetail(w, r, id, http.StatusOK)
}

func (s *Server) handleDeleteService(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "serviceID")
	if !isUUID(id) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_id", "Invalid service id.", nil)
		return
	}
	v, err := s.services.Get(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", "Service not found.", nil)
		return
	}
	if err != nil {
		s.logger.Error("service delete: load failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not delete service.", nil)
		return
	}
	if err := s.services.Delete(r.Context(), id); err != nil {
		s.logger.Error("delete service failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not delete service.", nil)
		return
	}
	s.auditChange(r, currentUser(r), "service", id, v.Name, "deleted", nil)
	httpx.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---------- endpoints ----------

type endpointPayload struct {
	Label         string `json:"label"`
	Protocol      string `json:"protocol"`
	Port          *int   `json:"port"`
	URL           string `json:"url"`
	IsExternal    bool   `json:"is_external"`
	HealthEnabled bool   `json:"health_enabled"`
}

func validateEndpointPayload(p *endpointPayload) *httpxError {
	if p.Protocol == "" {
		p.Protocol = "http"
	}
	if !setOf("http", "https", "tcp", "udp")[p.Protocol] {
		return errInvalid("invalid_protocol", "Protocol must be http, https, tcp or udp.")
	}
	if p.Port != nil && (*p.Port < 1 || *p.Port > 65535) {
		return errInvalid("invalid_port", "Port must be between 1 and 65535.")
	}
	if p.HealthEnabled && !setOf("http", "https")[p.Protocol] {
		return errInvalid("invalid_protocol", "Health checks need an http or https endpoint with a URL.")
	}
	return nil
}

func (s *Server) handleCreateServiceEndpoint(w http.ResponseWriter, r *http.Request) {
	serviceID := chi.URLParam(r, "serviceID")
	if !isUUID(serviceID) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_id", "Invalid service id.", nil)
		return
	}
	var p endpointPayload
	if err := httpx.DecodeJSON(r, &p); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if herr := validateEndpointPayload(&p); herr != nil {
		herr.write(reqCtx{w, r, currentUser(r)})
		return
	}
	e := store.ServiceEndpoint{
		ServiceID: serviceID, Label: p.Label, Protocol: p.Protocol,
		Port: p.Port, URL: p.URL, IsExternal: p.IsExternal, HealthEnabled: p.HealthEnabled,
	}
	if err := s.services.CreateEndpoint(r.Context(), &e); err != nil {
		s.logger.Error("create endpoint failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not add endpoint.", nil)
		return
	}
	s.auditChange(r, currentUser(r), "service_endpoint", e.ID, e.Label, "created",
		map[string]any{"service_id": serviceID})
	httpx.WriteJSON(w, http.StatusCreated, e)
}

func (s *Server) handleDeleteServiceEndpoint(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "endpointID")
	if !isUUID(id) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_id", "Invalid endpoint id.", nil)
		return
	}
	if err := s.services.DeleteEndpoint(r.Context(), id); err != nil {
		s.logger.Error("delete endpoint failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not remove endpoint.", nil)
		return
	}
	s.auditChange(r, currentUser(r), "service_endpoint", id, "", "deleted", nil)
	httpx.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleToggleEndpointHealth flips the scheduler probe for one endpoint.
func (s *Server) handleToggleEndpointHealth(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "endpointID")
	if !isUUID(id) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_id", "Invalid endpoint id.", nil)
		return
	}
	var p struct {
		Enabled bool `json:"enabled"`
	}
	if err := httpx.DecodeJSON(r, &p); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	health, err := s.services.Endpoint(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", "Endpoint not found.", nil)
		return
	}
	if err != nil {
		s.logger.Error("endpoint health toggle: load failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not update endpoint.", nil)
		return
	}
	if p.Enabled && !setOf("http", "https")[health.Protocol] {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "invalid_protocol",
			"Health checks need an http or https endpoint with a URL.", nil)
		return
	}
	if err := s.services.SetEndpointHealth(r.Context(), id, p.Enabled); err != nil {
		s.logger.Error("endpoint health toggle failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not update endpoint.", nil)
		return
	}
	action := "health_disabled"
	if p.Enabled {
		action = "health_enabled"
	}
	s.auditChange(r, currentUser(r), "service_endpoint", id, health.Label, action,
		map[string]any{"url": health.URL})
	httpx.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true, "health_enabled": p.Enabled})
}

// handleSetEndpointMaintenance declares (or clears) a planned maintenance
// window for one endpoint. While the window is in the future the scheduler
// keeps probing but suppresses down/recovered notifications and the dashboard
// "services down" alert excludes the endpoint.
func (s *Server) handleSetEndpointMaintenance(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "endpointID")
	if !isUUID(id) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_id", "Invalid endpoint id.", nil)
		return
	}
	var p struct {
		Until *string `json:"until"`
	}
	if err := httpx.DecodeJSON(r, &p); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}

	endpoint, err := s.services.Endpoint(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", "Endpoint not found.", nil)
		return
	}
	if err != nil {
		s.logger.Error("endpoint maintenance: load failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not update endpoint.", nil)
		return
	}

	var until *time.Time
	if p.Until != nil && *p.Until != "" {
		parsed, perr := time.Parse(time.RFC3339, strings.TrimSpace(*p.Until))
		if perr != nil {
			httpx.Error(w, r, http.StatusUnprocessableEntity, "invalid_time",
				"until must be an RFC 3339 timestamp or null.", nil)
			return
		}
		if !parsed.After(time.Now()) {
			httpx.Error(w, r, http.StatusUnprocessableEntity, "invalid_time",
				"The maintenance window must end in the future.", nil)
			return
		}
		until = &parsed
	}

	if err := s.services.SetEndpointMaintenance(r.Context(), id, until); err != nil {
		s.logger.Error("endpoint maintenance update failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not update endpoint.", nil)
		return
	}

	label := endpoint.Label
	if label == "" {
		label = endpoint.URL
	}
	action := "maintenance_set"
	changes := map[string]any{"maintenance_until": map[string]any{"to": until}}
	if until == nil {
		action = "maintenance_cleared"
	}
	s.auditChange(r, currentUser(r), "service_endpoint", id, label, action, changes)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "maintenance_until": until})
}

// ---------- deployments ----------

type deploymentPayload struct {
	AssetID        string `json:"asset_id"`
	DeploymentType string `json:"deployment_type"`
	Notes          string `json:"notes"`
}

func (s *Server) handleCreateServiceDeployment(w http.ResponseWriter, r *http.Request) {
	serviceID := chi.URLParam(r, "serviceID")
	if !isUUID(serviceID) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_id", "Invalid service id.", nil)
		return
	}
	var p deploymentPayload
	if err := httpx.DecodeJSON(r, &p); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if !isUUID(p.AssetID) {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "invalid_asset", "A valid host asset is required.", nil)
		return
	}
	asset, err := s.assets.GetByID(r.Context(), p.AssetID)
	if errors.Is(err, store.ErrNotFound) {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "unknown_asset", "Host asset does not exist.", nil)
		return
	}
	if err != nil {
		s.logger.Error("deployment create: asset load failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not add deployment.", nil)
		return
	}
	if p.DeploymentType == "" {
		p.DeploymentType = "native"
	}
	if !deploymentTypes[p.DeploymentType] {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "invalid_deployment_type",
			"Deployment type must be native, docker, vm, lxc or kubernetes.", nil)
		return
	}

	d := store.ServiceDeployment{
		ServiceID: serviceID, AssetID: p.AssetID, DeploymentType: p.DeploymentType, Notes: p.Notes,
	}
	if err := s.services.CreateDeployment(r.Context(), &d); errors.Is(err, store.ErrConflict) {
		httpx.Error(w, r, http.StatusConflict, "duplicate_deployment",
			"This service is already deployed on the selected host.", nil)
		return
	} else if err != nil {
		s.logger.Error("create deployment failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not add deployment.", nil)
		return
	}
	d.AssetName, d.AssetHostname, d.AssetCategory = asset.Name, asset.Hostname, asset.Category
	s.auditChange(r, currentUser(r), "service_deployment", d.ID, asset.Name, "created",
		map[string]any{"service_id": serviceID, "asset_id": p.AssetID})
	httpx.WriteJSON(w, http.StatusCreated, d)
}

func (s *Server) handleDeleteServiceDeployment(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "deploymentID")
	if !isUUID(id) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_id", "Invalid deployment id.", nil)
		return
	}
	if err := s.services.DeleteDeployment(r.Context(), id); err != nil {
		s.logger.Error("delete deployment failed", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal", "Could not remove deployment.", nil)
		return
	}
	s.auditChange(r, currentUser(r), "service_deployment", id, "", "deleted", nil)
	httpx.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
