package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDotEnvMissingFile(t *testing.T) {
	t.Setenv("FILESTORE_DATABASE_URL", "")
	if err := LoadDotEnv(filepath.Join(t.TempDir(), ".env")); err != nil {
		t.Fatalf("LoadDotEnv() error = %v", err)
	}
}

func TestLoadDotEnvSetsUnsetValuesOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	data := []byte("# comment\nFILESTORE_DATABASE_URL=postgres://localhost/db\nFILESTORE_API_LISTEN=:9090\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	t.Setenv("FILESTORE_API_LISTEN", ":8088")
	t.Setenv("FILESTORE_DATABASE_URL", "")
	if err := os.Unsetenv("FILESTORE_DATABASE_URL"); err != nil {
		t.Fatalf("Unsetenv() error = %v", err)
	}

	if err := LoadDotEnv(path); err != nil {
		t.Fatalf("LoadDotEnv() error = %v", err)
	}
	if got := os.Getenv("FILESTORE_DATABASE_URL"); got != "postgres://localhost/db" {
		t.Fatalf("FILESTORE_DATABASE_URL = %q", got)
	}
	if got := os.Getenv("FILESTORE_API_LISTEN"); got != ":8088" {
		t.Fatalf("FILESTORE_API_LISTEN = %q", got)
	}
}

func TestLoadDotEnvRejectsInvalidLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("broken-line\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if err := LoadDotEnv(path); err == nil {
		t.Fatal("LoadDotEnv() error = nil, want error")
	}
}
