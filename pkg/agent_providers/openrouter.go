package providers

import (
	"bufio"
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

	api "github.com/alantheprice/ledit/pkg/agent_api"
)

// OpenRouterProvider implements the OpenAI-compatible OpenRouter API
type OpenRouterProvider struct {
	httpClient      *http.Client
	streamingClient *http.Client
	apiToken        string
	debug           bool
	model           string
	models          []api.ModelInfo
	modelsCached    bool
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
		streamingClient: &http.Client{
			Timeout: 900 * time.Second, // 15 minutes for streaming requests
		},
		apiToken: token,
		debug:    false,
		model:    "openai/gpt-5", // Default OpenRouter model (matches config.go defaults)
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
func (p *OpenRouterProvider) SendChatRequest(messages []api.Message, tools []api.Tool, reasoning string) (*api.ChatResponse, error) {
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
			message := map[string]interface{}{
				"role":    msg.Role,
				"content": content,
			}
			// Add tool_call_id for tool result messages
			if msg.ToolCallId != "" {
				message["tool_call_id"] = msg.ToolCallId
			}
			openRouterMessages[i] = message
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

	httpReq.Header.Set("HTTP-Referer", "https://github.com/alantheprice/ledit")
	httpReq.Header.Set("X-Title", "Ledit Coding Assistant")

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

// SendChatRequestStream sends a streaming chat request to OpenRouter
func (p *OpenRouterProvider) SendChatRequestStream(messages []api.Message, tools []api.Tool, reasoning string, callback api.StreamCallback) (*api.ChatResponse, error) {
	url := "https://openrouter.ai/api/v1/chat/completions"

	// Convert our messages to OpenAI format
	openAIMessages := make([]interface{}, len(messages))
	for i, msg := range messages {
		message := map[string]interface{}{
			"role":    msg.Role,
			"content": msg.Content,
		}
		// Add tool_call_id for tool result messages
		if msg.ToolCallId != "" {
			message["tool_call_id"] = msg.ToolCallId
		}
		openAIMessages[i] = message
	}

	reqBody := map[string]interface{}{
		"model":       p.model,
		"messages":    openAIMessages,
		"temperature": 0.7,
		"stream":      true, // Enable streaming
	}

	// Add tools if present
	if len(tools) > 0 {
		openAITools := make([]map[string]interface{}, len(tools))
		for i, tool := range tools {
			openAITools[i] = map[string]interface{}{
				"type": tool.Type,
				"function": map[string]interface{}{
					"name":        tool.Function.Name,
					"description": tool.Function.Description,
					"parameters":  tool.Function.Parameters,
				},
			}
		}
		reqBody["tools"] = openAITools
		reqBody["tool_choice"] = "auto"
	}

	reqBodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequest("POST", url, bytes.NewBuffer(reqBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiToken)
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("HTTP-Referer", "https://github.com/alantheprice/ledit")
	httpReq.Header.Set("X-Title", "Ledit Coding Assistant")

	if p.debug {
		fmt.Printf("🔍 OpenRouter Streaming Request URL: %s\n", url)
		fmt.Printf("🔍 OpenRouter Streaming Request Body: %s\n", string(reqBodyBytes))
	}

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("OpenRouter API error (status %d): %s", resp.StatusCode, string(body))
	}

	// Process SSE stream
	scanner := bufio.NewScanner(resp.Body)
	var content strings.Builder
	var toolCalls []api.ToolCall
	var toolCallsMap = make(map[string]*api.ToolCall) // Track tool calls by ID for proper accumulation
	var finishReason string
	var usage struct {
		PromptTokens        int     `json:"prompt_tokens"`
		CompletionTokens    int     `json:"completion_tokens"`
		TotalTokens         int     `json:"total_tokens"`
		EstimatedCost       float64 `json:"estimated_cost"`
		PromptTokensDetails struct {
			CachedTokens     int  `json:"cached_tokens"`
			CacheWriteTokens *int `json:"cache_write_tokens"`
		} `json:"prompt_tokens_details,omitempty"`
	}
	var actualCost float64

	for scanner.Scan() {
		line := scanner.Text()

		// Skip empty lines
		if line == "" {
			continue
		}

		// Parse SSE data
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")

			// Check for end of stream
			if data == "[DONE]" {
				break
			}

			// Parse JSON chunk
			var chunk map[string]interface{}
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				if p.debug {
					fmt.Printf("Failed to parse chunk: %s\n", err)
				}
				continue
			}

			// Extract usage information (OpenRouter sends this in the final chunk)
			if usageData, ok := chunk["usage"].(map[string]interface{}); ok {
				if promptTokens, ok := usageData["prompt_tokens"].(float64); ok {
					usage.PromptTokens = int(promptTokens)
				}
				if completionTokens, ok := usageData["completion_tokens"].(float64); ok {
					usage.CompletionTokens = int(completionTokens)
				}
				if totalTokens, ok := usageData["total_tokens"].(float64); ok {
					usage.TotalTokens = int(totalTokens)
				}
				// Extract OpenRouter's actual cost
				if cost, ok := usageData["cost"].(float64); ok {
					actualCost = cost
				}
			}

			// Extract choices
			if choices, ok := chunk["choices"].([]interface{}); ok && len(choices) > 0 {
				if choice, ok := choices[0].(map[string]interface{}); ok {
					// Extract delta content
					if delta, ok := choice["delta"].(map[string]interface{}); ok {
						if contentChunk, ok := delta["content"].(string); ok && contentChunk != "" {
							content.WriteString(contentChunk)
							// Call the streaming callback
							if callback != nil {
								callback(contentChunk)
							}
						}

						// Handle tool calls in delta - proper incremental parsing
						if toolCallsData, ok := delta["tool_calls"].([]interface{}); ok {
							for _, tcData := range toolCallsData {
								if tc, ok := tcData.(map[string]interface{}); ok {
									// Get tool call index and ID
									var toolCallIndex int
									var toolCallID string

									if idx, ok := tc["index"].(float64); ok {
										toolCallIndex = int(idx)
									}
									if id, ok := tc["id"].(string); ok && id != "" {
										toolCallID = id
									}

									// Create a unique key for this tool call (use ID if available, otherwise use index)
									key := toolCallID
									if key == "" {
										key = fmt.Sprintf("tc_%d", toolCallIndex)
									}

									// Get or create the tool call
									if _, exists := toolCallsMap[key]; !exists {
										toolCallsMap[key] = &api.ToolCall{
											ID:   toolCallID,
											Type: "function",
										}
										// Initialize the function struct
										toolCallsMap[key].Function.Arguments = "" // Will be built incrementally
									}

									currentTC := toolCallsMap[key]

									// Update the tool call ID if we got it in this chunk
									if toolCallID != "" && currentTC.ID == "" {
										currentTC.ID = toolCallID
									}

									// Handle function data
									if fn, ok := tc["function"].(map[string]interface{}); ok {
										// Set function name if present
										if name, ok := fn["name"].(string); ok && name != "" {
											currentTC.Function.Name = name
										}

										// Append incremental arguments
										if args, ok := fn["arguments"].(string); ok && args != "" {
											currentTC.Function.Arguments += args
										}
									}
								}
							}
						}
					}

					// Extract finish reason
					if fr, ok := choice["finish_reason"].(string); ok && fr != "" {
						finishReason = fr
					}
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading stream: %w", err)
	}

	// Convert tool calls map to slice and validate JSON arguments
	for _, tc := range toolCallsMap {
		// Validate that arguments form valid JSON before adding
		if tc.Function.Arguments != "" {
			var testJSON interface{}
			if json.Unmarshal([]byte(tc.Function.Arguments), &testJSON) != nil {
				if p.debug {
					fmt.Printf("🔍 Invalid JSON in tool call arguments for %s: %s\n", tc.Function.Name, tc.Function.Arguments)
				}
				// Skip malformed tool calls
				continue
			}
		}
		toolCalls = append(toolCalls, *tc)
	}

	// Set the actual cost if provided by OpenRouter
	if actualCost > 0 {
		usage.EstimatedCost = actualCost
	} else if !strings.Contains(p.model, ":free") && (usage.PromptTokens > 0 || usage.CompletionTokens > 0) {
		// Calculate cost based on the correct pricing model for OpenRouter - use the existing pricing system
		// First, try to get model information from cached models if available
		var inputCost, outputCost float64

		// Check if we already have pricing information cached
		if p.modelsCached {
			for _, m := range p.models {
				if m.ID == p.model {
					inputCost = m.InputCost
					outputCost = m.OutputCost
					break
				}
			}
		}

		// If we don't have cached model info, fallback to calculate cost
		if inputCost == 0 && outputCost == 0 {
			// Only calculate if we have the model info but it's missing pricing
			// This is a fallback to avoid breaking existing functionality
			usage.EstimatedCost = p.calculateCost(usage.PromptTokens, usage.CompletionTokens)
		} else if inputCost > 0 || outputCost > 0 {
			// Calculate using the actual model pricing
			inputCost = inputCost * float64(usage.PromptTokens) / 1000000.0
			outputCost = outputCost * float64(usage.CompletionTokens) / 1000000.0
			usage.EstimatedCost = inputCost + outputCost
		}
	}

	// Log usage information for debugging
	if p.debug {
		fmt.Printf("🔍 OpenRouter Streaming Usage: prompt=%d, completion=%d, total=%d, cost=%f\n",
			usage.PromptTokens, usage.CompletionTokens, usage.TotalTokens, usage.EstimatedCost)
	}

	// Build response
	response := &api.ChatResponse{
		Model: p.model,
		Choices: []api.Choice{
			{
				Index: 0,
				Message: struct {
					Role             string          `json:"role"`
					Content          string          `json:"content"`
					ReasoningContent string          `json:"reasoning_content,omitempty"`
					Images           []api.ImageData `json:"images,omitempty"`
					ToolCalls        []api.ToolCall  `json:"tool_calls,omitempty"`
				}{
					Role:      "assistant",
					Content:   content.String(),
					ToolCalls: toolCalls,
				},
				FinishReason: finishReason,
			},
		},
		Usage: usage,
	}

	return response, nil
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
			fmt.Printf("Warning: Failed to load OpenRouter models for %s, using fallback\n", p.model)
			return 128000, err
		}
	}

	for _, m := range p.models {
		if m.ID == p.model || strings.HasPrefix(m.ID, p.model) || strings.Contains(m.ID, p.model) {
			if m.ContextLength > 0 {
				return m.ContextLength, nil
			}
		}
	}

	// Model-aware fallback
	switch {
	case strings.Contains(p.model, "gpt-3.5") || strings.Contains(p.model, "llama-2"):
		return 4096, nil
	case strings.Contains(p.model, "gpt-4") || strings.Contains(p.model, "claude") || strings.Contains(p.model, "llama-3"):
		return 128000, nil
	default:
		return 128000, nil
	}
}

// ListModels returns available models from OpenRouter API
func (p *OpenRouterProvider) ListModels() ([]api.ModelInfo, error) {
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

	models := make([]api.ModelInfo, len(response.Data))
	for i, model := range response.Data {
		modelInfo := api.ModelInfo{
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

	// Cache the models for future use
	p.models = models
	p.modelsCached = true

	return models, nil
}

// sendRequestWithRetry implements exponential backoff retry logic for rate limits
func (p *OpenRouterProvider) sendRequestWithRetry(httpReq *http.Request, reqBody []byte) (*api.ChatResponse, error) {
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
			var chatResp api.ChatResponse
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
func (p *OpenRouterProvider) calculateMaxTokens(messages []api.Message, tools []api.Tool) int {
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
	// Return default vision model
	return "gpt-5"
}

// SendVisionRequest sends a vision-enabled chat request
func (p *OpenRouterProvider) SendVisionRequest(messages []api.Message, tools []api.Tool, reasoning string) (*api.ChatResponse, error) {
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

// TPS methods - OpenRouter provider doesn't track TPS internally
func (p *OpenRouterProvider) GetLastTPS() float64 {
	return 0.0
}

func (p *OpenRouterProvider) GetAverageTPS() float64 {
	return 0.0
}

func (p *OpenRouterProvider) GetTPSStats() map[string]float64 {
	return map[string]float64{}
}

func (p *OpenRouterProvider) ResetTPSStats() {
	// No-op - this provider doesn't track TPS
}
