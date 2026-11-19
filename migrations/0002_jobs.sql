-- +goose Up
CREATE TABLE jobs (
    id           bigserial PRIMARY KEY,
    kind         text NOT NULL,
    payload      jsonb NOT NULL DEFAULT '{}',
    unique_key   text,
    run_at       timestamptz NOT NULL,
    attempts     integer NOT NULL DEFAULT 0,
    max_attempts integer NOT NULL DEFAULT 5,
    locked_until timestamptz,
    last_error   text NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL DEFAULT now(),
    finished_at  timestamptz,
    failed_at    timestamptz
);
-- Como mucho una tarea viva (ni terminada ni fallida) por clave única.
CREATE UNIQUE INDEX jobs_unique_key ON jobs (unique_key) WHERE finished_at IS NULL AND failed_at IS NULL;
CREATE INDEX jobs_ready ON jobs (run_at) WHERE finished_at IS NULL AND failed_at IS NULL;

-- +goose Down
DROP TABLE jobs;
