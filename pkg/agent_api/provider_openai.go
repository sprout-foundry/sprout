package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
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

	// Set default model if not already set
	if provider.model == "" {
		provider.model = "gpt-4o-mini"
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
	// Use the model registry for consistent context limit lookup
	registry := GetModelRegistry()
	contextLimit := registry.GetModelContextLength(p.model)
	return contextLimit, nil
}

// GetAvailableModels returns the list of available OpenAI models
func (p *OpenAIProvider) GetAvailableModels(ctx context.Context) ([]ModelDetails, error) {
	// Define featured models with their properties
	models := []ModelDetails{
		// GPT-5 models
		{
			ID:              "gpt-5",
			Name:            "GPT-5",
			ContextLength:   272000,
			InputCostPer1K:  0.005,
			OutputCostPer1K: 0.025,
			Features:        []string{"vision", "tools"},
		},
		{
			ID:              "gpt-5-mini",
			Name:            "GPT-5 Mini",
			ContextLength:   272000,
			InputCostPer1K:  0.000125,
			OutputCostPer1K: 0.0000625,
			Features:        []string{"vision", "tools"},
		},
		// O3 models
		{
			ID:              "o3",
			Name:            "O3",
			ContextLength:   200000,
			InputCostPer1K:  0.001,
			OutputCostPer1K: 0.004,
			Features:        []string{"reasoning"},
		},
		{
			ID:              "o3-mini",
			Name:            "O3 Mini",
			ContextLength:   200000,
			InputCostPer1K:  0.00055,
			OutputCostPer1K: 0.000138,
			Features:        []string{"reasoning"},
		},
		// GPT-4o models
		{
			ID:              "gpt-4o",
			Name:            "GPT-4o",
			ContextLength:   128000,
			InputCostPer1K:  0.005,
			OutputCostPer1K: 0.015,
			Features:        []string{"vision", "tools"},
		},
		{
			ID:              "gpt-4o-mini",
			Name:            "GPT-4o Mini",
			ContextLength:   128000,
			InputCostPer1K:  0.00015,
			OutputCostPer1K: 0.0006,
			Features:        []string{"vision", "tools"},
			IsDefault:       true,
		},
		// O1 models
		{
			ID:              "o1",
			Name:            "O1",
			ContextLength:   128000,
			InputCostPer1K:  0.001,
			OutputCostPer1K: 0.004,
			Features:        []string{"reasoning"},
		},
		{
			ID:              "o1-mini",
			Name:            "O1 Mini",
			ContextLength:   128000,
			InputCostPer1K:  0.00055,
			OutputCostPer1K: 0.000138,
			Features:        []string{"reasoning"},
		},
	}

	return models, nil
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
				ReasoningContent: choice.Message.ReasoningContent,
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
	// Use the model registry for consistent pricing lookup
	registry := GetModelRegistry()
	inputCostPer1M, outputCostPer1M := registry.GetModelPricing(model)

	// Convert from per 1M to per token costs
	inputCost := float64(promptTokens) * inputCostPer1M / 1_000_000
	outputCost := float64(completionTokens) * outputCostPer1M / 1_000_000

	return inputCost + outputCost
}
