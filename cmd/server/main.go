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
