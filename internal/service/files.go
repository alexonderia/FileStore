package service

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash"
	"io"
	"net/http"
	"path"
	"strings"

	"github.com/alexonderia/filestore/internal/authorization"
	"github.com/alexonderia/filestore/internal/domain"
	"github.com/alexonderia/filestore/internal/storage"
)

type FileRepository interface {
	Create(context.Context, string, string, string, string, string, domain.StoredObject) (domain.File, error)
	Access(context.Context, string, string) (domain.File, domain.Workspace, *domain.WorkspaceRole, error)
	List(context.Context, string, string, bool, string, int) ([]domain.File, error)
	History(context.Context, string, string, int) ([]domain.FileVersion, error)
	Version(context.Context, string, int) (domain.FileVersion, error)
	SetEncoding(context.Context, string, string) (domain.File, error)
}

type Files struct {
	repository FileRepository
	workspaces WorkspaceRepository
	objects    storage.ObjectStore
	policy     authorization.Policy
	maxSize    int64
	encodings  map[string]bool
}

func NewFiles(repository FileRepository, workspaces WorkspaceRepository, objects storage.ObjectStore, maxSize int64, encodings []string) *Files {
	allowed := make(map[string]bool, len(encodings))
	for _, encoding := range encodings {
		allowed[encoding] = true
	}
	return &Files{repository: repository, workspaces: workspaces, objects: objects, policy: authorization.Policy{}, maxSize: maxSize, encodings: allowed}
}

func (service *Files) Create(ctx context.Context, actor domain.User, workspaceID, name, encoding, originalName string, source io.Reader) (domain.File, error) {
	if !validUUID(workspaceID) || source == nil {
		return domain.File{}, ErrInvalid
	}
	workspace, role, err := service.workspaces.Access(ctx, workspaceID, actor.ID)
	if err != nil {
		return domain.File{}, err
	}
	if !service.policy.CanCreateFile(actor, workspace, role) {
		if role == nil && workspace.Kind == domain.WorkspacePrivate && !actor.IsSuperadmin {
			return domain.File{}, ErrNotFound
		}
		return domain.File{}, ErrForbidden
	}
	name = cleanFilename(name)
	originalName = cleanFilename(originalName)
	if name == "" {
		name = originalName
	}
	encoding = strings.ToLower(strings.TrimSpace(encoding))
	if encoding == "" {
		encoding = "utf-8"
	}
	if name == "" || len(name) > 255 || originalName == "" || len(originalName) > 255 || !service.encodings[encoding] {
		return domain.File{}, ErrInvalid
	}

	buffered := bufio.NewReaderSize(source, 512)
	header, peekErr := buffered.Peek(512)
	if peekErr != nil && !errors.Is(peekErr, io.EOF) && !errors.Is(peekErr, bufio.ErrBufferFull) {
		return domain.File{}, peekErr
	}
	mimeType := http.DetectContentType(header)
	key, err := randomObjectKey("published")
	if err != nil {
		return domain.File{}, err
	}
	reader := newLimitedHashReader(buffered, service.maxSize)
	if err := service.objects.Put(ctx, key, reader, mimeType); err != nil {
		if errors.Is(err, ErrTooLarge) || errors.Is(reader.err, ErrTooLarge) {
			return domain.File{}, ErrTooLarge
		}
		return domain.File{}, err
	}
	object := domain.StoredObject{Key: key, Size: reader.size, SHA256: hex.EncodeToString(reader.hash.Sum(nil)), MIMEType: mimeType}
	file, err := service.repository.Create(ctx, workspaceID, actor.ID, name, encoding, originalName, object)
	if err != nil {
		_ = service.objects.Delete(context.Background(), key)
		return domain.File{}, err
	}
	return file, nil
}

func (service *Files) List(ctx context.Context, actor domain.User, workspaceID, cursor string, limit int) (domain.FilePage, error) {
	if !validUUID(workspaceID) || (cursor != "" && !validUUID(cursor)) {
		return domain.FilePage{}, ErrInvalid
	}
	workspace, role, err := service.workspaces.Access(ctx, workspaceID, actor.ID)
	if err != nil {
		return domain.FilePage{}, err
	}
	if !service.policy.CanReadWorkspace(actor, workspace, role) {
		return domain.FilePage{}, ErrNotFound
	}
	limit = normalizeLimit(limit)
	items, err := service.repository.List(ctx, workspaceID, actor.ID, actor.IsSuperadmin, cursor, limit+1)
	if err != nil {
		return domain.FilePage{}, err
	}
	page := domain.FilePage{Items: items}
	if len(items) > limit {
		page.Items = items[:limit]
		page.NextCursor = items[limit-1].ID
	}
	return page, nil
}

