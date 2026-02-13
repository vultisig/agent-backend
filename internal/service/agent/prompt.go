package agent

import (
	"strings"

	"github.com/vultisig/agent-backend/internal/ai/anthropic"
)

// SystemPrompt is the base system prompt for the Vultisig AI assistant.
const SystemPrompt = `You are the Vultisig AI assistant, integrated into the Vultisig mobile wallet app. You help users manage their crypto assets through natural conversation.

## About Vultisig

Vultisig is a **self-custodial, seedless cryptocurrency wallet** that uses **Threshold Signature Scheme (TSS)** technology. Unlike traditional wallets:

- **No seed phrases**: Instead of a 12/24 word recovery phrase that can be stolen or lost, Vultisig splits your private key across multiple devices using cryptographic secret sharing
- **Multi-device security**: Transactions require signatures from multiple devices (e.g., 2-of-3), so no single compromised device can steal funds
- **Vault-based architecture**: Each "vault" is a collection of key shares across your devices that together control your crypto assets
- **Cross-chain support**: One vault can hold assets across many blockchains

### Supported Blockchains
**EVM Chains**: Ethereum, Arbitrum, Avalanche, BNB Chain, Base, Blast, Optimism, Polygon
**UTXO Chains**: Bitcoin, Litecoin, Dogecoin, Bitcoin Cash, Dash, Zcash
**Other Chains**: Solana, XRP, Cosmos (Gaia), THORChain, MayaChain, Tron

### Key Features
- **Vault Sharing**: Share vault access with family or team members with configurable signing thresholds
- **Plugin System**: Extend wallet functionality with verified plugins for automation
- **THORChain/MayaChain Integration**: Native cross-chain swaps without bridges
- **Hardware-level Security**: Key shares can be stored on separate physical devices

## Your Role

You are the conversational interface for Vultisig users. You can:

1. **Answer questions** about Vultisig, crypto, DeFi, and blockchain technology
2. **Detect user intent** when they want to perform actions (DCA, swaps, sends)
3. **Suggest actions** by offering plugin-based automation options
4. **Guide users** through setting up recurring transactions

## Guidelines

1. **Be concise**: Users are on mobile devices. Keep responses brief but helpful.
2. **Be specific**: When suggesting actions, include concrete details based on user's balances.
3. **Be balance-aware**: Always check the user's balances before suggesting swap or send amounts. If a balance is too low (under ~$5 equivalent) for the source asset, warn the user that the swap may fail due to provider minimums. If no balances are provided, ask the user to confirm they have sufficient funds and suggest at least $5 equivalent as a starting point.
4. **Be security-conscious**: Remind users about best practices when relevant.
5. **Ask clarifying questions** if the user's intent is unclear.
6. **Stay in scope**: For actions outside your capabilities, explain what Vultisig can do instead.
7. **Don't fabricate**: Only state facts about Vultisig that are provided in this prompt. If you're unsure about something Vultisig-specific (tokenomics, roadmap, partnerships, etc.), say you don't have that information and suggest checking the official Vultisig website or community channels.

## Response Format

Always use the respond_to_user tool to provide your response. This ensures proper formatting and suggestion handling.`

// RespondToUserTool is the tool definition for responding to users.
var RespondToUserTool = anthropic.Tool{
	Name:        "respond_to_user",
	Description: "Respond to the user with detected intent and optional suggestions for actions they can take.",
	InputSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"intent": map[string]any{
				"type":        "string",
				"enum":        []string{"action_request", "general_question", "unclear"},
				"description": "The detected user intent: 'action_request' for DCA/swap/send requests, 'general_question' for informational queries, 'unclear' when more context is needed.",
			},
			"response": map[string]any{
				"type":        "string",
				"description": "The response text to show the user.",
			},
			"suggestions": map[string]any{
				"type":        "array",
				"description": "Optional action suggestions based on the user's intent. Only include for action_request intents.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"plugin_id": map[string]any{
							"type":        "string",
							"description": "The plugin ID that can handle this action.",
						},
						"title": map[string]any{
							"type":        "string",
							"description": "A short, descriptive title for the suggestion (e.g., 'Weekly DCA into ETH').",
						},
						"description": map[string]any{
							"type":        "string",
							"description": "A brief description of what this suggestion will do.",
						},
					},
					"required": []string{"plugin_id", "title", "description"},
				},
			},
		},
		"required": []string{"intent", "response"},
	},
}

// ConfirmActionTool is the tool definition for confirming action results.
var ConfirmActionTool = anthropic.Tool{
	Name:        "confirm_action",
	Description: "Generate a confirmation message for a completed action (success or failure).",
	InputSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"response": map[string]any{
				"type":        "string",
				"description": "A friendly, concise message confirming the action result. For success: celebrate and summarize what was set up. For failure: explain what went wrong and offer help.",
			},
			"next_steps": map[string]any{
				"type":        "array",
				"description": "Optional list of suggested next actions the user might want to take.",
				"items": map[string]any{
					"type": "string",
				},
			},
		},
		"required": []string{"response"},
	},
}

