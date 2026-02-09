package api

import (
	"github.com/sirupsen/logrus"

	"github.com/vultisig/agent-backend/internal/service"
	"github.com/vultisig/agent-backend/internal/service/agent"
	"github.com/vultisig/agent-backend/internal/storage/postgres"
)

// Server holds API dependencies.
type Server struct {
	authService  *service.AuthService
	convRepo     *postgres.ConversationRepository
	agentService *agent.AgentService
	logger       *logrus.Logger
}

// NewServer creates a new API server.
func NewServer(authService *service.AuthService, convRepo *postgres.ConversationRepository, agentService *agent.AgentService, logger *logrus.Logger) *Server {
	return &Server{
		authService:  authService,
		convRepo:     convRepo,
		agentService: agentService,
		logger:       logger,
	}
}
