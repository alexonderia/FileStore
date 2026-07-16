CREATE TABLE user_tokens (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash bytea NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    last_used_at timestamptz,
    CHECK (expires_at > created_at)
);

CREATE INDEX user_tokens_active_user_idx
    ON user_tokens (user_id, expires_at)
    WHERE revoked_at IS NULL;
