package api

import (
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/vultisig/agent-backend/internal/service/agent"
	"github.com/vultisig/agent-backend/internal/storage/postgres"
)

// SendMessage handles POST /agent/conversations/:id/messages
func (s *Server) SendMessage(c echo.Context) error {
	// 1. Parse conversation ID from :id param
	idStr := c.Param("id")
	convID, err := uuid.Parse(idStr)
	if err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{Error: "invalid conversation id"})
	}

	// 2. Bind request body
	var req agent.SendMessageRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
	}

	// 3. Validate request has content, suggestion selection, or action result
	if req.Content == "" && req.SelectedSuggestionID == nil && req.ActionResult == nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{Error: "content, selected_suggestion_id, or action_result is required"})
	}

	// 4. Validate public_key matches JWT
	authPublicKey := GetPublicKey(c)
	if req.PublicKey != authPublicKey {
		return c.JSON(http.StatusForbidden, ErrorResponse{Error: "public key mismatch"})
	}

	// 5. Pass access token to request for plugin installation checks
	req.AccessToken = GetAccessToken(c)

	// 6. Call agentService.ProcessMessage
	resp, err := s.agentService.ProcessMessage(c.Request().Context(), convID, req.PublicKey, &req)
	if err != nil {
		if errors.Is(err, postgres.ErrNotFound) || err.Error() == "conversation not found" {
			return c.JSON(http.StatusNotFound, ErrorResponse{Error: "conversation not found"})
		}
		s.logger.WithError(err).Error("failed to process message")
		return c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to process message"})
	}

	// 6. Return SendMessageResponse
	return c.JSON(http.StatusOK, resp)
}
