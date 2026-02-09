package api

import (
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/vultisig/agent-backend/internal/storage/postgres"
	"github.com/vultisig/agent-backend/internal/types"
)

// CreateConversationRequest is the request body for creating a conversation.
type CreateConversationRequest struct {
	PublicKey string `json:"public_key"`
}

// ListConversationsRequest is the request body for listing conversations.
type ListConversationsRequest struct {
	PublicKey string `json:"public_key"`
	Skip      int    `json:"skip"`
	Take      int    `json:"take"`
}

// ListConversationsResponse is the response for listing conversations.
type ListConversationsResponse struct {
	Conversations []types.Conversation `json:"conversations"`
	TotalCount    int                  `json:"total_count"`
}

// GetConversationRequest is the request body for getting a conversation.
type GetConversationRequest struct {
	PublicKey string `json:"public_key"`
}

// DeleteConversationRequest is the request body for deleting a conversation.
type DeleteConversationRequest struct {
	PublicKey string `json:"public_key"`
}

// CreateConversation creates a new conversation.
func (s *Server) CreateConversation(c echo.Context) error {
	var req CreateConversationRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
	}

	authPublicKey := GetPublicKey(c)
	if req.PublicKey != authPublicKey {
		return c.JSON(http.StatusForbidden, ErrorResponse{Error: "public key mismatch"})
	}

	conv, err := s.convRepo.Create(c.Request().Context(), req.PublicKey)
	if err != nil {
		s.logger.WithError(err).Error("failed to create conversation")
		return c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to create conversation"})
	}

	return c.JSON(http.StatusCreated, conv)
}

// ListConversations returns a paginated list of conversations.
func (s *Server) ListConversations(c echo.Context) error {
	var req ListConversationsRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
	}

	authPublicKey := GetPublicKey(c)
	if req.PublicKey != authPublicKey {
		return c.JSON(http.StatusForbidden, ErrorResponse{Error: "public key mismatch"})
	}

	// Default pagination
	if req.Take <= 0 {
		req.Take = 20
	}
	if req.Take > 100 {
		req.Take = 100
	}

	conversations, totalCount, err := s.convRepo.List(c.Request().Context(), req.PublicKey, req.Skip, req.Take)
	if err != nil {
		s.logger.WithError(err).Error("failed to list conversations")
		return c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to list conversations"})
	}

	if conversations == nil {
		conversations = []types.Conversation{}
	}

	return c.JSON(http.StatusOK, ListConversationsResponse{
		Conversations: conversations,
		TotalCount:    totalCount,
	})
}

// GetConversation returns a conversation with its messages.
func (s *Server) GetConversation(c echo.Context) error {
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{Error: "invalid conversation id"})
	}

	var req GetConversationRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
	}

	authPublicKey := GetPublicKey(c)
	if req.PublicKey != authPublicKey {
		return c.JSON(http.StatusForbidden, ErrorResponse{Error: "public key mismatch"})
	}

	conv, err := s.convRepo.GetWithMessages(c.Request().Context(), id, req.PublicKey)
	if err != nil {
		if errors.Is(err, postgres.ErrNotFound) {
			return c.JSON(http.StatusNotFound, ErrorResponse{Error: "conversation not found"})
		}
		s.logger.WithError(err).Error("failed to get conversation")
		return c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to get conversation"})
	}

	if conv.Messages == nil {
		conv.Messages = []types.Message{}
	}

	return c.JSON(http.StatusOK, conv)
}

// DeleteConversation archives a conversation (soft delete).
func (s *Server) DeleteConversation(c echo.Context) error {
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{Error: "invalid conversation id"})
	}

	var req DeleteConversationRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
	}

	authPublicKey := GetPublicKey(c)
	if req.PublicKey != authPublicKey {
		return c.JSON(http.StatusForbidden, ErrorResponse{Error: "public key mismatch"})
	}

	err = s.convRepo.Archive(c.Request().Context(), id, req.PublicKey)
	if err != nil {
		if errors.Is(err, postgres.ErrNotFound) {
			return c.JSON(http.StatusNotFound, ErrorResponse{Error: "conversation not found"})
		}
		s.logger.WithError(err).Error("failed to delete conversation")
		return c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to delete conversation"})
	}

	return c.JSON(http.StatusOK, SuccessResponse{Success: true})
}
