package config

import (
	"errors"
	"flag"
	"io"
	"strconv"
	"strings"
	"time"
)

const (
	defaultListenAddress     = ":8080"
	defaultReadHeaderTimeout = 5 * time.Second
	defaultShutdownTimeout   = 10 * time.Second
	defaultAuthTokenTTL      = 24 * time.Hour
	defaultMaxFileSize       = int64(100 * 1024 * 1024)
	defaultUpdateSessionTTL  = 24 * time.Hour
	defaultDiffMaxInputBytes = int64(1024 * 1024)
	defaultDiffMaxLines      = 20_000
	defaultDiffMaxOutput     = int64(1024 * 1024)
	defaultOrphanGracePeriod = 24 * time.Hour
	defaultTextEncodings     = "utf-8,utf-16le,utf-16be,windows-1251"
)

// API contains process-level settings for the HTTP API.
type API struct {
	ListenAddress     string
	ReadHeaderTimeout time.Duration
	ShutdownTimeout   time.Duration
	DatabaseURL       string
	AuthTokenTTL      time.Duration
	MaxFileSize       int64
	UpdateSessionTTL  time.Duration
	DiffMaxInputBytes int64
	DiffMaxLines      int
	DiffMaxOutput     int64
	OrphanGracePeriod time.Duration
	TextEncodings     []string
	S3Endpoint        string
	S3Region          string
	S3Bucket          string
	S3AccessKey       string
	S3SecretKey       string
}

