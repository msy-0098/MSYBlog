package database

import (
	"os"
	"strings"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"masenyu.top/blog/backend/internal/config"
	"masenyu.top/blog/backend/internal/model"
)

func TestSeedDefaultSiteSettingsIsIdempotentAfterAdminUpdates(t *testing.T) {
	db := testPostgresDatabase(t)
	if err := db.AutoMigrate(&model.SiteSetting{}); err != nil {
		t.Fatalf("migrate settings: %v", err)
	}

	if err := SeedDefaultSiteSettings(db); err != nil {
		t.Fatalf("seed defaults: %v", err)
	}
	if err := db.Model(&model.SiteSetting{}).Where("key = ?", "siteTitle").Update("value", "后台改过的标题").Error; err != nil {
		t.Fatalf("update site title: %v", err)
	}
	if err := SeedDefaultSiteSettings(db); err != nil {
		t.Fatalf("seed defaults after update: %v", err)
	}

	var settings []model.SiteSetting
	if err := db.Where("key = ?", "siteTitle").Find(&settings).Error; err != nil {
		t.Fatalf("query site title: %v", err)
	}
	if len(settings) != 1 {
		t.Fatalf("expected one site title row, got %d", len(settings))
	}
	if settings[0].Value != "后台改过的标题" {
		t.Fatalf("expected admin-updated title to remain, got %q", settings[0].Value)
	}
}

func TestSiteSettingLookupUsesPostgresQuotedKeyColumn(t *testing.T) {
	db, err := gorm.Open(postgres.New(postgres.Config{
		DSN:                  "host=127.0.0.1 user=blog_user password=test dbname=blog_test port=5432 sslmode=disable TimeZone=Asia/Shanghai",
		PreferSimpleProtocol: true,
	}), &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}

	statement := db.Where(siteSettingLookup("siteTitle")).First(&model.SiteSetting{}).Statement
	sql := statement.SQL.String()
	if strings.Contains(sql, "WHERE key =") {
		t.Fatalf("site setting lookup must not use a bare reserved key column: %s", sql)
	}
	if !strings.Contains(sql, `"site_settings"."key"`) {
		t.Fatalf("site setting lookup should quote the key column, got: %s", sql)
	}
}

func TestOpenRejectsLegacyDatabaseDrivers(t *testing.T) {
	for _, driver := range []string{"sqlite", "mysql"} {
		t.Run(driver, func(t *testing.T) {
			cfg := config.Default()
			cfg.Database.Driver = driver
			cfg.Database.DSN = "unused"

			_, err := Open(Options{Config: cfg})
			if err == nil {
				t.Fatal("expected unsupported database driver error")
			}
			if !strings.Contains(err.Error(), "unsupported database driver") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func testPostgresDatabase(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := os.Getenv("BLOG_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("BLOG_TEST_DATABASE_DSN is required for PostgreSQL database integration tests")
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open postgres test database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql database: %v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})

	if err := db.Exec("SELECT pg_advisory_lock(?)", int64(81220260709)).Error; err != nil {
		t.Fatalf("lock postgres test database: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Exec("SELECT pg_advisory_unlock(?)", int64(81220260709)).Error
	})

	if err := db.Exec("DROP TABLE IF EXISTS site_settings CASCADE").Error; err != nil {
		t.Fatalf("drop test tables: %v", err)
	}

	return db
}
