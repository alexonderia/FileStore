package config

import (
	"testing"
	"time"
)

func TestLoadAPIPrecedence(t *testing.T) {
	environment := map[string]string{
		"FILESTORE_API_LISTEN":              ":9000",
		"FILESTORE_API_READ_HEADER_TIMEOUT": "7s",
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
