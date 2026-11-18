// Package api wires the HTTP transport: routing, middleware, and JSON handlers.
package api

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aancw/lanvory/internal/auth"
	"github.com/aancw/lanvory/internal/config"
	"github.com/aancw/lanvory/internal/httpx"
	"github.com/aancw/lanvory/internal/integrations"
	"github.com/aancw/lanvory/internal/scheduler"
	"github.com/aancw/lanvory/internal/secrets"
	"github.com/aancw/lanvory/internal/store"
	"github.com/aancw/lanvory/internal/web"
)

// Version is stamped by release builds via -ldflags -X; "dev" otherwise.
// Surfaced in the health endpoints and the startup log.
var Version = "dev"

// Server carries the dependencies handlers need.
type Server struct {
	cfg             config.Config
	logger          *slog.Logger
	pool            *pgxpool.Pool
	auth            *auth.Service
	users           *store.Users
	sessions        *store.Sessions
	settings        *store.Settings
	audit           *store.AuditEvents
	stats           *store.Stats
	search          *store.Search
	assets          *store.Assets
	interfaces      *store.Interfaces
	relationships   *store.Relationships
	locations       *store.Locations
	tags            *store.Tags
	subnets         *store.Subnets
	ipAddresses     *store.IPAddresses
	vlans           *store.Vlans
	networkContexts *store.NetworkContexts
	services        *store.Services
	importer        *store.Importer
	integrations    *store.Integrations
	tokens          *store.APITokens
	domains         *store.Domains
	// dataRouter exposes the authenticated data APIs without middleware;
	// MCP tools dispatch through it after bearer auth resolves the caller.
	dataRouter   http.Handler
	runner       *integrations.Runner
	box          *secrets.Box
	loginLimiter *httpx.IPRateLimiter
	// failedLoginNotified throttles burst notifications (see
	// notifyFailedLoginBurst); guarded by securityMu.
	securityMu          sync.Mutex
	failedLoginNotified map[string]time.Time
}

// NewServer constructs the API server with its stores.
func NewServer(cfg config.Config, logger *slog.Logger, pool *pgxpool.Pool, authSvc *auth.Service) *Server {
	var box *secrets.Box
	if cfg.SecretKey != "" {
		if b, err := secrets.NewBox(cfg.SecretKey); err == nil {
			box = b
		} else {
			logger.Warn("secret key unusable — integration secrets disabled", "error", err)
		}
	}
	// The runner is always available: syncs without credentials (ICMP, DNS)
	// must work even when no secret key is configured.
	runner := integrations.NewRunner(pool, box)
	return &Server{
		cfg:                 cfg,
		logger:              logger,
		pool:                pool,
		auth:                authSvc,
		users:               store.NewUsers(pool),
		sessions:            store.NewSessions(pool),
		settings:            store.NewSettings(pool),
		audit:               store.NewAuditEvents(pool),
		stats:               store.NewStats(pool),
		search:              store.NewSearch(pool),
		assets:              store.NewAssets(pool),
		interfaces:          store.NewInterfaces(pool),
		relationships:       store.NewRelationships(pool),
		locations:           store.NewLocations(pool),
		tags:                store.NewTags(pool),
		subnets:             store.NewSubnets(pool),
		ipAddresses:         store.NewIPAddresses(pool),
		vlans:               store.NewVlans(pool),
		networkContexts:     store.NewNetworkContexts(pool),
		services:            store.NewServices(pool),
		importer:            store.NewImporter(pool),
		integrations:        store.NewIntegrations(pool),
		tokens:              store.NewAPITokens(pool),
		domains:             store.NewDomains(pool),
		runner:              runner,
		box:                 box,
		loginLimiter:        httpx.NewIPRateLimiter(12*time.Second, 8),
		failedLoginNotified: map[string]time.Time{},
	}
}

// StartScheduler runs the background scheduler (per-connector auto-syncs,
// later health checks) until ctx is canceled. Call from main in a goroutine.
func (s *Server) StartScheduler(ctx context.Context) {
	scheduler.New(s.pool, s.runner, s.logger).Run(ctx)
}

