CREATE TABLE file_update_sessions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    file_id uuid NOT NULL REFERENCES files(id) ON DELETE CASCADE,
    base_version_id uuid NOT NULL,
    candidate_object_id uuid UNIQUE REFERENCES storage_objects(id),
    candidate_original_name text NOT NULL CHECK (length(btrim(candidate_original_name)) BETWEEN 1 AND 255),
    resolved_version_id uuid,
    status text NOT NULL CHECK (status IN ('active', 'resolved', 'rejected', 'expired')),
    idempotency_key text NOT NULL CHECK (length(idempotency_key) BETWEEN 16 AND 128),
    expires_at timestamptz NOT NULL,
    created_by_user_id uuid NOT NULL REFERENCES users(id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    completed_at timestamptz,
    FOREIGN KEY (base_version_id, file_id) REFERENCES file_versions(id, file_id),
    FOREIGN KEY (resolved_version_id, file_id) REFERENCES file_versions(id, file_id),
    UNIQUE (created_by_user_id, file_id, idempotency_key),
    CHECK (
        (status = 'active' AND candidate_object_id IS NOT NULL AND resolved_version_id IS NULL AND completed_at IS NULL)
        OR (status = 'resolved' AND candidate_object_id IS NOT NULL AND resolved_version_id IS NOT NULL AND completed_at IS NOT NULL)
        OR (status IN ('rejected', 'expired') AND candidate_object_id IS NULL AND resolved_version_id IS NULL AND completed_at IS NOT NULL)
    )
);

CREATE UNIQUE INDEX file_update_sessions_one_active
    ON file_update_sessions (file_id) WHERE status = 'active';
CREATE INDEX file_update_sessions_expiry_idx
    ON file_update_sessions (expires_at) WHERE status = 'active';
