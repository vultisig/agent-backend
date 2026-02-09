package postgres

import (
	"context"
	"fmt"

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
