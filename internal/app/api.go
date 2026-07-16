package app

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	httpapi "github.com/alexonderia/filestore/internal/api/http"
	"github.com/alexonderia/filestore/internal/config"
)

type API struct {
	server          *http.Server
	shutdownTimeout time.Duration
	logger          *slog.Logger
}

func NewAPI(cfg config.API, logger *slog.Logger) *API {
	return &API{
		server: &http.Server{
			Addr:              cfg.ListenAddress,
			Handler:           httpapi.NewHandler(),
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
