package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/vultisig/agent-backend/internal/ai/anthropic"
	"github.com/vultisig/agent-backend/internal/types"
)

const suggestionTTL = 1 * time.Hour

// detectIntent handles Ability 1: detect user intent and generate response with suggestions.
func (s *AgentService) detectIntent(ctx context.Context, convID uuid.UUID, req *SendMessageRequest, window *conversationWindow) (*SendMessageResponse, error) {
	// 1. Store user message in DB
	userMsg := &types.Message{
		ConversationID: convID,
		Role:           types.RoleUser,
		Content:        req.Content,
		ContentType:    "text",
	}
	if err := s.msgRepo.Create(ctx, userMsg); err != nil {
		return nil, fmt.Errorf("store user message: %w", err)
	}

	// 2. Build system prompt with user context and plugin skills
	var balances []Balance
	var addresses map[string]string
	if req.Context != nil {
		balances = req.Context.Balances
		addresses = req.Context.Addresses
	}

	var pluginSkills []PluginSkill
	if s.pluginProvider != nil {
		pluginSkills = s.pluginProvider.GetSkills(ctx)
	}

	basePrompt := BuildFullPrompt(balances, addresses, pluginSkills)

	// 3. Load memory and build system prompt
	systemPrompt := BuildSystemPromptWithSummary(
		basePrompt+s.loadMemorySection(ctx, req.PublicKey)+MemoryManagementInstructions,
		window.summary,
	)

	// 4. Build messages for Anthropic
	messages := anthropicMessagesFromWindow(window)
	messages = append(messages, anthropic.Message{
		Role:    "user",
		Content: req.Content,
	})

	// 5. Build tools list (respond_to_user + optional update_memory)
	tools := []anthropic.Tool{RespondToUserTool}
	tools = append(tools, s.memoryTools()...)

	// 6. Single Claude call — force respond_to_user (update_memory can still be called in parallel)
	anthropicReq := &anthropic.Request{
		System:   systemPrompt,
		Messages: messages,
		Tools:    tools,
		ToolChoice: &anthropic.ToolChoice{
			Type: "tool",
			Name: "respond_to_user",
		},
	}

	resp, err := s.anthropic.SendMessage(ctx, anthropicReq)
	if err != nil {
		return nil, fmt.Errorf("call anthropic: %w", err)
	}

	// 7. Parse response: extract respond_to_user and optional update_memory
	s.logger.WithFields(logrus.Fields{
		"stop_reason":   resp.StopReason,
		"content_count": len(resp.Content),
	}).Debug("claude response received")
	for i, block := range resp.Content {
		s.logger.WithFields(logrus.Fields{
			"index":    i,
			"type":     block.Type,
			"name":     block.Name,
			"text_len": len(block.Text),
		}).Debug("response content block")
	}

	var toolResp *ToolResponse
	var textContent string

	for _, block := range resp.Content {
		switch block.Type {
		case "text":
			textContent = block.Text
		case "tool_use":
			if block.Name == "respond_to_user" {
				var tr ToolResponse
				if err := json.Unmarshal(block.Input, &tr); err != nil {
					s.logger.WithError(err).Warn("failed to unmarshal respond_to_user")
					continue
				}
				toolResp = &tr
			}
		}
	}

	// 8. Fire-and-forget: persist memory update if present
	s.persistMemoryUpdate(ctx, req.PublicKey, s.extractMemoryUpdate(resp))

	// 9. Build response
	if toolResp != nil {
		return s.buildIntentResponse(ctx, convID, req, toolResp, window)
	}

	// Text fallback (no tool called)
	if textContent != "" {
		return s.buildIntentResponseFromText(ctx, convID, textContent, window)
	}

	return nil, errors.New("no response content from Claude")
}

// buildIntentResponse builds the final response when respond_to_user was called.
func (s *AgentService) buildIntentResponse(ctx context.Context, convID uuid.UUID, req *SendMessageRequest, toolResp *ToolResponse, window *conversationWindow) (*SendMessageResponse, error) {
	responseContent := toolResp.Response

	// Store suggestions in Redis (1hr TTL)
	var suggestions []Suggestion
	if len(toolResp.Suggestions) > 0 {
		for _, ts := range toolResp.Suggestions {
			suggID := "sug_" + uuid.New().String()
			sugg := Suggestion{
				ID:          suggID,
				PluginID:    ts.PluginID,
				Title:       ts.Title,
				Description: ts.Description,
			}
			suggestions = append(suggestions, sugg)

			// Store in Redis
			suggJSON, err := json.Marshal(sugg)
			if err != nil {
				s.logger.WithError(err).Warn("failed to marshal suggestion")
				continue
			}
			if err := s.redis.Set(ctx, suggID, string(suggJSON), suggestionTTL); err != nil {
				s.logger.WithError(err).Warn("failed to store suggestion in redis")
			}
		}
	}

	// Store assistant message in DB
	intent := toolResp.Intent
	metadata, _ := json.Marshal(map[string]any{
		"intent":      intent,
		"suggestions": suggestions,
	})
	assistantMsg := &types.Message{
		ConversationID: convID,
		Role:           types.RoleAssistant,
		Content:        responseContent,
		ContentType:    "text",
		Metadata:       metadata,
	}
	if err := s.msgRepo.Create(ctx, assistantMsg); err != nil {
		return nil, fmt.Errorf("store assistant message: %w", err)
	}

	// Update conversation title if this is the first exchange
	if window.total <= 2 {
		title := truncateTitle(req.Content)
		if err := s.convRepo.UpdateTitle(ctx, convID, req.PublicKey, title); err != nil {
			s.logger.WithError(err).Warn("failed to update conversation title")
		}
	}

	return &SendMessageResponse{
		Message:     *assistantMsg,
		Suggestions: suggestions,
	}, nil
}

// buildIntentResponseFromText builds a response from text fallback (no tool called).
func (s *AgentService) buildIntentResponseFromText(ctx context.Context, convID uuid.UUID, text string, window *conversationWindow) (*SendMessageResponse, error) {
	assistantMsg := &types.Message{
		ConversationID: convID,
		Role:           types.RoleAssistant,
		Content:        text,
		ContentType:    "text",
	}
	if err := s.msgRepo.Create(ctx, assistantMsg); err != nil {
		return nil, fmt.Errorf("store assistant message: %w", err)
	}
	return &SendMessageResponse{
		Message: *assistantMsg,
	}, nil
}

// truncateTitle truncates content to create a conversation title.
func truncateTitle(content string) string {
	const maxLen = 50
	if len(content) <= maxLen {
		return content
	}
	return content[:maxLen-3] + "..."
}
