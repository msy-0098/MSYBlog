package config_test

import (
	"path/filepath"
	"testing"

	"masenyu.top/blog/backend/internal/config"
)

func TestLoadAppliesEnvironmentOverrides(t *testing.T) {
	t.Setenv("BLOG_SERVER_ADDRESS", "127.0.0.1:18080")
	t.Setenv("BLOG_DATABASE_DSN", "file::memory:?cache=shared")

	cfg, err := config.Load(filepath.Join(t.TempDir(), "missing-config.yaml"))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.Server.Address != "127.0.0.1:18080" {
		t.Fatalf("expected env server address, got %q", cfg.Server.Address)
	}
	if cfg.Database.DSN != "file::memory:?cache=shared" {
		t.Fatalf("expected env database dsn, got %q", cfg.Database.DSN)
	}
}
