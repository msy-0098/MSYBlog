package database

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"masenyu.top/blog/backend/internal/model"
)

func TestSeedDefaultSiteSettingsIsIdempotentAfterAdminUpdates(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
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
