-- +goose Up
CREATE TABLE asset_categories (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name       text NOT NULL,
    sort       int NOT NULL DEFAULT 100,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX asset_categories_name_idx ON asset_categories (lower(name));
INSERT INTO asset_categories (name, sort) VALUES
    ('Portátil', 10), ('Sobremesa', 20), ('Ordenador', 30), ('Servidor', 40), ('Móvil', 50), ('Tableta', 60),
    ('Monitor', 70), ('Impresora', 80), ('Red', 90), ('Periférico', 100), ('Otro', 110);

CREATE TABLE directory_users (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    entra_id     text NOT NULL UNIQUE,
    display_name text NOT NULL DEFAULT '',
    email        text NOT NULL DEFAULT '',
    upn          text NOT NULL DEFAULT '',
    department   text NOT NULL DEFAULT '',
    job_title    text NOT NULL DEFAULT '',
    employee_id  text NOT NULL DEFAULT '',
    phone        text NOT NULL DEFAULT '',
    office       text NOT NULL DEFAULT '',
    enabled      boolean NOT NULL DEFAULT true,
    deleted_at   timestamptz,
    synced_at    timestamptz NOT NULL DEFAULT now(),
    notes        text NOT NULL DEFAULT ''
);
CREATE INDEX directory_users_email_idx ON directory_users (lower(email));
CREATE INDEX directory_users_upn_idx ON directory_users (lower(upn));

CREATE TABLE entra_config (
    id              boolean PRIMARY KEY DEFAULT true CHECK (id),
    tenant_id       text NOT NULL DEFAULT '',
    client_id       text NOT NULL DEFAULT '',
    sealed_secret   text NOT NULL DEFAULT '',
    secret_expires  date,
    last_sync_at    timestamptz,
    last_success_at timestamptz,
    last_error      text NOT NULL DEFAULT '',
    last_count      int NOT NULL DEFAULT 0
);

CREATE TABLE assets (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id      uuid UNIQUE REFERENCES devices (id) ON DELETE SET NULL,
    category_id    uuid NOT NULL REFERENCES asset_categories (id),
    category_auto  boolean NOT NULL DEFAULT false,
    name           text NOT NULL,
    brand          text NOT NULL DEFAULT '',
    model          text NOT NULL DEFAULT '',
    serial         text NOT NULL DEFAULT '',
    -- serial_key es el número de serie normalizado, vacío si no sirve para unir.
    serial_key     text NOT NULL DEFAULT '',
    asset_tag      text NOT NULL DEFAULT '',
    status         text NOT NULL CHECK (status IN ('en_uso', 'en_almacen', 'en_reparacion', 'baja')),
    owner_id       uuid REFERENCES directory_users (id) ON DELETE SET NULL,
    department     text NOT NULL DEFAULT '',
    location       text NOT NULL DEFAULT '',
    price          numeric(12, 2),
    purchase_date  date,
    warranty_until date,
    purchase_order text NOT NULL DEFAULT '',
    supplier       text NOT NULL DEFAULT '',
    notes          text NOT NULL DEFAULT '',
    deleted_at     timestamptz,
    deleted_by     uuid,
    deleted_reason text NOT NULL DEFAULT '',
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX assets_serial_key_idx ON assets (serial_key) WHERE serial_key <> '' AND deleted_at IS NULL;
CREATE UNIQUE INDEX assets_tag_idx ON assets (lower(asset_tag)) WHERE asset_tag <> '' AND deleted_at IS NULL;
CREATE INDEX assets_owner_idx ON assets (owner_id);
CREATE INDEX assets_category_idx ON assets (category_id);

CREATE TABLE asset_documents (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    asset_id     uuid NOT NULL REFERENCES assets (id) ON DELETE CASCADE,
    sha256       text NOT NULL,
    filename     text NOT NULL,
    content_type text NOT NULL,
    size         bigint NOT NULL,
    uploaded_by  uuid,
    uploaded_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX asset_documents_asset_idx ON asset_documents (asset_id);
CREATE INDEX asset_documents_sha_idx ON asset_documents (sha256);

CREATE TABLE licenses (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name           text NOT NULL,
    vendor         text NOT NULL DEFAULT '',
    version        text NOT NULL DEFAULT '',
    license_type   text NOT NULL CHECK (license_type IN ('perpetua', 'suscripcion', 'volumen', 'oem', 'freeware', 'open_source', 'prueba')),
    seats          int NOT NULL DEFAULT 0 CHECK (seats >= 0),
    key_sealed     text NOT NULL DEFAULT '',
    purchase_date  date,
    expiry_date    date,
    purchase_order text NOT NULL DEFAULT '',
    price          numeric(12, 2),
    department     text NOT NULL DEFAULT '',
    status         text NOT NULL CHECK (status IN ('activa', 'expirada', 'baja')),
    match_pattern  text NOT NULL DEFAULT '',
    notes          text NOT NULL DEFAULT '',
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE license_assignments (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    license_id  uuid NOT NULL REFERENCES licenses (id) ON DELETE CASCADE,
    asset_id    uuid REFERENCES assets (id) ON DELETE CASCADE,
    user_id     uuid REFERENCES directory_users (id) ON DELETE CASCADE,
    notes       text NOT NULL DEFAULT '',
    assigned_at timestamptz NOT NULL DEFAULT now(),
    CHECK ((asset_id IS NULL) <> (user_id IS NULL))
);
CREATE UNIQUE INDEX license_assignments_asset_idx ON license_assignments (license_id, asset_id) WHERE asset_id IS NOT NULL;
CREATE UNIQUE INDEX license_assignments_user_idx ON license_assignments (license_id, user_id) WHERE user_id IS NOT NULL;

CREATE TABLE delivery_counters (
    type text NOT NULL,
    year int NOT NULL,
    last int NOT NULL,
    PRIMARY KEY (type, year)
);

CREATE TABLE delivery_records (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    number        text NOT NULL DEFAULT '',
    type          text NOT NULL CHECK (type IN ('entrega', 'devolucion')),
    user_id       uuid NOT NULL REFERENCES directory_users (id),
    recipient_dni text NOT NULL DEFAULT '',
    date          date NOT NULL,
    responsible   text NOT NULL DEFAULT '',
    status        text NOT NULL DEFAULT 'borrador' CHECK (status IN ('borrador', 'confirmada', 'anulada')),
    notes         text NOT NULL DEFAULT '',
    cancel_reason text NOT NULL DEFAULT '',
    created_by    uuid,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    confirmed_at  timestamptz
);
CREATE UNIQUE INDEX delivery_records_number_idx ON delivery_records (number) WHERE number <> '';
CREATE INDEX delivery_records_user_idx ON delivery_records (user_id);

CREATE TABLE delivery_items (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    record_id    uuid NOT NULL REFERENCES delivery_records (id) ON DELETE CASCADE,
    position     int NOT NULL,
    asset_id     uuid REFERENCES assets (id) ON DELETE SET NULL,
    category     text NOT NULL DEFAULT '',
    brand        text NOT NULL DEFAULT '',
    model        text NOT NULL DEFAULT '',
    serial       text NOT NULL DEFAULT '',
    observations text NOT NULL DEFAULT ''
);
CREATE INDEX delivery_items_record_idx ON delivery_items (record_id);
CREATE INDEX delivery_items_asset_idx ON delivery_items (asset_id);

CREATE TABLE delivery_settings (
    id   boolean PRIMARY KEY DEFAULT true CHECK (id),
    data jsonb NOT NULL
);

-- +goose Down
DROP TABLE delivery_settings;
DROP TABLE delivery_items;
DROP TABLE delivery_records;
DROP TABLE delivery_counters;
DROP TABLE license_assignments;
DROP TABLE licenses;
DROP TABLE asset_documents;
DROP TABLE assets;
DROP TABLE entra_config;
DROP TABLE directory_users;
DROP TABLE asset_categories;
