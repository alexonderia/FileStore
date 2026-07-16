package config

import (
	"testing"
	"time"
)

func TestLoadAPIPrecedence(t *testing.T) {
	environment := map[string]string{
		"FILESTORE_API_LISTEN":              ":9000",
		"FILESTORE_API_READ_HEADER_TIMEOUT": "7s",
		"FILESTORE_MAX_FILE_SIZE":           "2048",
		"FILESTORE_TEXT_ENCODINGS":          "utf-8,windows-1251,utf-8",
	}
	getenv := func(name string) string { return environment[name] }

	cfg, err := LoadAPI([]string{"--listen=:7000"}, getenv)
	if err != nil {
		t.Fatalf("LoadAPI() error = %v", err)
	}
	if cfg.ListenAddress != ":7000" {
		t.Fatalf("ListenAddress = %q, want :7000", cfg.ListenAddress)
	}
	if cfg.ReadHeaderTimeout != 7*time.Second {
		t.Fatalf("ReadHeaderTimeout = %s, want 7s", cfg.ReadHeaderTimeout)
	}
	if cfg.MaxFileSize != 2048 {
		t.Fatalf("MaxFileSize = %d, want 2048", cfg.MaxFileSize)
	}
	if len(cfg.TextEncodings) != 2 || cfg.TextEncodings[0] != "utf-8" || cfg.TextEncodings[1] != "windows-1251" {
		t.Fatalf("TextEncodings = %v", cfg.TextEncodings)
	}
}

func TestLoadAPIRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name string
		args []string
		env  map[string]string
	}{
		{name: "empty listen", args: []string{"--listen="}},
		{name: "zero timeout", args: []string{"--shutdown-timeout=0s"}},
		{name: "invalid environment duration", env: map[string]string{"FILESTORE_API_SHUTDOWN_TIMEOUT": "later"}},
		{name: "invalid size", env: map[string]string{"FILESTORE_MAX_FILE_SIZE": "large"}},
		{name: "zero diff lines", args: []string{"--diff-max-lines=0"}},
		{name: "unsupported encoding", args: []string{"--text-encodings=utf-8,koi8-r"}},
		{name: "missing utf8", args: []string{"--text-encodings=windows-1251"}},
		{name: "positional argument", args: []string{"unexpected"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			getenv := func(name string) string { return test.env[name] }
			if _, err := LoadAPI(test.args, getenv); err == nil {
				t.Fatal("LoadAPI() error = nil, want error")
			}
		})
	}
}
