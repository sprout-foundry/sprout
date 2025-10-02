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
	"github.com/alantheprice/ledit/pkg/utils"
)

// DeepInfraProvider implements the OpenAI-compatible DeepInfra API
type DeepInfraProvider struct {
	httpClient      *http.Client
	streamingClient *http.Client
	apiToken        string
	debug           bool
	model           string
	models          []api.ModelInfo
	modelsCached    bool
}

// NewDeepInfraProvider creates a new DeepInfra provider instance
func NewDeepInfraProvider() (*DeepInfraProvider, error) {
	token := os.Getenv("DEEPINFRA_API_KEY")
	if token == "" {
		return nil, fmt.Errorf("DEEPINFRA_API_KEY environment variable not set")
	}

	timeout := 120 * time.Second

	return &DeepInfraProvider{
		httpClient: &http.Client{
			Timeout: timeout,
		},
		streamingClient: &http.Client{
			Timeout: 900 * time.Second, // 15 minutes for streaming requests
		},
		apiToken: token,
		debug:    false,
		model:    "meta-llama/Llama-3.3-70B-Instruct", // Default DeepInfra model (matches config.go defaults)
	}, nil
}

// NewDeepInfraProviderWithModel creates a DeepInfra provider with a specific model
func NewDeepInfraProviderWithModel(model string) (*DeepInfraProvider, error) {
	provider, err := NewDeepInfraProvider()
	if err != nil {
		return nil, err
	}
	provider.model = model
	return provider, nil
}

// SendChatRequest sends a chat completion request to DeepInfra
func (p *DeepInfraProvider) SendChatRequest(messages []api.Message, tools []api.Tool, reasoning string) (*api.ChatResponse, error) {
	messageOpts := MessageConversionOptions{ConvertToolRoleToUser: true}
	deepinfraMessages := BuildOpenAIChatMessages(messages, messageOpts)

	contextLimit, err := p.GetModelContextLimit()
	if err != nil {
		contextLimit = p.getKnownModelContextLimit()
	}
	maxTokens := CalculateMaxTokens(contextLimit, messages, tools)

	// Build request payload
	requestBody := map[string]interface{}{
		"model":       p.model,
		"messages":    deepinfraMessages,
		"max_tokens":  maxTokens,
		"temperature": 0.7,
	}

	// Add tools if provided
	if openAITools := BuildOpenAIToolsPayload(tools); openAITools != nil {
		requestBody["tools"] = openAITools
		requestBody["tool_choice"] = "auto"
	}

	reqBody, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequest("POST", "https://api.deepinfra.com/v1/openai/chat/completions", bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiToken)

	// Log the model for debugging if debug is enabled
	if p.debug {
		fmt.Printf("🔍 Using DeepInfra model: %s\n", p.model)
	}
	if p.debug {
		fmt.Printf("🔍 DeepInfra Request URL: %s\n", "https://api.deepinfra.com/v1/openai/chat/completions")
		fmt.Printf("🔍 DeepInfra Request Body: %s\n", string(reqBody))
	}

	return p.sendRequestWithRetry(httpReq, reqBody)
}

