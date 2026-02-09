package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vultisig/agent-backend/internal/storage/postgres/queries"
	"github.com/vultisig/agent-backend/internal/types"
)

// ErrNotFound is returned when a resource is not found.
var ErrNotFound = errors.New("not found")

// ConversationRepository handles database operations for conversations.
type ConversationRepository struct {
	q *queries.Queries
}

// NewConversationRepository creates a new ConversationRepository.
func NewConversationRepository(pool *pgxpool.Pool) *ConversationRepository {
	return &ConversationRepository{
		q: queries.New(pool),
	}
}

// Create creates a new conversation for the given public key.
func (r *ConversationRepository) Create(ctx context.Context, publicKey string) (*types.Conversation, error) {
	conv, err := r.q.CreateConversation(ctx, publicKey)
	if err != nil {
		return nil, fmt.Errorf("create conversation: %w", err)
	}
	return conversationFromDB(conv), nil
}

// GetByID returns a conversation if it exists and belongs to the given public key.
func (r *ConversationRepository) GetByID(ctx context.Context, id uuid.UUID, publicKey string) (*types.Conversation, error) {
	conv, err := r.q.GetConversationByID(ctx, &queries.GetConversationByIDParams{
		ID:        uuidToPgtype(id),
		PublicKey: publicKey,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get conversation: %w", err)
	}
	return conversationFromDB(conv), nil
}

// GetWithMessages returns a conversation with all its messages.
func (r *ConversationRepository) GetWithMessages(ctx context.Context, id uuid.UUID, publicKey string) (*types.ConversationWithMessages, error) {
	conv, err := r.GetByID(ctx, id, publicKey)
	if err != nil {
		return nil, err
	}

	msgs, err := r.q.GetMessagesByConversationID(ctx, uuidToPgtype(id))
	if err != nil {
		return nil, fmt.Errorf("get messages: %w", err)
	}

	return &types.ConversationWithMessages{
		Conversation: *conv,
		Messages:     messagesFromDB(msgs),
	}, nil
}

// List returns paginated conversations for a public key.
func (r *ConversationRepository) List(ctx context.Context, publicKey string, skip, take int) ([]types.Conversation, int, error) {
	totalCount, err := r.q.CountConversations(ctx, publicKey)
	if err != nil {
		return nil, 0, fmt.Errorf("count conversations: %w", err)
	}

	convs, err := r.q.ListConversations(ctx, &queries.ListConversationsParams{
		PublicKey: publicKey,
		Limit:     int32(take),
		Offset:    int32(skip),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("list conversations: %w", err)
	}

	return conversationsFromDB(convs), int(totalCount), nil
}

// Archive soft-deletes a conversation by setting archived_at.
func (r *ConversationRepository) Archive(ctx context.Context, id uuid.UUID, publicKey string) error {
	rowsAffected, err := r.q.ArchiveConversation(ctx, &queries.ArchiveConversationParams{
		ID:        uuidToPgtype(id),
		PublicKey: publicKey,
	})
	if err != nil {
		return fmt.Errorf("archive conversation: %w", err)
	}
	if rowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateTitle updates the title of a conversation.
func (r *ConversationRepository) UpdateTitle(ctx context.Context, id uuid.UUID, title string) error {
	rowsAffected, err := r.q.UpdateConversationTitle(ctx, &queries.UpdateConversationTitleParams{
		Title: stringPtrToPgtext(&title),
		ID:    uuidToPgtype(id),
	})
	if err != nil {
		return fmt.Errorf("update title: %w", err)
	}
	if rowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
