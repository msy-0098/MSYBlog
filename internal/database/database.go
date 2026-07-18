package database

import (
	"fmt"
	"strings"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"masenyu.top/blog/backend/internal/auth"
	"masenyu.top/blog/backend/internal/config"
	"masenyu.top/blog/backend/internal/model"
)

type Options struct {
	DSN      string
	Config   config.Config
	SkipSeed bool
}

func Open(options Options) (*gorm.DB, error) {
	dsn := options.DSN
	if dsn == "" {
		dsn = options.Config.Database.DSN
	}
	if dsn == "" {
		dsn = config.Default().Database.DSN
	}

	driver := strings.ToLower(strings.TrimSpace(options.Config.Database.Driver))
	if driver == "" {
		driver = config.Default().Database.Driver
	}
	if driver == "postgresql" {
		driver = "postgres"
	}
	if driver != "postgres" {
		return nil, fmt.Errorf("unsupported database driver %q", driver)
	}

	db, err := gorm.Open(dialector(driver, dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, err
	}

	bootstrap, err := isUninitializedDatabase(db)
	if err != nil {
		return nil, fmt.Errorf("inspect database bootstrap state: %w", err)
	}
	if err := AutoMigrate(db); err != nil {
		return nil, err
	}
	if bootstrap && !options.SkipSeed {
		if err := SeedDefaults(db, options.Config); err != nil {
			return nil, err
		}
	}

	return db, nil
}

// isUninitializedDatabase reports whether this is the first service bootstrap.
// Any existing table means the database is not truly empty, so startup must not
// write defaults or synchronize the configured administrator password.
func isUninitializedDatabase(db *gorm.DB) (bool, error) {
	tables, err := db.Migrator().GetTables()
	if err != nil {
		return false, err
	}
	return len(tables) == 0, nil
}

func dialector(driver string, dsn string) gorm.Dialector {
	switch driver {
	case "postgres":
		return postgres.Open(dsn)
	default:
		panic("unsupported database driver was not validated")
	}
}

func SeedInitialAdmin(db *gorm.DB, cfg config.Config) error {
	if cfg.Admin.InitialPassword == "" {
		return nil
	}

	var admin model.User
	err := db.Where("username = ?", cfg.Admin.InitialUsername).First(&admin).Error
	if err != nil {
		// Administrator not found, create new
		hash, err := auth.HashPassword(cfg.Admin.InitialPassword)
		if err != nil {
			return err
		}
		email := cfg.Admin.InitialUsername
		return db.Create(&model.User{
			Username:     cfg.Admin.InitialUsername,
			Email:        email,
			Nickname:     "管理员",
			Role:         model.UserRoleAdmin,
			PasswordHash: hash,
		}).Error
	}

	// Administrator found, synchronize with latest configured initial password
	hash, err := auth.HashPassword(cfg.Admin.InitialPassword)
	if err != nil {
		return err
	}
	admin.PasswordHash = hash
	admin.Role = model.UserRoleAdmin
	if admin.Email == "" {
		admin.Email = admin.Username
	}
	if admin.Nickname == "" {
		admin.Nickname = "管理员"
	}
	return db.Save(&admin).Error
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
		setting := model.SiteSetting{Key: key, Value: value}
		if err := db.Where(siteSettingLookup(key)).FirstOrCreate(&setting).Error; err != nil {
			return err
		}
	}

	return nil
}

func siteSettingLookup(key string) model.SiteSetting {
	return model.SiteSetting{Key: key}
}

type seedPost struct {
	Title       string
	Slug        string
	Summary     string
	Content     string
	Cover       string
	Category    string
	Tags        []string
	ViewCount   int
	PublishedAt time.Time
}

