package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vultisig/agent-backend/internal/storage/postgres/queries"
	"github.com/vultisig/agent-backend/internal/types"
)

// MemoryRepository handles user memory persistence.
type MemoryRepository struct {
	q *queries.Queries
}

// NewMemoryRepository creates a new MemoryRepository.
func NewMemoryRepository(pool *pgxpool.Pool) *MemoryRepository {
	return &MemoryRepository{q: queries.New(pool)}
}

// GetMemory returns the user's memory document. Returns nil if no row exists.
func (r *MemoryRepository) GetMemory(ctx context.Context, publicKey string) (*types.UserMemory, error) {
	result, err := r.q.GetMemory(ctx, publicKey)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get memory: %w", err)
	}
	return userMemoryFromDB(result), nil
}

// UpsertMemory inserts or updates the user's memory document.
func (r *MemoryRepository) UpsertMemory(ctx context.Context, publicKey, content string) error {
	err := r.q.UpsertMemory(ctx, &queries.UpsertMemoryParams{
		PublicKey: publicKey,
		Content:   content,
	})
	if err != nil {
		return fmt.Errorf("upsert memory: %w", err)
	}
	return nil
}
