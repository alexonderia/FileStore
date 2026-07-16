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

type Workspaces struct {
	pool *pgxpool.Pool
}

func NewWorkspaces(pool *pgxpool.Pool) *Workspaces {
	return &Workspaces{pool: pool}
}

func (repository *Workspaces) Base(ctx context.Context) (domain.Workspace, error) {
	return repository.workspaceByQuery(ctx, `SELECT id::text, name, kind, created_at FROM workspaces WHERE kind = 'base'`)
}

func (repository *Workspaces) Access(ctx context.Context, workspaceID, userID string) (domain.Workspace, *domain.WorkspaceRole, error) {
	var workspace domain.Workspace
	var role sql.NullString
	err := repository.pool.QueryRow(ctx, `
        SELECT w.id::text, w.name, w.kind, w.created_at, m.role
        FROM workspaces w
        LEFT JOIN workspace_members m ON m.workspace_id = w.id AND m.user_id = $2
        WHERE w.id = $1`, workspaceID, userID).
		Scan(&workspace.ID, &workspace.Name, &workspace.Kind, &workspace.CreatedAt, &role)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Workspace{}, nil, domain.ErrNotFound
	}
	if err != nil {
		return domain.Workspace{}, nil, fmt.Errorf("get workspace access: %w", err)
	}
	if !role.Valid {
		return workspace, nil, nil
	}
	result := domain.WorkspaceRole(role.String)
	return workspace, &result, nil
}

func (repository *Workspaces) List(ctx context.Context, userID string, superadmin bool) ([]domain.Workspace, error) {
	rows, err := repository.pool.Query(ctx, `
        SELECT w.id::text, w.name, w.kind, w.created_at
        FROM workspaces w
        WHERE $2 OR w.kind = 'base' OR EXISTS (
            SELECT 1 FROM workspace_members m WHERE m.workspace_id = w.id AND m.user_id = $1
        )
        ORDER BY CASE WHEN w.kind = 'base' THEN 0 ELSE 1 END, lower(w.name)`, userID, superadmin)
	if err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}
	defer rows.Close()
	result := make([]domain.Workspace, 0)
	for rows.Next() {
		var workspace domain.Workspace
		if err := rows.Scan(&workspace.ID, &workspace.Name, &workspace.Kind, &workspace.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan workspace: %w", err)
		}
		result = append(result, workspace)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate workspaces: %w", err)
	}
	return result, nil
}

