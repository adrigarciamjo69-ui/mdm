-- +goose Up
ALTER TABLE groups
    ADD COLUMN kind text NOT NULL DEFAULT 'static' CHECK (kind IN ('static', 'dynamic')),
    ADD COLUMN rule jsonb,
    ADD CONSTRAINT groups_rule_matches_kind CHECK ((kind = 'dynamic') = (rule IS NOT NULL));

-- Pertenencia calculada de los grupos dinámicos. La de los estáticos sigue en devices.group_id.
CREATE TABLE group_members (
    group_id  uuid NOT NULL REFERENCES groups (id) ON DELETE CASCADE,
    device_id uuid NOT NULL REFERENCES devices (id) ON DELETE CASCADE,
    PRIMARY KEY (group_id, device_id)
);
CREATE INDEX group_members_device_idx ON group_members (device_id);

-- +goose Down
DROP TABLE group_members;
ALTER TABLE groups DROP CONSTRAINT groups_rule_matches_kind, DROP COLUMN rule, DROP COLUMN kind;
