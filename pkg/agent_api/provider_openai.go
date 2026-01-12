package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// OpenAIProvider implements the Provider interface for OpenAI
type OpenAIProvider struct {
	*BaseProvider

	// OpenAI-specific fields
	organization string
}

// NewOpenAIProvider creates a new OpenAI provider
func NewOpenAIProvider() (*OpenAIProvider, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY environment variable not set")
	}

	base := NewBaseProvider(
		"OpenAI",
		OpenAIClientType,
		"https://api.openai.com/v1/chat/completions",
		apiKey,
	)

	// Set OpenAI-specific features
	base.supportsVision = true
	base.supportsReasoning = true

	provider := &OpenAIProvider{
		BaseProvider: base,
		organization: os.Getenv("OPENAI_ORG_ID"),
	}

	// Override HTTP client timeout for OpenAI (5 minutes)
	provider.httpClient = &http.Client{
		Timeout: 5 * time.Minute,
	}

	// Set default model if not already set
	if provider.model == "" {
		provider.model = "gpt-5-mini"
	}

	return provider, nil
}

// SendChatRequest sends a chat request to OpenAI
func (p *OpenAIProvider) SendChatRequest(ctx context.Context, req *ProviderChatRequest) (*ChatResponse, error) {
	// Convert to OpenAI-specific request format
	openAIReq := p.buildOpenAIRequest(req)

	// Marshal request
	body, err := json.Marshal(openAIReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	if p.debug {
		fmt.Printf("[OpenAI] Request: %s\n", string(body))
	}

	// Minimax-specific tool call validation
	if p.debug && strings.Contains(strings.ToLower(p.model), "minimax") {
		fmt.Printf("[Minimax] Validating tool call ordering in request:\n")
		for i, msg := range openAIReq.Messages {
			if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
				fmt.Printf("  [%d] Assistant with %d tool calls:\n", i, len(msg.ToolCalls))
				for j, tc := range msg.ToolCalls {
					fmt.Printf("    [%d] id=%s name=%s\n", j, tc.ID, tc.Function.Name)
				}
			} else if msg.Role == "tool" {
				fmt.Printf("  [%d] Tool result for tool_call_id=%s\n", i, msg.ToolCallId)
			}
		}
	}

	// Create HTTP request
	httpReq, err := p.MakeAuthRequest(ctx, "POST", p.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	// Add OpenAI-specific headers
	if p.organization != "" {
		httpReq.Header.Set("OpenAI-Organization", p.organization)
	}

	// Send request
	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Read response
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if p.debug {
		fmt.Printf("[OpenAI] Response: %s\n", string(respBody))
	}

	// Parse response
	var openAIResp OpenAIResponse
	if err := json.Unmarshal(respBody, &openAIResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Check for API errors
	if openAIResp.Error != nil {
		return nil, fmt.Errorf("OpenAI API error: %s", openAIResp.Error.Message)
	}

	// Convert to unified response
	return p.convertToUnifiedResponse(&openAIResp), nil
}

// CheckConnection verifies the connection to OpenAI
func (p *OpenAIProvider) CheckConnection(ctx context.Context) error {
	// Send a minimal request to verify connectivity
	req := &ProviderChatRequest{
		Messages: []Message{
			{Role: "user", Content: "Hi"},
		},
	}

	_, err := p.SendChatRequest(ctx, req)
	return err
}

// GetModelContextLimit returns the context window size for the current model
func (p *OpenAIProvider) GetModelContextLimit() (int, error) {
	// Most modern OpenAI models support 128K context
	// This should ideally come from the API, not hardcoded
	return 128000, nil
}

// GetAvailableModels returns the list of available OpenAI models
func (p *OpenAIProvider) GetAvailableModels(ctx context.Context) ([]ModelDetails, error) {
	// This should query the OpenAI API for available models
	// For now, return an error to force using the actual API
	return nil, fmt.Errorf("GetAvailableModels not implemented - use OpenAI API directly")
}

// buildOpenAIRequest converts a unified request to OpenAI format
func (p *OpenAIProvider) buildOpenAIRequest(req *ProviderChatRequest) *OpenAIRequest {
	openAIReq := &OpenAIRequest{
		Model:    p.model,
		Messages: req.Messages,
		Tools:    req.Tools,
		Stream:   false,
	}

	// Apply request options
	if req.Options != nil {
		// Temperature (not for GPT-5 models)
		if req.Options.Temperature != nil && !strings.Contains(p.model, "gpt-5") {
			openAIReq.Temperature = req.Options.Temperature
		}

		// Max tokens
		if req.Options.MaxTokens != nil {
			openAIReq.MaxTokens = *req.Options.MaxTokens
		} else {
			// Calculate appropriate default
			openAIReq.MaxTokens = p.calculateMaxTokens(req.Messages, req.Tools)
		}

		// Reasoning effort for O-series models
		if req.Options.ReasoningEffort != "" && strings.Contains(p.model, "o") {
			openAIReq.Reasoning = req.Options.ReasoningEffort
		}

		// Stream
		openAIReq.Stream = req.Options.Stream
	} else {
		// Default max tokens
		openAIReq.MaxTokens = p.calculateMaxTokens(req.Messages, req.Tools)
	}

	return openAIReq
}

// calculateMaxTokens estimates appropriate max tokens based on context
func (p *OpenAIProvider) calculateMaxTokens(messages []Message, tools []Tool) int {
	contextLimit, _ := p.GetModelContextLimit()

	// Estimate tokens used by messages and tools
	estimatedUsage := 0
	for _, msg := range messages {
		estimatedUsage += len(msg.Content) / 4 // Rough estimate
	}
	for range tools {
		estimatedUsage += 200 // Rough estimate per tool
	}

	// Leave room for response (use 75% of remaining context)
	remaining := contextLimit - estimatedUsage
	maxTokens := int(float64(remaining) * 0.75)

	// Apply reasonable limits
	if maxTokens < 1000 {
		maxTokens = 1000
	}
	if maxTokens > 16000 {
		maxTokens = 16000
	}

	return maxTokens
}

// convertToUnifiedResponse converts OpenAI response to unified format
func (p *OpenAIProvider) convertToUnifiedResponse(resp *OpenAIResponse) *ChatResponse {
	unified := &ChatResponse{
		ID:      resp.ID,
		Object:  resp.Object,
		Created: resp.Created,
		Model:   resp.Model,
		Usage: struct {
			PromptTokens        int     `json:"prompt_tokens"`
			CompletionTokens    int     `json:"completion_tokens"`
			TotalTokens         int     `json:"total_tokens"`
			EstimatedCost       float64 `json:"estimated_cost"`
			PromptTokensDetails struct {
				CachedTokens     int  `json:"cached_tokens"`
				CacheWriteTokens *int `json:"cache_write_tokens"`
			} `json:"prompt_tokens_details,omitempty"`
		}{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
			EstimatedCost:    p.EstimateCost(resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Model),
		},
	}

	// Convert choices
	for _, choice := range resp.Choices {
		// Handle reasoning content - prioritize reasoning_details (Minimax format) over reasoning_content
		reasoningContent := choice.Message.ReasoningContent
		if choice.Message.ReasoningDetails != "" {
			reasoningContent = choice.Message.ReasoningDetails
		}

		unifiedChoice := Choice{
			Index: choice.Index,
			Message: struct {
				Role             string      `json:"role"`
				Content          string      `json:"content"`
				ReasoningContent string      `json:"reasoning_content,omitempty"`
				Images           []ImageData `json:"images,omitempty"`
				ToolCalls        []ToolCall  `json:"tool_calls,omitempty"`
			}{
				Role:             choice.Message.Role,
				Content:          choice.Message.Content,
				ReasoningContent: reasoningContent,
				ToolCalls:        choice.Message.ToolCalls,
			},
			FinishReason: choice.FinishReason,
		}
		unified.Choices = append(unified.Choices, unifiedChoice)
	}

	return unified
}

// EstimateCost calculates the cost for OpenAI models
func (p *OpenAIProvider) EstimateCost(promptTokens, completionTokens int, model string) float64 {
	// Get pricing for the model
	inputCostPer1M, outputCostPer1M := p.getModelPricing(model)

	// Convert from per 1M to per token costs
	inputCost := float64(promptTokens) * inputCostPer1M / 1_000_000
	outputCost := float64(completionTokens) * outputCostPer1M / 1_000_000

	return inputCost + outputCost
}

// getModelPricing returns input and output costs per 1M tokens for OpenAI models
func (p *OpenAIProvider) getModelPricing(model string) (inputCostPer1M, outputCostPer1M float64) {
	// OpenAI pricing (as of last known update)
	// TODO: This should ideally come from the OpenAI API
	switch {
	case strings.Contains(model, "gpt-5-mini"):
		return 0.15, 0.60
	case strings.Contains(model, "gpt-5"):
		return 2.50, 10.00
	case strings.Contains(model, "gpt-4-turbo"):
		return 10.00, 30.00
	case strings.Contains(model, "gpt-4"):
		return 30.00, 60.00
	case strings.Contains(model, "gpt-3.5-turbo"):
		return 0.50, 1.50
	case strings.Contains(model, "o1-preview"):
		return 15.00, 60.00
	case strings.Contains(model, "o1-mini"):
		return 3.00, 12.00
	default:
		// Conservative fallback
		return 1.0, 2.0
	}
}
