-- +goose Up
CREATE TABLE alert_rules (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name            text NOT NULL,
    type            text NOT NULL,
    params          jsonb NOT NULL DEFAULT '{}',
    severity        text NOT NULL CHECK (severity IN ('info', 'warning', 'critical')),
    channels        uuid[] NOT NULL DEFAULT '{}',
    emails          text[] NOT NULL DEFAULT '{}',
    notify_resolved boolean NOT NULL DEFAULT true,
    enabled         boolean NOT NULL DEFAULT true,
    created_by      uuid,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX alert_rules_name_key ON alert_rules (lower(name));

CREATE TABLE alert_rule_assignments (
    id       bigserial PRIMARY KEY,
    rule_id  uuid NOT NULL REFERENCES alert_rules (id) ON DELETE CASCADE,
    group_id uuid REFERENCES groups (id) ON DELETE CASCADE,
    exclude  boolean NOT NULL DEFAULT false
);

CREATE TABLE alerts (
    id                bigserial PRIMARY KEY,
    rule_id           uuid REFERENCES alert_rules (id) ON DELETE SET NULL,
    rule_name         text NOT NULL,
    device_id         uuid REFERENCES devices (id) ON DELETE CASCADE,
    state             text NOT NULL DEFAULT 'open' CHECK (state IN ('open', 'acknowledged', 'resolved')),
    severity          text NOT NULL,
    title             text NOT NULL,
    details           jsonb NOT NULL DEFAULT '{}',
    opened_at         timestamptz NOT NULL,
    acknowledged_at   timestamptz,
    acknowledged_by   uuid,
    resolved_at       timestamptz,
    resolved_by       uuid,
    notified_open     boolean NOT NULL DEFAULT false,
    notified_resolved boolean NOT NULL DEFAULT false
);
-- Como mucho una alerta sin resolver por regla y equipo (o global).
CREATE UNIQUE INDEX alerts_one_open ON alerts (rule_id, COALESCE(device_id, '00000000-0000-0000-0000-000000000000'::uuid)) WHERE state <> 'resolved';
CREATE INDEX alerts_state_idx ON alerts (state, opened_at DESC);
CREATE INDEX alerts_device_idx ON alerts (device_id, opened_at DESC);

-- +goose Down
DROP TABLE alerts, alert_rule_assignments, alert_rules;
