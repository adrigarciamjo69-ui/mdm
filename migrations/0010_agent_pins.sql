-- +goose Up
-- Grupos con la versión del agente congelada: no reciben actualizaciones.
CREATE TABLE agent_pins (
    group_id   uuid PRIMARY KEY REFERENCES groups (id) ON DELETE CASCADE,
    version    text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE agent_pins;
