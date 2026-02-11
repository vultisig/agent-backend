package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vultisig/agent-backend/internal/storage/postgres/queries"
	"github.com/vultisig/agent-backend/internal/types"
)

// MessageRepository handles database operations for messages.
type MessageRepository struct {
	q *queries.Queries
}

// NewMessageRepository creates a new MessageRepository.
func NewMessageRepository(pool *pgxpool.Pool) *MessageRepository {
	return &MessageRepository{
		q: queries.New(pool),
	}
}

// Create creates a new message.
func (r *MessageRepository) Create(ctx context.Context, msg *types.Message) error {
	created, err := r.q.CreateMessage(ctx, &queries.CreateMessageParams{
		ConversationID: uuidToPgtype(msg.ConversationID),
		Role:           messageRoleToDB(msg.Role),
		Content:        msg.Content,
		ContentType:    msg.ContentType,
		AudioUrl:       stringPtrToPgtext(msg.AudioURL),
		Metadata:       msg.Metadata,
	})
	if err != nil {
		return fmt.Errorf("create message: %w", err)
	}

	// Update the input message with generated values
	msg.ID = pgtypeToUUID(created.ID)
	msg.CreatedAt = pgtimestamptzToTime(created.CreatedAt)

	return nil
}

// GetByConversationID returns all messages for a conversation, ordered by creation time.
func (r *MessageRepository) GetByConversationID(ctx context.Context, convID uuid.UUID) ([]types.Message, error) {
	msgs, err := r.q.GetMessagesByConversationID(ctx, uuidToPgtype(convID))
	if err != nil {
		return nil, fmt.Errorf("get messages: %w", err)
	}
	return messagesFromDB(msgs), nil
}

// GetRecent returns the most recent messages for a conversation in chronological order.
func (r *MessageRepository) GetRecent(ctx context.Context, convID uuid.UUID, limit int) ([]types.Message, error) {
	msgs, err := r.q.GetRecentMessages(ctx, &queries.GetRecentMessagesParams{
		ConversationID: uuidToPgtype(convID),
		Limit:          int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("get recent messages: %w", err)
	}
	result := messagesFromDB(msgs)
	// Reverse to get chronological order (query returns DESC)
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	return result, nil
}

// CountByConversationID returns the total number of messages in a conversation.
func (r *MessageRepository) CountByConversationID(ctx context.Context, convID uuid.UUID) (int, error) {
	count, err := r.q.CountMessagesByConversationID(ctx, uuidToPgtype(convID))
	if err != nil {
		return 0, fmt.Errorf("count messages: %w", err)
	}
	return int(count), nil
}

// CountSince returns the number of messages created after the given timestamp.
func (r *MessageRepository) CountSince(ctx context.Context, convID uuid.UUID, since time.Time) (int, error) {
	count, err := r.q.CountMessagesSince(ctx, &queries.CountMessagesSinceParams{
		ConversationID: uuidToPgtype(convID),
		CreatedAt:      timeToPgtimestamptz(since),
	})
	if err != nil {
		return 0, fmt.Errorf("count messages since: %w", err)
	}
	return int(count), nil
}

// GetSince returns all messages created after the given timestamp in chronological order.
func (r *MessageRepository) GetSince(ctx context.Context, convID uuid.UUID, since time.Time) ([]types.Message, error) {
	msgs, err := r.q.GetMessagesSince(ctx, &queries.GetMessagesSinceParams{
		ConversationID: uuidToPgtype(convID),
		CreatedAt:      timeToPgtimestamptz(since),
	})
	if err != nil {
		return nil, fmt.Errorf("get messages since: %w", err)
	}
	return messagesFromDB(msgs), nil
}

// GetRecentSince returns the most recent messages after the given timestamp in chronological order.
func (r *MessageRepository) GetRecentSince(ctx context.Context, convID uuid.UUID, since time.Time, limit int) ([]types.Message, error) {
	msgs, err := r.q.GetRecentMessagesSince(ctx, &queries.GetRecentMessagesSinceParams{
		ConversationID: uuidToPgtype(convID),
		CreatedAt:      timeToPgtimestamptz(since),
		Limit:          int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("get recent messages since: %w", err)
	}
	result := messagesFromDB(msgs)
	// Reverse to get chronological order (query returns DESC)
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	return result, nil
}
