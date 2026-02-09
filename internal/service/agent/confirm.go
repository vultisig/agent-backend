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
func (s *AgentService) confirmAction(ctx context.Context, convID uuid.UUID, req *SendMessageRequest) (*SendMessageResponse, error) {
	if req.ActionResult == nil {
		return nil, errors.New("action_result is required for action confirmation")
	}

	// 1. Get conversation history for context
	history, err := s.msgRepo.GetByConversationID(ctx, convID)
	if err != nil {
		return nil, fmt.Errorf("get conversation history: %w", err)
	}

	// 2. Build system prompt for action confirmation
	systemPrompt := BuildConfirmActionPrompt(req.ActionResult)

	// 3. Build messages for Anthropic
	// Include history plus a synthetic user message about the action result
	var messages []anthropic.Message
	for _, msg := range history {
		if msg.Role == types.RoleSystem {
			continue
		}
		messages = append(messages, anthropic.Message{
			Role:    string(msg.Role),
			Content: msg.Content,
		})
	}

	// Add synthetic user message describing the action result
	actionMsg := buildActionResultMessage(req.ActionResult)
	messages = append(messages, anthropic.Message{
		Role:    "user",
		Content: actionMsg,
	})

	// 4. Store the user's action result as a message
	userMsg := &types.Message{
		ConversationID: convID,
		Role:           types.RoleUser,
		Content:        actionMsg,
		ContentType:    "text",
	}
	if err := s.msgRepo.Create(ctx, userMsg); err != nil {
		return nil, fmt.Errorf("store user message: %w", err)
	}

	// 5. Call Anthropic with confirm_action tool (forced)
	anthropicReq := &anthropic.Request{
		System:   systemPrompt,
		Messages: messages,
		Tools:    []anthropic.Tool{ConfirmActionTool},
		ToolChoice: &anthropic.ToolChoice{
			Type: "tool",
			Name: "confirm_action",
		},
	}

	resp, err := s.anthropic.SendMessage(ctx, anthropicReq)
	if err != nil {
		return nil, fmt.Errorf("call anthropic: %w", err)
	}

	// 6. Parse tool response
	confirmResp, err := parseConfirmResponse(resp)
	if err != nil {
		return nil, fmt.Errorf("parse confirm response: %w", err)
	}

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

	// 8. Return response (suggestions could be added based on next_steps in future)
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

// parseConfirmResponse extracts the confirm response from Claude's response.
func parseConfirmResponse(resp *anthropic.Response) (*ConfirmResponse, error) {
	for _, block := range resp.Content {
		if block.Type == "tool_use" && block.Name == "confirm_action" {
			var cr ConfirmResponse
			if err := json.Unmarshal(block.Input, &cr); err != nil {
				return nil, fmt.Errorf("unmarshal tool input: %w", err)
			}
			return &cr, nil
		}
	}
	return nil, errors.New("no confirm_action tool response found")
}
