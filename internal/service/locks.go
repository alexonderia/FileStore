package service

import (
	"context"
	"time"

	"github.com/alexonderia/filestore/internal/authorization"
	"github.com/alexonderia/filestore/internal/domain"
)

type LockRepository interface {
	Get(context.Context, string) (domain.FileLock, error)
	Create(context.Context, string, string) (domain.FileLock, error)
	Release(context.Context, string, string, time.Time) (domain.FileLock, error)
}

type Locks struct {
	repository LockRepository
	files      *Files
	policy     authorization.Policy
	now        func() time.Time
}

func NewLocks(repository LockRepository, files *Files) *Locks {
	return &Locks{repository: repository, files: files, policy: authorization.Policy{}, now: time.Now}
}

func (service *Locks) Get(ctx context.Context, actor domain.User, fileID string) (domain.FileLock, error) {
	if _, _, _, err := service.files.authorize(ctx, actor, fileID, false); err != nil {
		return domain.FileLock{}, err
	}
	return service.repository.Get(ctx, fileID)
}

func (service *Locks) Create(ctx context.Context, actor domain.User, fileID string) (domain.FileLock, error) {
	if _, _, _, err := service.files.authorize(ctx, actor, fileID, true); err != nil {
		return domain.FileLock{}, err
	}
	return service.repository.Create(ctx, fileID, actor.ID)
}

func (service *Locks) Release(ctx context.Context, actor domain.User, fileID string) (domain.FileLock, error) {
	if !validUUID(fileID) {
		return domain.FileLock{}, ErrInvalid
	}
	file, workspace, role, err := service.files.repository.Access(ctx, fileID, actor.ID)
	if err != nil {
		return domain.FileLock{}, err
	}
	lock, err := service.repository.Get(ctx, fileID)
	if err != nil {
		return domain.FileLock{}, err
	}
	if !service.policy.CanUnlock(actor, workspace, role, file.CreatedBy, lock.LockedBy) {
		return domain.FileLock{}, ErrNotFound
	}
	return service.repository.Release(ctx, fileID, actor.ID, service.now())
}
