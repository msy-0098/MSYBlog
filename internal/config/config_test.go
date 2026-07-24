package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"masenyu.top/blog/backend/internal/config"
)

func TestLoadAppliesEnvironmentOverrides(t *testing.T) {
	t.Setenv("BLOG_SERVER_ADDRESS", "127.0.0.1:18080")
	t.Setenv("BLOG_DATABASE_DRIVER", "postgres")
	t.Setenv("BLOG_DATABASE_DSN", "host=127.0.0.1 user=blog_test password=test dbname=blog_test port=5432 sslmode=disable TimeZone=Asia/Shanghai")
	t.Setenv("BLOG_SMTP_HOST", "smtp.qq.com")
	t.Setenv("BLOG_SMTP_PORT", "587")
	t.Setenv("BLOG_SMTP_USERNAME", "reader@example.com")
	t.Setenv("BLOG_SMTP_PASSWORD", "smtp-password")
	t.Setenv("BLOG_SMTP_FROM", "reader@example.com")
	t.Setenv("BLOG_AI_PROVIDER", "openai-compatible")
	t.Setenv("BLOG_AI_MODEL", "analysis-model")
	t.Setenv("BLOG_AI_API_KEY", "analysis-key")
	t.Setenv("BLOG_AI_BASE_URL", "https://api.deepseek.com")

	cfg, err := config.Load(filepath.Join(t.TempDir(), "missing-config.yaml"))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.Server.Address != "127.0.0.1:18080" {
		t.Fatalf("expected env server address, got %q", cfg.Server.Address)
	}
	if cfg.Database.DSN != "host=127.0.0.1 user=blog_test password=test dbname=blog_test port=5432 sslmode=disable TimeZone=Asia/Shanghai" {
		t.Fatalf("expected env database dsn, got %q", cfg.Database.DSN)
	}
	if cfg.Database.Driver != "postgres" {
		t.Fatalf("expected env database driver, got %q", cfg.Database.Driver)
	}
	if cfg.Mail.SMTPHost != "smtp.qq.com" || cfg.Mail.Username != "reader@example.com" || cfg.Mail.Password != "smtp-password" {
		t.Fatalf("expected SMTP env overrides, got %#v", cfg.Mail)
	}
	if cfg.AI.Provider != "openai-compatible" || cfg.AI.Model != "analysis-model" || cfg.AI.APIKey != "analysis-key" || cfg.AI.BaseURL != "https://api.deepseek.com" {
		t.Fatalf("expected AI env overrides, got %#v", cfg.AI)
	}
}

func TestDefaultDatabaseDriverIsPostgres(t *testing.T) {
	cfg := config.Default()

	if cfg.Database.Driver != "postgres" {
		t.Fatalf("expected postgres default driver, got %q", cfg.Database.Driver)
	}
}

func TestDefaultAIProviderIsDeepSeek(t *testing.T) {
	cfg := config.Default()

	if cfg.AI.Provider != "deepseek" {
		t.Fatalf("default AI provider = %q, want deepseek", cfg.AI.Provider)
	}
}

