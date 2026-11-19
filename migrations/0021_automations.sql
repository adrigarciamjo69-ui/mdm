-- +goose Up
CREATE TABLE automations (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name        text NOT NULL,
    enabled     boolean NOT NULL DEFAULT true,
    trigger     jsonb NOT NULL,
    actions     jsonb NOT NULL,
    max_devices integer NOT NULL DEFAULT 500 CHECK (max_devices BETWEEN 1 AND 1000),
    last_run_at timestamptz,
    created_by  uuid,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX automations_name_key ON automations (lower(name));

CREATE TABLE automation_assignments (
    id            bigserial PRIMARY KEY,
    automation_id uuid NOT NULL REFERENCES automations (id) ON DELETE CASCADE,
    group_id      uuid REFERENCES groups (id) ON DELETE CASCADE,
    exclude       boolean NOT NULL DEFAULT false
);

CREATE TABLE automation_runs (
    id            bigserial PRIMARY KEY,
    automation_id uuid NOT NULL REFERENCES automations (id) ON DELETE CASCADE,
    trigger_info  jsonb NOT NULL DEFAULT '{}',
    started_at    timestamptz NOT NULL,
    finished_at   timestamptz,
    status        text NOT NULL CHECK (status IN ('succeeded', 'failed', 'blocked', 'partial')),
    device_count  integer NOT NULL DEFAULT 0,
    batch_ids     uuid[] NOT NULL DEFAULT '{}',
    error         text NOT NULL DEFAULT ''
);
CREATE INDEX automation_runs_idx ON automation_runs (automation_id, started_at DESC);

-- Antirrebote: una automatización por suceso no se dispara dos veces para el
-- mismo equipo y suceso en 10 minutos.
CREATE TABLE automation_fired (
    automation_id uuid NOT NULL REFERENCES automations (id) ON DELETE CASCADE,
    device_id     uuid NOT NULL,
    event         text NOT NULL,
    fired_at      timestamptz NOT NULL,
    PRIMARY KEY (automation_id, device_id, event)
);

-- +goose Down
DROP TABLE automation_fired, automation_runs, automation_assignments, automations;
