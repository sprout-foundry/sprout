package providers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	types "github.com/alantheprice/ledit/pkg/agent_types"
)

// OpenRouterProvider implements the OpenAI-compatible OpenRouter API
type OpenRouterProvider struct {
	httpClient   *http.Client
	apiToken     string
	debug        bool
	model        string
	models       []types.ModelInfo
	modelsCached bool
}

// NewOpenRouterProvider creates a new OpenRouter provider instance
func NewOpenRouterProvider() (*OpenRouterProvider, error) {
	token := os.Getenv("OPENROUTER_API_KEY")
	if token == "" {
		return nil, fmt.Errorf("OPENROUTER_API_KEY environment variable not set")
	}

	return &OpenRouterProvider{
		httpClient: &http.Client{
			Timeout: 300 * time.Second,
		},
		apiToken: token,
		debug:    false,
		model:    "deepseek/deepseek-chat-v3.1:free", // Default OpenRouter model
	}, nil
}

// NewOpenRouterProviderWithModel creates an OpenRouter provider with a specific model
func NewOpenRouterProviderWithModel(model string) (*OpenRouterProvider, error) {
	provider, err := NewOpenRouterProvider()
	if err != nil {
		return nil, err
	}
	provider.model = model
	return provider, nil
}

// SendChatRequest sends a chat completion request to OpenRouter
func (p *OpenRouterProvider) SendChatRequest(messages []types.Message, tools []types.Tool, reasoning string) (*types.ChatResponse, error) {
	// Convert messages to OpenRouter format
	openRouterMessages := make([]map[string]interface{}, len(messages))
	for i, msg := range messages {
		// Start with text content
		content := msg.Content

		// For messages with images, convert to OpenAI-compatible format
		if len(msg.Images) > 0 {
			// Create multimodal content array
			contentArray := []map[string]interface{}{
				{
					"type": "text",
					"text": content,
				},
			}

			// Add images
			for _, img := range msg.Images {
				imageContent := map[string]interface{}{
					"type": "image_url",
				}

				if img.URL != "" {
					imageContent["image_url"] = map[string]interface{}{
						"url": img.URL,
					}
				} else if img.Base64 != "" {
					// Format as data URL
					mimeType := img.Type
					if mimeType == "" {
						mimeType = "image/jpeg" // default
					}
					dataURL := fmt.Sprintf("data:%s;base64,%s", mimeType, img.Base64)
					imageContent["image_url"] = map[string]interface{}{
						"url": dataURL,
					}
				}

				contentArray = append(contentArray, imageContent)
			}

			openRouterMessages[i] = map[string]interface{}{
				"role":    msg.Role,
				"content": contentArray,
			}
		} else {
			// Regular text-only message
			openRouterMessages[i] = map[string]interface{}{
				"role":    msg.Role,
				"content": content,
			}
		}
	}

	// Calculate appropriate max_tokens based on context limits
	maxTokens := p.calculateMaxTokens(messages, tools)

	// Build request payload
	requestBody := map[string]interface{}{
		"model":       p.model,
		"messages":    openRouterMessages,
		"max_tokens":  maxTokens,
		"temperature": 0.7,
		"usage":       map[string]interface{}{"include": true}, // Enable usage accounting for cost tracking
	}

	// Add tools if provided
	if len(tools) > 0 {
		requestBody["tools"] = tools
		requestBody["tool_choice"] = "auto"
	}

	reqBody, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequest("POST", "https://openrouter.ai/api/v1/chat/completions", bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiToken)
	httpReq.Header.Set("HTTP-Referer", "https://github.com/alantheprice/coder") // Required by OpenRouter
	httpReq.Header.Set("X-Title", "Coder AI Assistant")                         // Required by OpenRouter

	// Log the model for debugging if debug is enabled
	if p.debug {
		fmt.Printf("🔍 Using OpenRouter model: %s\n", p.model)
	}
	if p.debug {
		fmt.Printf("🔍 OpenRouter Request URL: %s\n", "https://openrouter.ai/api/v1/chat/completions")
		fmt.Printf("🔍 OpenRouter Request Body: %s\n", string(reqBody))
	}

	return p.sendRequestWithRetry(httpReq, reqBody)
}

