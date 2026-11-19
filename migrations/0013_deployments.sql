-- +goose Up
CREATE TABLE deployments (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    app_id         uuid NOT NULL REFERENCES apps (id) ON DELETE CASCADE,
    intent         text NOT NULL CHECK (intent IN ('required', 'uninstall', 'available')),
    keep_updated   boolean NOT NULL DEFAULT false,
    start_at       timestamptz,
    respect_window boolean NOT NULL DEFAULT false,
    reboot         text NOT NULL DEFAULT 'never' CHECK (reboot IN ('never', 'notify', 'in_window')),
    created_by     uuid,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE deployment_assignments (
    id            bigserial PRIMARY KEY,
    deployment_id uuid NOT NULL REFERENCES deployments (id) ON DELETE CASCADE,
    group_id      uuid REFERENCES groups (id) ON DELETE CASCADE,
    exclude       boolean NOT NULL DEFAULT false
);
CREATE INDEX deployment_assignments_dep_idx ON deployment_assignments (deployment_id);

CREATE TABLE deployment_status (
    device_id       uuid NOT NULL REFERENCES devices (id) ON DELETE CASCADE,
    deployment_id   uuid NOT NULL REFERENCES deployments (id) ON DELETE CASCADE,
    state           text NOT NULL DEFAULT 'pending',
    message         text NOT NULL DEFAULT '',
    attempts        integer NOT NULL DEFAULT 0,
    exit_code       integer,
    reboot_required boolean NOT NULL DEFAULT false,
    retry_epoch     integer NOT NULL DEFAULT 0,
    updated_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (device_id, deployment_id)
);
CREATE INDEX deployment_status_dep_idx ON deployment_status (deployment_id, state);

-- +goose Down
DROP TABLE deployment_status, deployment_assignments, deployments;
