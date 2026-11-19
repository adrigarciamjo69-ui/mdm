-- +goose Up
CREATE TABLE files (
    sha256        text PRIMARY KEY,
    size          bigint NOT NULL,
    original_name text NOT NULL DEFAULT '',
    content_type  text NOT NULL DEFAULT '',
    uploaded_by   uuid,
    uploaded_at   timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE files;
