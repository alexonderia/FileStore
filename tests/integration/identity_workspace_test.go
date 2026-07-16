package integration

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/alexonderia/filestore/internal/auth"
	"github.com/alexonderia/filestore/internal/database"
	"github.com/alexonderia/filestore/internal/domain"
	"github.com/alexonderia/filestore/internal/repository/postgres"
	"github.com/alexonderia/filestore/internal/service"
)

func TestIdentityAndWorkspaceJourney(t *testing.T) {
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
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatalf("repeat migration: %v", err)
	}
	var baseCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM workspaces WHERE kind = 'base'`).Scan(&baseCount); err != nil || baseCount != 1 {
		t.Fatalf("base workspace count = %d, error = %v", baseCount, err)
	}

	prefix := fmt.Sprintf("stage1-%d", time.Now().UnixNano())
	defer func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM workspaces WHERE lower(name) LIKE lower($1)`, prefix+"-%")
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE email::text LIKE $1`, prefix+"-%@example.test")
	}()
	hasher := auth.PasswordHasher{Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}
	users := postgres.NewUsers(pool)
	identity := service.NewIdentity(users, hasher, time.Hour)
	workspaces := service.NewWorkspace(postgres.NewWorkspaces(pool), slog.New(slog.NewTextHandler(io.Discard, nil)))
	password := "correct horse battery staple"

	ownerAuth, err := identity.Register(ctx, "Owner", prefix+"-owner@example.test", password)
	if err != nil {
		t.Fatal(err)
	}
	if ownerAuth.User.IsSuperadmin {
		t.Fatal("public registrant became superadmin")
	}
	if _, err := identity.Register(ctx, "Duplicate", prefix+"-OWNER@example.test", password); !errors.Is(err, service.ErrConflict) {
		t.Fatalf("duplicate registration error = %v, want conflict", err)
	}
	loggedIn, err := identity.Login(ctx, prefix+"-OWNER@example.test", password)
	if err != nil || loggedIn.User.ID != ownerAuth.User.ID {
		t.Fatalf("login = %#v, %v", loggedIn, err)
	}
	actor, err := identity.Authenticate(ctx, loggedIn.Token)
	if err != nil || actor.User.ID != ownerAuth.User.ID {
		t.Fatalf("authenticate = %#v, %v", actor, err)
	}
	if err := identity.Logout(ctx, loggedIn.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := identity.Authenticate(ctx, loggedIn.Token); !errors.Is(err, service.ErrUnauthorized) {
		t.Fatalf("revoked token error = %v, want unauthorized", err)
	}

	base, err := workspaces.Base(ctx, ownerAuth.User)
	if err != nil || base.Kind != domain.WorkspaceBase {
		t.Fatalf("base = %#v, %v", base, err)
	}
	workspace, err := workspaces.Create(ctx, ownerAuth.User, prefix+"-Workspace")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := workspaces.Create(ctx, ownerAuth.User, prefix+"-workspace"); !errors.Is(err, service.ErrConflict) {
		t.Fatalf("duplicate workspace error = %v, want conflict", err)
	}

	editorAuth, err := identity.Register(ctx, "Editor", prefix+"-editor@example.test", password)
	if err != nil {
		t.Fatal(err)
	}
	member, err := workspaces.PutMember(ctx, ownerAuth.User, workspace.ID, "", editorAuth.User.Email, domain.RoleEditor)
	if err != nil || member.Role != domain.RoleEditor {
		t.Fatalf("put editor = %#v, %v", member, err)
	}
	if _, err := workspaces.Get(ctx, editorAuth.User, workspace.ID); err != nil {
		t.Fatalf("editor cannot read workspace: %v", err)
	}
	if _, err := workspaces.PutMember(ctx, editorAuth.User, workspace.ID, ownerAuth.User.ID, "", domain.RoleViewer); !errors.Is(err, service.ErrForbidden) {
		t.Fatalf("editor membership error = %v, want forbidden", err)
	}
	if err := workspaces.RemoveMember(ctx, ownerAuth.User, workspace.ID, ownerAuth.User.ID); !errors.Is(err, service.ErrConflict) {
		t.Fatalf("last owner removal error = %v, want conflict", err)
	}
	adminEmail := prefix + "-admin@example.test"
	if _, err := service.NewBootstrap(users, hasher).Superadmin(ctx, "Admin", adminEmail, password); err != nil {
		t.Fatal(err)
	}
	adminAuth, err := identity.Login(ctx, adminEmail, password)
	if err != nil || !adminAuth.User.IsSuperadmin {
		t.Fatalf("superadmin login = %#v, %v", adminAuth, err)
	}
	if _, err := workspaces.Get(ctx, adminAuth.User, workspace.ID); err != nil {
		t.Fatalf("superadmin cannot read private workspace: %v", err)
	}
	if _, err := workspaces.PutMember(ctx, adminAuth.User, workspace.ID, editorAuth.User.ID, "", domain.RoleViewer); err != nil {
		t.Fatalf("superadmin cannot manage membership: %v", err)
	}
	if _, err := workspaces.PutMember(ctx, ownerAuth.User, workspace.ID, editorAuth.User.ID, "", domain.RoleEditor); err != nil {
		t.Fatal(err)
	}

	secondOwnerAuth, err := identity.Register(ctx, "Second Owner", prefix+"-second@example.test", password)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := workspaces.PutMember(ctx, ownerAuth.User, workspace.ID, secondOwnerAuth.User.ID, "", domain.RoleOwner); err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	errorsFound := make(chan error, 2)
	wait.Add(2)
	go func() {
		defer wait.Done()
		errorsFound <- workspaces.RemoveMember(ctx, ownerAuth.User, workspace.ID, secondOwnerAuth.User.ID)
	}()
	go func() {
		defer wait.Done()
		errorsFound <- workspaces.RemoveMember(ctx, secondOwnerAuth.User, workspace.ID, ownerAuth.User.ID)
	}()
	wait.Wait()
	close(errorsFound)
	successes := 0
	for err := range errorsFound {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent owner removals successes = %d, want 1", successes)
	}
	var owners int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM workspace_members WHERE workspace_id = $1 AND role = 'owner'`, workspace.ID).Scan(&owners); err != nil || owners != 1 {
		t.Fatalf("owners = %d, err = %v, want exactly one", owners, err)
	}
}
