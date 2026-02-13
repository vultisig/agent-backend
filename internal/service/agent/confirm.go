package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/vultisig/agent-backend/internal/ai/anthropic"
	"github.com/vultisig/agent-backend/internal/types"
)

// ConfirmResponse is the parsed response from the confirm_action tool.
type ConfirmResponse struct {
	Response  string   `json:"response"`
	NextSteps []string `json:"next_steps,omitempty"`
}

// confirmAction handles Ability 3: confirm action result.
// Called when the frontend/mobile app reports the result of an action (e.g., policy created, install completed).
func (s *AgentService) confirmAction(ctx context.Context, convID uuid.UUID, req *SendMessageRequest, window *conversationWindow) (*SendMessageResponse, error) {
	if req.ActionResult == nil {
		return nil, errors.New("action_result is required for action confirmation")
	}

	// 1. Build system prompt for action confirmation
	basePrompt := BuildConfirmActionPrompt(req.ActionResult)
	basePrompt += s.loadMemorySection(ctx, req.PublicKey) + MemoryManagementInstructions
	systemPrompt := BuildSystemPromptWithSummary(basePrompt, window.summary)

	// 2. Build messages for Anthropic
	messages := anthropicMessagesFromWindow(window)

	// Add synthetic user message describing the action result
	actionMsg := buildActionResultMessage(req.ActionResult)
	messages = append(messages, anthropic.Message{
		Role:    "user",
		Content: actionMsg,
	})

	// 3. Store the user's action result as a message (marked as action_result so frontend can hide it)
	userMsg := &types.Message{
		ConversationID: convID,
		Role:           types.RoleUser,
		Content:        actionMsg,
		ContentType:    "action_result",
	}
	if err := s.msgRepo.Create(ctx, userMsg); err != nil {
		return nil, fmt.Errorf("store user message: %w", err)
	}

	// 4. Call Anthropic with forced confirm_action + optional update_memory
	tools := []anthropic.Tool{ConfirmActionTool}
	tools = append(tools, s.memoryTools()...)

	anthropicReq := &anthropic.Request{
		System:   systemPrompt,
		Messages: messages,
		Tools:    tools,
		ToolChoice: &anthropic.ToolChoice{
			Type: "tool",
			Name: "confirm_action",
		},
	}

	resp, err := s.anthropic.SendMessage(ctx, anthropicReq)
	if err != nil {
		return nil, fmt.Errorf("call anthropic: %w", err)
	}

	// 5. Parse confirm_action (guaranteed by forced tool choice)
	confirmResp, err := parseConfirmResponse(resp)
	if err != nil {
		return nil, fmt.Errorf("parse confirm response: %w", err)
	}

	// 6. Fire-and-forget: persist memory update if present
	s.persistMemoryUpdate(ctx, req.PublicKey, s.extractMemoryUpdate(resp))

	// 7. Store assistant message in DB
	assistantMsg := &types.Message{
		ConversationID: convID,
		Role:           types.RoleAssistant,
		Content:        confirmResp.Response,
		ContentType:    "text",
	}
	if err := s.msgRepo.Create(ctx, assistantMsg); err != nil {
		return nil, fmt.Errorf("store assistant message: %w", err)
	}

	// 8. Auto-continue: if install_plugin succeeded, check for pending policy build
	if req.ActionResult.Action == "install_plugin" && req.ActionResult.Success {
		pendingKey := fmt.Sprintf("pending_build:%s", convID)
		suggID, err := s.redis.Get(ctx, pendingKey)
		if err == nil && suggID != "" {
			_ = s.redis.Delete(ctx, pendingKey)
			buildReq := &SendMessageRequest{
				SelectedSuggestionID: &suggID,
				Context:              req.Context,
				AccessToken:          req.AccessToken,
			}
			buildResp, err := s.buildPolicy(ctx, convID, buildReq, window)
			if err != nil {
				s.logger.WithError(err).Warn("auto-continue to buildPolicy failed")
			} else {
				buildResp.Message = *assistantMsg
				return buildResp, nil
			}
		}
	}

	return &SendMessageResponse{
		Message: *assistantMsg,
	}, nil
}

// buildActionResultMessage creates a user message describing the action result.
func buildActionResultMessage(result *ActionResult) string {
	if result.Success {
		return fmt.Sprintf("[Action completed: %s was successful]", result.Action)
	}
	if result.Error != "" {
		return fmt.Sprintf("[Action failed: %s failed with error: %s]", result.Action, result.Error)
	}
	return fmt.Sprintf("[Action failed: %s was not successful]", result.Action)
}

// parseConfirmResponse extracts the confirm_action tool response from Claude's response.
func parseConfirmResponse(resp *anthropic.Response) (*ConfirmResponse, error) {
	for _, block := range resp.Content {
		if block.Type == "tool_use" && block.Name == "confirm_action" {
			var cr ConfirmResponse
			if err := json.Unmarshal(block.Input, &cr); err != nil {
				return nil, fmt.Errorf("unmarshal confirm_action: %w", err)
			}
			return &cr, nil
		}
	}
	return nil, errors.New("no confirm_action tool response found")
}
