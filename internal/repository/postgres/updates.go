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

type Updates struct{ pool *pgxpool.Pool }

func NewUpdates(pool *pgxpool.Pool) *Updates { return &Updates{pool: pool} }

func (repository *Updates) ByIdempotency(ctx context.Context, fileID, actorID, key string) (domain.UpdateSession, error) {
	return repository.sessionByQuery(ctx, sessionSelect+` WHERE s.file_id=$1 AND s.created_by_user_id=$2 AND s.idempotency_key=$3`, fileID, actorID, key)
}

func (repository *Updates) Create(ctx context.Context, fileID, actorID, idempotencyKey, originalName string, object domain.StoredObject, expiresAt time.Time) (domain.UpdateSession, error) {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return domain.UpdateSession{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var baseVersionID string
	if err := tx.QueryRow(ctx, `SELECT current_version_id::text FROM files WHERE id=$1 FOR UPDATE`, fileID).Scan(&baseVersionID); errors.Is(err, pgx.ErrNoRows) {
		return domain.UpdateSession{}, domain.ErrNotFound
	} else if err != nil {
		return domain.UpdateSession{}, fmt.Errorf("lock file for update session: %w", err)
	}
	var hardLocked, sessionActive bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM file_locks WHERE file_id=$1 AND status='active'), EXISTS(SELECT 1 FROM file_update_sessions WHERE file_id=$1 AND status='active')`, fileID).Scan(&hardLocked, &sessionActive); err != nil {
		return domain.UpdateSession{}, err
	}
	if hardLocked {
		return domain.UpdateSession{}, domain.ErrLocked
	}
	if sessionActive {
		return domain.UpdateSession{}, domain.ErrConflict
	}
	var objectID, sessionID string
	if err := tx.QueryRow(ctx, `INSERT INTO storage_objects(object_key,size_bytes,sha256,mime_type) VALUES($1,$2,$3,$4) RETURNING id::text`, object.Key, object.Size, object.SHA256, object.MIMEType).Scan(&objectID); err != nil {
		return domain.UpdateSession{}, fmt.Errorf("insert candidate object: %w", err)
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO file_update_sessions(file_id,base_version_id,candidate_object_id,candidate_original_name,status,idempotency_key,expires_at,created_by_user_id)
		VALUES($1,$2,$3,$4,'active',$5,$6,$7) RETURNING id::text`, fileID, baseVersionID, objectID, originalName, idempotencyKey, expiresAt, actorID).Scan(&sessionID)
	if err != nil {
		if uniqueViolation(err) {
			return domain.UpdateSession{}, domain.ErrConflict
		}
		return domain.UpdateSession{}, fmt.Errorf("insert update session: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.UpdateSession{}, err
	}
	return repository.Get(ctx, fileID, sessionID)
}

func (repository *Updates) Get(ctx context.Context, fileID, sessionID string) (domain.UpdateSession, error) {
	return repository.sessionByQuery(ctx, sessionSelect+` WHERE s.file_id=$1 AND s.id=$2`, fileID, sessionID)
}

func (repository *Updates) RollbackWarning(ctx context.Context, fileID, currentVersionID, sha string) (bool, error) {
	var result bool
	err := repository.pool.QueryRow(ctx, `
        SELECT EXISTS(SELECT 1 FROM file_versions v JOIN storage_objects o ON o.id=v.storage_object_id
        WHERE v.file_id=$1 AND v.id<>$2 AND o.sha256=$3)`, fileID, currentVersionID, sha).Scan(&result)
	return result, err
}

