package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/alexonderia/filestore/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Users struct {
	pool *pgxpool.Pool
}

func (repository *Users) Register(ctx context.Context, name, email, passwordHash string, tokenHash []byte, expiresAt time.Time) (domain.User, error) {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return domain.User{}, fmt.Errorf("begin registration: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	var user domain.User
	err = tx.QueryRow(ctx, `
        INSERT INTO users (name, email, password_hash)
        VALUES ($1, $2, $3)
        RETURNING id::text, name, email::text, is_superadmin, created_at`, name, email, passwordHash).
		Scan(&user.ID, &user.Name, &user.Email, &user.IsSuperadmin, &user.CreatedAt)
	if err != nil {
		if uniqueViolation(err) {
			return domain.User{}, domain.ErrConflict
		}
		return domain.User{}, fmt.Errorf("insert user: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO user_tokens (user_id, token_hash, expires_at) VALUES ($1, $2, $3)`, user.ID, tokenHash, expiresAt); err != nil {
		return domain.User{}, fmt.Errorf("insert registration token: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		if uniqueViolation(err) {
			return domain.User{}, domain.ErrConflict
		}
		return domain.User{}, fmt.Errorf("commit registration: %w", err)
	}
	return user, nil
}

func (repository *Users) CredentialsByEmail(ctx context.Context, email string) (domain.User, string, error) {
	var user domain.User
	var passwordHash string
	err := repository.pool.QueryRow(ctx, `
        SELECT id::text, name, email::text, is_superadmin, created_at, password_hash
        FROM users WHERE email = $1`, email).
		Scan(&user.ID, &user.Name, &user.Email, &user.IsSuperadmin, &user.CreatedAt, &passwordHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, "", domain.ErrNotFound
	}
	if err != nil {
		return domain.User{}, "", fmt.Errorf("get credentials: %w", err)
	}
	return user, passwordHash, nil
}

func (repository *Users) CreateToken(ctx context.Context, userID string, tokenHash []byte, expiresAt time.Time) error {
	_, err := repository.pool.Exec(ctx, `INSERT INTO user_tokens (user_id, token_hash, expires_at) VALUES ($1, $2, $3)`, userID, tokenHash, expiresAt)
	if err != nil {
		return fmt.Errorf("insert user token: %w", err)
	}
	return nil
}

func (repository *Users) Authenticate(ctx context.Context, tokenHash []byte, now time.Time) (domain.Actor, error) {
	var actor domain.Actor
	err := repository.pool.QueryRow(ctx, `
        UPDATE user_tokens t
        SET last_used_at = $2
        FROM users u
        WHERE t.token_hash = $1
          AND t.user_id = u.id
          AND t.revoked_at IS NULL
          AND t.expires_at > $2
        RETURNING t.id::text, u.id::text, u.name, u.email::text, u.is_superadmin, u.created_at`, tokenHash, now).
		Scan(&actor.TokenID, &actor.User.ID, &actor.User.Name, &actor.User.Email, &actor.User.IsSuperadmin, &actor.User.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Actor{}, domain.ErrUnauthorized
	}
	if err != nil {
		return domain.Actor{}, fmt.Errorf("authenticate token: %w", err)
	}
	return actor, nil
}

func (repository *Users) RevokeToken(ctx context.Context, tokenHash []byte, now time.Time) error {
	command, err := repository.pool.Exec(ctx, `
        UPDATE user_tokens SET revoked_at = $2
        WHERE token_hash = $1 AND revoked_at IS NULL AND expires_at > $2`, tokenHash, now)
	if err != nil {
		return fmt.Errorf("revoke token: %w", err)
	}
	if command.RowsAffected() == 0 {
		return domain.ErrUnauthorized
	}
	return nil
}

func uniqueViolation(err error) bool {
	var pgError *pgconn.PgError
	return errors.As(err, &pgError) && pgError.Code == "23505"
}

func NewUsers(pool *pgxpool.Pool) *Users {
	return &Users{pool: pool}
}

func (repository *Users) BootstrapSuperadmin(ctx context.Context, name, email, passwordHash string) (string, error) {
	const query = `
        INSERT INTO users (name, email, password_hash, is_superadmin)
        VALUES ($1, $2, $3, true)
        ON CONFLICT (email) DO UPDATE
        SET name = EXCLUDED.name,
            password_hash = EXCLUDED.password_hash,
            is_superadmin = true,
            updated_at = clock_timestamp()
        RETURNING id::text`
	var id string
	if err := repository.pool.QueryRow(ctx, query, name, email, passwordHash).Scan(&id); err != nil {
		return "", fmt.Errorf("bootstrap superadmin: %w", err)
	}
	return id, nil
}
