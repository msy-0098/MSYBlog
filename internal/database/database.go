package database

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"masenyu.top/blog/backend/internal/model"
)

type Options struct {
	DSN string
}

func Open(options Options) (*gorm.DB, error) {
	dsn := options.DSN
	if dsn == "" {
		dsn = "data/blog.db"
	}

	if shouldCreateParentDir(dsn) {
		if err := os.MkdirAll(filepath.Dir(dsn), 0o755); err != nil {
			return nil, err
		}
	}

	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, err
	}

	if err := db.AutoMigrate(&model.SiteSetting{}); err != nil {
		return nil, err
	}

	return db, SeedDefaultSiteSettings(db)
}

func SeedDefaultSiteSettings(db *gorm.DB) error {
	defaults := map[string]string{
		"siteTitle":   "马森雨的技术博客",
		"subtitle":    "用 Go、Vue 和 AI 工具构建清爽可靠的技术作品",
		"owner":       "马森雨",
		"domain":      "masenyu.top",
		"description": "记录项目实践、技术复盘和持续成长。",
		"navItems":    "首页,文章,分类,项目,关于",
	}

	for key, value := range defaults {
		setting := model.SiteSetting{Key: key}
		if err := db.FirstOrCreate(&setting, model.SiteSetting{Key: key, Value: value}).Error; err != nil {
			return err
		}
	}

	return nil
}

func shouldCreateParentDir(dsn string) bool {
	if dsn == ":memory:" || strings.HasPrefix(dsn, "file:") {
		return false
	}

	return filepath.Dir(dsn) != "."
}
