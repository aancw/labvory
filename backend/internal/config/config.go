// Package config loads LabVory configuration: a TOML file for the standalone
// binary (labvory.toml, see deploy/labvory.toml.example) and LV_* environment
// variables — the latter win when both define a setting. Docker deployments
// keep using environment variables alone.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/BurntSushi/toml"
)

// DefaultConfigFile is the TOML file the standalone binary reads when started
// without -config, if it exists in the working directory.
const DefaultConfigFile = "labvory.toml"

// Config is the resolved runtime configuration.
type Config struct {
	// Addr is the HTTP listen address, e.g. ":8090".
	Addr string
	// DatabaseURL is the PostgreSQL connection string.
	DatabaseURL string
	// CookieSecure marks session/CSRF cookies Secure (enable behind HTTPS).
	CookieSecure bool
	// SessionTTL is the sliding session lifetime.
	SessionTTL time.Duration
	// Env is "production" or "development" (controls log format and CORS-free dev proxy).
	Env string
	// SeedMode: empty for production; "dev" applies the homelab seed after migrations.
	SeedMode string
	// SecretKey encrypts integration credentials at rest.
	SecretKey string
	// UpdateCheck enables the GitHub Releases update check (and with it the
	// in-UI install). Default true; opt out for fully offline instances.
	UpdateCheck bool
}

// fileConfig mirrors the TOML config file. Pointer fields distinguish
// "unset" from Go zero values, so the merge below knows what the file said.
type fileConfig struct {
	Addr         *string `toml:"addr"`
	DatabaseURL  *string `toml:"database_url"`
	SecretKey    *string `toml:"secret_key"`
	CookieSecure *bool   `toml:"cookie_secure"`
	SessionTTL   *string `toml:"session_ttl"`
	Env          *string `toml:"env"`
	SeedMode     *string `toml:"seed_mode"`
	UpdateCheck  *bool   `toml:"update_check"`
}

// Load resolves configuration with precedence: environment variables over
// the TOML config file over built-in defaults. A non-empty configPath must
// exist; the empty path falls back to ./labvory.toml when present. Unknown
// keys in the file are errors — typos must not silently disable a setting.
func Load(configPath string) (Config, error) {
	explicit := configPath != ""
	if !explicit {
		configPath = DefaultConfigFile
	}

	var file fileConfig
	data, err := os.ReadFile(configPath)
	switch {
	case errors.Is(err, fs.ErrNotExist) && !explicit:
		// No default file: environment and defaults only.
	case err != nil:
		return Config{}, err
	default:
		if err := decodeFile(data, &file); err != nil {
			return Config{}, fmt.Errorf("config file %s: %w", configPath, err)
		}
		warnWorldReadable(configPath, file)
	}

	cfg := Config{
		Addr:         resolveString(file.Addr, "LV_ADDR", "0.0.0.0:8090"),
		DatabaseURL:  resolveDatabaseURL(file.DatabaseURL),
		SecretKey:    resolveString(file.SecretKey, "LV_SECRET_KEY", ""),
		Env:          resolveString(file.Env, "LV_ENV", "production"),
		SeedMode:     resolveString(file.SeedMode, "LV_SEED_MODE", ""),
		CookieSecure: resolveBool(file.CookieSecure, "LV_COOKIE_SECURE", false),
		UpdateCheck:  resolveBool(file.UpdateCheck, "LV_UPDATE_CHECK", true),
	}

	ttl := os.Getenv("LV_SESSION_TTL")
	switch {
	case ttl != "":
		cfg.SessionTTL = envDuration("LV_SESSION_TTL", 14*24*time.Hour)
	case file.SessionTTL != nil:
		d, err := time.ParseDuration(*file.SessionTTL)
		if err != nil {
			return Config{}, fmt.Errorf("config file %s: session_ttl: %w", configPath, err)
		}
		cfg.SessionTTL = d
	default:
		cfg.SessionTTL = 14 * 24 * time.Hour
	}
	return cfg, nil
}

// decodeFile parses TOML strictly: any key not consumed by fileConfig (i.e. a
// typo) is reported instead of ignored.
func decodeFile(data []byte, into *fileConfig) error {
	md, err := toml.NewDecoder(bytes.NewReader(data)).Decode(into)
	if err != nil {
		return err
	}
	if unknown := md.Undecoded(); len(unknown) > 0 {
		names := make([]string, len(unknown))
		for i, k := range unknown {
			names[i] = k.String()
		}
		return fmt.Errorf("unknown key(s): %q", names)
	}
	return nil
}

// warnWorldReadable flags a config file carrying secret_key that other users
// on the host could read. Startup diagnostics run before the real logger
// exists, so the default logger is fine here.
func warnWorldReadable(path string, file fileConfig) {
	if file.SecretKey == nil || *file.SecretKey == "" {
		return
	}
	if info, err := os.Stat(path); err == nil && info.Mode().Perm()&0o044 != 0 {
		slog.Default().Warn("config file is world-readable and holds secret_key — consider chmod 600",
			"file", path)
	}
}

// resolveString: environment wins, then the file value, then the default.
func resolveString(fileVal *string, envKey, fallback string) string {
	if v := os.Getenv(envKey); v != "" {
		return v
	}
	if fileVal != nil {
		return *fileVal
	}
	return fallback
}

// resolveDatabaseURL also honours the DATABASE_URL spelling before defaults.
func resolveDatabaseURL(fileVal *string) string {
	for _, key := range []string{"LV_DATABASE_URL", "DATABASE_URL"} {
		if v := os.Getenv(key); v != "" {
			return v
		}
	}
	if fileVal != nil {
		return *fileVal
	}
	return "postgres://labvory:labvory@localhost:5432/labvory?sslmode=disable"
}

// resolveBool treats a missing environment variable as unset so the file can
// still provide the value.
func resolveBool(fileVal *bool, envKey string, fallback bool) bool {
	if os.Getenv(envKey) != "" {
		return envBool(envKey, fallback)
	}
	if fileVal != nil {
		return *fileVal
	}
	return fallback
}

// Production reports whether the server runs in production mode.
func (c Config) Production() bool { return c.Env == "production" }

// Logger builds the process logger: JSON in production, readable text in development.
func (c Config) Logger() *slog.Logger {
	level := slog.LevelInfo
	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	if c.Production() {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}
	return slog.New(handler)
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

func envDuration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}
