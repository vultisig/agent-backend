package postgres

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/vultisig/agent-backend/internal/storage/postgres/queries"
	"github.com/vultisig/agent-backend/internal/types"
)

// UUID conversions

func uuidToPgtype(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{
		Bytes: id,
		Valid: true,
	}
}

func pgtypeToUUID(id pgtype.UUID) uuid.UUID {
	if !id.Valid {
		return uuid.Nil
	}
	return id.Bytes
}

// Text conversions

func stringPtrToPgtext(s *string) pgtype.Text {
	if s == nil {
		return pgtype.Text{Valid: false}
	}
	return pgtype.Text{String: *s, Valid: true}
}

func pgtextToStringPtr(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	return &t.String
}

// Timestamptz conversions

func pgtimestamptzToTime(t pgtype.Timestamptz) time.Time {
	if !t.Valid {
		return time.Time{}
	}
	return t.Time
}

func pgtimestamptzToTimePtr(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	return &t.Time
}

// Model conversions

func conversationFromDB(c *queries.AgentConversation) *types.Conversation {
	if c == nil {
		return nil
	}
	return &types.Conversation{
		ID:         pgtypeToUUID(c.ID),
		PublicKey:  c.PublicKey,
		Title:      pgtextToStringPtr(c.Title),
		Summary:    pgtextToStringPtr(c.Summary),
		CreatedAt:  pgtimestamptzToTime(c.CreatedAt),
		UpdatedAt:  pgtimestamptzToTime(c.UpdatedAt),
		ArchivedAt: pgtimestamptzToTimePtr(c.ArchivedAt),
	}
}

func conversationsFromDB(cs []*queries.AgentConversation) []types.Conversation {
	result := make([]types.Conversation, len(cs))
	for i, c := range cs {
		conv := conversationFromDB(c)
		if conv != nil {
			result[i] = *conv
		}
	}
	return result
}

func messageRoleFromDB(r queries.AgentMessageRole) types.MessageRole {
	return types.MessageRole(r)
}

func messageRoleToDB(r types.MessageRole) queries.AgentMessageRole {
	return queries.AgentMessageRole(r)
}

func messageFromDB(m *queries.AgentMessage) *types.Message {
	if m == nil {
		return nil
	}
	return &types.Message{
		ID:             pgtypeToUUID(m.ID),
		ConversationID: pgtypeToUUID(m.ConversationID),
		Role:           messageRoleFromDB(m.Role),
		Content:        m.Content,
		ContentType:    m.ContentType,
		AudioURL:       pgtextToStringPtr(m.AudioUrl),
		Metadata:       json.RawMessage(m.Metadata),
		CreatedAt:      pgtimestamptzToTime(m.CreatedAt),
	}
}

func messagesFromDB(ms []*queries.AgentMessage) []types.Message {
	result := make([]types.Message, len(ms))
	for i, m := range ms {
		msg := messageFromDB(m)
		if msg != nil {
			result[i] = *msg
		}
	}
	return result
}
