-- Agent Backend Schema
-- This file is used by sqlc to generate Go types

CREATE TYPE agent_message_role AS ENUM ('user', 'assistant', 'system');

CREATE TABLE agent_conversations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    public_key VARCHAR(66) NOT NULL,
    title TEXT,
    summary TEXT,
    summary_up_to TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    archived_at TIMESTAMPTZ
);

CREATE INDEX idx_agent_conversations_public_key ON agent_conversations(public_key);

CREATE TABLE agent_messages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id UUID NOT NULL REFERENCES agent_conversations(id) ON DELETE CASCADE,
    role agent_message_role NOT NULL,
    content TEXT NOT NULL,
    content_type VARCHAR(50) NOT NULL DEFAULT 'text',
    audio_url TEXT,
    metadata JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_agent_messages_conversation ON agent_messages(conversation_id);

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
