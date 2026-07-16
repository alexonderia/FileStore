CREATE EXTENSION IF NOT EXISTS citext;

CREATE TABLE users (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 200),
    email citext NOT NULL UNIQUE,
    password_hash text NOT NULL,
    is_superadmin boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

CREATE TABLE workspaces (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 200),
    kind text NOT NULL CHECK (kind IN ('base', 'private')),
    created_by_user_id uuid REFERENCES users(id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CHECK (
        (kind = 'base' AND created_by_user_id IS NULL)
        OR (kind = 'private' AND created_by_user_id IS NOT NULL)
    )
);

CREATE UNIQUE INDEX workspaces_name_ci_unique ON workspaces (lower(name));
CREATE UNIQUE INDEX workspaces_single_base ON workspaces (kind) WHERE kind = 'base';

CREATE TABLE workspace_members (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role text NOT NULL CHECK (role IN ('owner', 'editor', 'viewer')),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (workspace_id, user_id)
);

INSERT INTO workspaces (id, name, kind)
VALUES ('00000000-0000-0000-0000-000000000001', 'base', 'base')
ON CONFLICT DO NOTHING;