// SendChatRequestStream sends a streaming chat request to DeepInfra
func (p *DeepInfraProvider) SendChatRequestStream(messages []api.Message, tools []api.Tool, reasoning string, callback api.StreamCallback) (*api.ChatResponse, error) {
	url := "https://api.deepinfra.com/v1/openai/chat/completions"

	openAIMessages := BuildOpenAIStreamingMessages(messages, MessageConversionOptions{ConvertToolRoleToUser: true})

	reqBody := map[string]interface{}{
		"model":       p.model,
		"messages":    openAIMessages,
		"temperature": 0.7,
		"stream":      true, // Enable streaming
	}

	// Add tools if present
	// NOTE: Some DeepInfra models may not support tools in streaming mode
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

		if p.debug {
			fmt.Printf("🔍 DeepInfra: Sending %d tools with streaming request\n", len(tools))
		}
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

	if p.debug {
		fmt.Printf("🔍 DeepInfra Streaming Request URL: %s\n", url)
		fmt.Printf("🔍 DeepInfra Streaming Request Body: %s\n", string(reqBodyBytes))
	}

	resp, err := p.streamingClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		if p.debug {
			fmt.Printf("🔍 DeepInfra Error Response (status %d): %s\n", resp.StatusCode, string(body))
		}
		return nil, fmt.Errorf("DeepInfra API error (status %d): %s", resp.StatusCode, string(body))
	}

	// Process SSE stream
	scanner := bufio.NewScanner(resp.Body)
	var content strings.Builder
	var toolCalls []api.ToolCall
	var toolCallsMap = make(map[string]*api.ToolCall) // Track tool calls by ID for proper accumulation
	var finishReason string
	var usage struct {
		PromptTokens     int     `json:"prompt_tokens"`
		CompletionTokens int     `json:"completion_tokens"`
		TotalTokens      int     `json:"total_tokens"`
		EstimatedCost    float64 `json:"estimated_cost"`
	}

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

			// Extract usage information (DeepInfra sends this in the final chunk)
			if usageData, ok := chunk["usage"].(map[string]interface{}); ok && usageData != nil {
				if promptTokens, ok := usageData["prompt_tokens"].(float64); ok {
					usage.PromptTokens = int(promptTokens)
				}
				if completionTokens, ok := usageData["completion_tokens"].(float64); ok {
					usage.CompletionTokens = int(completionTokens)
				}
				if totalTokens, ok := usageData["total_tokens"].(float64); ok {
					usage.TotalTokens = int(totalTokens)
				}
				// Extract DeepInfra's estimated cost
				if cost, ok := usageData["estimated_cost"].(float64); ok {
					usage.EstimatedCost = cost
				}
				if p.debug {
					fmt.Printf("🔍 DeepInfra Streaming Usage: prompt=%d, completion=%d, total=%d, cost=%f\n",
						usage.PromptTokens, usage.CompletionTokens, usage.TotalTokens, usage.EstimatedCost)
				}
			}

			// Extract choices
			if choices, ok := chunk["choices"].([]interface{}); ok && len(choices) > 0 {
				if choice, ok := choices[0].(map[string]interface{}); ok {
					// Extract delta content
					if delta, ok := choice["delta"].(map[string]interface{}); ok {
						if contentChunk, ok := delta["content"].(string); ok && contentChunk != "" {
							// Debug logging for duplicate detection
							if p.debug && strings.Contains(content.String(), contentChunk) && len(contentChunk) > 10 {
								fmt.Printf("🔍 DeepInfra: Possible duplicate chunk detected: %q\n", contentChunk)
							}

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
							// Signal activity for streaming timeout even when only tool_calls are present
							if callback != nil {
								callback("") // empty chunk: updates watchdog without printing
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

	// Convert accumulated tool calls map to slice
	for _, tc := range toolCallsMap {
		// Validate tool call has required fields
		if tc.Function.Name != "" {
			// Validate JSON arguments if present
			if tc.Function.Arguments != "" {
				var argsTest interface{}
				if err := json.Unmarshal([]byte(tc.Function.Arguments), &argsTest); err != nil {
					if p.debug {
						fmt.Printf("🔍 DeepInfra: Invalid tool call JSON arguments for %s: %s\n", tc.Function.Name, tc.Function.Arguments)
					}
					continue // Skip malformed tool calls
				}
			}
			toolCalls = append(toolCalls, *tc)
		}
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
			PromptTokens:     usage.PromptTokens,
			CompletionTokens: usage.CompletionTokens,
			TotalTokens:      usage.TotalTokens,
			EstimatedCost:    usage.EstimatedCost,
		},
	}

	return response, nil
}

// CheckConnection checks if the DeepInfra connection is valid
func (p *DeepInfraProvider) CheckConnection() error {
	if p.apiToken == "" {
		return fmt.Errorf("DEEPINFRA_API_KEY environment variable not set")
	}
	return nil
}

// SetDebug enables or disables debug mode
func (p *DeepInfraProvider) SetDebug(debug bool) {
	p.debug = debug
}

// SetModel sets the model to use
func (p *DeepInfraProvider) SetModel(model string) error {
	p.model = model
	return nil
}

// GetModel returns the current model
func (p *DeepInfraProvider) GetModel() string {
	return p.model
}

// GetProvider returns the provider name
func (p *DeepInfraProvider) GetProvider() string {
	return "deepinfra"
}

// GetModelContextLimit returns the context limit for the current model
func (p *DeepInfraProvider) GetModelContextLimit() (int, error) {
	if !p.modelsCached {
		if _, err := p.ListModels(); err != nil {
			// If we can't fetch models, return known defaults for common models
			return p.getKnownModelContextLimit(), nil
		}
	}

	for _, m := range p.models {
		if m.ID == p.model {
			if m.ContextLength > 0 {
				return m.ContextLength, nil
			}
		}
	}

	// Fallback to known model context limits
	return p.getKnownModelContextLimit(), nil
}

// getKnownModelContextLimit returns known context limits for common DeepInfra models
func (p *DeepInfraProvider) getKnownModelContextLimit() int {
	switch {
	case strings.Contains(p.model, "deepseek-ai/DeepSeek-V3"):
		return 65536 // 64K context
	case strings.Contains(p.model, "deepseek-ai/DeepSeek-R1"):
		return 65536 // 64K context
	case strings.Contains(p.model, "meta-llama/Llama-3.3-70B"):
		return 131072 // 128K context
	case strings.Contains(p.model, "meta-llama/Llama-4"):
		return 131072 // 128K context
	case strings.Contains(p.model, "Qwen/Qwen3-Coder"):
		return 131072 // 128K context
	case strings.Contains(p.model, "llama"):
		return 131072 // Most Llama models have 128K
	case strings.Contains(p.model, "deepseek"):
		return 65536 // Most DeepSeek models have 64K
	case strings.Contains(p.model, "qwen"):
		return 131072 // Most Qwen models have 128K
	default:
		return 32768 // Conservative default
	}
}

// ListModels returns available models from DeepInfra API
func (p *DeepInfraProvider) ListModels() ([]api.ModelInfo, error) {
	if p.modelsCached {
		return p.models, nil
	}

	httpReq, err := http.NewRequest("GET", "https://api.deepinfra.com/v1/openai/models", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+p.apiToken)

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to get models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		if p.debug {
			fmt.Printf("🔍 DeepInfra Error Response (status %d): %s\n", resp.StatusCode, string(body))
		}
		return nil, fmt.Errorf("DeepInfra API error (status %d): %s", resp.StatusCode, string(body))
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
			Provider: "deepinfra",
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
				// DeepInfra pricing is per token, not per million tokens
				// Convert to per million tokens for consistency with other providers
				modelInfo.InputCost = promptCost * 1000000
			}
			if completionCost, err := strconv.ParseFloat(model.Pricing.Completion, 64); err == nil {
				// DeepInfra pricing is per token, not per million tokens
				// Convert to per million tokens for consistency with other providers
				modelInfo.OutputCost = completionCost * 1000000
			}
			if modelInfo.InputCost > 0 || modelInfo.OutputCost > 0 {
				modelInfo.Cost = (modelInfo.InputCost + modelInfo.OutputCost) / 2.0
			}
		}

		models[i] = modelInfo
	}

	p.models = models
	p.modelsCached = true
	return models, nil
}

