CREATE TABLE file_locks (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    file_id uuid NOT NULL REFERENCES files(id) ON DELETE CASCADE,
    status text NOT NULL CHECK (status IN ('active', 'released')),
    locked_by_user_id uuid NOT NULL REFERENCES users(id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    released_at timestamptz,
    released_by_user_id uuid REFERENCES users(id),
    CHECK (
        (status = 'active' AND released_at IS NULL AND released_by_user_id IS NULL)
        OR (status = 'released' AND released_at IS NOT NULL AND released_by_user_id IS NOT NULL)
    )
);

CREATE UNIQUE INDEX file_locks_one_active ON file_locks (file_id) WHERE status = 'active';
CREATE INDEX file_locks_history_idx ON file_locks (file_id, created_at DESC);
