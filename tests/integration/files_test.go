package integration

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/alexonderia/filestore/internal/auth"
	"github.com/alexonderia/filestore/internal/config"
	"github.com/alexonderia/filestore/internal/database"
	"github.com/alexonderia/filestore/internal/repository/postgres"
	"github.com/alexonderia/filestore/internal/service"
	"github.com/alexonderia/filestore/internal/storage/seaweedfs"
)

func TestFilesJourney(t *testing.T) {
	databaseURL := os.Getenv("FILESTORE_TEST_DATABASE_URL")
	endpoint := os.Getenv("FILESTORE_TEST_S3_ENDPOINT")
	if databaseURL == "" || endpoint == "" {
		t.Skip("FILESTORE_TEST_DATABASE_URL and FILESTORE_TEST_S3_ENDPOINT are required")
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
	cfg := config.API{
		S3Endpoint:  endpoint,
		S3Region:    "us-east-1",
		S3Bucket:    "filestore",
		S3AccessKey: "filestore",
		S3SecretKey: "filestore-local",
	}
	objects, err := seaweedfs.New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 20; attempt++ {
		err = objects.EnsureBucket(ctx)
		if err == nil {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}

	prefix := fmt.Sprintf("files-%d", time.Now().UnixNano())
	hasher := auth.PasswordHasher{Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}
	identity := service.NewIdentity(postgres.NewUsers(pool), hasher, time.Hour)
	workspaceRepository := postgres.NewWorkspaces(pool)
	workspaces := service.NewWorkspace(workspaceRepository, slog.New(slog.NewTextHandler(io.Discard, nil)))
	files := service.NewFiles(postgres.NewFiles(pool), workspaceRepository, objects, 1024*1024, []string{"utf-8", "windows-1251"})
	password := "correct horse battery staple"
	owner, err := identity.Register(ctx, "Owner", prefix+"-owner@example.test", password)
	if err != nil {
		t.Fatal(err)
	}
	outsider, err := identity.Register(ctx, "Outsider", prefix+"-outsider@example.test", password)
	if err != nil {
		t.Fatal(err)
	}
	base, err := workspaces.Base(ctx, owner.User)
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("hello, immutable world\n")
	file, err := files.Create(ctx, owner.User, base.ID, "Greeting.txt", "utf-8", "greeting.txt", bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = objects.Delete(context.Background(), file.CurrentVersion.ObjectKey)
		_, _ = pool.Exec(context.Background(), `DELETE FROM files WHERE id = $1`, file.ID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM storage_objects WHERE object_key = $1`, file.CurrentVersion.ObjectKey)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE email::text LIKE $1`, prefix+"-%@example.test")
	}()
	if file.CurrentVersion.VersionNumber != 1 || file.CurrentVersion.Size != int64(len(content)) {
		t.Fatalf("file = %#v", file)
	}
	if _, err := files.Get(ctx, outsider.User, file.ID); err == nil {
		t.Fatal("outsider read a base file")
	}
	version, object, err := files.Download(ctx, owner.User, file.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	downloaded, err := io.ReadAll(object.Body)
	_ = object.Body.Close()
	if err != nil || !bytes.Equal(downloaded, content) || version.SHA256 != file.CurrentVersion.SHA256 {
		t.Fatalf("download mismatch: %q, %v", downloaded, err)
	}
	updated, err := files.SetEncoding(ctx, owner.User, file.ID, "windows-1251")
	if err != nil || updated.CurrentVersion.ID != file.CurrentVersion.ID {
		t.Fatalf("encoding update = %#v, %v", updated, err)
	}
	history, err := files.History(ctx, owner.User, file.ID, "", 50)
	if err != nil || len(history.Items) != 1 {
		t.Fatalf("history = %#v, %v", history, err)
	}
	before, _ := objects.List(ctx, "published/")
	if _, err := files.Create(ctx, owner.User, base.ID, "greeting.TXT", "utf-8", "duplicate.txt", bytes.NewReader([]byte("duplicate"))); err == nil {
		t.Fatal("case-insensitive duplicate file succeeded")
	}
	after, _ := objects.List(ctx, "published/")
	if len(after) != len(before) {
		t.Fatalf("duplicate upload leaked an object: before=%d after=%d", len(before), len(after))
	}
}
