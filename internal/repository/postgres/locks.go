package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/alexonderia/filestore/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Locks struct{ pool *pgxpool.Pool }

func NewLocks(pool *pgxpool.Pool) *Locks { return &Locks{pool: pool} }

func (repository *Locks) Get(ctx context.Context, fileID string) (domain.FileLock, error) {
	return scanLock(repository.pool.QueryRow(ctx, lockSelect+` WHERE l.file_id=$1 AND l.status='active'`, fileID))
}

func (repository *Locks) Create(ctx context.Context, fileID, actorID string) (domain.FileLock, error) {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return domain.FileLock{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err := tx.QueryRow(ctx, `SELECT id FROM files WHERE id=$1 FOR UPDATE`, fileID).Scan(new(string)); errors.Is(err, pgx.ErrNoRows) {
		return domain.FileLock{}, domain.ErrNotFound
	} else if err != nil {
		return domain.FileLock{}, err
	}
	var activeSession bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM file_update_sessions WHERE file_id=$1 AND status='active')`, fileID).Scan(&activeSession); err != nil {
		return domain.FileLock{}, err
	}
	if activeSession {
		return domain.FileLock{}, domain.ErrConflict
	}
	var value domain.FileLock
	err = tx.QueryRow(ctx, `INSERT INTO file_locks(file_id,status,locked_by_user_id) VALUES($1,'active',$2) RETURNING id::text,file_id::text,status,locked_by_user_id::text,created_at`, fileID, actorID).
		Scan(&value.ID, &value.FileID, &value.Status, &value.LockedBy, &value.CreatedAt)
	if err != nil {
		if uniqueViolation(err) {
			return domain.FileLock{}, domain.ErrLocked
		}
		return domain.FileLock{}, fmt.Errorf("create file lock: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.FileLock{}, err
	}
	return value, nil
}

func (repository *Locks) Release(ctx context.Context, fileID, actorID string, now time.Time) (domain.FileLock, error) {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return domain.FileLock{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err := tx.QueryRow(ctx, `SELECT id FROM files WHERE id=$1 FOR UPDATE`, fileID).Scan(new(string)); errors.Is(err, pgx.ErrNoRows) {
		return domain.FileLock{}, domain.ErrNotFound
	} else if err != nil {
		return domain.FileLock{}, err
	}
	var value domain.FileLock
	err = tx.QueryRow(ctx, `UPDATE file_locks SET status='released',released_at=$2,released_by_user_id=$3 WHERE file_id=$1 AND status='active' RETURNING id::text,file_id::text,status,locked_by_user_id::text,created_at,released_at,released_by_user_id::text`, fileID, now, actorID).
		Scan(&value.ID, &value.FileID, &value.Status, &value.LockedBy, &value.CreatedAt, &value.ReleasedAt, &value.ReleasedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.FileLock{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.FileLock{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.FileLock{}, err
	}
	return value, nil
}

const lockSelect = `SELECT l.id::text,l.file_id::text,l.status,l.locked_by_user_id::text,l.created_at,l.released_at,l.released_by_user_id::text FROM file_locks l`

func scanLock(row scanner) (domain.FileLock, error) {
	var value domain.FileLock
	var releasedAt sql.NullTime
	var releasedBy sql.NullString
	err := row.Scan(&value.ID, &value.FileID, &value.Status, &value.LockedBy, &value.CreatedAt, &releasedAt, &releasedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.FileLock{}, domain.ErrNotFound
	}
	if releasedAt.Valid {
		value.ReleasedAt = releasedAt.Time
	}
	if releasedBy.Valid {
		value.ReleasedBy = releasedBy.String
	}
	return value, err
}
