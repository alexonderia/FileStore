package app

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	httpapi "github.com/alexonderia/filestore/internal/api/http"
	"github.com/alexonderia/filestore/internal/auth"
	"github.com/alexonderia/filestore/internal/config"
	"github.com/alexonderia/filestore/internal/repository/postgres"
	"github.com/alexonderia/filestore/internal/service"
	"github.com/alexonderia/filestore/internal/storage"
	"github.com/jackc/pgx/v5/pgxpool"
)

type API struct {
	server          *http.Server
	shutdownTimeout time.Duration
	logger          *slog.Logger
	cleanup         func(context.Context) error
}

func NewAPI(cfg config.API, logger *slog.Logger) *API {
	return NewAPIWithDatabase(cfg, logger, nil)
}

func NewAPIWithDatabase(cfg config.API, logger *slog.Logger, pool *pgxpool.Pool) *API {
	return NewMVPAPI(cfg, logger, pool, nil)
}

func NewMVPAPI(cfg config.API, logger *slog.Logger, pool *pgxpool.Pool, objects storage.ObjectStore) *API {
	handler := httpapi.NewHandler()
	if pool != nil {
		identity := service.NewIdentity(postgres.NewUsers(pool), auth.DefaultPasswordHasher(), cfg.AuthTokenTTL)
		workspaceRepository := postgres.NewWorkspaces(pool)
		workspaces := service.NewWorkspace(workspaceRepository, logger)
		if objects != nil {
			files := service.NewFiles(postgres.NewFiles(pool), workspaceRepository, objects, cfg.MaxFileSize, cfg.TextEncodings)
			updates := service.NewUpdates(postgres.NewUpdates(pool), files, objects, cfg.UpdateSessionTTL, cfg.OrphanGracePeriod, cfg.MaxFileSize, cfg.DiffMaxInputBytes, cfg.DiffMaxLines, cfg.DiffMaxOutput)
			locks := service.NewLocks(postgres.NewLocks(pool), files)
			links := service.NewLinks(postgres.NewLinks(pool), files, objects)
			handler = httpapi.NewFullHandler(identity, workspaces, files, updates, locks, links, cfg.MaxFileSize)
			api := newAPI(cfg, logger, handler)
			api.cleanup = updates.CleanupExpired
			return api
		} else {
			handler = httpapi.NewProductHandler(identity, workspaces)
		}
	}
	return newAPI(cfg, logger, handler)
}

func newAPI(cfg config.API, logger *slog.Logger, handler http.Handler) *API {
	return &API{
		server: &http.Server{
			Addr:              cfg.ListenAddress,
			Handler:           handler,
			ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		},
		shutdownTimeout: cfg.ShutdownTimeout,
		logger:          logger,
	}
}

func (api *API) Run(ctx context.Context) error {
	serverError := make(chan error, 1)
	go func() {
		api.logger.Info("API listening", "address", api.server.Addr)
		serverError <- api.server.ListenAndServe()
	}()
	if api.cleanup != nil {
		go api.runCleanup(ctx)
	}

	select {
	case err := <-serverError:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), api.shutdownTimeout)
		defer cancel()
		if err := api.server.Shutdown(shutdownContext); err != nil {
			return err
		}
		err := <-serverError
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func (api *API) runCleanup(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cleanupCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			err := api.cleanup(cleanupCtx)
			cancel()
			if err != nil {
				api.logger.Error("expired update cleanup failed", "error", err)
			}
		}
	}
}
