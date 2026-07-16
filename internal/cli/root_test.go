package cli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alexonderia/filestore/internal/config"
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

func TestLoginStoresButDoesNotPrintToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/auth/login" {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"token":"raw-secret-token","user":{"id":"00000000-0000-4000-8000-000000000002","name":"User","email":"user@example.test","is_superadmin":false,"created_at":"2026-07-16T00:00:00Z"}}`))
	}))
	defer server.Close()
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := config.SaveClient(configPath, config.Client{APIURL: server.URL}); err != nil {
		t.Fatal(err)
	}
	getenv := func(name string) string {
		if name == "FILESTORE_CONFIG" {
			return configPath
		}
		return ""
	}
	var stdout, stderr bytes.Buffer
	code := RunWithInput([]string{"login", "--email", "user@example.test", "--password-stdin"}, strings.NewReader("correct horse battery staple\n"), &stdout, &stderr, getenv, "test")
	if code != 0 {
		t.Fatalf("login code = %d, stderr = %q", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "raw-secret-token") {
		t.Fatal("raw token was printed")
	}
	cfg, err := config.LoadClient(configPath)
	if err != nil || cfg.Token != "raw-secret-token" {
		t.Fatalf("stored token = %q, error = %v", cfg.Token, err)
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
