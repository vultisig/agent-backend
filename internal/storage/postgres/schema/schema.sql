-- Agent Backend Schema
-- This file is used by sqlc to generate Go types

CREATE TYPE agent_message_role AS ENUM ('user', 'assistant', 'system');

CREATE TABLE agent_conversations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    public_key VARCHAR(66) NOT NULL,
    title TEXT,
    summary TEXT,
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
