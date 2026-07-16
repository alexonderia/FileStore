package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/alexonderia/filestore/internal/app"
	"github.com/alexonderia/filestore/internal/config"
	"github.com/alexonderia/filestore/internal/database"
	"github.com/alexonderia/filestore/internal/storage"
	"github.com/alexonderia/filestore/internal/storage/seaweedfs"
	"github.com/jackc/pgx/v5/pgxpool"
)

var version = "dev"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if err := config.LoadDotEnv(".env"); err != nil {
		logger.Error("failed to load .env", "error", err)
		os.Exit(2)
	}
	if len(os.Args) > 1 && os.Args[1] == "bootstrap-superadmin" {
		os.Exit(runBootstrapSuperadmin(os.Args[2:], logger))
	}
	cfg, err := config.LoadAPI(os.Args[1:], os.Getenv)
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var pool *pgxpool.Pool
	var objectStore storage.ObjectStore
	if cfg.DatabaseURL != "" {
		pool, err = database.Open(ctx, cfg.DatabaseURL)
		if err != nil {
			logger.Error("database startup failed", "error", err)
			os.Exit(1)
		}
		defer pool.Close()
		if err := database.Migrate(ctx, pool); err != nil {
			logger.Error("database migration failed", "error", err)
			os.Exit(1)
		}
		logger.Info("database migrations applied")
	} else {
		logger.Warn("database is not configured; product endpoints are unavailable")
	}
	if cfg.StorageConfigured() {
		store, err := seaweedfs.New(ctx, cfg)
		if err != nil {
			logger.Error("object storage startup failed", "error", err)
			os.Exit(1)
		}
		if err := store.EnsureBucket(ctx); err != nil {
			logger.Error("object storage bucket failed", "error", err)
			os.Exit(1)
		}
		objectStore = store
	} else if pool != nil {
		logger.Warn("object storage is not configured; file endpoints are unavailable")
	}

	logger.Info("starting FileStore API", "version", version)
	if err := app.NewMVPAPI(cfg, logger, pool, objectStore).Run(ctx); err != nil {
		logger.Error("API stopped with error", "error", err)
		os.Exit(1)
	}
	logger.Info("FileStore API stopped")
}
