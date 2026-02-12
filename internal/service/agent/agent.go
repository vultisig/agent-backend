package agent

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/vultisig/agent-backend/internal/ai/anthropic"
	"github.com/vultisig/agent-backend/internal/cache/redis"
	"github.com/vultisig/agent-backend/internal/config"
	"github.com/vultisig/agent-backend/internal/service/verifier"
	"github.com/vultisig/agent-backend/internal/storage/postgres"
	"github.com/vultisig/agent-backend/internal/types"
)

// PluginSkillsProvider provides plugin skills for prompt building.
type PluginSkillsProvider interface {
	GetSkills(ctx context.Context) []PluginSkill
}

// AgentService handles AI agent operations.
type AgentService struct {
	anthropic        *anthropic.Client
	msgRepo          *postgres.MessageRepository
	convRepo         *postgres.ConversationRepository
	memRepo          *postgres.MemoryRepository
	redis            *redis.Client
	verifier         *verifier.Client
	pluginProvider   PluginSkillsProvider
	logger           *logrus.Logger
	summaryModel     string
	windowSize       int
	summarizeTrigger int
	summaryMaxTokens int
}

// conversationWindow holds a windowed view of conversation messages plus optional summary.
type conversationWindow struct {
	messages []types.Message
	summary  *string
	total    int
}

// NewAgentService creates a new AgentService.
func NewAgentService(
	anthropicClient *anthropic.Client,
	msgRepo *postgres.MessageRepository,
	convRepo *postgres.ConversationRepository,
	memRepo *postgres.MemoryRepository,
	redisClient *redis.Client,
	verifierClient *verifier.Client,
	pluginProvider PluginSkillsProvider,
	logger *logrus.Logger,
	summaryModel string,
	ctxCfg config.ContextConfig,
) *AgentService {
	return &AgentService{
		anthropic:        anthropicClient,
		msgRepo:          msgRepo,
		convRepo:         convRepo,
		memRepo:          memRepo,
		redis:            redisClient,
		verifier:         verifierClient,
		pluginProvider:   pluginProvider,
		logger:           logger,
		summaryModel:     summaryModel,
		windowSize:       ctxCfg.WindowSize,
		summarizeTrigger: ctxCfg.SummarizeTrigger,
		summaryMaxTokens: ctxCfg.SummaryMaxTokens,
	}
}

// ProcessMessage routes the request to the appropriate ability handler.
func (s *AgentService) ProcessMessage(ctx context.Context, convID uuid.UUID, publicKey string, req *SendMessageRequest) (*SendMessageResponse, error) {
	// Validate conversation exists and belongs to user
	_, err := s.convRepo.GetByID(ctx, convID, publicKey)
	if err != nil {
		if errors.Is(err, postgres.ErrNotFound) {
			return nil, fmt.Errorf("conversation not found")
		}
		return nil, fmt.Errorf("get conversation: %w", err)
	}

	// Load conversation window once before routing to abilities
	window, err := s.getConversationWindow(ctx, convID)
	if err != nil {
		return nil, fmt.Errorf("get conversation window: %w", err)
	}

	// Route based on request content
	switch {
	case req.ActionResult != nil:
		// Ability 3: Action confirmation
		return s.confirmAction(ctx, convID, req, window)
	case req.SelectedSuggestionID != nil:
		// Ability 2: Policy builder
		return s.buildPolicy(ctx, convID, req, window)
	default:
		// Ability 1: Intent detection (default)
		return s.detectIntent(ctx, convID, req, window)
	}
}

