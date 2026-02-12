-- User memories table queries

-- name: CreateMemory :one
INSERT INTO agent_user_memories (public_key, content, category, memory_type)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetCoreMemories :many
SELECT * FROM agent_user_memories
WHERE public_key = $1 AND memory_type = 'core'
ORDER BY created_at ASC;

-- name: CountCoreMemories :one
SELECT COUNT(*) FROM agent_user_memories
WHERE public_key = $1 AND memory_type = 'core';

-- name: SearchArchivalMemories :many
SELECT * FROM agent_user_memories
WHERE public_key = $1 AND memory_type = 'archival' AND content ILIKE '%' || $2 || '%'
ORDER BY created_at DESC
LIMIT 10;

-- name: SearchArchivalMemoriesByCategory :many
SELECT * FROM agent_user_memories
WHERE public_key = $1 AND memory_type = 'archival' AND content ILIKE '%' || $2 || '%' AND category = $3
ORDER BY created_at DESC
LIMIT 10;

-- name: DeleteMemoryByID :execrows
DELETE FROM agent_user_memories
WHERE id = $1 AND public_key = $2;

-- name: GetMemoryByContent :one
SELECT * FROM agent_user_memories
WHERE public_key = $1 AND content = $2
LIMIT 1;
