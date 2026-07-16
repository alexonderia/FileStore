CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE file_links (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    file_id uuid NOT NULL REFERENCES files(id) ON DELETE CASCADE,
    version_id uuid,
    kind text NOT NULL CHECK (kind IN ('current', 'version')),
    token text NOT NULL UNIQUE CHECK (token ~ '^[A-Za-z0-9_-]{43}$'),
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'revoked')),
    created_by_user_id uuid NOT NULL REFERENCES users(id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    revoked_at timestamptz,
    FOREIGN KEY (version_id, file_id) REFERENCES file_versions(id, file_id),
    CHECK ((kind = 'current' AND version_id IS NULL) OR (kind = 'version' AND version_id IS NOT NULL)),
    CHECK ((status = 'active' AND revoked_at IS NULL) OR (status = 'revoked' AND revoked_at IS NOT NULL))
);

CREATE UNIQUE INDEX file_links_one_current ON file_links (file_id) WHERE kind = 'current';
CREATE UNIQUE INDEX file_links_one_version ON file_links (version_id) WHERE kind = 'version';

INSERT INTO file_links (file_id, kind, token, created_by_user_id)
SELECT f.id, 'current', translate(rtrim(encode(gen_random_bytes(32), 'base64'), '='), '+/', '-_'), f.created_by_user_id
FROM files f
ON CONFLICT DO NOTHING;

INSERT INTO file_links (file_id, version_id, kind, token, created_by_user_id)
SELECT v.file_id, v.id, 'version', translate(rtrim(encode(gen_random_bytes(32), 'base64'), '='), '+/', '-_'), v.created_by_user_id
FROM file_versions v
ON CONFLICT DO NOTHING;
