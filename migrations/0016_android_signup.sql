-- +goose Up
-- Valor de un solo uso para la vuelta del registro en Google: la cookie de
-- sesión (SameSite=Strict) no llega en esa redirección entre sitios.
ALTER TABLE android_config ADD COLUMN signup_state text NOT NULL DEFAULT '', ADD COLUMN signup_by uuid;

-- +goose Down
ALTER TABLE android_config DROP COLUMN signup_state, DROP COLUMN signup_by;
