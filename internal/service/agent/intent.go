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

const (
	suggestionTTL  = 1 * time.Hour
	maxMemoryChars = 4000
)

// updateMemoryInput is the parsed input for update_memory tool.
type updateMemoryInput struct {
	Content string `json:"content"`
}

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

	// 3. Load memory document and inject into system prompt
	var memoryContent string
	if s.memRepo != nil {
		mem, err := s.memRepo.GetMemory(ctx, req.PublicKey)
		if err != nil {
			s.logger.WithError(err).Warn("failed to load memory")
		}
		if mem != nil {
			memoryContent = mem.Content
		}
	}

	systemPrompt := BuildSystemPromptWithSummary(basePrompt+BuildMemorySection(memoryContent)+MemoryManagementInstructions, window.summary)

	// 4. Build messages for Anthropic
	messages := anthropicMessagesFromWindow(window)
	messages = append(messages, anthropic.Message{
		Role:    "user",
		Content: req.Content,
	})

	// 5. Build tools list (respond_to_user + update_memory)
	tools := []anthropic.Tool{RespondToUserTool}
	if s.memRepo != nil {
		tools = append(tools, UpdateMemoryTool)
	}

	// 6. Single Claude call — no agentic loop
	anthropicReq := &anthropic.Request{
		System:   systemPrompt,
		Messages: messages,
		Tools:    tools,
	}

	resp, err := s.anthropic.SendMessage(ctx, anthropicReq)
	if err != nil {
		return nil, fmt.Errorf("call anthropic: %w", err)
	}

	// 7. Parse response: extract respond_to_user and optional update_memory
	var toolResp *ToolResponse
	var textContent string
	var memoryUpdate *updateMemoryInput

	for _, block := range resp.Content {
		switch block.Type {
		case "text":
			textContent = block.Text
		case "tool_use":
			switch block.Name {
			case "respond_to_user":
				var tr ToolResponse
				if err := json.Unmarshal(block.Input, &tr); err != nil {
					s.logger.WithError(err).Warn("failed to unmarshal respond_to_user")
					continue
				}
				toolResp = &tr
			case "update_memory":
				var mu updateMemoryInput
				if err := json.Unmarshal(block.Input, &mu); err != nil {
					s.logger.WithError(err).Warn("failed to unmarshal update_memory")
					continue
				}
				memoryUpdate = &mu
			}
		}
	}

	// 8. Fire-and-forget: persist memory update if present
	if memoryUpdate != nil && s.memRepo != nil {
		if len(memoryUpdate.Content) <= maxMemoryChars {
			if err := s.memRepo.UpsertMemory(ctx, req.PublicKey, memoryUpdate.Content); err != nil {
				s.logger.WithError(err).Error("failed to update memory")
			} else {
				s.logger.WithFields(logrus.Fields{
					"public_key": req.PublicKey,
					"length":     len(memoryUpdate.Content),
				}).Debug("memory updated")
			}
		} else {
			s.logger.WithFields(logrus.Fields{
				"public_key": req.PublicKey,
				"length":     len(memoryUpdate.Content),
				"max":        maxMemoryChars,
			}).Warn("memory update rejected: too large")
		}
	}

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
		if err := s.convRepo.UpdateTitle(ctx, convID, title); err != nil {
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