// LoadAPI applies defaults, environment variables, and flags in that order.
func LoadAPI(args []string, getenv func(string) string) (API, error) {
	cfg := API{
		ListenAddress:     defaultListenAddress,
		ReadHeaderTimeout: defaultReadHeaderTimeout,
		ShutdownTimeout:   defaultShutdownTimeout,
		AuthTokenTTL:      defaultAuthTokenTTL,
		MaxFileSize:       defaultMaxFileSize,
		UpdateSessionTTL:  defaultUpdateSessionTTL,
		DiffMaxInputBytes: defaultDiffMaxInputBytes,
		DiffMaxLines:      defaultDiffMaxLines,
		DiffMaxOutput:     defaultDiffMaxOutput,
		OrphanGracePeriod: defaultOrphanGracePeriod,
		TextEncodings:     strings.Split(defaultTextEncodings, ","),
		S3Region:          "us-east-1",
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
	if err := durationFromEnv(getenv, "FILESTORE_AUTH_TOKEN_TTL", &cfg.AuthTokenTTL); err != nil {
		return API{}, err
	}
	if err := int64FromEnv(getenv, "FILESTORE_MAX_FILE_SIZE", &cfg.MaxFileSize); err != nil {
		return API{}, err
	}
	if err := durationFromEnv(getenv, "FILESTORE_UPDATE_SESSION_TTL", &cfg.UpdateSessionTTL); err != nil {
		return API{}, err
	}
	if err := int64FromEnv(getenv, "FILESTORE_DIFF_MAX_INPUT_BYTES", &cfg.DiffMaxInputBytes); err != nil {
		return API{}, err
	}
	if err := intFromEnv(getenv, "FILESTORE_DIFF_MAX_LINES", &cfg.DiffMaxLines); err != nil {
		return API{}, err
	}
	if err := int64FromEnv(getenv, "FILESTORE_DIFF_MAX_OUTPUT_BYTES", &cfg.DiffMaxOutput); err != nil {
		return API{}, err
	}
	if err := durationFromEnv(getenv, "FILESTORE_ORPHAN_GRACE_PERIOD", &cfg.OrphanGracePeriod); err != nil {
		return API{}, err
	}
	encodings := strings.TrimSpace(getenv("FILESTORE_TEXT_ENCODINGS"))
	if encodings == "" {
		encodings = strings.Join(cfg.TextEncodings, ",")
	}
	cfg.S3Endpoint = strings.TrimSpace(getenv("FILESTORE_S3_ENDPOINT"))
	if value := strings.TrimSpace(getenv("FILESTORE_S3_REGION")); value != "" {
		cfg.S3Region = value
	}
	cfg.S3Bucket = strings.TrimSpace(getenv("FILESTORE_S3_BUCKET"))
	cfg.S3AccessKey = strings.TrimSpace(getenv("FILESTORE_S3_ACCESS_KEY"))
	cfg.S3SecretKey = strings.TrimSpace(getenv("FILESTORE_S3_SECRET_KEY"))

	flags := flag.NewFlagSet("filestore-api", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&cfg.ListenAddress, "listen", cfg.ListenAddress, "HTTP listen address")
	flags.DurationVar(&cfg.ReadHeaderTimeout, "read-header-timeout", cfg.ReadHeaderTimeout, "HTTP header read timeout")
	flags.DurationVar(&cfg.ShutdownTimeout, "shutdown-timeout", cfg.ShutdownTimeout, "graceful shutdown timeout")
	flags.StringVar(&cfg.DatabaseURL, "database-url", cfg.DatabaseURL, "PostgreSQL connection URL")
	flags.DurationVar(&cfg.AuthTokenTTL, "auth-token-ttl", cfg.AuthTokenTTL, "authentication token lifetime")
	flags.Int64Var(&cfg.MaxFileSize, "max-file-size", cfg.MaxFileSize, "maximum upload size in bytes")
	flags.DurationVar(&cfg.UpdateSessionTTL, "update-session-ttl", cfg.UpdateSessionTTL, "update session lifetime")
	flags.Int64Var(&cfg.DiffMaxInputBytes, "diff-max-input-bytes", cfg.DiffMaxInputBytes, "maximum decoded bytes per diff side")
	flags.IntVar(&cfg.DiffMaxLines, "diff-max-lines", cfg.DiffMaxLines, "maximum lines per diff side")
	flags.Int64Var(&cfg.DiffMaxOutput, "diff-max-output-bytes", cfg.DiffMaxOutput, "maximum unified diff output bytes")
	flags.DurationVar(&cfg.OrphanGracePeriod, "orphan-grace-period", cfg.OrphanGracePeriod, "minimum orphan age before deletion")
	flags.StringVar(&encodings, "text-encodings", encodings, "comma-separated canonical text encoding allowlist")
	flags.StringVar(&cfg.S3Endpoint, "s3-endpoint", cfg.S3Endpoint, "S3-compatible endpoint URL")
	flags.StringVar(&cfg.S3Region, "s3-region", cfg.S3Region, "S3 region")
	flags.StringVar(&cfg.S3Bucket, "s3-bucket", cfg.S3Bucket, "S3 bucket")
	flags.StringVar(&cfg.S3AccessKey, "s3-access-key", cfg.S3AccessKey, "S3 access key")
	flags.StringVar(&cfg.S3SecretKey, "s3-secret-key", cfg.S3SecretKey, "S3 secret key")
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
	if cfg.AuthTokenTTL <= 0 {
		return API{}, errors.New("auth token TTL must be positive")
	}
	if cfg.MaxFileSize <= 0 {
		return API{}, errors.New("maximum file size must be positive")
	}
	if cfg.UpdateSessionTTL <= 0 {
		return API{}, errors.New("update session TTL must be positive")
	}
	if cfg.DiffMaxInputBytes <= 0 || cfg.DiffMaxLines <= 0 || cfg.DiffMaxOutput <= 0 {
		return API{}, errors.New("diff limits must be positive")
	}
	if cfg.OrphanGracePeriod <= 0 {
		return API{}, errors.New("orphan grace period must be positive")
	}
	parsedEncodings, err := parseTextEncodings(encodings)
	if err != nil {
		return API{}, err
	}
	cfg.TextEncodings = parsedEncodings
	if err := validateS3(cfg); err != nil {
		return API{}, err
	}
	return cfg, nil
}

func (cfg API) StorageConfigured() bool {
	return cfg.S3Endpoint != "" && cfg.S3Bucket != ""
}

func validateS3(cfg API) error {
	configured := cfg.S3Endpoint != "" || cfg.S3Bucket != "" || cfg.S3AccessKey != "" || cfg.S3SecretKey != ""
	if !configured {
		return nil
	}
	if cfg.S3Endpoint == "" || cfg.S3Bucket == "" || cfg.S3Region == "" {
		return errors.New("S3 endpoint, region, and bucket must be configured together")
	}
	if (cfg.S3AccessKey == "") != (cfg.S3SecretKey == "") {
		return errors.New("S3 access key and secret key must be configured together")
	}
	return nil
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

func int64FromEnv(getenv func(string) string, name string, target *int64) error {
	value := strings.TrimSpace(getenv(name))
	if value == "" {
		return nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return errors.New(name + " must be a valid integer")
	}
	*target = parsed
	return nil
}

func intFromEnv(getenv func(string) string, name string, target *int) error {
	value := strings.TrimSpace(getenv(name))
	if value == "" {
		return nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return errors.New(name + " must be a valid integer")
	}
	*target = parsed
	return nil
}

func parseTextEncodings(value string) ([]string, error) {
	supported := map[string]bool{"utf-8": true, "utf-16le": true, "utf-16be": true, "windows-1251": true}
	seen := make(map[string]bool)
	result := make([]string, 0, 4)
	for _, item := range strings.Split(value, ",") {
		item = strings.ToLower(strings.TrimSpace(item))
		if !supported[item] {
			return nil, errors.New("text encodings must contain only utf-8, utf-16le, utf-16be, windows-1251")
		}
		if !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}
	if len(result) == 0 || !seen["utf-8"] {
		return nil, errors.New("text encodings must include utf-8")
	}
	return result, nil
}
