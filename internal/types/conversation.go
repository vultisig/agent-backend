package types

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// MessageRole represents the role of a message sender.
type MessageRole string

const (
	RoleUser      MessageRole = "user"
	RoleAssistant MessageRole = "assistant"
	RoleSystem    MessageRole = "system"
)

// Conversation represents a chat conversation.
type Conversation struct {
	ID         uuid.UUID  `json:"id"`
	PublicKey  string     `json:"public_key"`
	Title      *string    `json:"title"`
	Summary    *string    `json:"summary,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	ArchivedAt *time.Time `json:"archived_at,omitempty"`
}

// Message represents a single message in a conversation.
type Message struct {
	ID             uuid.UUID       `json:"id"`
	ConversationID uuid.UUID       `json:"conversation_id"`
	Role           MessageRole     `json:"role"`
	Content        string          `json:"content"`
	ContentType    string          `json:"content_type"`
	AudioURL       *string         `json:"audio_url,omitempty"`
	Metadata       json.RawMessage `json:"metadata,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
}

// ConversationWithMessages includes a conversation and its messages.
type ConversationWithMessages struct {
	Conversation
	Messages []Message `json:"messages"`
}