// CheckConnection checks if the OpenRouter connection is valid
func (p *OpenRouterProvider) CheckConnection() error {
	if p.apiToken == "" {
		return fmt.Errorf("OPENROUTER_API_KEY environment variable not set")
	}
	return nil
}

// SetDebug enables or disables debug mode
func (p *OpenRouterProvider) SetDebug(debug bool) {
	p.debug = debug
}

// SetModel sets the model to use
func (p *OpenRouterProvider) SetModel(model string) error {
	p.model = model
	return nil
}

// GetModel returns the current model
func (p *OpenRouterProvider) GetModel() string {
	return p.model
}

// GetProvider returns the provider name
func (p *OpenRouterProvider) GetProvider() string {
	return "openrouter"
}

// GetModelContextLimit returns the context limit for the current model
func (p *OpenRouterProvider) GetModelContextLimit() (int, error) {
	if !p.modelsCached {
		if _, err := p.ListModels(); err != nil {
			return 32000, nil // fallback
		}
	}

	for _, m := range p.models {
		if m.ID == p.model {
			return m.ContextLength, nil
		}
	}

	return 128000, nil // default
}

// ListModels returns available models from OpenRouter API
func (p *OpenRouterProvider) ListModels() ([]types.ModelInfo, error) {
	if p.modelsCached {
		return p.models, nil
	}

	httpReq, err := http.NewRequest("GET", "https://openrouter.ai/api/v1/models", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+p.apiToken)
	httpReq.Header.Set("HTTP-Referer", "https://github.com/alantheprice/coder")
	httpReq.Header.Set("X-Title", "Coder AI Assistant")

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to get models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("OpenRouter API error (status %d): %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var response struct {
		Data []struct {
			ID            string `json:"id"`
			Name          string `json:"name"`
			Description   string `json:"description"`
			ContextLength int    `json:"context_length"`
			Pricing       *struct {
				Prompt     string `json:"prompt"`
				Completion string `json:"completion"`
			} `json:"pricing"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	models := make([]types.ModelInfo, len(response.Data))
	for i, model := range response.Data {
		modelInfo := types.ModelInfo{
			ID:       model.ID,
			Name:     model.Name,
			Provider: "openrouter",
		}

		if model.Description != "" {
			modelInfo.Description = model.Description
		}
		if model.ContextLength > 0 {
			modelInfo.ContextLength = model.ContextLength
		}

		// Parse pricing if available
		if model.Pricing != nil {
			if promptCost, err := strconv.ParseFloat(model.Pricing.Prompt, 64); err == nil {
				// OpenRouter pricing is per token, not per million tokens
				// Convert to per million tokens for consistency with other providers
				modelInfo.InputCost = promptCost * 1000000
			}
			if completionCost, err := strconv.ParseFloat(model.Pricing.Completion, 64); err == nil {
				// OpenRouter pricing is per token, not per million tokens
				// Convert to per million tokens for consistency with other providers
				modelInfo.OutputCost = completionCost * 1000000
			}
			if modelInfo.InputCost > 0 || modelInfo.OutputCost > 0 {
				modelInfo.Cost = (modelInfo.InputCost + modelInfo.OutputCost) / 2.0
			}
		}

		models[i] = modelInfo
	}

	return models, nil
}

// sendRequestWithRetry implements exponential backoff retry logic for rate limits
func (p *OpenRouterProvider) sendRequestWithRetry(httpReq *http.Request, reqBody []byte) (*types.ChatResponse, error) {
	maxRetries := 3
	baseDelay := 1 * time.Second

	for attempt := 0; attempt <= maxRetries; attempt++ {
		// Clone the request body for retry attempts
		httpReq.Body = io.NopCloser(bytes.NewBuffer(reqBody))

		resp, err := p.httpClient.Do(httpReq)
		if err != nil {
			return nil, fmt.Errorf("failed to send request: %w", err)
		}

		respBody, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()

		if readErr != nil {
			return nil, fmt.Errorf("failed to read response body: %w", readErr)
		}

		// Log the response for debugging
		if p.debug {
			fmt.Printf("🔍 OpenRouter Response Status (attempt %d): %s\n", attempt+1, resp.Status)
			fmt.Printf("🔍 OpenRouter Response Body: %s\n", string(respBody))
		}

		// Success case
		if resp.StatusCode == http.StatusOK {
			// First parse into a generic map to extract OpenRouter-specific fields
			var rawResp map[string]interface{}
			if err := json.Unmarshal(respBody, &rawResp); err != nil {
				return nil, fmt.Errorf("failed to unmarshal raw response: %w", err)
			}

			// Parse into our standard response structure
			var chatResp types.ChatResponse
			if err := json.Unmarshal(respBody, &chatResp); err != nil {
				return nil, fmt.Errorf("failed to unmarshal response: %w", err)
			}

			// Extract OpenRouter's actual cost from the "usage.cost" field
			if usage, ok := rawResp["usage"].(map[string]interface{}); ok {
				if cost, ok := usage["cost"].(float64); ok {
					chatResp.Usage.EstimatedCost = cost
				}
			}

			// Log usage information for debugging
			if p.debug {
				fmt.Printf("🔍 OpenRouter Usage: prompt=%d, completion=%d, total=%d, actual_cost=%f\n",
					chatResp.Usage.PromptTokens, chatResp.Usage.CompletionTokens,
					chatResp.Usage.TotalTokens, chatResp.Usage.EstimatedCost)
			}

			// Only calculate cost if OpenRouter didn't provide it and it's not a free model
			if chatResp.Usage.EstimatedCost == 0 && !strings.Contains(p.model, ":free") && (chatResp.Usage.PromptTokens > 0 || chatResp.Usage.CompletionTokens > 0) {
				chatResp.Usage.EstimatedCost = p.calculateCost(chatResp.Usage.PromptTokens, chatResp.Usage.CompletionTokens)
				if p.debug {
					fmt.Printf("🔍 Fallback calculated cost: %f\n", chatResp.Usage.EstimatedCost)
				}
			}

			return &chatResp, nil
		}

		// Handle error cases
		if resp.StatusCode == 429 {
			// Parse the error response to check for daily limits
			var errorResp map[string]interface{}
			if err := json.Unmarshal(respBody, &errorResp); err == nil {
				if errorObj, ok := errorResp["error"].(map[string]interface{}); ok {
					if message, ok := errorObj["message"].(string); ok {
						// Check for daily limit - don't retry these
						if strings.Contains(strings.ToLower(message), "daily limit") {
							return nil, fmt.Errorf("daily limit exceeded: %s", message)
						}

						// For rate limits, implement backoff
						if attempt < maxRetries {
							// Check for rate limit headers to get reset time
							waitTime := p.calculateBackoffDelay(resp, attempt, baseDelay)
							fmt.Printf("⏳ Rate limit hit (attempt %d/%d), waiting %v before retry...\n",
								attempt+1, maxRetries+1, waitTime)
							time.Sleep(waitTime)
							continue
						}
					}
				}
			}
		}

		// For non-retry errors or max retries exceeded
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil, fmt.Errorf("max retries exceeded")
}

// calculateMaxTokens calculates appropriate max_tokens based on input size and model limits
func (p *OpenRouterProvider) calculateMaxTokens(messages []types.Message, tools []types.Tool) int {
	// Get model context limit
	contextLimit, err := p.GetModelContextLimit()
	if err != nil || contextLimit == 0 {
		contextLimit = 32000 // Conservative default
	}

	// Rough estimation: 1 token ≈ 4 characters
	inputTokens := 0

	// Estimate tokens from messages
	for _, msg := range messages {
		inputTokens += len(msg.Content) / 4
	}

	// Estimate tokens from tools (tools descriptions can be large)
	inputTokens += len(tools) * 200 // Rough estimate per tool

	// Reserve buffer for safety and leave room for response
	maxOutput := contextLimit - inputTokens - 1000 // 1000 token safety buffer

	// Ensure reasonable bounds
	if maxOutput > 16000 {
		maxOutput = 16000 // Cap at 16K for most responses
	} else if maxOutput < 1000 {
		maxOutput = 1000 // Minimum useful response size
	}

	return maxOutput
}

// calculateBackoffDelay calculates the delay for exponential backoff
func (p *OpenRouterProvider) calculateBackoffDelay(resp *http.Response, attempt int, baseDelay time.Duration) time.Duration {
	// Try to use X-RateLimit-Reset header if available
	if resetHeader := resp.Header.Get("X-RateLimit-Reset"); resetHeader != "" {
		if resetTime, err := strconv.ParseInt(resetHeader, 10, 64); err == nil {
			// Convert from milliseconds to time
			resetAt := time.Unix(resetTime/1000, (resetTime%1000)*1000000)
			waitTime := time.Until(resetAt)

			// Add small buffer and cap at reasonable maximum
			waitTime += 2 * time.Second
			if waitTime > 60*time.Second {
				waitTime = 60 * time.Second
			}
			if waitTime > 0 {
				return waitTime
			}
		}
	}

	// Fallback to exponential backoff
	delay := baseDelay * time.Duration(math.Pow(2, float64(attempt)))
	// Cap at 60 seconds
	if delay > 60*time.Second {
		delay = 60 * time.Second
	}
	return delay
}

// SupportsVision checks if the current model supports vision
func (p *OpenRouterProvider) SupportsVision() bool {
	// Check if we have a vision model available
	visionModel := p.GetVisionModel()
	return visionModel != ""
}

// GetVisionModel returns the vision model for OpenRouter
func (p *OpenRouterProvider) GetVisionModel() string {
	// Return first featured vision model
	featuredVisionModels := p.GetFeaturedVisionModels()
	if len(featuredVisionModels) > 0 {
		return featuredVisionModels[0]
	}
	return ""
}

// SendVisionRequest sends a vision-enabled chat request
func (p *OpenRouterProvider) SendVisionRequest(messages []types.Message, tools []types.Tool, reasoning string) (*types.ChatResponse, error) {
	// If the model doesn't support vision, fall back to regular chat
	if !p.SupportsVision() {
		return p.SendChatRequest(messages, tools, reasoning)
	}

	// Temporarily switch to vision model for this request
	originalModel := p.model
	visionModel := p.GetVisionModel()

	p.model = visionModel

	// Send the vision request using regular chat logic (images are handled automatically)
	response, err := p.SendChatRequest(messages, tools, reasoning)

	// Restore original model
	p.model = originalModel

	return response, err
}

func (p *OpenRouterProvider) GetFeaturedModels() []string {
	return []string{
		"qwen/qwen3-coder:free",
		"qwen/qwen3-coder-30b-a3b-instruct",
		"qwen/qwen3-coder",
		"qwen/qwen3-235b-a22b-thinking-2507",
		"deepseek/deepseek-chat-v3.1:free",
		"deepseek/deepseek-chat-v3.1",
		"mistralai/codestral-2508",
		"mistralai/devstral-small-2505",
		"x-ai/grok-code-fast-1",
	}
}

func (p *OpenRouterProvider) GetFeaturedVisionModels() []string {
	return []string{
		"google/gemma-3-27b-it",      // Primary vision model for open providers
		"google/gemma-3-27b-it:free", // Free vision model option
	}
}

// calculateCost calculates the cost based on token usage and model pricing
func (p *OpenRouterProvider) calculateCost(promptTokens, completionTokens int) float64 {
	// Get pricing information from cached models if available
	if !p.modelsCached {
		// Try to load models to get pricing, but don't fail if we can't
		p.ListModels()
	}

	// Find pricing for current model
	for _, m := range p.models {
		if m.ID == p.model {
			// Calculate cost: pricing is per million tokens
			inputCost := float64(promptTokens) * m.InputCost / 1000000.0
			outputCost := float64(completionTokens) * m.OutputCost / 1000000.0
			return inputCost + outputCost
		}
	}

	// For free models, return 0
	if strings.Contains(p.model, ":free") {
		return 0
	}

	// No hardcoded fallback pricing - return 0 for unknown models
	// This ensures we always use the actual pricing from the API
	// rather than potentially incorrect hardcoded values
	return 0
}
