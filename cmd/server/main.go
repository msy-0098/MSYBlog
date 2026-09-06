// @title 马森雨个人技术博客 API
// @version 1.0
// @description 马森雨个人技术博客（masenyu.top）RESTful API 接口文档，涵盖文章、分类、标签、项目、友链、评论、AI助手、数据看板与站点管理等模块。
// @contact.name 马森雨
// @contact.url https://masenyu.top
// @host localhost:8080
// @BasePath /api
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description 请输入 JWT Token，格式为: Bearer <token>
// @securityDefinitions.apikey CsrfToken
// @in header
// @name X-CSRF-Token
// @description 管理端防跨站请求伪造令牌
package main

import (
	"log/slog"
	"os"

	"masenyu.top/blog/backend/internal/config"
	"masenyu.top/blog/backend/internal/database"
	"masenyu.top/blog/backend/internal/router"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	cfg, err := config.Load("config.yaml")
	if err != nil {
		logger.Error("load config", "error", err)
		os.Exit(1)
	}

	db, err := database.Open(database.Options{DSN: cfg.Database.DSN, Config: cfg})
	if err != nil {
		logger.Error("open database", "error", err)
		os.Exit(1)
	}

	engine := router.New(router.Dependencies{
		Config:   cfg,
		Database: db,
	})

	logger.Info("starting blog server", "address", cfg.Server.Address)
	if err := engine.Run(cfg.Server.Address); err != nil {
		logger.Error("run server", "error", err)
		os.Exit(1)
	}
}
