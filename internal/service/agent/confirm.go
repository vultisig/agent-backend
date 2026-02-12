package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

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

	// Inject memory document if available
	if s.memRepo != nil {
		mem, err := s.memRepo.GetMemory(ctx, req.PublicKey)
		if err != nil {
			s.logger.WithError(err).Warn("failed to load memory for confirm")
		}
		if mem != nil {
			basePrompt += BuildMemorySection(mem.Content)
		}
	}

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

	// 4. Call Anthropic with confirm_action + optional update_memory
	tools := []anthropic.Tool{ConfirmActionTool}
	if s.memRepo != nil {
		tools = append(tools, UpdateMemoryTool)
	}

	anthropicReq := &anthropic.Request{
		System:   systemPrompt,
		Messages: messages,
		Tools:    tools,
	}

	resp, err := s.anthropic.SendMessage(ctx, anthropicReq)
	if err != nil {
		return nil, fmt.Errorf("call anthropic: %w", err)
	}

	// 5. Parse response: extract confirm_action and optional update_memory
	confirmResp, memoryUpdate, err := parseConfirmResponseWithMemory(resp)
	if err != nil {
		return nil, fmt.Errorf("parse confirm response: %w", err)
	}

	// 6. Fire-and-forget: persist memory update if present
	if memoryUpdate != nil && s.memRepo != nil {
		if len(memoryUpdate.Content) <= maxMemoryChars {
			if err := s.memRepo.UpsertMemory(ctx, req.PublicKey, memoryUpdate.Content); err != nil {
				s.logger.WithError(err).Error("failed to update memory")
			} else {
				s.logger.WithFields(logrus.Fields{
					"public_key": req.PublicKey,
					"length":     len(memoryUpdate.Content),
				}).Debug("memory updated from confirm")
			}
		} else {
			s.logger.WithFields(logrus.Fields{
				"public_key": req.PublicKey,
				"length":     len(memoryUpdate.Content),
				"max":        maxMemoryChars,
			}).Warn("memory update rejected: too large")
		}
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

// parseConfirmResponseWithMemory extracts confirm_action and optional update_memory from Claude's response.
func parseConfirmResponseWithMemory(resp *anthropic.Response) (*ConfirmResponse, *updateMemoryInput, error) {
	var confirmResp *ConfirmResponse
	var memoryUpdate *updateMemoryInput

	for _, block := range resp.Content {
		if block.Type != "tool_use" {
			continue
		}
		switch block.Name {
		case "confirm_action":
			var cr ConfirmResponse
			if err := json.Unmarshal(block.Input, &cr); err != nil {
				return nil, nil, fmt.Errorf("unmarshal confirm_action: %w", err)
			}
			confirmResp = &cr
		case "update_memory":
			var mu updateMemoryInput
			if err := json.Unmarshal(block.Input, &mu); err != nil {
				continue // non-fatal
			}
			memoryUpdate = &mu
		}
	}

	if confirmResp == nil {
		return nil, nil, errors.New("no confirm_action tool response found")
	}
	return confirmResp, memoryUpdate, nil
}