// Routes builds the chi router for the API.
func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()

	r.Use(httpx.RequestIDMiddleware)
	r.Use(httpx.SecurityHeaders)
	r.Use(httpx.Recoverer(s.logger))
	r.Use(httpx.RequestLogger(s.logger))

	// Liveness probes (no version prefix, no auth).
	r.Get("/healthz", s.handleHealth)
	r.Get("/readyz", s.handleHealth)

	r.Route("/api/v1", func(r chi.Router) {
		// Double-submit CSRF enforcement for everything under /api/v1.
		r.Use(httpx.CSRF)

		r.Get("/health", s.handleHealth)
		r.Get("/csrf", httpx.CSRFFetch(s.cfg.CookieSecure))

		// First-run setup.
		r.Get("/setup/status", s.handleSetupStatus)
		r.With(s.limitLogin).Post("/setup/admin", s.handleSetupAdmin)

		// Authentication.
		r.With(s.limitLogin).Post("/auth/login", s.handleLogin)
		r.With(s.requireAuth).Post("/auth/logout", s.handleLogout)
		r.With(s.requireAuth).Get("/auth/me", s.handleMe)
		r.With(s.requireAuth).Post("/auth/change-password", s.handleChangePassword)

		// Two-factor authentication (TOTP).
		r.With(s.requireAuth).Post("/auth/2fa/setup", s.handleTOTPSetup)
		r.With(s.requireAuth).Post("/auth/2fa/enable", s.handleTOTPEnable)
		r.With(s.requireAuth).Post("/auth/2fa/disable", s.handleTOTPDisable)

		// Session management for the signed-in operator.
		r.With(s.requireAuth).Get("/auth/sessions", s.handleListSessions)
		r.With(s.requireAuth).Delete("/auth/sessions/others", s.handleRevokeOtherSessions)

		// Authenticated application APIs. Role enforcement (viewer read-only,
		// operator without accounts/settings/tokens) runs after auth resolves
		// the caller; MCP dispatch is admin-equivalent by design.
		r.Group(func(r chi.Router) {
			r.Use(s.requireAuth)
			r.Use(s.enforceRoles)
			s.mountDataRoutes(r)
		})

		// Account management (administrators only).
		r.Route("/users", func(r chi.Router) {
			r.Use(s.requireAuth)
			r.Use(s.requireAdmin)
			r.Get("/", s.handleListUsers)
			r.Post("/", s.handleCreateUser)
			r.Put("/{userID}", s.handleUpdateUser)
			r.Delete("/{userID}", s.handleDeleteUser)
		})

		// API token management (session + CSRF; tokens power bearer auth for
		// machine clients such as MCP agents). Administrator-only: tokens act
		// with their owner's role on the REST API and admin-equivalent on MCP.
		r.Route("/tokens", func(r chi.Router) {
			r.Use(s.requireAuth)
			r.Use(s.requireAdmin)
			r.Get("/", s.handleListTokens)
			r.Post("/", s.handleCreateToken)
			r.Delete("/{tokenID}", s.handleRevokeToken)
		})
	})

	// MCP: streamable HTTP with bearer-token auth (no CSRF — tokens are
	// explicit credentials, not ambient cookies).
	r.Mount("/mcp", s.mcpRouter())

	// Internal no-middleware copy of the data APIs used by MCP tools; the
	// caller is already authenticated when a tool is dispatched.
	dataMux := chi.NewRouter()
	s.mountDataRoutes(dataMux)
	s.dataRouter = dataMux

	// Unknown route. API paths keep JSON errors; everything else goes to the
	// embedded web UI (index.html fallback for client-side routes). GET/HEAD
	// only — other methods on unknown paths have no meaningful page to render.
	webUI := web.Handler()
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		if (r.Method == http.MethodGet || r.Method == http.MethodHead) &&
			!strings.HasPrefix(r.URL.Path, "/api") {
			webUI.ServeHTTP(w, r)
			return
		}
		httpx.Error(w, r, http.StatusNotFound, "not_found", "Resource not found.", nil)
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		httpx.Error(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.", nil)
	})

	return r
}