func (repository *Updates) Resolve(ctx context.Context, fileID, sessionID, actorID string, now time.Time) (domain.FileVersion, error) {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return domain.FileVersion{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var currentID string
	if err := tx.QueryRow(ctx, `SELECT current_version_id::text FROM files WHERE id=$1 FOR UPDATE`, fileID).Scan(&currentID); errors.Is(err, pgx.ErrNoRows) {
		return domain.FileVersion{}, domain.ErrNotFound
	} else if err != nil {
		return domain.FileVersion{}, err
	}
	var status, baseID, resolvedID, objectID, originalName string
	var expiresAt time.Time
	var nullableResolved sql.NullString
	err = tx.QueryRow(ctx, `
        SELECT s.status,s.base_version_id::text,s.resolved_version_id::text,s.candidate_object_id::text,s.expires_at,
		       s.candidate_original_name
        FROM file_update_sessions s WHERE s.id=$1 AND s.file_id=$2 FOR UPDATE`, sessionID, fileID).
		Scan(&status, &baseID, &nullableResolved, &objectID, &expiresAt, &originalName)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.FileVersion{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.FileVersion{}, err
	}
	if nullableResolved.Valid {
		resolvedID = nullableResolved.String
	}
	if status == "resolved" {
		if err := tx.Commit(ctx); err != nil {
			return domain.FileVersion{}, err
		}
		return repository.version(ctx, resolvedID)
	}
	if status != "active" || now.After(expiresAt) || currentID != baseID {
		return domain.FileVersion{}, domain.ErrConflict
	}
	var locked bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM file_locks WHERE file_id=$1 AND status='active')`, fileID).Scan(&locked); err != nil {
		return domain.FileVersion{}, err
	}
	if locked {
		return domain.FileVersion{}, domain.ErrLocked
	}
	var nextNumber int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(max(version_number),0)+1 FROM file_versions WHERE file_id=$1`, fileID).Scan(&nextNumber); err != nil {
		return domain.FileVersion{}, err
	}
	if err := tx.QueryRow(ctx, `
        INSERT INTO file_versions(file_id,version_number,storage_object_id,original_name,created_by_user_id)
        VALUES($1,$2,$3,$4,$5) RETURNING id::text`, fileID, nextNumber, objectID, originalName, actorID).Scan(&resolvedID); err != nil {
		return domain.FileVersion{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO file_links(file_id,version_id,kind,token,created_by_user_id) VALUES($1,$2,'version',translate(rtrim(encode(gen_random_bytes(32),'base64'),'='),'+/','-_'),$3)`, fileID, resolvedID, actorID); err != nil {
		return domain.FileVersion{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE files SET current_version_id=$2,updated_at=$3 WHERE id=$1`, fileID, resolvedID, now); err != nil {
		return domain.FileVersion{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE file_update_sessions SET status='resolved',resolved_version_id=$2,completed_at=$3 WHERE id=$1`, sessionID, resolvedID, now); err != nil {
		return domain.FileVersion{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.FileVersion{}, err
	}
	return repository.version(ctx, resolvedID)
}

func (repository *Updates) Reject(ctx context.Context, fileID, sessionID string, now time.Time, status string) (string, string, error) {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return "", "", err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := tx.Exec(ctx, `SELECT id FROM files WHERE id=$1 FOR UPDATE`, fileID); err != nil {
		return "", "", err
	}
	var objectID, key, currentStatus string
	err = tx.QueryRow(ctx, `
        SELECT s.status,s.candidate_object_id::text,o.object_key
        FROM file_update_sessions s JOIN storage_objects o ON o.id=s.candidate_object_id
        WHERE s.id=$1 AND s.file_id=$2 FOR UPDATE`, sessionID, fileID).Scan(&currentStatus, &objectID, &key)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", domain.ErrNotFound
	}
	if err != nil {
		return "", "", err
	}
	if currentStatus != "active" {
		return "", "", domain.ErrConflict
	}
	if _, err := tx.Exec(ctx, `UPDATE file_update_sessions SET status=$2,candidate_object_id=NULL,completed_at=$3 WHERE id=$1`, sessionID, status, now); err != nil {
		return "", "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", "", err
	}
	return objectID, key, nil
}

func (repository *Updates) DeleteStorageRow(ctx context.Context, objectID string) error {
	_, err := repository.pool.Exec(ctx, `DELETE FROM storage_objects WHERE id=$1 AND NOT EXISTS(SELECT 1 FROM file_versions WHERE storage_object_id=$1) AND NOT EXISTS(SELECT 1 FROM file_update_sessions WHERE candidate_object_id=$1)`, objectID)
	return err
}

func (repository *Updates) Expired(ctx context.Context, now time.Time, limit int) ([]domain.UpdateSession, error) {
	rows, err := repository.pool.Query(ctx, sessionSelect+` WHERE s.status='active' AND s.expires_at <= $1 ORDER BY s.expires_at LIMIT $2`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []domain.UpdateSession
	for rows.Next() {
		value, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

func (repository *Updates) Orphans(ctx context.Context, before time.Time, limit int) ([]domain.StoredObject, error) {
	rows, err := repository.pool.Query(ctx, `
		SELECT o.id::text,o.object_key,o.size_bytes,o.sha256,o.mime_type
		FROM storage_objects o
		WHERE o.created_at <= $1
		  AND NOT EXISTS(SELECT 1 FROM file_versions v WHERE v.storage_object_id=o.id)
		  AND NOT EXISTS(SELECT 1 FROM file_update_sessions s WHERE s.candidate_object_id=o.id)
		ORDER BY o.created_at LIMIT $2`, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []domain.StoredObject
	for rows.Next() {
		var value domain.StoredObject
		if err := rows.Scan(&value.ID, &value.Key, &value.Size, &value.SHA256, &value.MIMEType); err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

func (repository *Updates) version(ctx context.Context, versionID string) (domain.FileVersion, error) {
	return scanVersion(repository.pool.QueryRow(ctx, versionSelect+` WHERE v.id=$1`, versionID))
}

func (repository *Updates) sessionByQuery(ctx context.Context, query string, args ...any) (domain.UpdateSession, error) {
	value, err := scanSession(repository.pool.QueryRow(ctx, query, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.UpdateSession{}, domain.ErrNotFound
	}
	return value, err
}

const sessionSelect = `
    SELECT s.id::text,s.file_id::text,s.base_version_id::text,COALESCE(s.resolved_version_id::text,''),s.status,
           s.expires_at,s.created_by_user_id::text,s.created_at,s.completed_at,
           COALESCE(s.candidate_object_id::text,''),COALESCE(co.object_key,''),
		   bo.object_key,bv.original_name,bo.mime_type,bo.size_bytes,bo.sha256,
		   s.candidate_original_name,COALESCE(co.mime_type,''),COALESCE(co.size_bytes,0),COALESCE(co.sha256,'')
    FROM file_update_sessions s
    JOIN file_versions bv ON bv.id=s.base_version_id
    JOIN storage_objects bo ON bo.id=bv.storage_object_id
	LEFT JOIN storage_objects co ON co.id=s.candidate_object_id`

func scanSession(row scanner) (domain.UpdateSession, error) {
	var value domain.UpdateSession
	var completed sql.NullTime
	err := row.Scan(&value.ID, &value.FileID, &value.BaseVersionID, &value.ResolvedVersionID, &value.Status,
		&value.ExpiresAt, &value.CreatedBy, &value.CreatedAt, &completed,
		&value.CandidateObjectID, &value.CandidateKey,
		&value.BaseKey, &value.Base.OriginalName, &value.Base.MIMEType, &value.Base.Size, &value.Base.SHA256,
		&value.Candidate.OriginalName, &value.Candidate.MIMEType, &value.Candidate.Size, &value.Candidate.SHA256)
	if completed.Valid {
		value.CompletedAt = completed.Time
	}
	return value, err
}