func SeedDefaultBlogContent(db *gorm.DB) error {
	categories := []model.Category{
		{Name: "Go", Slug: "go"},
		{Name: "Vue", Slug: "vue"},
		{Name: "部署", Slug: "deploy"},
	}
	for _, seed := range categories {
		if err := db.Where("slug = ?", seed.Slug).FirstOrCreate(&model.Category{}, seed).Error; err != nil {
			return err
		}
	}

	tags := []model.Tag{
		{Name: "后端", Slug: "backend"},
		{Name: "PostgreSQL", Slug: "postgres"},
		{Name: "前端", Slug: "frontend"},
		{Name: "性能", Slug: "performance"},
		{Name: "Nginx", Slug: "nginx"},
	}
	for _, seed := range tags {
		if err := db.Where("slug = ?", seed.Slug).FirstOrCreate(&model.Tag{}, seed).Error; err != nil {
			return err
		}
	}

	posts := []seedPost{
		{
			Title:    "用 Go 和 PostgreSQL 搭建长期博客",
			Slug:     "go-gin-postgresql-blog",
			Summary:  "从 Gin 路由、GORM 模型到 PostgreSQL 持久化，梳理个人博客后端的长期维护路径。",
			Cover:    "",
			Category: "go",
			Tags:     []string{"backend", "postgres"},
			Content: strings.TrimSpace(`# 用 Go 和 PostgreSQL 搭建长期博客

## 为什么选择 PostgreSQL

个人博客的第一目标是稳定可维护。Go 服务负责公开阅读接口，PostgreSQL 承担文章、分类、标签和评论的持久化，后续扩展搜索、统计和 JSON 数据也更从容。

## 数据模型

文章通过分类形成主线，再用标签补充交叉主题：

` + "```go" + `
type Post struct {
    Title string
    Slug  string
}
` + "```" + `

## 下一步

继续完善备份、监控和后台发布能力，让内容维护可以长期稳定在线完成。`),
			ViewCount:   128,
			PublishedAt: time.Date(2026, 6, 24, 9, 30, 0, 0, time.UTC),
		},
		{
			Title:    "Vue 首页动效如何保持清爽",
			Slug:     "vue-particle-homepage",
			Summary:  "记录首页粒子穹顶、卡片动效和移动端降级之间的取舍。",
			Cover:    "",
			Category: "vue",
			Tags:     []string{"frontend", "performance"},
			Content: strings.TrimSpace(`# Vue 首页动效如何保持清爽

## 动效服务于阅读

首页需要有记忆点，但不能抢走文章内容的注意力。粒子动效放在文字背后，并通过透明度和数量控制保持轻盈。

## 移动端策略

移动端减少粒子数量，保留布局稳定性，优先让导航、标题和文章入口清晰可点。`),
			ViewCount:   96,
			PublishedAt: time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC),
		},
		{
			Title:    "Nginx 反向代理中的 /api 约定",
			Slug:     "nginx-api-proxy",
			Summary:  "梳理前端静态资源、Go API 和上传目录在 Nginx 中的路径分工。",
			Cover:    "",
			Category: "deploy",
			Tags:     []string{"backend", "nginx"},
			Content: strings.TrimSpace(`# Nginx 反向代理中的 /api 约定

## 路径分工

` + "```nginx" + `
location /api/ {
    proxy_pass http://127.0.0.1:8080/api/;
}
` + "```" + `

静态资源由 Nginx 托管，API 请求转发到 Go 服务，上传文件单独映射目录。

## 验收重点

上线后需要分别验证首页、` + "`/api/site`" + ` 和上传资源访问。`),
			ViewCount:   77,
			PublishedAt: time.Date(2026, 6, 20, 15, 45, 0, 0, time.UTC),
		},
	}

	for _, seed := range posts {
		if err := seedOnePost(db, seed); err != nil {
			return err
		}
	}

	return nil
}

func seedOnePost(db *gorm.DB, seed seedPost) error {
	var category model.Category
	if err := db.Where("slug = ?", seed.Category).First(&category).Error; err != nil {
		return err
	}

	post := model.Post{}
	err := db.Where("slug = ?", seed.Slug).Attrs(model.Post{
		Title:       seed.Title,
		Slug:        seed.Slug,
		Summary:     seed.Summary,
		Content:     seed.Content,
		Cover:       seed.Cover,
		Status:      model.PostStatusPublished,
		ViewCount:   seed.ViewCount,
		CategoryID:  category.ID,
		PublishedAt: seed.PublishedAt,
	}).FirstOrCreate(&post).Error
	if err != nil {
		return err
	}

	var postTags []model.Tag
	if err := db.Where("slug IN ?", seed.Tags).Find(&postTags).Error; err != nil {
		return err
	}

	return db.Model(&post).Association("Tags").Replace(postTags)
}
