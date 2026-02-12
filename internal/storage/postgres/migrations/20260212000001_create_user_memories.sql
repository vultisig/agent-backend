-- +goose Up
-- +goose StatementBegin
CREATE TABLE agent_user_memories (
    public_key VARCHAR(66) PRIMARY KEY,
    content TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS agent_user_memories;
