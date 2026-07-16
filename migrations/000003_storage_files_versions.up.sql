CREATE TABLE storage_objects (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    object_key text NOT NULL UNIQUE,
    size_bytes bigint NOT NULL CHECK (size_bytes >= 0),
    sha256 text NOT NULL CHECK (sha256 ~ '^[a-f0-9]{64}$'),
    mime_type text NOT NULL DEFAULT 'application/octet-stream',
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

CREATE TABLE files (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id),
    name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 255),
    text_encoding text NOT NULL DEFAULT 'utf-8'
        CHECK (text_encoding IN ('utf-8', 'utf-16le', 'utf-16be', 'windows-1251')),
    current_version_id uuid NOT NULL,
    created_by_user_id uuid NOT NULL REFERENCES users(id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

CREATE UNIQUE INDEX files_workspace_name_ci_unique ON files (workspace_id, lower(name));
CREATE UNIQUE INDEX files_id_workspace_unique ON files (id, workspace_id);

CREATE TABLE file_versions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    file_id uuid NOT NULL REFERENCES files(id) ON DELETE CASCADE,
    version_number integer NOT NULL CHECK (version_number > 0),
    storage_object_id uuid NOT NULL UNIQUE REFERENCES storage_objects(id),
    original_name text NOT NULL CHECK (length(btrim(original_name)) BETWEEN 1 AND 255),
    created_by_user_id uuid NOT NULL REFERENCES users(id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (file_id, version_number),
    UNIQUE (id, file_id)
);

ALTER TABLE files
    ADD CONSTRAINT files_current_version_same_file_fk
    FOREIGN KEY (current_version_id, id)
    REFERENCES file_versions(id, file_id)
    DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX files_workspace_created_idx ON files (workspace_id, created_at, id);
CREATE INDEX file_versions_history_idx ON file_versions (file_id, version_number DESC);
