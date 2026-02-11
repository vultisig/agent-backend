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
// If total messages fit in the window, returns all messages.
// If total exceeds the summarize trigger, runs summarization synchronously before returning.
// Otherwise returns the most recent messages plus any existing summary.
func (s *AgentService) getConversationWindow(ctx context.Context, convID uuid.UUID) (*conversationWindow, error) {
	total, err := s.msgRepo.CountByConversationID(ctx, convID)
	if err != nil {
		return nil, fmt.Errorf("count messages: %w", err)
	}

	// If messages fit in the window, load everything — no summarization needed
	if total <= s.windowSize {
		msgs, err := s.msgRepo.GetByConversationID(ctx, convID)
		if err != nil {
			return nil, fmt.Errorf("get messages: %w", err)
		}
		return &conversationWindow{messages: msgs, total: total}, nil
	}

	// Total exceeds window — run synchronous summarization if past trigger threshold
	if total > s.summarizeTrigger {
		allMsgs, err := s.msgRepo.GetByConversationID(ctx, convID)
		if err != nil {
			return nil, fmt.Errorf("get messages: %w", err)
		}

		if err := s.summarizeOldMessages(ctx, convID, allMsgs); err != nil {
			s.logger.WithError(err).Error("synchronous summarization failed")
		}

		// Load recent window + summary (guaranteed to exist after successful summarization)
		recentMsgs, err := s.msgRepo.GetRecent(ctx, convID, s.windowSize)
		if err != nil {
			return nil, fmt.Errorf("get recent messages: %w", err)
		}

		summary, err := s.convRepo.GetSummary(ctx, convID)
		if err != nil {
			return nil, fmt.Errorf("get summary: %w", err)
		}

		// If summarization failed and no prior summary exists, fall back to full history
		if summary == nil {
			return &conversationWindow{messages: allMsgs, total: total}, nil
		}

		return &conversationWindow{messages: recentMsgs, summary: summary, total: total}, nil
	}

	// Between windowSize and summarizeTrigger — use existing summary if available
	summary, err := s.convRepo.GetSummary(ctx, convID)
	if err != nil {
		return nil, fmt.Errorf("get summary: %w", err)
	}

	if summary == nil {
		msgs, err := s.msgRepo.GetByConversationID(ctx, convID)
		if err != nil {
			return nil, fmt.Errorf("get messages: %w", err)
		}
		return &conversationWindow{messages: msgs, total: total}, nil
	}

	msgs, err := s.msgRepo.GetRecent(ctx, convID, s.windowSize)
	if err != nil {
		return nil, fmt.Errorf("get recent messages: %w", err)
	}

	return &conversationWindow{messages: msgs, summary: summary, total: total}, nil
}

// summarizeOldMessages summarizes messages outside the recent window and stores the summary.
// It runs synchronously and returns an error if summarization fails.
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
	existingSummary, _ := s.convRepo.GetSummary(ctx, convID)
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

	if err := s.convRepo.UpdateSummary(ctx, convID, summaryText); err != nil {
		return fmt.Errorf("store summary: %w", err)
	}

	s.logger.WithField("conversation_id", convID).Info("conversation summary updated")
	return nil
}

// anthropicMessagesFromWindow converts conversation window messages to Anthropic message format,
// skipping system messages.
func anthropicMessagesFromWindow(window *conversationWindow) []anthropic.Message {
	var msgs []anthropic.Message
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

