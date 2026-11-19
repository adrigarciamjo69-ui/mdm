-- +goose Up
CREATE TABLE metrics_daily (
    day   date NOT NULL,
    key   text NOT NULL,
    value double precision NOT NULL,
    PRIMARY KEY (day, key)
);

CREATE TABLE admin_preferences (
    admin_id uuid NOT NULL REFERENCES admins (id) ON DELETE CASCADE,
    key      text NOT NULL,
    value    jsonb NOT NULL,
    PRIMARY KEY (admin_id, key)
);

-- +goose Down
DROP TABLE admin_preferences, metrics_daily;
