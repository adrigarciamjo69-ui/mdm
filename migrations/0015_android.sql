-- +goose Up
ALTER TABLE devices DROP CONSTRAINT devices_platform_check;
ALTER TABLE devices ADD CONSTRAINT devices_platform_check CHECK (platform IN ('windows', 'linux', 'android'));
-- Nombre del dispositivo en la Android Management API (enterprises/…/devices/…).
ALTER TABLE devices ADD COLUMN amapi_name text;
CREATE UNIQUE INDEX devices_amapi_name_key ON devices (amapi_name) WHERE amapi_name IS NOT NULL;

-- Configuración de la integración con Google (fila única).
CREATE TABLE android_config (
    id                      boolean PRIMARY KEY DEFAULT true CHECK (id),
    service_account         text NOT NULL,            -- clave JSON cifrada con MDM_SECRET_KEY
    project_id              text NOT NULL,
    client_email            text NOT NULL,
    signup_url_name         text NOT NULL DEFAULT '',
    enterprise_name         text NOT NULL DEFAULT '',
    enterprise_display_name text NOT NULL DEFAULT '',
    linked_at               timestamptz,
    linked_by               uuid,
    last_sync_at            timestamptz,
    last_sync_error         text NOT NULL DEFAULT '',
    updated_at              timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE android_enrollment_tokens (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name       text NOT NULL,
    amapi_name text NOT NULL,
    qr_code    text NOT NULL,
    group_id   uuid REFERENCES groups (id) ON DELETE SET NULL,
    one_time   boolean NOT NULL,
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    created_by uuid,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- Políticas publicadas en Google, una por combinación distinta (por huella).
CREATE TABLE android_policies (
    hash         text PRIMARY KEY,
    amapi_name   text NOT NULL,
    published_at timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE android_policies, android_enrollment_tokens, android_config;
DROP INDEX devices_amapi_name_key;
ALTER TABLE devices DROP COLUMN amapi_name;
ALTER TABLE devices DROP CONSTRAINT devices_platform_check;
ALTER TABLE devices ADD CONSTRAINT devices_platform_check CHECK (platform IN ('windows', 'linux'));
