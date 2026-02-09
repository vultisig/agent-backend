package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/vultisig/agent-backend/internal/ai/anthropic"
	"github.com/vultisig/agent-backend/internal/types"
)

const suggestionTTL = 1 * time.Hour

// detectIntent handles Ability 1: detect user intent and generate response with suggestions.
func (s *AgentService) detectIntent(ctx context.Context, convID uuid.UUID, req *SendMessageRequest) (*SendMessageResponse, error) {
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

	// 2. Get conversation history from DB
	history, err := s.msgRepo.GetByConversationID(ctx, convID)
	if err != nil {
		return nil, fmt.Errorf("get conversation history: %w", err)
	}

	// 3. Build system prompt with user context and plugin skills
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

	systemPrompt := BuildFullPrompt(balances, addresses, pluginSkills)

	// 4. Build messages for Anthropic
	var messages []anthropic.Message
	for _, msg := range history {
		if msg.Role == types.RoleSystem {
			continue // Skip system messages
		}
		messages = append(messages, anthropic.Message{
			Role:    string(msg.Role),
			Content: msg.Content,
		})
	}

	// 5. Call Anthropic with respond_to_user tool
	anthropicReq := &anthropic.Request{
		System:   systemPrompt,
		Messages: messages,
		Tools:    []anthropic.Tool{RespondToUserTool},
	}

	resp, err := s.anthropic.SendMessage(ctx, anthropicReq)
	if err != nil {
		return nil, fmt.Errorf("call anthropic: %w", err)
	}

	// 6. Parse tool response
	toolResp, textContent, err := parseToolResponse(resp)
	if err != nil {
		return nil, fmt.Errorf("parse tool response: %w", err)
	}

	// Determine the response content
	responseContent := ""
	if toolResp != nil {
		responseContent = toolResp.Response
	} else if textContent != "" {
		responseContent = textContent
	} else {
		return nil, errors.New("no response content from Claude")
	}

	// 7. Store suggestions in Redis (1hr TTL)
	var suggestions []Suggestion
	if toolResp != nil && len(toolResp.Suggestions) > 0 {
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

	// 8. Store assistant message in DB
	intent := ""
	if toolResp != nil {
		intent = toolResp.Intent
	}
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

	// 9. Update conversation title if this is the first exchange
	if len(history) <= 1 {
		title := truncateTitle(req.Content)
		if err := s.convRepo.UpdateTitle(ctx, convID, title); err != nil {
			s.logger.WithError(err).Warn("failed to update conversation title")
		}
	}

	return &SendMessageResponse{
		Message:     *assistantMsg,
		Suggestions: suggestions,
	}, nil
}

// parseToolResponse extracts the tool response from Claude's response.
func parseToolResponse(resp *anthropic.Response) (*ToolResponse, string, error) {
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
					return nil, "", fmt.Errorf("unmarshal tool input: %w", err)
				}
				toolResp = &tr
			}
		}
	}

	return toolResp, textContent, nil
}

// truncateTitle truncates content to create a conversation title.
func truncateTitle(content string) string {
	const maxLen = 50
	if len(content) <= maxLen {
		return content
	}
	return content[:maxLen-3] + "..."
}
