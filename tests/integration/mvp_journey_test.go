package integration

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	httpapi "github.com/alexonderia/filestore/internal/api/http"
	"github.com/alexonderia/filestore/internal/auth"
	"github.com/alexonderia/filestore/internal/client"
	"github.com/alexonderia/filestore/internal/config"
	"github.com/alexonderia/filestore/internal/database"
	"github.com/alexonderia/filestore/internal/repository/postgres"
	"github.com/alexonderia/filestore/internal/service"
	"github.com/alexonderia/filestore/internal/storage/seaweedfs"
)

func TestMVPUpdateLockAndLinkJourney(t *testing.T) {
	databaseURL, endpoint := os.Getenv("FILESTORE_TEST_DATABASE_URL"), os.Getenv("FILESTORE_TEST_S3_ENDPOINT")
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
	objects, err := seaweedfs.New(ctx, config.API{S3Endpoint: endpoint, S3Region: "us-east-1", S3Bucket: "filestore", S3AccessKey: "filestore", S3SecretKey: "filestore-local"})
	if err != nil {
		t.Fatal(err)
	}
	if err := objects.EnsureBucket(ctx); err != nil {
		t.Fatal(err)
	}

	prefix := fmt.Sprintf("mvp-%d", time.Now().UnixNano())
	defer func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE email::text LIKE $1`, prefix+"-%@example.test")
	}()
	hasher := auth.PasswordHasher{Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}
	identity := service.NewIdentity(postgres.NewUsers(pool), hasher, time.Hour)
	workspaceRepository := postgres.NewWorkspaces(pool)
	workspaces := service.NewWorkspace(workspaceRepository, slog.New(slog.NewTextHandler(io.Discard, nil)))
	files := service.NewFiles(postgres.NewFiles(pool), workspaceRepository, objects, 1024*1024, []string{"utf-8", "windows-1251"})
	updates := service.NewUpdates(postgres.NewUpdates(pool), files, objects, time.Hour, time.Hour, 1024*1024, 1024*1024, 20000, 1024*1024)
	locks := service.NewLocks(postgres.NewLocks(pool), files)
	links := service.NewLinks(postgres.NewLinks(pool), files, objects)
	beforeKeys, err := objects.List(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	before := make(map[string]bool, len(beforeKeys))
	for _, key := range beforeKeys {
		before[key] = true
	}
	owner, err := identity.Register(ctx, "Owner", prefix+"-owner@example.test", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.NewFullHandler(identity, workspaces, files, updates, locks, links, 1024*1024))
	defer server.Close()
	api := client.New(server.URL, owner.Token)
	base, err := workspaces.Base(ctx, owner.User)
	if err != nil {
		t.Fatal(err)
	}
	first, second := []byte("first line\n"), []byte("second line\n")
	file, err := files.Create(ctx, owner.User, base.ID, prefix+".txt", "utf-8", "v1.txt", bytes.NewReader(first))
	if err != nil {
		t.Fatal(err)
	}
	createdFileIDs := []string{file.ID}
	createdWorkspaceIDs := []string{}
	defer func() {
		keys, _ := objects.List(context.Background(), "")
		for _, id := range createdFileIDs {
			_, _ = pool.Exec(context.Background(), `DELETE FROM files WHERE id=$1`, id)
		}
		for _, id := range createdWorkspaceIDs {
			_, _ = pool.Exec(context.Background(), `DELETE FROM workspaces WHERE id=$1`, id)
		}
		for _, key := range keys {
			if !before[key] {
				_ = objects.Delete(context.Background(), key)
				_, _ = pool.Exec(context.Background(), `DELETE FROM storage_objects WHERE object_key=$1`, key)
			}
		}
	}()

	session, err := updates.Create(ctx, owner.User, file.ID, "idempotency-key-0001", "v2.txt", bytes.NewReader(second))
	if err != nil {
		t.Fatal(err)
	}
	retry, err := updates.Create(ctx, owner.User, file.ID, "idempotency-key-0001", "ignored.txt", bytes.NewReader([]byte("ignored")))
	if err != nil || retry.ID != session.ID {
		t.Fatalf("idempotent create = %#v, %v", retry, err)
	}
	diff, err := updates.Diff(ctx, owner.User, file.ID, session.ID)
	if err != nil || diff.Kind != "text" || diff.UnifiedDiff == "" {
		t.Fatalf("diff = %#v, %v", diff, err)
	}
	unchanged, _ := files.Get(ctx, owner.User, file.ID)
	if unchanged.CurrentVersion.VersionNumber != 1 {
		t.Fatalf("candidate changed current version: %#v", unchanged)
	}
	version, err := updates.Resolve(ctx, owner.User, file.ID, session.ID)
	if err != nil || version.VersionNumber != 2 {
		t.Fatalf("resolve = %#v, %v", version, err)
	}
	resolvedAgain, err := updates.Resolve(ctx, owner.User, file.ID, session.ID)
	if err != nil || resolvedAgain.ID != version.ID {
		t.Fatalf("idempotent resolve = %#v, %v", resolvedAgain, err)
	}
	_, object, err := files.Download(ctx, owner.User, file.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(object.Body)
	_ = object.Body.Close()
	if !bytes.Equal(got, second) {
		t.Fatalf("resolved bytes = %q", got)
	}

	page, err := links.List(ctx, owner.User, file.ID, "", 50)
	if err != nil || len(page.Items) != 3 {
		t.Fatalf("links = %#v, %v", page, err)
	}
	firstPage, err := api.LinksPage(ctx, file.ID, "", 1)
	if err != nil || len(firstPage.Items) != 1 || firstPage.NextCursor == "" {
		t.Fatalf("first link page = %#v, %v", firstPage, err)
	}
	secondPage, err := api.LinksPage(ctx, file.ID, firstPage.NextCursor, 1)
	if err != nil || len(secondPage.Items) != 1 || secondPage.Items[0].ID == firstPage.Items[0].ID {
		t.Fatalf("second link page = %#v, %v", secondPage, err)
	}
	var currentToken, firstLinkID, firstToken string
	for _, link := range page.Items {
		if link.Kind == "current" {
			currentToken = link.Token
		}
		if link.VersionID == file.CurrentVersion.ID {
			firstLinkID, firstToken = link.ID, link.Token
		}
	}
	_, linked, err := links.Download(ctx, nil, currentToken)
	if err != nil {
		t.Fatal(err)
	}
	got, _ = io.ReadAll(linked.Body)
	_ = linked.Body.Close()
	if !bytes.Equal(got, second) {
		t.Fatalf("current link bytes = %q", got)
	}
	_, linked, err = links.Download(ctx, nil, firstToken)
	if err != nil {
		t.Fatal(err)
	}
	got, _ = io.ReadAll(linked.Body)
	_ = linked.Body.Close()
	if !bytes.Equal(got, first) {
		t.Fatalf("version link bytes = %q", got)
	}

	privateWorkspace, err := workspaces.Create(ctx, owner.User, prefix+"-private")
	if err != nil {
		t.Fatal(err)
	}
	createdWorkspaceIDs = append(createdWorkspaceIDs, privateWorkspace.ID)
	privateFile, err := files.Create(ctx, owner.User, privateWorkspace.ID, "private.txt", "utf-8", "private.txt", bytes.NewReader([]byte("private")))
	if err != nil {
		t.Fatal(err)
	}
	createdFileIDs = append(createdFileIDs, privateFile.ID)
	privateLinks, err := api.Links(ctx, privateFile.ID)
	if err != nil {
		t.Fatal(err)
	}
	privateToken := privateLinks.Items[0].Token
	if err := client.New(server.URL, "").DownloadLink(ctx, privateToken, io.Discard); apiErrorStatus(err) != http.StatusUnauthorized {
		t.Fatalf("anonymous private link = %v", err)
	}
	var privateBytes bytes.Buffer
	if err := api.DownloadLink(ctx, privateToken, &privateBytes); err != nil || privateBytes.String() != "private" {
		t.Fatalf("member private link = %q, %v", privateBytes.String(), err)
	}

	if _, err := api.Lock(ctx, file.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := api.SetEncoding(ctx, file.ID, "windows-1251"); apiErrorStatus(err) != http.StatusLocked {
		t.Fatalf("encoding under lock = %v", err)
	}
	if err := api.RevokeLink(ctx, file.ID, firstLinkID); apiErrorStatus(err) != http.StatusLocked {
		t.Fatalf("revoke under lock = %v", err)
	}
	if err := api.Unlock(ctx, file.ID); err != nil {
		t.Fatal(err)
	}
	if err := api.RevokeLink(ctx, file.ID, firstLinkID); err != nil {
		t.Fatal(err)
	}
	if err := api.DownloadLink(ctx, firstToken, io.Discard); apiErrorStatus(err) != http.StatusNotFound {
		t.Fatalf("revoked link = %v", err)
	}

	rejected, err := api.CreateUpdate(ctx, file.ID, "idempotency-key-0002", "rejected.txt", bytes.NewReader([]byte("reject me")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := api.RejectUpdate(ctx, file.ID, rejected.ID); err != nil {
		t.Fatal(err)
	}

	t.Run("only one concurrent update session wins", func(t *testing.T) {
		type result struct {
			sessionID string
			err       error
		}
		results := make(chan result, 2)
		start := make(chan struct{})
		var wait sync.WaitGroup
		for index := 0; index < 2; index++ {
			index := index
			wait.Add(1)
			go func() {
				defer wait.Done()
				<-start
				session, err := updates.Create(ctx, owner.User, file.ID, fmt.Sprintf("concurrent-key-%04d", index), "race.txt", bytes.NewReader([]byte("race")))
				results <- result{sessionID: session.ID, err: err}
			}()
		}
		close(start)
		wait.Wait()
		close(results)
		var winner string
		for value := range results {
			if value.err == nil {
				if winner != "" {
					t.Fatal("two update sessions succeeded")
				}
				winner = value.sessionID
			} else if !errors.Is(value.err, service.ErrConflict) {
				t.Fatalf("loser error = %v", value.err)
			}
		}
		if winner == "" {
			t.Fatal("no update session succeeded")
		}
		if _, err := updates.Reject(ctx, owner.User, file.ID, winner); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("lock and update session are mutually exclusive", func(t *testing.T) {
		start := make(chan struct{})
		var wait sync.WaitGroup
		var lockValueID, sessionID string
		var lockErr, sessionErr error
		wait.Add(2)
		go func() {
			defer wait.Done()
			<-start
			value, err := locks.Create(ctx, owner.User, file.ID)
			lockValueID, lockErr = value.ID, err
		}()
		go func() {
			defer wait.Done()
			<-start
			value, err := updates.Create(ctx, owner.User, file.ID, "lock-race-key-0001", "race.txt", bytes.NewReader([]byte("race")))
			sessionID, sessionErr = value.ID, err
		}()
		close(start)
		wait.Wait()
		if (lockErr == nil) == (sessionErr == nil) {
			t.Fatalf("lock=%q/%v session=%q/%v", lockValueID, lockErr, sessionID, sessionErr)
		}
		if lockErr == nil {
			if !errors.Is(sessionErr, service.ErrLocked) {
				t.Fatalf("session error = %v", sessionErr)
			}
			if _, err := locks.Release(ctx, owner.User, file.ID); err != nil {
				t.Fatal(err)
			}
		} else {
			if !errors.Is(lockErr, service.ErrConflict) {
				t.Fatalf("lock error = %v", lockErr)
			}
			if _, err := updates.Reject(ctx, owner.User, file.ID, sessionID); err != nil {
				t.Fatal(err)
			}
		}
	})

	t.Run("expired candidate is cleaned", func(t *testing.T) {
		session, err := updates.Create(ctx, owner.User, file.ID, "expiry-test-key-0001", "expired.txt", bytes.NewReader([]byte("expired")))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE file_update_sessions SET expires_at=clock_timestamp()-interval '1 minute' WHERE id=$1`, session.ID); err != nil {
			t.Fatal(err)
		}
		if err := updates.CleanupExpired(ctx); err != nil {
			t.Fatal(err)
		}
		stored, err := postgres.NewUpdates(pool).Get(ctx, file.ID, session.ID)
		if err != nil || stored.Status != "expired" {
			t.Fatalf("expired session = %#v, %v", stored, err)
		}
		if _, err := objects.Get(ctx, session.CandidateKey); err == nil {
			t.Fatal("expired candidate object still exists")
		}
	})
}

func apiErrorStatus(err error) int {
	var value *client.APIError
	if errors.As(err, &value) {
		return value.Status
	}
	return 0
}
