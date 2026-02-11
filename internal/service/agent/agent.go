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

	// Route based on request content
	switch {
	case req.ActionResult != nil:
		// Ability 3: Action confirmation
		return s.confirmAction(ctx, convID, req)
	case req.SelectedSuggestionID != nil:
		// Ability 2: Policy builder
		return s.buildPolicy(ctx, convID, req)
	default:
		// Ability 1: Intent detection (default)
		return s.detectIntent(ctx, convID, req)
	}
}

// getConversationWindow returns a windowed view of the conversation.
// If total messages fit in the window, returns all messages.
// Otherwise returns the most recent messages plus any existing summary.
// Falls back to full history if summary hasn't been generated yet.
func (s *AgentService) getConversationWindow(ctx context.Context, convID uuid.UUID) (*conversationWindow, error) {
	total, err := s.msgRepo.CountByConversationID(ctx, convID)
	if err != nil {
		return nil, fmt.Errorf("count messages: %w", err)
	}

	// If messages fit in the window, load everything
	if total <= s.windowSize {
		msgs, err := s.msgRepo.GetByConversationID(ctx, convID)
		if err != nil {
			return nil, fmt.Errorf("get messages: %w", err)
		}
		return &conversationWindow{messages: msgs, total: total}, nil
	}

	// Check for existing summary
	summary, err := s.convRepo.GetSummary(ctx, convID)
	if err != nil {
		return nil, fmt.Errorf("get summary: %w", err)
	}

	// If no summary exists, fall back to full history
	if summary == nil {
		msgs, err := s.msgRepo.GetByConversationID(ctx, convID)
		if err != nil {
			return nil, fmt.Errorf("get messages: %w", err)
		}
		return &conversationWindow{messages: msgs, total: total}, nil
	}

	// Load recent messages only
	msgs, err := s.msgRepo.GetRecent(ctx, convID, s.windowSize)
	if err != nil {
		return nil, fmt.Errorf("get recent messages: %w", err)
	}

	return &conversationWindow{messages: msgs, summary: summary, total: total}, nil
}

// triggerSummarizationIfNeeded fires a background goroutine to summarize old messages
// if the total message count exceeds the trigger threshold.
func (s *AgentService) triggerSummarizationIfNeeded(convID uuid.UUID, total int) {
	if total > s.summarizeTrigger {
		go s.summarizeConversation(convID)
	}
}

// summarizeConversation loads all messages, summarizes the older ones, and stores the summary.
func (s *AgentService) summarizeConversation(convID uuid.UUID) {
	ctx := context.Background()

	allMsgs, err := s.msgRepo.GetByConversationID(ctx, convID)
	if err != nil {
		s.logger.WithError(err).Error("summarization: failed to load messages")
		return
	}

	if len(allMsgs) <= s.windowSize {
		return
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
		s.logger.WithError(err).Error("summarization: failed to call anthropic")
		return
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
		s.logger.Error("summarization: empty response from anthropic")
		return
	}

	if err := s.convRepo.UpdateSummary(ctx, convID, summaryText); err != nil {
		s.logger.WithError(err).Error("summarization: failed to store summary")
		return
	}

	s.logger.WithField("conversation_id", convID).Info("conversation summary updated")
}

