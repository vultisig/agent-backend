-- Conversations table queries

-- name: CreateConversation :one
INSERT INTO agent_conversations (public_key)
VALUES ($1)
RETURNING *;

-- name: GetConversationByID :one
SELECT * FROM agent_conversations
WHERE id = $1 AND public_key = $2 AND archived_at IS NULL;

-- name: ListConversations :many
SELECT * FROM agent_conversations
WHERE public_key = $1 AND archived_at IS NULL
ORDER BY updated_at DESC
LIMIT $2 OFFSET $3;

-- name: CountConversations :one
SELECT COUNT(*) FROM agent_conversations
WHERE public_key = $1 AND archived_at IS NULL;

-- name: ArchiveConversation :execrows
UPDATE agent_conversations
SET archived_at = NOW(), updated_at = NOW()
WHERE id = $1 AND public_key = $2 AND archived_at IS NULL;

-- name: UpdateConversationTitle :execrows
UPDATE agent_conversations
SET title = $1, updated_at = NOW()
WHERE id = $2 AND archived_at IS NULL;

-- name: UpdateConversationSummary :execrows
UPDATE agent_conversations
SET summary = $1, updated_at = NOW()
WHERE id = $2;

-- name: GetConversationSummary :one
SELECT summary FROM agent_conversations
WHERE id = $1;
