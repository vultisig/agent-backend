-- name: GetMemory :one
SELECT * FROM agent_user_memories
WHERE public_key = $1;

-- name: UpsertMemory :exec
INSERT INTO agent_user_memories (public_key, content, updated_at)
VALUES ($1, $2, NOW())
ON CONFLICT (public_key) DO UPDATE
SET content = $2, updated_at = NOW();
