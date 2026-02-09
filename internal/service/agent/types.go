package agent

import "github.com/vultisig/agent-backend/internal/types"

// SendMessageRequest is the request body for sending a message.
type SendMessageRequest struct {
	PublicKey            string          `json:"public_key"`
	Content              string          `json:"content"`
	Context              *MessageContext `json:"context,omitempty"`
	SelectedSuggestionID *string         `json:"selected_suggestion_id,omitempty"` // Ability 2 (TBD)
	ActionResult         *ActionResult   `json:"action_result,omitempty"`          // Ability 3 (TBD)
	AccessToken          string          `json:"-"`                                // Populated by API layer, not from JSON
	// TODO: Audio support
	// AudioURL *string `json:"audio_url,omitempty"`
}

// MessageContext provides context about the user's wallet state.
type MessageContext struct {
	VaultAddress string            `json:"vault_address,omitempty"`
	Balances     []Balance         `json:"balances,omitempty"`
	Addresses    map[string]string `json:"addresses,omitempty"`
}

// Balance represents a token balance in the user's wallet.
type Balance struct {
	Chain    string `json:"chain"`
	Asset    string `json:"asset"`
	Symbol   string `json:"symbol"`
	Amount   string `json:"amount"`
	Decimals int    `json:"decimals"`
}

// ActionResult contains the result of a user action (Ability 3).
type ActionResult struct {
	Action  string `json:"action"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// SendMessageResponse is the response for sending a message.
type SendMessageResponse struct {
	Message     types.Message `json:"message"`
	Suggestions []Suggestion  `json:"suggestions,omitempty"`
	// PolicyReady is set when Ability 2 completes and a policy is ready for confirmation
	PolicyReady *PolicyReady `json:"policy_ready,omitempty"`
	// InstallRequired is set when a plugin must be installed before proceeding
	InstallRequired *InstallRequired `json:"install_required,omitempty"`
}

// InstallRequired signals that a plugin must be installed before proceeding.
type InstallRequired struct {
	PluginID    string `json:"plugin_id"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

// PolicyReady contains the policy details ready for user confirmation.
type PolicyReady struct {
	PluginID      string         `json:"plugin_id"`
	Configuration map[string]any `json:"configuration"`
	PolicySuggest any            `json:"policy_suggest"` // verifier.PolicySuggest
}

// Suggestion represents an action suggestion for the user.
type Suggestion struct {
	ID          string `json:"id"`
	PluginID    string `json:"plugin_id"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

// ToolResponse is the parsed response from the respond_to_user tool.
type ToolResponse struct {
	Intent      string              `json:"intent"`
	Response    string              `json:"response"`
	Suggestions []ToolSuggestion    `json:"suggestions,omitempty"`
}

// ToolSuggestion is a suggestion from the tool response.
type ToolSuggestion struct {
	PluginID    string `json:"plugin_id"`
	Title       string `json:"title"`
	Description string `json:"description"`
}
