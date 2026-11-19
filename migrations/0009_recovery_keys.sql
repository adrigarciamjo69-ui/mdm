-- +goose Up
CREATE TABLE recovery_keys (
    device_id   uuid NOT NULL REFERENCES devices (id) ON DELETE CASCADE,
    key_id      text NOT NULL,
    volume      text NOT NULL,
    -- Contraseña de recuperación cifrada con MDM_SECRET_KEY (secrets.Box).
    secret      text NOT NULL,
    reported_at timestamptz NOT NULL,
    PRIMARY KEY (device_id, key_id)
);

-- +goose Down
DROP TABLE recovery_keys;
