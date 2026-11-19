-- +goose Up
-- Operación de Google con la que se sigue una orden enviada a un móvil.
ALTER TABLE commands ADD COLUMN external_ref text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE commands DROP COLUMN external_ref;
