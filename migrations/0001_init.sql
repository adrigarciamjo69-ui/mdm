-- +goose Up
CREATE TABLE admins (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email         text NOT NULL,
    password_hash text NOT NULL,
    totp_secret   text,
    role          text NOT NULL CHECK (role IN ('admin', 'operator', 'readonly')),
    disabled      boolean NOT NULL DEFAULT false,
    created_at    timestamptz NOT NULL DEFAULT now(),
    last_login_at timestamptz
);
CREATE UNIQUE INDEX admins_email_key ON admins (lower(email));

-- id guarda el SHA-256 del identificador de sesión; el valor real solo vive en la cookie.
CREATE TABLE sessions (
    id         text PRIMARY KEY,
    admin_id   uuid NOT NULL REFERENCES admins (id) ON DELETE CASCADE,
    csrf_token text NOT NULL,
    verified   boolean NOT NULL DEFAULT false,
    -- Secreto TOTP propuesto durante el alta; pasa a admins.totp_secret al confirmarlo.
    pending_totp text,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    ip         text NOT NULL DEFAULT ''
);
CREATE INDEX sessions_expires_at_idx ON sessions (expires_at);

CREATE TABLE groups (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name        text NOT NULL,
    description text NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX groups_name_key ON groups (lower(name));

CREATE TABLE enrollment_tokens (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name       text NOT NULL,
    token_hash text NOT NULL UNIQUE,
    group_id   uuid REFERENCES groups (id) ON DELETE SET NULL,
    expires_at timestamptz,
    max_uses   integer CHECK (max_uses IS NULL OR max_uses > 0),
    uses       integer NOT NULL DEFAULT 0,
    revoked_at timestamptz,
    created_by uuid REFERENCES admins (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE devices (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    hardware_id     text NOT NULL UNIQUE,
    hostname        text NOT NULL,
    platform        text NOT NULL CHECK (platform IN ('windows', 'linux')),
    os_name         text NOT NULL DEFAULT '',
    os_version      text NOT NULL DEFAULT '',
    arch            text NOT NULL DEFAULT '',
    serial          text NOT NULL DEFAULT '',
    manufacturer    text NOT NULL DEFAULT '',
    model           text NOT NULL DEFAULT '',
    agent_version   text NOT NULL DEFAULT '',
    credential_hash text NOT NULL,
    revoked_at      timestamptz,
    group_id        uuid REFERENCES groups (id) ON DELETE SET NULL,
    assigned_user   text NOT NULL DEFAULT '',
    tags            text[] NOT NULL DEFAULT '{}',
    enrolled_at     timestamptz NOT NULL DEFAULT now(),
    last_seen_at    timestamptz,
    last_ip         text NOT NULL DEFAULT '',
    inventory_hash  text NOT NULL DEFAULT '',
    inventory_at    timestamptz
);
CREATE INDEX devices_last_seen_at_idx ON devices (last_seen_at);
CREATE INDEX devices_group_id_idx ON devices (group_id);
CREATE INDEX devices_hostname_idx ON devices (lower(hostname));

CREATE TABLE device_inventory (
    device_id    uuid PRIMARY KEY REFERENCES devices (id) ON DELETE CASCADE,
    collected_at timestamptz NOT NULL,
    data         jsonb NOT NULL
);

CREATE TABLE device_software (
    device_id    uuid NOT NULL REFERENCES devices (id) ON DELETE CASCADE,
    name         text NOT NULL,
    version      text NOT NULL DEFAULT '',
    publisher    text NOT NULL DEFAULT '',
    installed_at timestamptz
);
CREATE INDEX device_software_device_id_idx ON device_software (device_id);
CREATE INDEX device_software_name_idx ON device_software (lower(name));

CREATE TABLE device_activity (
    device_id uuid NOT NULL REFERENCES devices (id) ON DELETE CASCADE,
    hour      timestamptz NOT NULL,
    checkins  integer NOT NULL DEFAULT 0,
    PRIMARY KEY (device_id, hour)
);
CREATE INDEX device_activity_hour_idx ON device_activity (hour);

-- Reservada para la fase 2: en la fase 1 nada escribe aquí.
CREATE TABLE commands (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id  uuid NOT NULL REFERENCES devices (id) ON DELETE CASCADE,
    type       text NOT NULL,
    payload    jsonb NOT NULL DEFAULT '{}',
    status     text NOT NULL DEFAULT 'pending',
    created_at timestamptz NOT NULL DEFAULT now(),
    created_by uuid
);
CREATE INDEX commands_device_id_idx ON commands (device_id);

-- Sin claves ajenas a propósito: las entradas sobreviven al borrado de su autor o su objetivo.
CREATE TABLE audit_log (
    id             bigserial PRIMARY KEY,
    at             timestamptz NOT NULL DEFAULT now(),
    actor_admin_id uuid,
    actor_email    text NOT NULL DEFAULT '',
    action         text NOT NULL,
    target_type    text NOT NULL DEFAULT '',
    target_id      text NOT NULL DEFAULT '',
    details        jsonb NOT NULL DEFAULT '{}',
    ip             text NOT NULL DEFAULT ''
);
CREATE INDEX audit_log_at_idx ON audit_log (at DESC);
CREATE INDEX audit_log_action_idx ON audit_log (action);

-- +goose Down
DROP TABLE audit_log, commands, device_activity, device_software, device_inventory,
    devices, enrollment_tokens, groups, sessions, admins;