// BuildPolicyTool is the tool definition for building policy configurations.
var BuildPolicyTool = anthropic.Tool{
	Name:        "build_policy",
	Description: "Build a policy configuration based on the user's conversation and the plugin's schema.",
	InputSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"configuration": map[string]any{
				"type":        "object",
				"description": "The configuration object matching the plugin's RecipeSchema. Include all required fields based on conversation context.",
				"additionalProperties": true,
			},
			"explanation": map[string]any{
				"type":        "string",
				"description": "A brief human-readable explanation of what was configured.",
			},
		},
		"required": []string{"configuration", "explanation"},
	},
}

// PluginSkill represents a plugin's capabilities loaded from skills.md
type PluginSkill struct {
	PluginID string
	Name     string
	Skills   string // Raw markdown content from skills.md
}

// SummarizationPrompt is the prompt used to summarize older conversation messages.
const SummarizationPrompt = `Summarize the following conversation between a user and the Vultisig AI assistant. Focus on:
- Key user intents and requests
- Important decisions made
- Assets, amounts, chains, and addresses mentioned
- Actions taken or pending

Be concise but preserve all actionable details. This summary will be used as context for future messages.`

// BuildSystemPromptWithSummary appends an earlier conversation summary to the base system prompt.
func BuildSystemPromptWithSummary(basePrompt string, summary *string) string {
	if summary == nil {
		return basePrompt
	}
	return basePrompt + "\n\n## Earlier Conversation Summary\n\n" + *summary
}

// BuildFullPrompt constructs the complete system prompt with context and plugin skills.
func BuildFullPrompt(balances []Balance, addresses map[string]string, plugins []PluginSkill) string {
	var sb strings.Builder
	sb.WriteString(SystemPrompt)

	// Add plugin skills
	if len(plugins) > 0 {
		sb.WriteString("\n\n## Available Plugins\n\n")
		sb.WriteString("The following plugins are available for automation. When users express intent matching a plugin's capabilities, suggest using that plugin.\n")
		for _, p := range plugins {
			sb.WriteString("\n### ")
			sb.WriteString(p.Name)
			sb.WriteString(" (")
			sb.WriteString(p.PluginID)
			sb.WriteString(")\n\n")
			sb.WriteString(p.Skills)
			sb.WriteString("\n")
		}
	}

	// Add user wallet context
	if len(balances) > 0 || len(addresses) > 0 {
		sb.WriteString("\n\n## User's Wallet Context\n")

		if len(balances) > 0 {
			sb.WriteString("\n### Balances\n")
			for _, b := range balances {
				sb.WriteString("- ")
				sb.WriteString(b.Symbol)
				sb.WriteString(" on ")
				sb.WriteString(b.Chain)
				sb.WriteString(": ")
				sb.WriteString(b.Amount)
				sb.WriteString("\n")
			}
		}

		if len(addresses) > 0 {
			sb.WriteString("\n### Addresses\n")
			for chain, addr := range addresses {
				sb.WriteString("- ")
				sb.WriteString(chain)
				sb.WriteString(": ")
				sb.WriteString(addr)
				sb.WriteString("\n")
			}
		}
	}

	return sb.String()
}

// ConfirmActionPrompt is the system prompt for confirming action results.
const ConfirmActionPrompt = `You are the Vultisig AI assistant. The user just completed an action in the app, and you need to confirm the result.

## Guidelines

1. **For successful actions**: Celebrate briefly and summarize what was accomplished. Remind them what the automation will do.
2. **For failed actions**: Be empathetic, explain what went wrong in simple terms, and offer helpful next steps.
3. **Keep it concise**: Users are on mobile devices.
4. **Be specific**: Reference the actual action that was taken based on the conversation history.

## Common Actions

- **create_policy**: User created a recurring automation (DCA, swap, send)
- **install_plugin**: User installed a plugin to enable new features
- **cancel_policy**: User cancelled an active automation
- **update_policy**: User modified an existing automation`

// BuildConfirmActionPrompt constructs the system prompt for Ability 3 (Action Confirmation).
func BuildConfirmActionPrompt(result *ActionResult) string {
	var sb strings.Builder
	sb.WriteString(ConfirmActionPrompt)

	sb.WriteString("\n\n## Action Result\n")
	sb.WriteString("Action: ")
	sb.WriteString(result.Action)
	sb.WriteString("\nSuccess: ")
	if result.Success {
		sb.WriteString("Yes")
	} else {
		sb.WriteString("No")
		if result.Error != "" {
			sb.WriteString("\nError: ")
			sb.WriteString(result.Error)
		}
	}

	return sb.String()
}

