package config

import (
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const defaultAPIURL = "http://localhost:8080"

// Client contains local CLI preferences. Authentication tokens are added in stage 1.
type Client struct {
	APIURL      string `json:"api_url"`
	WorkspaceID string `json:"workspace_id,omitempty"`
	Token       string `json:"token,omitempty"`
}

func DefaultClient() Client {
	return Client{APIURL: defaultAPIURL}
}

func ClientPath(getenv func(string) string) (string, error) {
	if path := strings.TrimSpace(getenv("FILESTORE_CONFIG")); path != "" {
		return path, nil
	}
	directory, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "filestore", "config.json"), nil
}

func LoadClient(path string) (Client, error) {
	cfg := DefaultClient()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return Client{}, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Client{}, err
	}
	if err := cfg.Validate(); err != nil {
		return Client{}, err
	}
	return cfg, nil
}

func SaveClient(path string, cfg Client) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o600)
}

func (cfg Client) Validate() error {
	parsed, err := url.ParseRequestURI(cfg.APIURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return errors.New("api-url must be an absolute http or https URL")
	}
	if parsed.User != nil {
		return errors.New("api-url must not contain credentials")
	}
	return nil
}
