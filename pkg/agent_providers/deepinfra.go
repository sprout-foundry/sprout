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

// DeepInfraProvider implements the OpenAI-compatible DeepInfra API
type DeepInfraProvider struct {
	httpClient   *http.Client
	apiToken     string
	debug        bool
	model        string
	models       []types.ModelInfo
	modelsCached bool
}

// NewDeepInfraProvider creates a new DeepInfra provider instance
func NewDeepInfraProvider() (*DeepInfraProvider, error) {
	token := os.Getenv("DEEPINFRA_API_KEY")
	if token == "" {
		return nil, fmt.Errorf("DEEPINFRA_API_KEY environment variable not set")
	}

	return &DeepInfraProvider{
		httpClient: &http.Client{
			Timeout: 300 * time.Second,
		},
		apiToken: token,
		debug:    false,
		model:    "deepseek-ai/DeepSeek-V3.1", // Default DeepInfra model
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
func (p *DeepInfraProvider) SendChatRequest(messages []types.Message, tools []types.Tool, reasoning string) (*types.ChatResponse, error) {
	// Convert messages to OpenAI-compatible format
	deepinfraMessages := make([]map[string]interface{}, len(messages))
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

			deepinfraMessages[i] = map[string]interface{}{
				"role":    msg.Role,
				"content": contentArray,
			}
		} else {
			// Regular text-only message
			deepinfraMessages[i] = map[string]interface{}{
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
		"messages":    deepinfraMessages,
		"max_tokens":  maxTokens,
		"temperature": 0.7,
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

// ListModels returns available models from DeepInfra API
func (p *DeepInfraProvider) ListModels() ([]types.ModelInfo, error) {
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

	models := make([]types.ModelInfo, len(response.Data))
	for i, model := range response.Data {
		modelInfo := types.ModelInfo{
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
func (p *DeepInfraProvider) sendRequestWithRetry(httpReq *http.Request, reqBody []byte) (*types.ChatResponse, error) {
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
			var chatResp types.ChatResponse
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
func (p *DeepInfraProvider) calculateMaxTokens(messages []types.Message, tools []types.Tool) int {
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
	// Return first featured vision model
	featuredVisionModels := p.GetFeaturedVisionModels()
	if len(featuredVisionModels) > 0 {
		return featuredVisionModels[0]
	}
	return ""
}

// SendVisionRequest sends a vision-enabled chat request
func (p *DeepInfraProvider) SendVisionRequest(messages []types.Message, tools []types.Tool, reasoning string) (*types.ChatResponse, error) {
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

func (p *DeepInfraProvider) GetFeaturedModels() []string {
	return []string{
		"Qwen/Qwen3-Coder-480B-A35B-Instruct-Turbo",         // Top coding model
		"deepseek-ai/DeepSeek-V3.1",                         // Latest DeepSeek model
		"meta-llama/Llama-4-Maverick-17B-128E-Instruct-FP8", // Latest Llama with tool support
		"Qwen/Qwen3-235B-A22B-Instruct-2507",                // Large general model
		"deepseek-ai/DeepSeek-V3",                           // DeepSeek V3
		"deepseek-ai/DeepSeek-R1",                           // DeepSeek R1 with longer context
		"meta-llama/Llama-3.2-11B-Vision-Instruct",          // Vision capable Llama
	}
}

func (p *DeepInfraProvider) GetFeaturedVisionModels() []string {
	return []string{
		"meta-llama/Llama-3.2-11B-Vision-Instruct",  // Vision-capable Llama 3.2
		"meta-llama/Llama-4-Scout-17B-16E-Instruct", // Vision-capable Llama 4
	}
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
	case strings.Contains(p.model, "meta-llama/Llama-3"):
		inputCostPerMillion = 0.08
		outputCostPerMillion = 0.08
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
