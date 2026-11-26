-- +goose Up
-- Persona y departamento de los activos con agente siguen al usuario con la
-- sesión iniciada en el equipo, salvo que se bloqueen a mano.
ALTER TABLE assets
    ADD COLUMN owner_locked      boolean NOT NULL DEFAULT false,
    ADD COLUMN department_locked boolean NOT NULL DEFAULT false;

-- Nombre de inicio de sesión de Windows (onPremisesSamAccountName) para unir
-- "DOMINIO\usuario" con la persona de Entra ID.
ALTER TABLE directory_users ADD COLUMN sam_account text NOT NULL DEFAULT '';
CREATE INDEX directory_users_sam_idx ON directory_users (lower(sam_account));

-- +goose Down
DROP INDEX directory_users_sam_idx;
ALTER TABLE directory_users DROP COLUMN sam_account;
ALTER TABLE assets DROP COLUMN department_locked, DROP COLUMN owner_locked;