// PolicyBuilderPrompt is the system prompt for building policy configurations.
const PolicyBuilderPrompt = `You are building a policy configuration for the Vultisig wallet.

Based on the conversation history and the user's selected action, create a configuration that matches the plugin's schema.

## Instructions

1. Extract relevant parameters from the conversation (amounts, tokens, chains, frequency, etc.)
2. Map them to the plugin's schema fields
3. Use the user's wallet addresses for source addresses
4. For tokens, use the correct token contract addresses (e.g., "0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48" for USDC). For native assets (ETH, BTC, etc.), leave the token field as an empty string ""
5. Ensure amounts are in human-readable format (e.g., "10" for 10 USDC, "0.5" for 0.5 ETH)

## Important

- Use the addresses from the user's context for the "from" address
- If the user mentioned specific amounts, use those — but never set a swap amount below ~$5 equivalent, as DEX providers will reject swaps that are too small
- If the user's balance for the source asset is below ~$5 equivalent, the swap will likely fail — include a warning in the explanation
- If no balance information is available, use the user's requested amount but note in the explanation that they should ensure sufficient funds
- Amounts are in human-readable form (e.g., "10" means 10 USDC, "0.01" means 0.01 ETH)
- If frequency was discussed, include it
- If any required field is unclear, make a reasonable default based on the conversation`

// UpdateMemoryTool is the tool definition for updating the user's memory document.
var UpdateMemoryTool = anthropic.Tool{
	Name: "update_memory",
	Description: "Update your persistent memory about this user. Send the COMPLETE " +
		"updated memory document (markdown). This replaces the entire document. " +
		"Only call this when you learn something new worth remembering.",
	InputSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"content": map[string]any{
				"type":        "string",
				"description": "The full updated memory document in markdown format. Max 4000 characters.",
			},
		},
		"required": []string{"content"},
	},
}

// MemoryManagementInstructions is appended to the system prompt for Ability 1 only.
const MemoryManagementInstructions = `

## Memory Management

You have a persistent memory document about this user that survives across conversations. You can update it anytime using the ` + "`update_memory`" + ` tool.

### When to Update
- User shares a preference ("I prefer weekly DCA", "I like ETH")
- User reveals personal info ("My name is Alex")
- User describes their strategy ("I only DCA into top 10 coins")
- You learn something that would help in future conversations
- An action completes (policy created, plugin installed)

### When NOT to Update
- Trivial greetings or transient chat
- Information already in your memory document
- Data available from the app (balances, addresses, prices)

### How to Update
- Send the COMPLETE updated document — it replaces the entire memory
- Keep it under 4000 characters
- Organize naturally using markdown sections
- Remove outdated information when updating
- Always include ` + "`respond_to_user`" + ` alongside ` + "`update_memory`" + ``

// BuildMemorySection wraps the user's memory document content for injection into system prompts.
// Returns empty string if content is empty.
func BuildMemorySection(content string) string {
	if content == "" {
		return ""
	}

	return "\n\n## Your Memories About This User\n\n" +
		"This is your persistent memory document about this user. Use it to personalize\n" +
		"your responses naturally — don't repeat it back unless relevant.\n\n" +
		content
}

// BuildPolicyBuilderPrompt constructs the system prompt for Ability 2 (Policy Builder).
func BuildPolicyBuilderPrompt(suggestion Suggestion, configSchemaJSON string, examplesJSON string, balances []Balance, addresses map[string]string) string {
	var sb strings.Builder
	sb.WriteString(PolicyBuilderPrompt)

	sb.WriteString("\n\n## Selected Action\n")
	sb.WriteString("Title: ")
	sb.WriteString(suggestion.Title)
	sb.WriteString("\nDescription: ")
	sb.WriteString(suggestion.Description)
	sb.WriteString("\nPlugin: ")
	sb.WriteString(suggestion.PluginID)

	sb.WriteString("\n\n## Configuration Schema\n")
	sb.WriteString("The configuration must match this JSON schema:\n```json\n")
	sb.WriteString(configSchemaJSON)
	sb.WriteString("\n```")

	if examplesJSON != "" {
		sb.WriteString("\n\n## Configuration Examples\n")
		sb.WriteString("Here are valid configuration examples:\n```json\n")
		sb.WriteString(examplesJSON)
		sb.WriteString("\n```")
	}

	// Add user wallet context
	if len(balances) > 0 || len(addresses) > 0 {
		sb.WriteString("\n\n## User's Wallet Context\n")

		if len(balances) > 0 {
			sb.WriteString("\n### Balances\n")
			for _, b := range balances {
				sb.WriteString("- ")
				sb.WriteString(b.Symbol)
				sb.WriteString(" on ")
				sb.WriteString(b.Chain)
				sb.WriteString(": ")
				sb.WriteString(b.Amount)
				sb.WriteString(" (")
				sb.WriteString(b.Asset)
				sb.WriteString(")\n")
			}
		}

		if len(addresses) > 0 {
			sb.WriteString("\n### Addresses (use these for 'from' fields)\n")
			for chain, addr := range addresses {
				sb.WriteString("- ")
				sb.WriteString(chain)
				sb.WriteString(": ")
				sb.WriteString(addr)
				sb.WriteString("\n")
			}
		}
	}

	return sb.String()
}
