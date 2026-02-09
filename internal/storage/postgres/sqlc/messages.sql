-- Messages table queries

-- name: CreateMessage :one
INSERT INTO agent_messages (conversation_id, role, content, content_type, audio_url, metadata)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetMessagesByConversationID :many
SELECT * FROM agent_messages
WHERE conversation_id = $1
ORDER BY created_at ASC;