// mountDataRoutes registers every authenticated data API route relative to
// /api/v1. Shared by the public API (behind requireAuth) and the MCP
// in-process proxy.
func (s *Server) mountDataRoutes(r chi.Router) {
	r.Get("/settings", s.handleGetSettings)
	r.Put("/settings", s.handleUpdateSettings)
	r.Post("/settings/notify-test", s.handleTestNotification)
	r.Get("/dashboard", s.handleDashboard)
	r.Get("/activity", s.handleActivity)
	r.Get("/search", s.handleSearch)

	// Asset inventory (Phase 2).
	r.Route("/assets", func(r chi.Router) {
		r.Get("/", s.handleListAssets)
		r.Post("/", s.handleCreateAsset)
		r.Post("/bulk", s.handleBulkAssets)
		r.Route("/{assetID}", func(r chi.Router) {
			r.Get("/", s.handleGetAsset)
			r.Put("/", s.handleUpdateAsset)
			r.Delete("/", s.handleDeleteAsset)
			r.Post("/interfaces", s.handleCreateInterface)
			r.Post("/relationships", s.handleCreateRelationship)
			r.Post("/check", s.handleCheckAsset)
		})
	})
	r.Route("/interfaces", func(r chi.Router) {
		r.Get("/", s.handleListInterfaces)
		r.Put("/{interfaceID}", s.handleUpdateInterface)
		r.Delete("/{interfaceID}", s.handleDeleteInterface)
	})
	r.Delete("/relationships/{relationshipID}", s.handleDeleteRelationship)

	r.Route("/locations", func(r chi.Router) {
		r.Get("/", s.handleListLocations)
		r.Post("/", s.handleCreateLocation)
		r.Put("/{locationID}", s.handleUpdateLocation)
		r.Delete("/{locationID}", s.handleDeleteLocation)
	})
	r.Get("/asset-types", s.handleListAssetTypes)

	// IPAM (Phase 3).
	r.Route("/subnets", func(r chi.Router) {
		r.Get("/", s.handleListSubnets)
		r.Post("/", s.handleCreateSubnet)
		r.Route("/{subnetID}", func(r chi.Router) {
			r.Get("/", s.handleGetSubnet)
			r.Put("/", s.handleUpdateSubnet)
			r.Delete("/", s.handleDeleteSubnet)
			r.Get("/grid", s.handleAddressGrid)
			r.Get("/next-available", s.handleNextAvailable)
			r.Post("/addresses", s.handleAllocateIP)
			r.Post("/addresses/bulk", s.handleBulkIP)

		})
	})
	r.Route("/ip-addresses", func(r chi.Router) {
		r.Get("/", s.handleListIPs)
		r.Put("/{ipID}", s.handleUpdateIP)
		r.Delete("/{ipID}", s.handleDeleteIP)
		r.Post("/release-bulk", s.handleReleaseIPsBulk)
		r.Post("/delete-bulk", s.handleDeleteIPsBulk)
		r.Post("/{ipID}/release", s.handleReleaseIP)
		r.Get("/{ipID}/history", s.handleIPHistory)
	})
	r.Route("/vlans", func(r chi.Router) {
		r.Get("/", s.handleListVlans)
		r.Post("/", s.handleCreateVlan)
		r.Put("/{vlanID}", s.handleUpdateVlan)
		r.Delete("/{vlanID}", s.handleDeleteVlan)
	})
	r.Route("/network-contexts", func(r chi.Router) {
		r.Get("/", s.handleListNetworkContexts)
		r.Post("/", s.handleCreateNetworkContext)
		r.Put("/{contextID}", s.handleUpdateNetworkContext)
	})

	// Services (Phase 4).
	r.Route("/services", func(r chi.Router) {
		r.Get("/", s.handleListServices)
		r.Post("/", s.handleCreateService)
		r.Route("/{serviceID}", func(r chi.Router) {
			r.Get("/", s.handleGetService)
			r.Put("/", s.handleUpdateService)
			r.Delete("/", s.handleDeleteService)
			r.Post("/endpoints", s.handleCreateServiceEndpoint)
			r.Post("/deployments", s.handleCreateServiceDeployment)
		})
	})
	r.Delete("/service-endpoints/{endpointID}", s.handleDeleteServiceEndpoint)
	r.Put("/service-endpoints/{endpointID}/health", s.handleToggleEndpointHealth)
	r.Delete("/service-deployments/{deploymentID}", s.handleDeleteServiceDeployment)

	// Topology + data portability (Phase 5).
	r.Get("/topology", s.handleTopology)
	r.Patch("/relationships/{relationshipID}", s.handleUpdateRelationship)
	r.Get("/export/inventory.json", s.handleExportInventory)
	r.Get("/export/assets.csv", s.handleExportAssetsCSV)
	r.Get("/export/ip-addresses.csv", s.handleExportIPsCSV)
	r.Post("/import/preview", s.handleImportPreview)
	r.Post("/import/commit", s.handleImportCommit)
	r.Post("/import/restore", s.handleImportRestore)

	// Integrations (Phase 6).
	r.Route("/integrations", func(r chi.Router) {
		r.Get("/", s.handleListIntegrations)
		r.Post("/", s.handleCreateIntegration)
		r.Route("/{integrationID}", func(r chi.Router) {
			r.Put("/", s.handleUpdateIntegration)
			r.Delete("/", s.handleDeleteIntegration)
			r.Post("/sync", s.handleSyncIntegration)
			r.Get("/sync-runs", s.handleListSyncRuns)
		})
	})
	r.Route("/tags", func(r chi.Router) {
		r.Get("/", s.handleListTags)
		r.Post("/", s.handleCreateTag)
		r.Put("/{tagID}", s.handleUpdateTag)
		r.Delete("/{tagID}", s.handleDeleteTag)
	})

	// DNS inventory (Phase 7): domains and their subdomain records.
	r.Route("/domains", func(r chi.Router) {
		r.Get("/", s.handleListDomains)
		r.Post("/", s.handleCreateDomain)
		r.Route("/{domainID}", func(r chi.Router) {
			r.Get("/", s.handleGetDomain)
			r.Put("/", s.handleUpdateDomain)
			r.Delete("/", s.handleDeleteDomain)
			r.Post("/resolve", s.handleResolveDomain)
			r.Post("/subdomains", s.handleCreateSubdomain)
		})
	})
	r.Route("/subdomains", func(r chi.Router) {
		r.Put("/{subdomainID}", s.handleUpdateSubdomain)
		r.Delete("/{subdomainID}", s.handleDeleteSubdomain)
		r.Post("/{subdomainID}/resolve", s.handleResolveSubdomain)
	})
}

func (s *Server) limitLogin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.loginLimiter.Allow(r) {
			httpx.Error(w, r, http.StatusTooManyRequests, "rate_limited",
				"Too many attempts. Try again in a minute.", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// handleHealth verifies process + database liveness.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r, 3*time.Second)
	defer cancel()
	if err := s.pool.Ping(ctx); err != nil {
		httpx.Error(w, r, http.StatusServiceUnavailable, "unhealthy",
			"Database unreachable.", nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{
		"status":   "ok",
		"database": "ok",
		"version":  Version,
	})
}