// sendRequestWithRetry implements exponential backoff retry logic for rate limits
func (p *DeepInfraProvider) sendRequestWithRetry(httpReq *http.Request, reqBody []byte) (*api.ChatResponse, error) {
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
			fmt.Printf("🔍 DeepInfra Response Status (attempt %d): %s\n", attempt+1, resp.Status)
			fmt.Printf("🔍 DeepInfra Response Body: %s\n", string(respBody))
		}

		// Success case
		if resp.StatusCode == http.StatusOK {
			var chatResp api.ChatResponse
			if err := json.Unmarshal(respBody, &chatResp); err != nil {
				return nil, fmt.Errorf("failed to unmarshal response: %w", err)
			}

			// Calculate cost for the response
			if chatResp.Usage.PromptTokens > 0 || chatResp.Usage.CompletionTokens > 0 {
				chatResp.Usage.EstimatedCost = p.calculateCost(chatResp.Usage.PromptTokens, chatResp.Usage.CompletionTokens)
			}

			return &chatResp, nil
		}

		// Handle error cases
		if resp.StatusCode == 429 {
			// Parse the error response to check for rate limits
			var errorResp map[string]interface{}
			if err := json.Unmarshal(respBody, &errorResp); err == nil {
				if errorObj, ok := errorResp["error"].(map[string]interface{}); ok {
					if _, ok := errorObj["message"].(string); ok {
						// For rate limits, implement backoff
						if attempt < maxRetries {
							// Check for rate limit headers to get reset time
							waitTime := p.calculateBackoffDelay(resp, attempt, baseDelay)
							message := fmt.Sprintf("⏳ Rate limit hit (attempt %d/%d), waiting %v before retry...",
								attempt+1, maxRetries+1, waitTime)
							if logger := utils.GetLogger(false); logger != nil {
								logger.LogProcessStep(message)
							} else {
								fmt.Println(message)
							}
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

// calculateBackoffDelay calculates the delay for exponential backoff
func (p *DeepInfraProvider) calculateBackoffDelay(resp *http.Response, attempt int, baseDelay time.Duration) time.Duration {
	// Fallback to exponential backoff
	delay := baseDelay * time.Duration(math.Pow(2, float64(attempt)))
	// Cap at 60 seconds
	if delay > 60*time.Second {
		delay = 60 * time.Second
	}
	return delay
}

// SupportsVision checks if the current model supports vision
func (p *DeepInfraProvider) SupportsVision() bool {
	// Check if we have a vision model available
	visionModel := p.GetVisionModel()
	return visionModel != ""
}

// GetVisionModel returns the vision model for DeepInfra
func (p *DeepInfraProvider) GetVisionModel() string {
	// DeepInfra supports vision models like deepseek-reasoner-v2
	return "deepseek-reasoner-v2"
}

// SendVisionRequest sends a vision-enabled chat request
func (p *DeepInfraProvider) SendVisionRequest(messages []api.Message, tools []api.Tool, reasoning string) (*api.ChatResponse, error) {
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
func (p *DeepInfraProvider) calculateCost(promptTokens, completionTokens int) float64 {
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

	// Fallback pricing for common models if not found in models list
	// These are approximate prices per million tokens based on DeepInfra's pricing
	var inputCostPerMillion, outputCostPerMillion float64

	switch {
	case strings.Contains(p.model, "deepseek-ai/DeepSeek-V3"):
		inputCostPerMillion = 0.27
		outputCostPerMillion = 1.10
	case strings.Contains(p.model, "deepseek-ai/DeepSeek-R1"):
		inputCostPerMillion = 0.55
		outputCostPerMillion = 2.19
	case strings.Contains(p.model, "Qwen/Qwen3-Coder-480B"):
		inputCostPerMillion = 1.62
		outputCostPerMillion = 1.62
	case strings.Contains(p.model, "meta-llama/Llama-4"):
		inputCostPerMillion = 0.35
		outputCostPerMillion = 0.40
	default:
		// Unknown model - return 0 (will show as $0.000 in footer)
		return 0
	}

	inputCost := float64(promptTokens) * inputCostPerMillion / 1000000.0
	outputCost := float64(completionTokens) * outputCostPerMillion / 1000000.0
	return inputCost + outputCost
}

// TPS methods - DeepInfra provider doesn't track TPS internally
func (p *DeepInfraProvider) GetLastTPS() float64 {
	return 0.0
}

func (p *DeepInfraProvider) GetAverageTPS() float64 {
	return 0.0
}

func (p *DeepInfraProvider) GetTPSStats() map[string]float64 {
	return map[string]float64{}
}

func (p *DeepInfraProvider) ResetTPSStats() {
	// No-op - this provider doesn't track T极S
}
