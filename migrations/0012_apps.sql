-- +goose Up
CREATE TABLE apps (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name        text NOT NULL,
    publisher   text NOT NULL DEFAULT '',
    description text NOT NULL DEFAULT '',
    platform    text NOT NULL CHECK (platform IN ('windows', 'linux', 'android')),
    source      text NOT NULL CHECK (source IN ('winget', 'apt', 'dnf', 'upload', 'google_play')),
    package_id  text NOT NULL DEFAULT '',
    created_by  uuid,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    archived_at timestamptz
);
CREATE UNIQUE INDEX apps_name_key ON apps (lower(name));

CREATE TABLE app_versions (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    app_id            uuid NOT NULL REFERENCES apps (id) ON DELETE CASCADE,
    version           text NOT NULL,
    file_sha256       text NOT NULL REFERENCES files (sha256),
    installer_type    text NOT NULL CHECK (installer_type IN ('msi', 'exe', 'deb', 'rpm')),
    install_command   text NOT NULL,
    uninstall_command text NOT NULL,
    success_codes     integer[] NOT NULL,
    detection         jsonb NOT NULL,
    is_current        boolean NOT NULL DEFAULT false,
    created_at        timestamptz NOT NULL DEFAULT now(),
    UNIQUE (app_id, version)
);
CREATE UNIQUE INDEX app_versions_current ON app_versions (app_id) WHERE is_current;

-- +goose Down
DROP TABLE app_versions, apps;
