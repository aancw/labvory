package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadFromFile(t *testing.T) {
	t.Run("file provides values", func(t *testing.T) {
		path := writeTemp(t, "lanvory.toml", strings.Join([]string{
			`addr = ":9100"`,
			`database_url = "postgres://file:file@localhost:9100/file?sslmode=disable"`,
			`secret_key = "abc123"`,
			`cookie_secure = true`,
			`session_ttl = "1h"`,
			`env = "development"`,
			`seed_mode = "dev"`,
		}, "\n")+"\n")

		cfg, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Addr != ":9100" {
			t.Errorf("Addr = %q, want :9100", cfg.Addr)
		}
		if cfg.DatabaseURL != "postgres://file:file@localhost:9100/file?sslmode=disable" {
			t.Errorf("DatabaseURL = %q", cfg.DatabaseURL)
		}
		if !cfg.CookieSecure {
			t.Error("CookieSecure = false, want true")
		}
		if cfg.SessionTTL != time.Hour {
			t.Errorf("SessionTTL = %v, want 1h", cfg.SessionTTL)
		}
		if cfg.Env != "development" || cfg.SeedMode != "dev" || cfg.SecretKey != "abc123" {
			t.Errorf("Env/SeedMode/SecretKey = %q/%q/%q", cfg.Env, cfg.SeedMode, cfg.SecretKey)
		}
	})

	t.Run("environment wins over file", func(t *testing.T) {
		t.Setenv("LV_ADDR", ":9200")
		path := writeTemp(t, "lanvory.toml", "addr = \":9101\"\n")
		cfg, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Addr != ":9200" {
			t.Errorf("Addr = %q, want :9200 from the environment", cfg.Addr)
		}
	})

	t.Run("DATABASE_URL spelling works", func(t *testing.T) {
		t.Setenv("DATABASE_URL", "postgres://spelled@localhost/x")
		path := writeTemp(t, "lanvory.toml", `database_url = "postgres://file@localhost/y"`+"\n")
		cfg, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.DatabaseURL != "postgres://spelled@localhost/x" {
			t.Errorf("DatabaseURL = %q, want the DATABASE_URL value", cfg.DatabaseURL)
		}
	})

	t.Run("absent default file yields defaults", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir) // no lanvory.toml here
		cfg, err := Load("")
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Addr != "0.0.0.0:8090" {
			t.Errorf("Addr = %q, want default", cfg.Addr)
		}
		if cfg.SessionTTL != 14*24*time.Hour {
			t.Errorf("SessionTTL = %v, want default", cfg.SessionTTL)
		}
	})

	t.Run("explicit missing file is an error", func(t *testing.T) {
		_, err := Load(filepath.Join(t.TempDir(), "absent.toml"))
		if err == nil {
			t.Error("want error for explicit missing file")
		}
	})

	t.Run("unknown keys are errors", func(t *testing.T) {
		path := writeTemp(t, "lanvory.toml", "databse_url = \"typo\"\n")
		_, err := Load(path)
		if err == nil || !strings.Contains(err.Error(), "databse_url") {
			t.Errorf("err = %v, want unknown-key error naming the typo", err)
		}
	})

	t.Run("invalid session_ttl is an error", func(t *testing.T) {
		path := writeTemp(t, "lanvory.toml", "session_ttl = \"forever\"\n")
		_, err := Load(path)
		if err == nil || !strings.Contains(err.Error(), "session_ttl") {
			t.Errorf("err = %v, want session_ttl parse error", err)
		}
	})

	t.Run("malformed TOML is an error", func(t *testing.T) {
		path := writeTemp(t, "lanvory.toml", "addr = \n")
		_, err := Load(path)
		if err == nil {
			t.Error("want parse error for malformed TOML")
		}
	})
}

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
