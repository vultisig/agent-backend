-- +goose Up
ALTER TABLE agent_conversations ADD COLUMN summary TEXT;

-- +goose Down
ALTER TABLE agent_conversations DROP COLUMN summary;
