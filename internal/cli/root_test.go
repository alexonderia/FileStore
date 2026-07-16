package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestHelpAndVersion(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{args: nil, want: "FileStore CLI"},
		{args: []string{"version"}, want: "test-version"},
	}
	for _, test := range tests {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		if code := Run(test.args, &stdout, &stderr, func(string) string { return "" }, "test-version"); code != 0 {
			t.Fatalf("Run(%v) code = %d, stderr = %q", test.args, code, stderr.String())
		}
		if !strings.Contains(stdout.String(), test.want) {
			t.Fatalf("Run(%v) output = %q, want substring %q", test.args, stdout.String(), test.want)
		}
	}
}

func TestConfigSetAndGet(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	getenv := func(name string) string {
		if name == "FILESTORE_CONFIG" {
			return configPath
		}
		return ""
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := Run([]string{"config", "set", "api-url", "https://files.example.test"}, &stdout, &stderr, getenv, "test"); code != 0 {
		t.Fatalf("set code = %d, stderr = %q", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"config", "get", "api-url"}, &stdout, &stderr, getenv, "test"); code != 0 {
		t.Fatalf("get code = %d, stderr = %q", code, stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != "https://files.example.test" {
		t.Fatalf("api-url = %q", got)
	}
}

func TestConfigRejectsInvalidURL(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	getenv := func(name string) string {
		if name == "FILESTORE_CONFIG" {
			return configPath
		}
		return ""
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := Run([]string{"config", "set", "api-url", "not-a-url"}, &stdout, &stderr, getenv, "test"); code == 0 {
		t.Fatal("Run() code = 0, want error")
	}
}
