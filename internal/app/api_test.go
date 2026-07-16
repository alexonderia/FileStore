package app

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/alexonderia/filestore/internal/config"
)

func TestAPIGracefulShutdown(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	api := NewAPI(config.API{
		ListenAddress:     "127.0.0.1:0",
		ReadHeaderTimeout: time.Second,
		ShutdownTimeout:   time.Second,
	}, logger)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- api.Run(ctx) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("API did not stop after context cancellation")
	}
}