func (repository *Workspaces) Create(ctx context.Context, name, ownerID string) (domain.Workspace, error) {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return domain.Workspace{}, fmt.Errorf("begin create workspace: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var workspace domain.Workspace
	err = tx.QueryRow(ctx, `
        INSERT INTO workspaces (name, kind, created_by_user_id)
        VALUES ($1, 'private', $2)
        RETURNING id::text, name, kind, created_at`, name, ownerID).
		Scan(&workspace.ID, &workspace.Name, &workspace.Kind, &workspace.CreatedAt)
	if err != nil {
		if uniqueViolation(err) {
			return domain.Workspace{}, domain.ErrConflict
		}
		return domain.Workspace{}, fmt.Errorf("insert workspace: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO workspace_members (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`, workspace.ID, ownerID); err != nil {
		return domain.Workspace{}, fmt.Errorf("insert workspace owner: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		if uniqueViolation(err) {
			return domain.Workspace{}, domain.ErrConflict
		}
		return domain.Workspace{}, fmt.Errorf("commit workspace: %w", err)
	}
	return workspace, nil
}

func (repository *Workspaces) PutMember(ctx context.Context, workspaceID, actorID, userID, email string, role domain.WorkspaceRole) (domain.WorkspaceMember, error) {
	tx, workspace, err := repository.lockManageableWorkspace(ctx, workspaceID, actorID)
	if err != nil {
		return domain.WorkspaceMember{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	var user domain.User
	query := `SELECT id::text, name, email::text, is_superadmin, created_at FROM users WHERE id = $1`
	argument := userID
	if email != "" {
		query = `SELECT id::text, name, email::text, is_superadmin, created_at FROM users WHERE email = $1`
		argument = email
	}
	err = tx.QueryRow(ctx, query, argument).Scan(&user.ID, &user.Name, &user.Email, &user.IsSuperadmin, &user.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.WorkspaceMember{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.WorkspaceMember{}, fmt.Errorf("find workspace member user: %w", err)
	}

	var oldRole sql.NullString
	err = tx.QueryRow(ctx, `SELECT role FROM workspace_members WHERE workspace_id = $1 AND user_id = $2`, workspace.ID, user.ID).Scan(&oldRole)
	if errors.Is(err, pgx.ErrNoRows) {
		oldRole = sql.NullString{}
	} else if err != nil {
		return domain.WorkspaceMember{}, fmt.Errorf("get existing member: %w", err)
	}
	if oldRole.Valid && oldRole.String == string(domain.RoleOwner) && role != domain.RoleOwner {
		if err := requireAnotherOwner(ctx, tx, workspace.ID); err != nil {
			return domain.WorkspaceMember{}, err
		}
	}
	_, err = tx.Exec(ctx, `
        INSERT INTO workspace_members (workspace_id, user_id, role) VALUES ($1, $2, $3)
        ON CONFLICT (workspace_id, user_id) DO UPDATE SET role = EXCLUDED.role`, workspace.ID, user.ID, role)
	if err != nil {
		return domain.WorkspaceMember{}, fmt.Errorf("put workspace member: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.WorkspaceMember{}, fmt.Errorf("commit workspace member: %w", err)
	}
	return domain.WorkspaceMember{WorkspaceID: workspace.ID, User: user, Role: role}, nil
}

func (repository *Workspaces) RemoveMember(ctx context.Context, workspaceID, actorID, userID string) error {
	tx, workspace, err := repository.lockManageableWorkspace(ctx, workspaceID, actorID)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var role domain.WorkspaceRole
	err = tx.QueryRow(ctx, `SELECT role FROM workspace_members WHERE workspace_id = $1 AND user_id = $2`, workspace.ID, userID).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("get member for removal: %w", err)
	}
	if role == domain.RoleOwner {
		if err := requireAnotherOwner(ctx, tx, workspace.ID); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM workspace_members WHERE workspace_id = $1 AND user_id = $2`, workspace.ID, userID); err != nil {
		return fmt.Errorf("remove workspace member: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit member removal: %w", err)
	}
	return nil
}

func (repository *Workspaces) lockManageableWorkspace(ctx context.Context, workspaceID, actorID string) (pgx.Tx, domain.Workspace, error) {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return nil, domain.Workspace{}, fmt.Errorf("begin membership change: %w", err)
	}
	var workspace domain.Workspace
	err = tx.QueryRow(ctx, `SELECT id::text, name, kind, created_at FROM workspaces WHERE id = $1 FOR UPDATE`, workspaceID).
		Scan(&workspace.ID, &workspace.Name, &workspace.Kind, &workspace.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		_ = tx.Rollback(context.Background())
		return nil, domain.Workspace{}, domain.ErrNotFound
	}
	if err != nil {
		_ = tx.Rollback(context.Background())
		return nil, domain.Workspace{}, fmt.Errorf("lock workspace: %w", err)
	}
	if workspace.Kind != domain.WorkspacePrivate {
		_ = tx.Rollback(context.Background())
		return nil, domain.Workspace{}, domain.ErrForbidden
	}
	var allowed bool
	err = tx.QueryRow(ctx, `
        SELECT u.is_superadmin OR EXISTS (
            SELECT 1 FROM workspace_members m
            WHERE m.workspace_id = $1 AND m.user_id = u.id AND m.role = 'owner'
        ) FROM users u WHERE u.id = $2`, workspaceID, actorID).Scan(&allowed)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !allowed) {
		_ = tx.Rollback(context.Background())
		return nil, domain.Workspace{}, domain.ErrForbidden
	}
	if err != nil {
		_ = tx.Rollback(context.Background())
		return nil, domain.Workspace{}, fmt.Errorf("authorize membership change: %w", err)
	}
	return tx, workspace, nil
}

func requireAnotherOwner(ctx context.Context, tx pgx.Tx, workspaceID string) error {
	var owners int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM workspace_members WHERE workspace_id = $1 AND role = 'owner'`, workspaceID).Scan(&owners); err != nil {
		return fmt.Errorf("count workspace owners: %w", err)
	}
	if owners <= 1 {
		return domain.ErrConflict
	}
	return nil
}

func (repository *Workspaces) workspaceByQuery(ctx context.Context, query string, arguments ...any) (domain.Workspace, error) {
	var workspace domain.Workspace
	err := repository.pool.QueryRow(ctx, query, arguments...).Scan(&workspace.ID, &workspace.Name, &workspace.Kind, &workspace.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Workspace{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Workspace{}, fmt.Errorf("get workspace: %w", err)
	}
	return workspace, nil
}
