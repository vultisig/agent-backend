-- +goose Up
-- +goose StatementBegin
CREATE TABLE agent_user_memories (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    public_key VARCHAR(66) NOT NULL,
    content TEXT NOT NULL,
    category VARCHAR(20) NOT NULL,
    memory_type VARCHAR(10) NOT NULL DEFAULT 'core',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_agent_user_memories_public_key ON agent_user_memories(public_key);
CREATE INDEX idx_agent_user_memories_type ON agent_user_memories(public_key, memory_type);
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS agent_user_memories;
