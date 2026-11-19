-- +goose Up
CREATE TABLE updates (
    platform       text NOT NULL,
    id             text NOT NULL,
    kb             text NOT NULL DEFAULT '',
    title          text NOT NULL DEFAULT '',
    classification text NOT NULL DEFAULT 'other',
    severity       text NOT NULL DEFAULT '',
    size_bytes     bigint NOT NULL DEFAULT 0,
    published_at   timestamptz,
    security       boolean NOT NULL DEFAULT false,
    first_seen_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (platform, id)
);

CREATE TABLE device_updates (
    device_id     uuid NOT NULL REFERENCES devices (id) ON DELETE CASCADE,
    platform      text NOT NULL,
    update_id     text NOT NULL,
    state         text NOT NULL DEFAULT 'missing' CHECK (state IN ('missing', 'installed', 'failed')),
    first_seen_at timestamptz NOT NULL,
    installed_at  timestamptz,
    last_error    text NOT NULL DEFAULT '',
    PRIMARY KEY (device_id, update_id),
    FOREIGN KEY (platform, update_id) REFERENCES updates (platform, id) ON DELETE CASCADE
);
CREATE INDEX device_updates_update_idx ON device_updates (platform, update_id, state);

CREATE TABLE device_patch_state (
    device_id       uuid PRIMARY KEY REFERENCES devices (id) ON DELETE CASCADE,
    last_scan_at    timestamptz,
    last_install_at timestamptz,
    reboot_pending  boolean NOT NULL DEFAULT false
);

CREATE TABLE patch_policies (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name           text NOT NULL,
    platform       text NOT NULL CHECK (platform IN ('windows', 'linux')),
    priority       integer NOT NULL,
    auto_approve   text[] NOT NULL DEFAULT '{}',
    defer_days     integer NOT NULL DEFAULT 0 CHECK (defer_days BETWEEN 0 AND 60),
    maint_window   jsonb NOT NULL,
    reboot         text NOT NULL CHECK (reboot IN ('never', 'notify', 'in_window')),
    notify_minutes integer NOT NULL DEFAULT 15 CHECK (notify_minutes BETWEEN 5 AND 120),
    linux_scope    text NOT NULL DEFAULT 'security' CHECK (linux_scope IN ('security', 'all')),
    excluded_kbs   text[] NOT NULL DEFAULT '{}',
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT patch_policies_priority_key UNIQUE (priority) DEFERRABLE INITIALLY DEFERRED
);
CREATE UNIQUE INDEX patch_policies_name_key ON patch_policies (lower(name));

CREATE TABLE patch_policy_assignments (
    id        bigserial PRIMARY KEY,
    policy_id uuid NOT NULL REFERENCES patch_policies (id) ON DELETE CASCADE,
    group_id  uuid REFERENCES groups (id) ON DELETE CASCADE,
    exclude   boolean NOT NULL DEFAULT false
);

-- Decisiones manuales sobre actualizaciones de Windows.
CREATE TABLE update_approvals (
    update_id  text PRIMARY KEY,
    decision   text NOT NULL CHECK (decision IN ('approved', 'declined')),
    decided_by uuid,
    decided_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE update_approvals, patch_policy_assignments, patch_policies, device_patch_state, device_updates, updates;
