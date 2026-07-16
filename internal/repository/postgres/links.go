package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/alexonderia/filestore/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Links struct{ pool *pgxpool.Pool }

func NewLinks(pool *pgxpool.Pool) *Links { return &Links{pool: pool} }

func (repository *Links) List(ctx context.Context, fileID, cursor string, limit int) ([]domain.FileLink, error) {
	rows, err := repository.pool.Query(ctx, linkSelect+` WHERE l.file_id=$1 AND ($2='' OR (l.created_at,l.id) > (SELECT c.created_at,c.id FROM file_links c WHERE c.id=NULLIF($2,'')::uuid AND c.file_id=$1)) ORDER BY l.created_at,l.id LIMIT $3`, fileID, cursor, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []domain.FileLink
	for rows.Next() {
		value, err := scanLink(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

func (repository *Links) Revoke(ctx context.Context, fileID, linkID string, now time.Time) error {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err := tx.QueryRow(ctx, `SELECT id FROM files WHERE id=$1 FOR UPDATE`, fileID).Scan(new(string)); errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	} else if err != nil {
		return err
	}
	var locked bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM file_locks WHERE file_id=$1 AND status='active')`, fileID).Scan(&locked); err != nil {
		return err
	}
	if locked {
		return domain.ErrLocked
	}
	command, err := tx.Exec(ctx, `UPDATE file_links SET status='revoked',revoked_at=$3 WHERE id=$2 AND file_id=$1 AND status='active'`, fileID, linkID, now)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return tx.Commit(ctx)
}

func (repository *Links) Resolve(ctx context.Context, token string) (domain.LinkTarget, error) {
	var target domain.LinkTarget
	var versionID sql.NullString
	var revokedAt sql.NullTime
	err := repository.pool.QueryRow(ctx, `
		SELECT l.id::text,l.file_id::text,l.version_id::text,l.kind,l.token,l.status,l.created_at,l.revoked_at,
		       w.kind,v.id::text,v.file_id::text,v.version_number,o.size_bytes,o.sha256,o.mime_type,v.original_name,v.created_by_user_id::text,v.created_at,o.object_key
		FROM file_links l JOIN files f ON f.id=l.file_id JOIN workspaces w ON w.id=f.workspace_id
		JOIN file_versions v ON v.id=CASE WHEN l.kind='current' THEN f.current_version_id ELSE l.version_id END
		JOIN storage_objects o ON o.id=v.storage_object_id
		WHERE l.token=$1 AND l.status='active'`, token).Scan(
		&target.Link.ID, &target.Link.FileID, &versionID, &target.Link.Kind, &target.Link.Token, &target.Link.Status, &target.Link.CreatedAt, &revokedAt,
		&target.WorkspaceKind, &target.Version.ID, &target.Version.FileID, &target.Version.VersionNumber, &target.Version.Size, &target.Version.SHA256,
		&target.Version.MIMEType, &target.Version.OriginalName, &target.Version.CreatedBy, &target.Version.CreatedAt, &target.Version.ObjectKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.LinkTarget{}, domain.ErrNotFound
	}
	if versionID.Valid {
		target.Link.VersionID = versionID.String
	}
	if revokedAt.Valid {
		target.Link.RevokedAt = revokedAt.Time
	}
	return target, err
}

const linkSelect = `SELECT l.id::text,l.file_id::text,l.version_id::text,l.kind,l.token,l.status,l.created_at,l.revoked_at FROM file_links l`

func scanLink(row scanner) (domain.FileLink, error) {
	var value domain.FileLink
	var versionID sql.NullString
	var revokedAt sql.NullTime
	err := row.Scan(&value.ID, &value.FileID, &versionID, &value.Kind, &value.Token, &value.Status, &value.CreatedAt, &revokedAt)
	if versionID.Valid {
		value.VersionID = versionID.String
	}
	if revokedAt.Valid {
		value.RevokedAt = revokedAt.Time
	}
	return value, err
}
