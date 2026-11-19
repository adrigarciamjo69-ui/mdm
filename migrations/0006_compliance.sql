-- +goose Up
ALTER TABLE devices ADD COLUMN capabilities text[] NOT NULL DEFAULT '{}';

CREATE TABLE device_compliance (
    device_id          uuid PRIMARY KEY REFERENCES devices (id) ON DELETE CASCADE,
    state              text NOT NULL DEFAULT 'unknown' CHECK (state IN ('compliant', 'noncompliant', 'unknown')),
    reasons            jsonb NOT NULL DEFAULT '[]',
    noncompliant_since timestamptz,
    report_hash        text NOT NULL DEFAULT '',
    reported_at        timestamptz,
    evaluated_at       timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX device_compliance_state_idx ON device_compliance (state);

-- Sin clave ajena a policies: si se borra una política, sus resultados se
-- conservan hasta el siguiente informe del equipo.
CREATE TABLE device_setting_results (
    device_id    uuid NOT NULL REFERENCES devices (id) ON DELETE CASCADE,
    policy_id    uuid NOT NULL,
    key          text NOT NULL,
    status       text NOT NULL CHECK (status IN ('compliant', 'noncompliant', 'error', 'not_applicable')),
    message      text NOT NULL DEFAULT '',
    applied      boolean NOT NULL DEFAULT false,
    evaluated_at timestamptz NOT NULL,
    PRIMARY KEY (device_id, policy_id, key)
);

-- +goose Down
DROP TABLE device_setting_results, device_compliance;
ALTER TABLE devices DROP COLUMN capabilities;
