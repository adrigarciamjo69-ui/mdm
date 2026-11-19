-- +goose Up
CREATE TABLE policies (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name        text NOT NULL,
    description text NOT NULL DEFAULT '',
    platform    text NOT NULL CHECK (platform IN ('windows', 'linux', 'android')),
    -- Menor número, mayor prioridad. Diferida para poder reordenar en una transacción.
    priority    integer NOT NULL,
    settings    jsonb NOT NULL DEFAULT '[]',
    created_by  uuid,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT policies_priority_key UNIQUE (priority) DEFERRABLE INITIALLY DEFERRED
);
CREATE UNIQUE INDEX policies_name_key ON policies (lower(name));

-- group_id nulo = todos los equipos de la plataforma.
CREATE TABLE policy_assignments (
    id        bigserial PRIMARY KEY,
    policy_id uuid NOT NULL REFERENCES policies (id) ON DELETE CASCADE,
    group_id  uuid REFERENCES groups (id) ON DELETE CASCADE,
    exclude   boolean NOT NULL DEFAULT false
);
CREATE INDEX policy_assignments_policy_idx ON policy_assignments (policy_id);

-- +goose Down
DROP TABLE policy_assignments, policies;
