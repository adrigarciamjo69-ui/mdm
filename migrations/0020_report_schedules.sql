-- +goose Up
CREATE TABLE report_schedules (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    report      text NOT NULL,
    filters     jsonb NOT NULL DEFAULT '{}',
    frequency   text NOT NULL,
    time        text NOT NULL,
    day         integer NOT NULL DEFAULT 0,
    channel_id  uuid NOT NULL REFERENCES notification_channels (id) ON DELETE CASCADE,
    enabled     boolean NOT NULL DEFAULT true,
    last_run_at timestamptz,
    last_status text NOT NULL DEFAULT '',
    created_by  uuid,
    created_at  timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE report_schedules;
