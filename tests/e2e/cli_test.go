package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	httpapi "github.com/alexonderia/filestore/internal/api/http"
	"github.com/alexonderia/filestore/internal/auth"
	"github.com/alexonderia/filestore/internal/cli"
	"github.com/alexonderia/filestore/internal/config"
	"github.com/alexonderia/filestore/internal/database"
	"github.com/alexonderia/filestore/internal/domain"
	"github.com/alexonderia/filestore/internal/repository/postgres"
	"github.com/alexonderia/filestore/internal/service"
)

func TestCLIIdentityWorkspaceJourney(t *testing.T) {
	databaseURL := os.Getenv("FILESTORE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("FILESTORE_TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := database.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	prefix := fmt.Sprintf("e2e-%d", time.Now().UnixNano())
	defer func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM workspaces WHERE lower(name) LIKE lower($1)`, prefix+"-%")
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE email::text LIKE $1`, prefix+"-%@example.test")
	}()

	hasher := auth.PasswordHasher{Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}
	identity := service.NewIdentity(postgres.NewUsers(pool), hasher, time.Hour)
	workspaces := service.NewWorkspace(postgres.NewWorkspaces(pool), slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := httptest.NewServer(httpapi.NewProductHandler(identity, workspaces))
	defer server.Close()

	ownerConfig := filepath.Join(t.TempDir(), "owner.json")
	editorConfig := filepath.Join(t.TempDir(), "editor.json")
	if err := config.SaveClient(ownerConfig, config.Client{APIURL: server.URL}); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveClient(editorConfig, config.Client{APIURL: server.URL}); err != nil {
		t.Fatal(err)
	}
	password := "correct horse battery staple\n"

	ownerOutput := runCLI(t, ownerConfig, password, "register", "--name", "Owner", "--email", prefix+"-owner@example.test", "--password-stdin")
	var owner domain.User
	if err := json.Unmarshal([]byte(ownerOutput), &owner); err != nil {
		t.Fatal(err)
	}
	workspaceOutput := runCLI(t, ownerConfig, "", "workspace", "create", prefix+"-workspace")
	var workspace domain.Workspace
	if err := json.Unmarshal([]byte(workspaceOutput), &workspace); err != nil {
		t.Fatal(err)
	}
	editorOutput := runCLI(t, editorConfig, password, "register", "--name", "Editor", "--email", prefix+"-editor@example.test", "--password-stdin")
	var editor domain.User
	if err := json.Unmarshal([]byte(editorOutput), &editor); err != nil {
		t.Fatal(err)
	}
	runCLI(t, ownerConfig, "", "workspace", "member", "add", workspace.ID, editor.Email, "editor")
	runCLI(t, editorConfig, "", "workspace", "use", workspace.ID)
	meOutput := runCLI(t, editorConfig, "", "auth", "me")
	var current domain.User
	if err := json.Unmarshal([]byte(meOutput), &current); err != nil || current.ID != editor.ID {
		t.Fatalf("auth me user = %#v, error = %v", current, err)
	}
	runCLI(t, ownerConfig, "", "workspace", "member", "remove", workspace.ID, editor.ID)
	runCLI(t, ownerConfig, "", "logout")
	if owner.ID == "" || workspace.ID == "" {
		t.Fatal("CLI returned incomplete resources")
	}
}

func runCLI(t *testing.T, configPath, stdin string, arguments ...string) string {
	t.Helper()
	getenv := func(name string) string {
		if name == "FILESTORE_CONFIG" {
			return configPath
		}
		return ""
	}
	var stdout, stderr bytes.Buffer
	code := cli.RunWithInput(arguments, bytes.NewBufferString(stdin), &stdout, &stderr, getenv, "test")
	if code != 0 {
		t.Fatalf("filestore %v code = %d, stderr = %q", arguments, code, stderr.String())
	}
	return stdout.String()
}
