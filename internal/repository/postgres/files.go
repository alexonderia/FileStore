package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/alexonderia/filestore/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Files struct {
	pool *pgxpool.Pool
}

func NewFiles(pool *pgxpool.Pool) *Files { return &Files{pool: pool} }

func (repository *Files) Create(ctx context.Context, workspaceID, actorID, name, encoding, originalName string, object domain.StoredObject) (domain.File, error) {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return domain.File{}, fmt.Errorf("begin file create: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var fileID, versionID, objectID string
	if err := tx.QueryRow(ctx, `SELECT gen_random_uuid()::text, gen_random_uuid()::text`).Scan(&fileID, &versionID); err != nil {
		return domain.File{}, fmt.Errorf("generate file ids: %w", err)
	}
	if err := tx.QueryRow(ctx, `
        INSERT INTO storage_objects (object_key, size_bytes, sha256, mime_type)
        VALUES ($1, $2, $3, $4) RETURNING id::text`, object.Key, object.Size, object.SHA256, object.MIMEType).Scan(&objectID); err != nil {
		return domain.File{}, fmt.Errorf("insert storage object: %w", err)
	}
	_, err = tx.Exec(ctx, `
        INSERT INTO files (id, workspace_id, name, text_encoding, current_version_id, created_by_user_id)
        VALUES ($1, $2, $3, $4, $5, $6)`, fileID, workspaceID, name, encoding, versionID, actorID)
	if err != nil {
		if uniqueViolation(err) {
			return domain.File{}, domain.ErrConflict
		}
		return domain.File{}, fmt.Errorf("insert file: %w", err)
	}
	_, err = tx.Exec(ctx, `
        INSERT INTO file_versions (id, file_id, version_number, storage_object_id, original_name, created_by_user_id)
        VALUES ($1, $2, 1, $3, $4, $5)`, versionID, fileID, objectID, originalName, actorID)
	if err != nil {
		return domain.File{}, fmt.Errorf("insert first file version: %w", err)
	}
	_, err = tx.Exec(ctx, `
        INSERT INTO file_links (file_id, version_id, kind, token, created_by_user_id)
        VALUES
          ($1, NULL, 'current', translate(rtrim(encode(gen_random_bytes(32), 'base64'), '='), '+/', '-_'), $3),
          ($1, $2, 'version', translate(rtrim(encode(gen_random_bytes(32), 'base64'), '='), '+/', '-_'), $3)`, fileID, versionID, actorID)
	if err != nil {
		return domain.File{}, fmt.Errorf("insert initial file links: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		if uniqueViolation(err) {
			return domain.File{}, domain.ErrConflict
		}
		return domain.File{}, fmt.Errorf("commit file create: %w", err)
	}
	return repository.Get(ctx, fileID)
}

func (repository *Files) Get(ctx context.Context, fileID string) (domain.File, error) {
	return scanFile(repository.pool.QueryRow(ctx, fileSelect+` WHERE f.id = $1`, fileID))
}

func (repository *Files) Access(ctx context.Context, fileID, userID string) (domain.File, domain.Workspace, *domain.WorkspaceRole, error) {
	file, err := repository.Get(ctx, fileID)
	if err != nil {
		return domain.File{}, domain.Workspace{}, nil, err
	}
	var workspace domain.Workspace
	var role sql.NullString
	err = repository.pool.QueryRow(ctx, `
        SELECT w.id::text, w.name, w.kind, w.created_at, m.role
        FROM workspaces w
        LEFT JOIN workspace_members m ON m.workspace_id = w.id AND m.user_id = $2
        WHERE w.id = $1`, file.WorkspaceID, userID).
		Scan(&workspace.ID, &workspace.Name, &workspace.Kind, &workspace.CreatedAt, &role)
	if err != nil {
		return domain.File{}, domain.Workspace{}, nil, fmt.Errorf("get file workspace access: %w", err)
	}
	if !role.Valid {
		return file, workspace, nil, nil
	}
	value := domain.WorkspaceRole(role.String)
	return file, workspace, &value, nil
}

func (repository *Files) List(ctx context.Context, workspaceID, actorID string, superadmin bool, cursor string, limit int) ([]domain.File, error) {
	rows, err := repository.pool.Query(ctx, fileSelect+`
        WHERE f.workspace_id = $1
          AND ($2 OR w.kind = 'private' OR f.created_by_user_id = $3)
		  AND ($4 = '' OR (lower(f.name), f.id) > (SELECT lower(c.name), c.id FROM files c WHERE c.id = NULLIF($4,'')::uuid AND c.workspace_id = $1))
        ORDER BY lower(f.name), f.id
		LIMIT $5`, workspaceID, superadmin, actorID, cursor, limit)
	if err != nil {
		return nil, fmt.Errorf("list files: %w", err)
	}
	defer rows.Close()
	result := make([]domain.File, 0)
	for rows.Next() {
		file, err := scanFile(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, file)
	}
	return result, rows.Err()
}

func (repository *Files) History(ctx context.Context, fileID, cursor string, limit int) ([]domain.FileVersion, error) {
	rows, err := repository.pool.Query(ctx, versionSelect+` WHERE v.file_id = $1 AND ($2 = '' OR v.version_number < (SELECT c.version_number FROM file_versions c WHERE c.id = NULLIF($2,'')::uuid AND c.file_id = $1)) ORDER BY v.version_number DESC LIMIT $3`, fileID, cursor, limit)
	if err != nil {
		return nil, fmt.Errorf("list file history: %w", err)
	}
	defer rows.Close()
	result := make([]domain.FileVersion, 0)
	for rows.Next() {
		version, err := scanVersion(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, version)
	}
	return result, rows.Err()
}

func (repository *Files) Version(ctx context.Context, fileID string, versionNumber int) (domain.FileVersion, error) {
	query := versionSelect + ` WHERE v.file_id = $1`
	args := []any{fileID}
	if versionNumber > 0 {
		query += ` AND v.version_number = $2`
		args = append(args, versionNumber)
	} else {
		query += ` AND v.id = (SELECT current_version_id FROM files WHERE id = $1)`
	}
	version, err := scanVersion(repository.pool.QueryRow(ctx, query, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.FileVersion{}, domain.ErrNotFound
	}
	return version, err
}

func (repository *Files) SetEncoding(ctx context.Context, fileID, encoding string) (domain.File, error) {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return domain.File{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err := tx.QueryRow(ctx, `SELECT id FROM files WHERE id=$1 FOR UPDATE`, fileID).Scan(new(string)); errors.Is(err, pgx.ErrNoRows) {
		return domain.File{}, domain.ErrNotFound
	} else if err != nil {
		return domain.File{}, err
	}
	var locked bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM file_locks WHERE file_id=$1 AND status='active')`, fileID).Scan(&locked); err != nil {
		return domain.File{}, err
	}
	if locked {
		return domain.File{}, domain.ErrLocked
	}
	command, err := tx.Exec(ctx, `UPDATE files SET text_encoding = $2, updated_at = clock_timestamp() WHERE id = $1`, fileID, encoding)
	if err != nil {
		return domain.File{}, fmt.Errorf("set file encoding: %w", err)
	}
	if command.RowsAffected() == 0 {
		return domain.File{}, domain.ErrNotFound
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.File{}, err
	}
	return repository.Get(ctx, fileID)
}

const fileSelect = `
    SELECT f.id::text, f.workspace_id::text, f.name, f.text_encoding,
           f.created_by_user_id::text, f.created_at, f.updated_at,
           v.id::text, v.file_id::text, v.version_number, o.size_bytes,
           o.sha256, o.mime_type, v.original_name, v.created_by_user_id::text,
           v.created_at, o.object_key
    FROM files f
    JOIN workspaces w ON w.id = f.workspace_id
    JOIN file_versions v ON v.id = f.current_version_id AND v.file_id = f.id
    JOIN storage_objects o ON o.id = v.storage_object_id`

const versionSelect = `
    SELECT v.id::text, v.file_id::text, v.version_number, o.size_bytes,
           o.sha256, o.mime_type, v.original_name, v.created_by_user_id::text,
           v.created_at, o.object_key
    FROM file_versions v JOIN storage_objects o ON o.id = v.storage_object_id`

type scanner interface{ Scan(...any) error }

func scanFile(row scanner) (domain.File, error) {
	var file domain.File
	err := row.Scan(&file.ID, &file.WorkspaceID, &file.Name, &file.TextEncoding,
		&file.CreatedBy, &file.CreatedAt, &file.UpdatedAt,
		&file.CurrentVersion.ID, &file.CurrentVersion.FileID, &file.CurrentVersion.VersionNumber,
		&file.CurrentVersion.Size, &file.CurrentVersion.SHA256, &file.CurrentVersion.MIMEType,
		&file.CurrentVersion.OriginalName, &file.CurrentVersion.CreatedBy,
		&file.CurrentVersion.CreatedAt, &file.CurrentVersion.ObjectKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.File{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.File{}, fmt.Errorf("scan file: %w", err)
	}
	return file, nil
}

func scanVersion(row scanner) (domain.FileVersion, error) {
	var version domain.FileVersion
	err := row.Scan(&version.ID, &version.FileID, &version.VersionNumber, &version.Size,
		&version.SHA256, &version.MIMEType, &version.OriginalName, &version.CreatedBy,
		&version.CreatedAt, &version.ObjectKey)
	if err != nil {
		return domain.FileVersion{}, err
	}
	return version, nil
}