func TestLoadRejectsUnknownAIProvider(t *testing.T) {
	t.Setenv("BLOG_AI_PROVIDER", "unsupported-provider")

	_, err := config.Load(filepath.Join(t.TempDir(), "missing-config.yaml"))
	if err == nil {
		t.Fatal("expected unknown AI provider to be rejected")
	}
	if !strings.Contains(err.Error(), "BLOG_AI_PROVIDER") {
		t.Fatalf("expected safe provider configuration error, got %q", err.Error())
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

func TestLoadRejectsNonPostgresDatabaseInReleaseMode(t *testing.T) {
	t.Setenv("GIN_MODE", "release")
	t.Setenv("BLOG_JWT_SECRET", "release-test-secret")
	t.Setenv("BLOG_ADMIN_INITIAL_PASSWORD", "release-admin-password")
	t.Setenv("BLOG_DATABASE_DRIVER", "mysql")
	t.Setenv("BLOG_DATABASE_DSN", "blog_user:replace-with-password@tcp(127.0.0.1:3306)/blog?charset=utf8mb4&parseTime=True&loc=Local")

	_, err := config.Load(filepath.Join(t.TempDir(), "missing-config.yaml"))
	if err == nil {
		t.Fatal("expected release config with non-postgres database to fail")
	}

	if !strings.Contains(err.Error(), "BLOG_DATABASE_DRIVER") {
		t.Fatalf("expected error to mention BLOG_DATABASE_DRIVER, got %q", err.Error())
	}
}

func TestLoadRejectsPlaceholderDatabaseDSNInReleaseMode(t *testing.T) {
	setValidReleaseEnvironment(t)
	t.Setenv("BLOG_DATABASE_DSN", "host=127.0.0.1 user=blog_user password=replace-with-postgres-password dbname=blog port=5432 sslmode=disable")

	_, err := config.Load(filepath.Join(t.TempDir(), "missing-config.yaml"))
	if err == nil {
		t.Fatal("expected release config with placeholder database DSN to fail")
	}

	if !strings.Contains(err.Error(), "BLOG_DATABASE_DSN") {
		t.Fatalf("expected error to mention BLOG_DATABASE_DSN, got %q", err.Error())
	}
}

func TestLoadRejectsPlaceholderAdminUsernameInReleaseMode(t *testing.T) {
	setValidReleaseEnvironment(t)
	t.Setenv("BLOG_ADMIN_INITIAL_USERNAME", "replace-with-admin-username")

	_, err := config.Load(filepath.Join(t.TempDir(), "missing-config.yaml"))
	if err == nil {
		t.Fatal("expected release config with placeholder admin username to fail")
	}

	if !strings.Contains(err.Error(), "BLOG_ADMIN_INITIAL_USERNAME") {
		t.Fatalf("expected error to mention BLOG_ADMIN_INITIAL_USERNAME, got %q", err.Error())
	}
}

func TestLoadRequiresSMTPConfigurationInReleaseMode(t *testing.T) {
	setValidReleaseEnvironment(t)
	t.Setenv("BLOG_SMTP_HOST", "")
	t.Setenv("BLOG_SMTP_USERNAME", "")
	t.Setenv("BLOG_SMTP_PASSWORD", "")
	t.Setenv("BLOG_SMTP_FROM", "")

	_, err := config.Load(filepath.Join(t.TempDir(), "missing-config.yaml"))
	if err == nil {
		t.Fatal("expected release config without SMTP settings to fail")
	}

	if !strings.Contains(err.Error(), "BLOG_SMTP") {
		t.Fatalf("expected error to mention BLOG_SMTP, got %q", err.Error())
	}
}

func setValidReleaseEnvironment(t *testing.T) {
	t.Helper()

	t.Setenv("GIN_MODE", "release")
	t.Setenv("BLOG_JWT_SECRET", "release-test-secret")
	t.Setenv("BLOG_ADMIN_INITIAL_USERNAME", "admin@example.test")
	t.Setenv("BLOG_ADMIN_INITIAL_PASSWORD", "release-admin-password")
	t.Setenv("BLOG_DATABASE_DRIVER", "postgres")
	t.Setenv("BLOG_DATABASE_DSN", "host=127.0.0.1 user=blog_user password=test-password dbname=blog port=5432 sslmode=disable")
	t.Setenv("BLOG_SMTP_HOST", "smtp.example.test")
	t.Setenv("BLOG_SMTP_USERNAME", "mailer@example.test")
	t.Setenv("BLOG_SMTP_PASSWORD", "smtp-test-password")
	t.Setenv("BLOG_SMTP_FROM", "mailer@example.test")
}

func TestDefaultVerificationCodeTiming(t *testing.T) {
	cfg := config.Default()

	if cfg.VerificationCode.Cooldown != 60*time.Second {
		t.Fatalf("default verification cooldown = %v, want %v", cfg.VerificationCode.Cooldown, 60*time.Second)
	}
	if cfg.VerificationCode.ExpiresIn != 10*time.Minute {
		t.Fatalf("default verification expiry = %v, want %v", cfg.VerificationCode.ExpiresIn, 10*time.Minute)
	}
}

func TestLoadParsesVerificationCodeTiming(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := []byte("verificationCode:\n  cooldown: 75s\n  expiresIn: 12m\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.VerificationCode.Cooldown != 75*time.Second {
		t.Fatalf("parsed verification cooldown = %v, want %v", cfg.VerificationCode.Cooldown, 75*time.Second)
	}
	if cfg.VerificationCode.ExpiresIn != 12*time.Minute {
		t.Fatalf("parsed verification expiry = %v, want %v", cfg.VerificationCode.ExpiresIn, 12*time.Minute)
	}
}
