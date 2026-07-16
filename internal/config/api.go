package config

import (
	"errors"
	"flag"
	"io"
	"strings"
	"time"
)

const (
	defaultListenAddress     = ":8080"
	defaultReadHeaderTimeout = 5 * time.Second
	defaultShutdownTimeout   = 10 * time.Second
)

// API contains process-level settings for the HTTP API.
type API struct {
	ListenAddress     string
	ReadHeaderTimeout time.Duration
	ShutdownTimeout   time.Duration
	DatabaseURL       string
}

// LoadAPI applies defaults, environment variables, and flags in that order.
func LoadAPI(args []string, getenv func(string) string) (API, error) {
	cfg := API{
		ListenAddress:     defaultListenAddress,
		ReadHeaderTimeout: defaultReadHeaderTimeout,
		ShutdownTimeout:   defaultShutdownTimeout,
	}

	if value := strings.TrimSpace(getenv("FILESTORE_API_LISTEN")); value != "" {
		cfg.ListenAddress = value
	}
	cfg.DatabaseURL = strings.TrimSpace(getenv("FILESTORE_DATABASE_URL"))
	if err := durationFromEnv(getenv, "FILESTORE_API_READ_HEADER_TIMEOUT", &cfg.ReadHeaderTimeout); err != nil {
		return API{}, err
	}
	if err := durationFromEnv(getenv, "FILESTORE_API_SHUTDOWN_TIMEOUT", &cfg.ShutdownTimeout); err != nil {
		return API{}, err
	}

	flags := flag.NewFlagSet("filestore-api", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&cfg.ListenAddress, "listen", cfg.ListenAddress, "HTTP listen address")
	flags.DurationVar(&cfg.ReadHeaderTimeout, "read-header-timeout", cfg.ReadHeaderTimeout, "HTTP header read timeout")
	flags.DurationVar(&cfg.ShutdownTimeout, "shutdown-timeout", cfg.ShutdownTimeout, "graceful shutdown timeout")
	flags.StringVar(&cfg.DatabaseURL, "database-url", cfg.DatabaseURL, "PostgreSQL connection URL")
	if err := flags.Parse(args); err != nil {
		return API{}, err
	}
	if flags.NArg() != 0 {
		return API{}, errors.New("unexpected positional arguments")
	}

	if strings.TrimSpace(cfg.ListenAddress) == "" {
		return API{}, errors.New("listen address must not be empty")
	}
	if cfg.ReadHeaderTimeout <= 0 {
		return API{}, errors.New("read header timeout must be positive")
	}
	if cfg.ShutdownTimeout <= 0 {
		return API{}, errors.New("shutdown timeout must be positive")
	}
	return cfg, nil
}

func durationFromEnv(getenv func(string) string, name string, target *time.Duration) error {
	value := strings.TrimSpace(getenv(name))
	if value == "" {
		return nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return errors.New(name + " must be a valid duration")
	}
	*target = parsed
	return nil
}
