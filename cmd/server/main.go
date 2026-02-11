package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/sirupsen/logrus"

	"github.com/vultisig/agent-backend/internal/ai/anthropic"
	"github.com/vultisig/agent-backend/internal/api"
	"github.com/vultisig/agent-backend/internal/cache/redis"
	"github.com/vultisig/agent-backend/internal/config"
	"github.com/vultisig/agent-backend/internal/service"
	"github.com/vultisig/agent-backend/internal/service/agent"
	"github.com/vultisig/agent-backend/internal/service/plugin"
	"github.com/vultisig/agent-backend/internal/service/verifier"
	"github.com/vultisig/agent-backend/internal/storage/postgres"
)

func main() {
	// Initialize logger
	logger := logrus.New()
	logger.SetFormatter(&logrus.JSONFormatter{})
	logger.SetOutput(os.Stdout)

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		logger.WithError(err).Fatal("failed to load configuration")
	}

	// Configure log format
	if cfg.LogFormat == "text" {
		logger.SetFormatter(&logrus.TextFormatter{})
	}

	logger.Info("starting agent-backend server")

	// Connect to database
	ctx := context.Background()
	db, err := postgres.New(ctx, cfg.Database.DSN)
	if err != nil {
		logger.WithError(err).Fatal("failed to connect to database")
	}
	defer db.Close()

	// Initialize Redis client
	redisClient, err := redis.New(cfg.Redis.URI)
	if err != nil {
		logger.WithError(err).Fatal("failed to connect to redis")
	}
	defer redisClient.Close()

	// Initialize Anthropic client
	anthropicClient := anthropic.NewClient(cfg.Anthropic.APIKey, cfg.Anthropic.Model)

	// Initialize services
	authService := service.NewAuthService(cfg.Server.JWTSecret)

	// Initialize plugin service (skills fetched dynamically on demand)
	pluginService := plugin.NewService(cfg.Verifier.URL, redisClient, logger)

	// Initialize verifier client
	verifierClient := verifier.NewClient(cfg.Verifier.URL)

	// Initialize repositories
	convRepo := postgres.NewConversationRepository(db.Pool())
	msgRepo := postgres.NewMessageRepository(db.Pool())

	// Initialize agent service
	agentService := agent.NewAgentService(anthropicClient, msgRepo, convRepo, redisClient, verifierClient, pluginService, logger, cfg.Anthropic.SummaryModel, cfg.Context)

	// Initialize API server
	server := api.NewServer(authService, convRepo, agentService, logger)

	// Create Echo server
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	// Add middleware
	e.Use(middleware.Recover())
	e.Use(middleware.CORS())
	e.Use(middleware.RequestID())
	e.Use(middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogURI:    true,
		LogStatus: true,
		LogMethod: true,
		LogValuesFunc: func(c echo.Context, v middleware.RequestLoggerValues) error {
			logger.WithFields(logrus.Fields{
				"method":     v.Method,
				"uri":        v.URI,
				"status":     v.Status,
				"request_id": c.Response().Header().Get(echo.HeaderXRequestID),
			}).Info("request")
			return nil
		},
	}))

	// Health check endpoint (public)
	e.GET("/healthz", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{
			"status": "ok",
		})
	})

	// Agent routes (authenticated)
	agent := e.Group("/agent", server.AuthMiddleware)
	agent.POST("/conversations", server.CreateConversation)
	agent.POST("/conversations/list", server.ListConversations)
	agent.POST("/conversations/:id", server.GetConversation)
	agent.DELETE("/conversations/:id", server.DeleteConversation)
	agent.POST("/conversations/:id/messages", server.SendMessage)

	// Start server
	addr := fmt.Sprintf("%s:%s", cfg.Server.Host, cfg.Server.Port)
	go func() {
		logger.WithField("addr", addr).Info("server listening")
		if err := e.Start(addr); err != nil && err != http.ErrServerClosed {
			logger.WithError(err).Fatal("server error")
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down server")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := e.Shutdown(shutdownCtx); err != nil {
		logger.WithError(err).Error("server shutdown error")
	}

	logger.Info("server stopped")
}
