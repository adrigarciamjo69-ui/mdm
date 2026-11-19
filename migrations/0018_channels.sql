-- +goose Up
CREATE TABLE smtp_config (
    id           boolean PRIMARY KEY DEFAULT true CHECK (id),
    host         text NOT NULL,
    port         integer NOT NULL CHECK (port BETWEEN 1 AND 65535),
    security     text NOT NULL CHECK (security IN ('starttls', 'tls', 'none')),
    username     text NOT NULL DEFAULT '',
    password     text NOT NULL DEFAULT '',   -- cifrada con MDM_SECRET_KEY
    from_address text NOT NULL,
    from_name    text NOT NULL DEFAULT '',
    updated_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE notification_channels (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name          text NOT NULL,
    type          text NOT NULL CHECK (type IN ('email', 'webhook')),
    recipients    text[] NOT NULL DEFAULT '{}',
    url           text NOT NULL DEFAULT '',
    secret        text NOT NULL DEFAULT '',  -- cifrado
    last_error    text NOT NULL DEFAULT '',
    last_error_at timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX notification_channels_name_key ON notification_channels (lower(name));

-- Cada envío a un canal, con sus reintentos.
CREATE TABLE deliveries (
    id          bigserial PRIMARY KEY,
    channel_id  uuid NOT NULL REFERENCES notification_channels (id) ON DELETE CASCADE,
    event       text NOT NULL,
    message     jsonb NOT NULL,
    extra_to    text[] NOT NULL DEFAULT '{}',
    alert_ids   bigint[] NOT NULL DEFAULT '{}',
    status      text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sent', 'failed')),
    attempts    integer NOT NULL DEFAULT 0,
    error       text NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now(),
    sent_at     timestamptz
);
CREATE INDEX deliveries_channel_idx ON deliveries (channel_id, created_at DESC);

-- +goose Down
DROP TABLE deliveries, notification_channels, smtp_config;