func (service *Files) Get(ctx context.Context, actor domain.User, fileID string) (domain.File, error) {
	file, _, _, err := service.authorize(ctx, actor, fileID, false)
	return file, err
}

func (service *Files) History(ctx context.Context, actor domain.User, fileID, cursor string, limit int) (domain.VersionPage, error) {
	if cursor != "" && !validUUID(cursor) {
		return domain.VersionPage{}, ErrInvalid
	}
	if _, _, _, err := service.authorize(ctx, actor, fileID, false); err != nil {
		return domain.VersionPage{}, err
	}
	limit = normalizeLimit(limit)
	items, err := service.repository.History(ctx, fileID, cursor, limit+1)
	if err != nil {
		return domain.VersionPage{}, err
	}
	page := domain.VersionPage{Items: items}
	if len(items) > limit {
		page.Items = items[:limit]
		page.NextCursor = items[limit-1].ID
	}
	return page, nil
}

func (service *Files) Download(ctx context.Context, actor domain.User, fileID string, versionNumber int) (domain.FileVersion, storage.Object, error) {
	if _, _, _, err := service.authorize(ctx, actor, fileID, false); err != nil {
		return domain.FileVersion{}, storage.Object{}, err
	}
	version, err := service.repository.Version(ctx, fileID, versionNumber)
	if err != nil {
		return domain.FileVersion{}, storage.Object{}, err
	}
	object, err := service.objects.Get(ctx, version.ObjectKey)
	return version, object, err
}

func (service *Files) SetEncoding(ctx context.Context, actor domain.User, fileID, encoding string) (domain.File, error) {
	encoding = strings.ToLower(strings.TrimSpace(encoding))
	if !service.encodings[encoding] {
		return domain.File{}, ErrInvalid
	}
	if _, _, _, err := service.authorize(ctx, actor, fileID, true); err != nil {
		return domain.File{}, err
	}
	return service.repository.SetEncoding(ctx, fileID, encoding)
}

func (service *Files) authorize(ctx context.Context, actor domain.User, fileID string, write bool) (domain.File, domain.Workspace, *domain.WorkspaceRole, error) {
	if !validUUID(fileID) {
		return domain.File{}, domain.Workspace{}, nil, ErrInvalid
	}
	file, workspace, role, err := service.repository.Access(ctx, fileID, actor.ID)
	if err != nil {
		return domain.File{}, domain.Workspace{}, nil, err
	}
	allowed := service.policy.CanReadFile(actor, workspace, role, file.CreatedBy)
	if write {
		allowed = service.policy.CanWriteFile(actor, workspace, role, file.CreatedBy)
	}
	if !allowed {
		return domain.File{}, domain.Workspace{}, nil, ErrNotFound
	}
	return file, workspace, role, nil
}

func cleanFilename(value string) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), "\\", "/")
	if value == "" {
		return ""
	}
	return path.Base(value)
}

func normalizeLimit(value int) int {
	if value <= 0 {
		return 50
	}
	if value > 200 {
		return 200
	}
	return value
}

func randomObjectKey(prefix string) (string, error) {
	data := make([]byte, 16)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return prefix + "/" + hex.EncodeToString(data), nil
}

type limitedHashReader struct {
	source    io.Reader
	remaining int64
	hash      hash.Hash
	size      int64
	err       error
}

func newLimitedHashReader(source io.Reader, max int64) *limitedHashReader {
	return &limitedHashReader{source: source, remaining: max, hash: sha256.New()}
}

func (reader *limitedHashReader) Read(target []byte) (int, error) {
	if reader.remaining == 0 {
		var probe [1]byte
		n, err := reader.source.Read(probe[:])
		if n > 0 {
			reader.err = ErrTooLarge
			return 0, reader.err
		}
		return 0, err
	}
	if int64(len(target)) > reader.remaining {
		target = target[:reader.remaining]
	}
	n, err := reader.source.Read(target)
	if n > 0 {
		_, _ = reader.hash.Write(target[:n])
		reader.size += int64(n)
		reader.remaining -= int64(n)
	}
	return n, err
}
