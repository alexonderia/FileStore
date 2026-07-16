package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Users struct {
	pool *pgxpool.Pool
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
