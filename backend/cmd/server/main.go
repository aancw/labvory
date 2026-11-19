// Command server runs the LabVory API.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aancw/labvory/internal/api"
	"github.com/aancw/labvory/internal/auth"
	"github.com/aancw/labvory/internal/config"
	"github.com/aancw/labvory/internal/db"
	"github.com/aancw/labvory/internal/secrets"
	"github.com/aancw/labvory/internal/store"
)

func main() {
	// Flags first: -version needs no config, and -config determines which
	// TOML file feeds config.Load().
	configPath := flag.String("config", "", "TOML config file (default: ./"+config.DefaultConfigFile+" if present)")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(api.Version)
		return
	}

	// Load resolves built-in defaults, the TOML config file, and LV_*
	// environment variables (environment wins). An explicitly named file
	// must exist; the default ./labvory.toml is used only when present.
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}
	logger := cfg.Logger()

	if err := db.Migrate(cfg.DatabaseURL); err != nil {
		logger.Error("migrations failed", "error", err)
		os.Exit(1)
	}
	logger.Info("migrations applied")

	ctx := context.Background()
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("database connect failed", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	// Optional demo seed — only with LV_SEED_MODE=dev AND an empty inventory.
	if cfg.SeedMode == "dev" {
		seedCtx, seedCancel := context.WithTimeout(ctx, 30*time.Second)
		if err := db.SeedDevHomelab(seedCtx, pool, logger); err != nil {
			logger.Error("dev seed failed", "error", err)
			os.Exit(1)
		}
		seedCancel()
	}

	// The secrets box is optional: without LV_SECRET_KEY, integration secrets
	// and two-factor enrollment are unavailable.
	var box *secrets.Box
	if cfg.SecretKey != "" {
		if b, err := secrets.NewBox(cfg.SecretKey); err != nil {
			logger.Warn("secret key unusable — secrets and 2FA disabled", "error", err)
		} else {
			box = b
		}
	}

	authSvc := auth.NewService(
		store.NewUsers(pool), store.NewSessions(pool), store.NewAuditEvents(pool),
		logger, cfg.SessionTTL, box,
	)

	apiSrv := api.NewServer(cfg, logger, pool, authSvc)

	// Background scheduler: per-connector auto-syncs and health checks. It
	// stops when the process shuts down; in-flight syncs record their history.
	schedCtx, schedCancel := context.WithCancel(ctx)
	defer schedCancel()
	go apiSrv.StartScheduler(schedCtx)

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           apiSrv.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// Periodically reap expired sessions.
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			if err := store.NewSessions(pool).DeleteExpired(ctx); err != nil {
				logger.Warn("expired session cleanup failed", "error", err)
			}
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	errCh := make(chan error, 1)
	go func() {
		logger.Info("labvory api listening", "addr", cfg.Addr, "env", cfg.Env, "version", api.Version)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server failed", "error", err)
			os.Exit(1)
		}
	case sig := <-stop:
		logger.Info("shutting down", "signal", sig.String())
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.Error("graceful shutdown failed", "error", err)
		}
	}
}
