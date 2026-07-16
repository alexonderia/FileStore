package service

import (
	"context"
	"strings"
	"time"

	"github.com/alexonderia/filestore/internal/domain"
	"github.com/alexonderia/filestore/internal/storage"
)

type LinkRepository interface {
	List(context.Context, string, string, int) ([]domain.FileLink, error)
	Revoke(context.Context, string, string, time.Time) error
	Resolve(context.Context, string) (domain.LinkTarget, error)
}

type Links struct {
	repository LinkRepository
	files      *Files
	objects    storage.ObjectStore
	now        func() time.Time
}

func NewLinks(repository LinkRepository, files *Files, objects storage.ObjectStore) *Links {
	return &Links{repository: repository, files: files, objects: objects, now: time.Now}
}

func (service *Links) List(ctx context.Context, actor domain.User, fileID, cursor string, limit int) (domain.LinkPage, error) {
	if cursor != "" && !validUUID(cursor) {
		return domain.LinkPage{}, ErrInvalid
	}
	if _, _, _, err := service.files.authorize(ctx, actor, fileID, true); err != nil {
		return domain.LinkPage{}, err
	}
	limit = normalizeLimit(limit)
	items, err := service.repository.List(ctx, fileID, cursor, limit+1)
	page := domain.LinkPage{Items: items}
	if len(items) > limit {
		page.Items = items[:limit]
		page.NextCursor = items[limit-1].ID
	}
	return page, err
}

func (service *Links) Revoke(ctx context.Context, actor domain.User, fileID, linkID string) error {
	if !validUUID(linkID) {
		return ErrInvalid
	}
	if _, _, _, err := service.files.authorize(ctx, actor, fileID, true); err != nil {
		return err
	}
	return service.repository.Revoke(ctx, fileID, linkID, service.now())
}

func (service *Links) Download(ctx context.Context, actor *domain.User, token string) (domain.FileVersion, storage.Object, error) {
	if len(strings.TrimSpace(token)) != 43 {
		return domain.FileVersion{}, storage.Object{}, ErrNotFound
	}
	target, err := service.repository.Resolve(ctx, token)
	if err != nil {
		return domain.FileVersion{}, storage.Object{}, ErrNotFound
	}
	if target.WorkspaceKind == domain.WorkspacePrivate {
		if actor == nil {
			return domain.FileVersion{}, storage.Object{}, ErrUnauthorized
		}
		if _, _, _, err := service.files.authorize(ctx, *actor, target.Link.FileID, false); err != nil {
			return domain.FileVersion{}, storage.Object{}, err
		}
	}
	object, err := service.objects.Get(ctx, target.Version.ObjectKey)
	return target.Version, object, err
}
