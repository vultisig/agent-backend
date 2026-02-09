package agent

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/vultisig/agent-backend/internal/ai/anthropic"
	"github.com/vultisig/agent-backend/internal/cache/redis"
	"github.com/vultisig/agent-backend/internal/service/verifier"
	"github.com/vultisig/agent-backend/internal/storage/postgres"
)

// PluginSkillsProvider provides plugin skills for prompt building.
type PluginSkillsProvider interface {
	GetSkills(ctx context.Context) []PluginSkill
}

// AgentService handles AI agent operations.
type AgentService struct {
	anthropic      *anthropic.Client
	msgRepo        *postgres.MessageRepository
	convRepo       *postgres.ConversationRepository
	redis          *redis.Client
	verifier       *verifier.Client
	pluginProvider PluginSkillsProvider
	logger         *logrus.Logger
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
) *AgentService {
	return &AgentService{
		anthropic:      anthropicClient,
		msgRepo:        msgRepo,
		convRepo:       convRepo,
		redis:          redisClient,
		verifier:       verifierClient,
		pluginProvider: pluginProvider,
		logger:         logger,
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

