-- +goose Up
CREATE TABLE command_batches (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    type               text NOT NULL,
    params             jsonb NOT NULL DEFAULT '{}',
    target_description text NOT NULL DEFAULT '',
    device_count       integer NOT NULL DEFAULT 0,
    created_by         uuid,
    created_by_email   text NOT NULL DEFAULT '',
    created_at         timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX command_batches_created_idx ON command_batches (created_at DESC);

ALTER TABLE commands
    ADD COLUMN batch_id    uuid REFERENCES command_batches (id) ON DELETE SET NULL,
    ADD COLUMN sent_at     timestamptz,
    ADD COLUMN started_at  timestamptz,
    ADD COLUMN finished_at timestamptz,
    ADD COLUMN expires_at  timestamptz NOT NULL DEFAULT now() + interval '7 days',
    ADD COLUMN exit_code   integer,
    ADD COLUMN stdout      text NOT NULL DEFAULT '',
    ADD COLUMN stderr      text NOT NULL DEFAULT '',
    ADD COLUMN error       text NOT NULL DEFAULT '',
    ADD CONSTRAINT commands_status_check CHECK (status IN ('pending', 'sent', 'running', 'succeeded', 'failed', 'expired', 'cancelled'));
CREATE INDEX commands_device_status_idx ON commands (device_id, status, created_at);
CREATE INDEX commands_batch_idx ON commands (batch_id);

-- +goose Down
ALTER TABLE commands DROP CONSTRAINT commands_status_check, DROP COLUMN batch_id, DROP COLUMN sent_at, DROP COLUMN started_at,
    DROP COLUMN finished_at, DROP COLUMN expires_at, DROP COLUMN exit_code, DROP COLUMN stdout, DROP COLUMN stderr, DROP COLUMN error;
DROP TABLE command_batches;
