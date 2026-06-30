package config_test

import (
	"path/filepath"
	"strings"
	"testing"

	"masenyu.top/blog/backend/internal/config"
)

func TestLoadAppliesEnvironmentOverrides(t *testing.T) {
	t.Setenv("BLOG_SERVER_ADDRESS", "127.0.0.1:18080")
	t.Setenv("BLOG_DATABASE_DRIVER", "mysql")
	t.Setenv("BLOG_DATABASE_DSN", "file::memory:?cache=shared")
	t.Setenv("BLOG_SMTP_HOST", "smtp.qq.com")
	t.Setenv("BLOG_SMTP_PORT", "587")
	t.Setenv("BLOG_SMTP_USERNAME", "reader@example.com")
	t.Setenv("BLOG_SMTP_PASSWORD", "smtp-password")
	t.Setenv("BLOG_SMTP_FROM", "reader@example.com")
	t.Setenv("BLOG_AI_PROVIDER", "openai-compatible")
	t.Setenv("BLOG_AI_MODEL", "analysis-model")
	t.Setenv("BLOG_AI_API_KEY", "analysis-key")

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
	if cfg.Database.Driver != "mysql" {
		t.Fatalf("expected env database driver, got %q", cfg.Database.Driver)
	}
	if cfg.Mail.SMTPHost != "smtp.qq.com" || cfg.Mail.Username != "reader@example.com" || cfg.Mail.Password != "smtp-password" {
		t.Fatalf("expected SMTP env overrides, got %#v", cfg.Mail)
	}
	if cfg.AI.Provider != "openai-compatible" || cfg.AI.Model != "analysis-model" || cfg.AI.APIKey != "analysis-key" {
		t.Fatalf("expected AI env overrides, got %#v", cfg.AI)
	}
}

func TestDefaultDatabaseDriverIsSQLite(t *testing.T) {
	cfg := config.Default()

	if cfg.Database.Driver != "sqlite" {
		t.Fatalf("expected sqlite default driver, got %q", cfg.Database.Driver)
	}
}

func TestLoadRejectsDefaultJWTSecretInReleaseMode(t *testing.T) {
	t.Setenv("GIN_MODE", "release")

	_, err := config.Load(filepath.Join(t.TempDir(), "missing-config.yaml"))
	if err == nil {
		t.Fatal("expected release config with default JWT secret to fail")
	}

	if !strings.Contains(err.Error(), "BLOG_JWT_SECRET") {
		t.Fatalf("expected error to mention BLOG_JWT_SECRET, got %q", err.Error())
	}
}

func TestLoadRejectsDefaultAdminPasswordInReleaseMode(t *testing.T) {
	t.Setenv("GIN_MODE", "release")
	t.Setenv("BLOG_JWT_SECRET", "release-test-secret")

	_, err := config.Load(filepath.Join(t.TempDir(), "missing-config.yaml"))
	if err == nil {
		t.Fatal("expected release config with default admin password to fail")
	}

	if !strings.Contains(err.Error(), "BLOG_ADMIN_INITIAL_PASSWORD") {
		t.Fatalf("expected error to mention BLOG_ADMIN_INITIAL_PASSWORD, got %q", err.Error())
	}
}

func TestLoadRejectsSQLiteDatabaseInReleaseMode(t *testing.T) {
	t.Setenv("GIN_MODE", "release")
	t.Setenv("BLOG_JWT_SECRET", "release-test-secret")
	t.Setenv("BLOG_ADMIN_INITIAL_PASSWORD", "release-admin-password")
	t.Setenv("BLOG_DATABASE_DRIVER", "sqlite")
	t.Setenv("BLOG_DATABASE_DSN", "data/blog.db")

	_, err := config.Load(filepath.Join(t.TempDir(), "missing-config.yaml"))
	if err == nil {
		t.Fatal("expected release config with sqlite database to fail")
	}

	if !strings.Contains(err.Error(), "BLOG_DATABASE_DRIVER") {
		t.Fatalf("expected error to mention BLOG_DATABASE_DRIVER, got %q", err.Error())
	}
}
