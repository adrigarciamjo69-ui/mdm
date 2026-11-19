-- +goose Up
CREATE TABLE scripts (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name        text NOT NULL,
    description text NOT NULL DEFAULT '',
    platform    text NOT NULL CHECK (platform IN ('windows', 'linux')),
    interpreter text NOT NULL,
    timeout_s   integer NOT NULL DEFAULT 300 CHECK (timeout_s BETWEEN 1 AND 3600),
    parameters  jsonb NOT NULL DEFAULT '[]',
    created_by  uuid,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    archived_at timestamptz
);
CREATE UNIQUE INDEX scripts_name_key ON scripts (lower(name));

CREATE TABLE script_versions (
    script_id        uuid NOT NULL REFERENCES scripts (id) ON DELETE CASCADE,
    version          integer NOT NULL,
    content          text NOT NULL,
    created_by       uuid,
    created_by_email text NOT NULL DEFAULT '',
    created_at       timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (script_id, version)
);

-- +goose Down
DROP TABLE script_versions, scripts;
