package verifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client is a client for the verifier service.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a new verifier client.
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Plugin represents a plugin from the verifier.
type Plugin struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

// InstalledPluginsResponse is the response from GET /plugins/installed.
type InstalledPluginsResponse struct {
	Code int `json:"code"`
	Data struct {
		Plugins    []Plugin `json:"plugins"`
		TotalCount int      `json:"total_count"`
	} `json:"data"`
}

// RecipeSchema represents a plugin's recipe specification.
type RecipeSchema struct {
	SupportedResources   []SupportedResource `json:"supported_resources"`
	Configuration        map[string]any      `json:"configuration,omitempty"`
	ConfigurationExample []map[string]any    `json:"configuration_example,omitempty"`
}

// SupportedResource represents a supported resource in a recipe schema.
type SupportedResource struct {
	ResourcePath         ResourcePath          `json:"resource_path"`
	ParameterConstraints []ParameterConstraint `json:"parameter_constraints"`
}

// ResourcePath identifies a resource type.
type ResourcePath struct {
	FunctionID   string `json:"function_id"`
	ResourceType string `json:"resource_type"`
}

// ParameterConstraint defines constraints on a parameter.
type ParameterConstraint struct {
	ParameterName string     `json:"parameterName"`
	Constraint    Constraint `json:"constraint"`
}

// Constraint defines the constraint value.
type Constraint struct {
	Type       string `json:"type,omitempty"`
	FixedValue string `json:"fixedValue,omitempty"`
	Required   bool   `json:"required,omitempty"`
}

// RecipeSpecResponse is the response from GET /plugins/:pluginId/recipe-specification.
type RecipeSpecResponse struct {
	Code int          `json:"code"`
	Data RecipeSchema `json:"data"`
}

// PolicySuggest represents the policy suggestion from the plugin.
// JSON field names use camelCase to match protobuf JSON encoding from the suggest API.
type PolicySuggest struct {
	Rules           []Rule `json:"rules"`
	RateLimitWindow int    `json:"rateLimitWindow,omitempty"`
	MaxTxsPerWindow int    `json:"maxTxsPerWindow,omitempty"`
}

// Rule represents a policy rule.
type Rule struct {
	Resource             string                `json:"resource"`
	Effect               string                `json:"effect,omitempty"`
	Target               *Target               `json:"target,omitempty"`
	ParameterConstraints []ParameterConstraint `json:"parameterConstraints,omitempty"`
}

// Target represents a rule target.
type Target struct {
	Address string `json:"address,omitempty"`
}

// PolicySuggestResponse is the response from POST /plugins/:pluginId/recipe-specification/suggest.
type PolicySuggestResponse struct {
	Data PolicySuggest `json:"data"`
}

// SuggestRequest is the request body for the suggest endpoint.
type SuggestRequest struct {
	Configuration map[string]any `json:"configuration"`
}

// IsPluginInstalled checks if a plugin is installed for the given user.
func (c *Client) IsPluginInstalled(ctx context.Context, accessToken, pluginID string) (bool, error) {
	url := fmt.Sprintf("%s/plugins/installed", c.baseURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return false, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	var apiResp InstalledPluginsResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return false, fmt.Errorf("decode response: %w", err)
	}

	for _, p := range apiResp.Data.Plugins {
		if p.ID == pluginID {
			return true, nil
		}
	}
	return false, nil
}

// GetRecipeSchema fetches the recipe specification for a plugin.
func (c *Client) GetRecipeSchema(ctx context.Context, pluginID string) (*RecipeSchema, error) {
	url := fmt.Sprintf("%s/plugins/%s/recipe-specification", c.baseURL, pluginID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	var apiResp RecipeSpecResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &apiResp.Data, nil
}

// GetPolicySuggest calls the plugin's suggest endpoint to build a policy.
func (c *Client) GetPolicySuggest(ctx context.Context, pluginID string, configuration map[string]any) (*PolicySuggest, error) {
	url := fmt.Sprintf("%s/plugins/%s/recipe-specification/suggest", c.baseURL, pluginID)

	body, err := json.Marshal(SuggestRequest{Configuration: configuration})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(respBody))
	}

	var apiResp PolicySuggestResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &apiResp.Data, nil
}
