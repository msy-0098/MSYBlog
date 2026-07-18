package main

import (
	"flag"
	"log/slog"
	"os"

	"masenyu.top/blog/backend/internal/config"
	"masenyu.top/blog/backend/internal/database"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to the backend config file")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Error("load config", "error", err)
		os.Exit(1)
	}

	db, err := database.Open(database.Options{DSN: cfg.Database.DSN, Config: cfg, SkipSeed: true})
	if err != nil {
		logger.Error("open database", "error", err)
		os.Exit(1)
	}
	if err := database.SeedCareerTimelinePosts(db); err != nil {
		logger.Error("seed career timeline posts", "error", err)
		os.Exit(1)
	}
	logger.Info("career timeline posts imported successfully")
}
