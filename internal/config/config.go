package config

import (
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	Site     SiteConfig     `yaml:"site"`
	Admin    AdminConfig    `yaml:"admin"`
	Auth     AuthConfig     `yaml:"auth"`
}

type ServerConfig struct {
	Address string `yaml:"address"`
}

type DatabaseConfig struct {
	Driver string `yaml:"driver"`
	DSN    string `yaml:"dsn"`
}

type SiteConfig struct {
	SiteTitle   string   `yaml:"siteTitle"`
	Subtitle    string   `yaml:"subtitle"`
	Owner       string   `yaml:"owner"`
	Domain      string   `yaml:"domain"`
	Description string   `yaml:"description"`
	NavItems    []string `yaml:"navItems"`
}

type AdminConfig struct {
	InitialUsername string `yaml:"initialUsername"`
	InitialPassword string `yaml:"initialPassword"`
}

type AuthConfig struct {
	JWTSecret string `yaml:"jwtSecret"`
}

func Default() Config {
	return Config{
		Server: ServerConfig{
			Address: "127.0.0.1:8080",
		},
		Database: DatabaseConfig{
			Driver: "sqlite",
			DSN:    "data/blog.db",
		},
		Site: SiteConfig{
			SiteTitle:   "马森雨的技术博客",
			Subtitle:    "用 Go、Vue 和 AI 工具构建清爽可靠的技术作品",
			Owner:       "马森雨",
			Domain:      "masenyu.top",
			Description: "记录项目实践、技术复盘和持续成长。",
			NavItems:    []string{"首页", "文章", "分类", "项目", "关于"},
		},
		Admin: AdminConfig{
			InitialUsername: "masenyu812@gmail.com",
			InitialPassword: "local-development-admin-password",
		},
		Auth: AuthConfig{
			JWTSecret: "local-development-secret",
		},
	}
}

func Load(path string) (Config, error) {
	cfg := Default()

	content, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return finalize(withEnvironmentOverrides(cfg))
		}
		return Config{}, err
	}

	if err := yaml.Unmarshal(content, &cfg); err != nil {
		return Config{}, err
	}

	return finalize(withEnvironmentOverrides(withDefaults(cfg)))
}

func finalize(cfg Config) (Config, error) {
	if os.Getenv("GIN_MODE") == "release" {
		defaults := Default()
		if cfg.Auth.JWTSecret == defaults.Auth.JWTSecret {
			return Config{}, fmt.Errorf("BLOG_JWT_SECRET must be configured in release mode")
		}
		if cfg.Admin.InitialPassword == defaults.Admin.InitialPassword {
			return Config{}, fmt.Errorf("BLOG_ADMIN_INITIAL_PASSWORD must be configured in release mode")
		}
	}

	return cfg, nil
}

func withDefaults(cfg Config) Config {
	defaults := Default()

	if cfg.Server.Address == "" {
		cfg.Server.Address = defaults.Server.Address
	}
	if cfg.Database.DSN == "" {
		cfg.Database.DSN = defaults.Database.DSN
	}
	if cfg.Database.Driver == "" {
		cfg.Database.Driver = defaults.Database.Driver
	}
	if cfg.Site.SiteTitle == "" {
		cfg.Site.SiteTitle = defaults.Site.SiteTitle
	}
	if cfg.Site.Subtitle == "" {
		cfg.Site.Subtitle = defaults.Site.Subtitle
	}
	if cfg.Site.Owner == "" {
		cfg.Site.Owner = defaults.Site.Owner
	}
	if cfg.Site.Domain == "" {
		cfg.Site.Domain = defaults.Site.Domain
	}
	if cfg.Site.Description == "" {
		cfg.Site.Description = defaults.Site.Description
	}
	if len(cfg.Site.NavItems) == 0 {
		cfg.Site.NavItems = defaults.Site.NavItems
	}
	if cfg.Admin.InitialUsername == "" {
		cfg.Admin.InitialUsername = defaults.Admin.InitialUsername
	}
	if cfg.Admin.InitialPassword == "" {
		cfg.Admin.InitialPassword = defaults.Admin.InitialPassword
	}
	if cfg.Auth.JWTSecret == "" {
		cfg.Auth.JWTSecret = defaults.Auth.JWTSecret
	}

	return cfg
}

func withEnvironmentOverrides(cfg Config) Config {
	if value := os.Getenv("BLOG_SERVER_ADDRESS"); value != "" {
		cfg.Server.Address = value
	}
	if value := os.Getenv("BLOG_DATABASE_DSN"); value != "" {
		cfg.Database.DSN = value
	}
	if value := os.Getenv("BLOG_DATABASE_DRIVER"); value != "" {
		cfg.Database.Driver = value
	}
	if value := os.Getenv("BLOG_ADMIN_INITIAL_USERNAME"); value != "" {
		cfg.Admin.InitialUsername = value
	}
	if value := os.Getenv("BLOG_ADMIN_INITIAL_PASSWORD"); value != "" {
		cfg.Admin.InitialPassword = value
	}
	if value := os.Getenv("BLOG_JWT_SECRET"); value != "" {
		cfg.Auth.JWTSecret = value
	}

	return cfg
}