// getConversationWindow returns a windowed view of the conversation.
// Uses a summary_up_to cursor to only count/load messages after the last summarization point.
// This prevents re-summarizing on every request once the trigger threshold is crossed.
func (s *AgentService) getConversationWindow(ctx context.Context, convID uuid.UUID) (*conversationWindow, error) {
	// Load summary and cursor together
	summary, cursor, err := s.convRepo.GetSummaryWithCursor(ctx, convID)
	if err != nil {
		return nil, fmt.Errorf("get summary with cursor: %w", err)
	}

	// Cursor-aware path: only count messages after the cursor
	if cursor != nil {
		count, err := s.msgRepo.CountSince(ctx, convID, *cursor)
		if err != nil {
			return nil, fmt.Errorf("count messages since cursor: %w", err)
		}

		s.logger.WithFields(logrus.Fields{
			"conversation_id":   convID,
			"active_count":      count,
			"window_size":       s.windowSize,
			"summarize_trigger": s.summarizeTrigger,
			"has_cursor":        true,
		}).Debug("context window state")

		// Active messages fit in window — load all since cursor
		if count <= s.windowSize {
			msgs, err := s.msgRepo.GetSince(ctx, convID, *cursor)
			if err != nil {
				return nil, fmt.Errorf("get messages since cursor: %w", err)
			}
			return &conversationWindow{messages: msgs, summary: summary, total: count}, nil
		}

		// Active messages exceed trigger — re-summarize
		if count > s.summarizeTrigger {
			allSinceCursor, err := s.msgRepo.GetSince(ctx, convID, *cursor)
			if err != nil {
				return nil, fmt.Errorf("get messages since cursor: %w", err)
			}

			if err := s.summarizeOldMessages(ctx, convID, allSinceCursor); err != nil {
				s.logger.WithError(err).Error("synchronous summarization failed")
				// Fall back to recent window + existing summary
			}

			// Reload summary+cursor after summarization (cursor has advanced)
			summary, cursor, err = s.convRepo.GetSummaryWithCursor(ctx, convID)
			if err != nil {
				return nil, fmt.Errorf("get summary after summarization: %w", err)
			}

			if cursor != nil {
				recentMsgs, err := s.msgRepo.GetRecentSince(ctx, convID, *cursor, s.windowSize)
				if err != nil {
					return nil, fmt.Errorf("get recent messages since cursor: %w", err)
				}
				return &conversationWindow{messages: recentMsgs, summary: summary, total: len(recentMsgs)}, nil
			}
		}

		// Between window and trigger — load recent since cursor
		msgs, err := s.msgRepo.GetRecentSince(ctx, convID, *cursor, s.windowSize)
		if err != nil {
			return nil, fmt.Errorf("get recent messages since cursor: %w", err)
		}
		return &conversationWindow{messages: msgs, summary: summary, total: count}, nil
	}

	// No cursor — first summarization hasn't happened yet
	total, err := s.msgRepo.CountByConversationID(ctx, convID)
	if err != nil {
		return nil, fmt.Errorf("count messages: %w", err)
	}

	s.logger.WithFields(logrus.Fields{
		"conversation_id":   convID,
		"total":             total,
		"window_size":       s.windowSize,
		"summarize_trigger": s.summarizeTrigger,
		"has_cursor":        false,
	}).Debug("context window state")

	// All messages fit in window
	if total <= s.windowSize {
		msgs, err := s.msgRepo.GetByConversationID(ctx, convID)
		if err != nil {
			return nil, fmt.Errorf("get messages: %w", err)
		}
		return &conversationWindow{messages: msgs, total: total}, nil
	}

	// Past trigger — first-time summarization
	if total > s.summarizeTrigger {
		allMsgs, err := s.msgRepo.GetByConversationID(ctx, convID)
		if err != nil {
			return nil, fmt.Errorf("get messages: %w", err)
		}

		if err := s.summarizeOldMessages(ctx, convID, allMsgs); err != nil {
			s.logger.WithError(err).Error("synchronous summarization failed")
			return &conversationWindow{messages: allMsgs, total: total}, nil
		}

		// Reload summary+cursor after first summarization
		summary, cursor, err = s.convRepo.GetSummaryWithCursor(ctx, convID)
		if err != nil {
			return nil, fmt.Errorf("get summary after summarization: %w", err)
		}

		if cursor != nil {
			recentMsgs, err := s.msgRepo.GetRecentSince(ctx, convID, *cursor, s.windowSize)
			if err != nil {
				return nil, fmt.Errorf("get recent messages since cursor: %w", err)
			}
			return &conversationWindow{messages: recentMsgs, summary: summary, total: len(recentMsgs)}, nil
		}

		// Fallback if cursor wasn't set (shouldn't happen)
		recentMsgs, err := s.msgRepo.GetRecent(ctx, convID, s.windowSize)
		if err != nil {
			return nil, fmt.Errorf("get recent messages: %w", err)
		}
		return &conversationWindow{messages: recentMsgs, summary: summary, total: total}, nil
	}

	// Between window and trigger, no cursor yet — load all messages
	msgs, err := s.msgRepo.GetByConversationID(ctx, convID)
	if err != nil {
		return nil, fmt.Errorf("get messages: %w", err)
	}
	return &conversationWindow{messages: msgs, total: total}, nil
}

// summarizeOldMessages summarizes messages outside the recent window and stores the summary.
// It runs synchronously and advances the summary_up_to cursor to the last summarized message.
func (s *AgentService) summarizeOldMessages(ctx context.Context, convID uuid.UUID, allMsgs []types.Message) error {
	if len(allMsgs) <= s.windowSize {
		return nil
	}

	// Split: old messages to summarize, recent window to keep
	oldMsgs := allMsgs[:len(allMsgs)-s.windowSize]

	// Build content to summarize
	var oldContent string
	for _, msg := range oldMsgs {
		oldContent += fmt.Sprintf("[%s]: %s\n\n", msg.Role, msg.Content)
	}

	// Include existing summary for incremental summarization
	existingSummary, _, _ := s.convRepo.GetSummaryWithCursor(ctx, convID)
	prompt := SummarizationPrompt
	if existingSummary != nil {
		prompt += "\n\n## Previous Summary\n\n" + *existingSummary
	}
	prompt += "\n\n## Messages to Summarize\n\n" + oldContent

	// Call Claude Haiku for summarization
	req := &anthropic.Request{
		Model:     s.summaryModel,
		MaxTokens: s.summaryMaxTokens,
		Messages: []anthropic.Message{
			{Role: "user", Content: prompt},
		},
	}

	resp, err := s.anthropic.SendMessage(ctx, req)
	if err != nil {
		return fmt.Errorf("call anthropic: %w", err)
	}

	// Extract text response
	var summaryText string
	for _, block := range resp.Content {
		if block.Type == "text" {
			summaryText = block.Text
			break
		}
	}

	if summaryText == "" {
		return fmt.Errorf("empty response from anthropic")
	}

	// Advance cursor to the last summarized message's timestamp
	summaryUpTo := oldMsgs[len(oldMsgs)-1].CreatedAt
	if err := s.convRepo.UpdateSummaryWithCursor(ctx, convID, summaryText, summaryUpTo); err != nil {
		return fmt.Errorf("store summary with cursor: %w", err)
	}

	s.logger.WithFields(logrus.Fields{
		"conversation_id": convID,
		"summary_length":  len(summaryText),
		"summary_up_to":   summaryUpTo,
	}).Info("conversation summary updated")
	return nil
}

// anthropicMessagesFromWindow converts conversation window messages to Anthropic message format,
// skipping system messages.
func anthropicMessagesFromWindow(window *conversationWindow) []anthropic.Message {
	msgs := make([]anthropic.Message, 0, len(window.messages))
	for _, msg := range window.messages {
		if msg.Role == types.RoleSystem {
			continue
		}
		msgs = append(msgs, anthropic.Message{
			Role:    string(msg.Role),
			Content: msg.Content,
		})
	}
	return msgs
}

