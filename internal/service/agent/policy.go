package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/vultisig/agent-backend/internal/ai/anthropic"
	"github.com/vultisig/agent-backend/internal/service/verifier"
	"github.com/vultisig/agent-backend/internal/types"
)

// PolicyResponse is the parsed response from the build_policy tool.
type PolicyResponse struct {
	Configuration map[string]any `json:"configuration"`
	Explanation   string         `json:"explanation"`
}

// PolicyReadyMetadata is the metadata for a policy-ready message.
type PolicyReadyMetadata struct {
	Type          string                 `json:"type"` // "policy_ready"
	Action        string                 `json:"action"` // "create_policy"
	PluginID      string                 `json:"plugin_id"`
	PolicySuggest *verifier.PolicySuggest `json:"policy_suggest"`
	Configuration map[string]any         `json:"configuration"`
}

// buildPolicy handles Ability 2: build policy from selected suggestion.
func (s *AgentService) buildPolicy(ctx context.Context, convID uuid.UUID, req *SendMessageRequest) (*SendMessageResponse, error) {
	if req.SelectedSuggestionID == nil {
		return nil, errors.New("selected_suggestion_id is required for policy builder")
	}

	// 1. Look up suggestion from Redis
	suggJSON, err := s.redis.Get(ctx, *req.SelectedSuggestionID)
	if err != nil {
		return nil, fmt.Errorf("suggestion not found or expired: %w", err)
	}

	var suggestion Suggestion
	if err := json.Unmarshal([]byte(suggJSON), &suggestion); err != nil {
		return nil, fmt.Errorf("unmarshal suggestion: %w", err)
	}

	// 2. Check if verifier client is available
	if s.verifier == nil {
		return nil, errors.New("verifier client not configured")
	}

	// 3. Check if plugin is installed
	if req.AccessToken != "" {
		installed, err := s.verifier.IsPluginInstalled(ctx, req.AccessToken, suggestion.PluginID)
		if err != nil {
			s.logger.WithError(err).Warn("failed to check plugin installation")
			// Continue anyway - verifier might be unavailable
		} else if !installed {
			// Plugin not installed - return install_required response
			return s.handleInstallRequired(ctx, convID, suggestion)
		}
	}

	// 4. Fetch plugin's RecipeSchema from verifier
	schema, err := s.verifier.GetRecipeSchema(ctx, suggestion.PluginID)
	if err != nil {
		return nil, fmt.Errorf("get recipe schema: %w", err)
	}

	// Extract configuration schema and examples for Claude
	configSchemaJSON, err := json.MarshalIndent(schema.Configuration, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal config schema: %w", err)
	}

	var examplesJSON []byte
	if len(schema.ConfigurationExample) > 0 {
		examplesJSON, _ = json.MarshalIndent(schema.ConfigurationExample, "", "  ")
	}

	// 5. Get conversation history
	history, err := s.msgRepo.GetByConversationID(ctx, convID)
	if err != nil {
		return nil, fmt.Errorf("get conversation history: %w", err)
	}

	// 6. Build system prompt for policy builder
	var balances []Balance
	var addresses map[string]string
	if req.Context != nil {
		balances = req.Context.Balances
		addresses = req.Context.Addresses
	}

	systemPrompt := BuildPolicyBuilderPrompt(suggestion, string(configSchemaJSON), string(examplesJSON), balances, addresses)

	// 7. Build messages for Anthropic
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

	// 8. Call Anthropic with build_policy tool (forced)
	anthropicReq := &anthropic.Request{
		System:   systemPrompt,
		Messages: messages,
		Tools:    []anthropic.Tool{BuildPolicyTool},
		ToolChoice: &anthropic.ToolChoice{
			Type: "tool",
			Name: "build_policy",
		},
	}

	resp, err := s.anthropic.SendMessage(ctx, anthropicReq)
	if err != nil {
		return nil, fmt.Errorf("call anthropic: %w", err)
	}

	// 9. Parse tool response
	policyResp, err := parsePolicyResponse(resp)
	if err != nil {
		return nil, fmt.Errorf("parse policy response: %w", err)
	}

	// 10. Call verifier's /suggest endpoint with the configuration
	policySuggest, err := s.verifier.GetPolicySuggest(ctx, suggestion.PluginID, policyResp.Configuration)
	if err != nil {
		return nil, fmt.Errorf("get policy suggest: %w", err)
	}

	// 11. Build response metadata
	metadata := PolicyReadyMetadata{
		Type:          "policy_ready",
		Action:        "create_policy",
		PluginID:      suggestion.PluginID,
		PolicySuggest: policySuggest,
		Configuration: policyResp.Configuration,
	}
	metadataJSON, _ := json.Marshal(metadata)

	// 12. Store assistant message in DB
	responseContent := fmt.Sprintf("I've prepared your %s. Please review the details below and confirm to create the policy.", suggestion.Title)
	if policyResp.Explanation != "" {
		responseContent = policyResp.Explanation + "\n\nPlease review and confirm to create the policy."
	}

	assistantMsg := &types.Message{
		ConversationID: convID,
		Role:           types.RoleAssistant,
		Content:        responseContent,
		ContentType:    "text",
		Metadata:       metadataJSON,
	}
	if err := s.msgRepo.Create(ctx, assistantMsg); err != nil {
		return nil, fmt.Errorf("store assistant message: %w", err)
	}

	return &SendMessageResponse{
		Message: *assistantMsg,
		PolicyReady: &PolicyReady{
			PluginID:      suggestion.PluginID,
			Configuration: policyResp.Configuration,
			PolicySuggest: policySuggest,
		},
	}, nil
}

// parsePolicyResponse extracts the policy response from Claude's response.
func parsePolicyResponse(resp *anthropic.Response) (*PolicyResponse, error) {
	for _, block := range resp.Content {
		if block.Type == "tool_use" && block.Name == "build_policy" {
			var pr PolicyResponse
			if err := json.Unmarshal(block.Input, &pr); err != nil {
				return nil, fmt.Errorf("unmarshal tool input: %w", err)
			}
			return &pr, nil
		}
	}
	return nil, errors.New("no build_policy tool response found")
}

// handleInstallRequired returns an install_required response when a plugin is not installed.
func (s *AgentService) handleInstallRequired(ctx context.Context, convID uuid.UUID, suggestion Suggestion) (*SendMessageResponse, error) {
	content := fmt.Sprintf("To use %s, you need to install the plugin first. Please install it and try again.", suggestion.Title)

	// Store assistant message in DB
	assistantMsg := &types.Message{
		ConversationID: convID,
		Role:           types.RoleAssistant,
		Content:        content,
		ContentType:    "text",
	}
	if err := s.msgRepo.Create(ctx, assistantMsg); err != nil {
		return nil, fmt.Errorf("store message: %w", err)
	}

	return &SendMessageResponse{
		Message: *assistantMsg,
		InstallRequired: &InstallRequired{
			PluginID:    suggestion.PluginID,
			Title:       suggestion.Title,
			Description: fmt.Sprintf("Install %s to set up your automation", suggestion.Title),
		},
	}, nil
}
