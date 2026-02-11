-- +goose Up
ALTER TABLE agent_conversations ADD COLUMN summary_up_to TIMESTAMPTZ;

-- +goose Down
ALTER TABLE agent_conversations DROP COLUMN summary_up_to;
