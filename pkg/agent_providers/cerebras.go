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

// CerebrasProvider implements the OpenAI-compatible Cerebras API
type CerebrasProvider struct {
	httpClient *http.Client
	apiToken   string
	debug      bool
	model      string
}

// NewCerebrasProvider creates a new Cerebras provider instance
func NewCerebrasProvider() (*CerebrasProvider, error) {
	token := os.Getenv("CEREBRAS_API_KEY")
	if token == "" {
		return nil, fmt.Errorf("CEREBRAS_API_KEY environment variable not set")
	}

	return &CerebrasProvider{
		httpClient: &http.Client{
			Timeout: 300 * time.Second,
		},
		apiToken: token,
		debug:    false,
		model:    "qwen-3-235b-a22b-instruct-2507", // Updated default model to a current Cerebras model
	}, nil
}

// NewCerebrasProviderWithModel creates a Cerebras provider with a specific model
func NewCerebrasProviderWithModel(model string) (*CerebrasProvider, error) {
	provider, err := NewCerebrasProvider()
	if err != nil {
		return nil, err
	}
	provider.model = model
	return provider, nil
}

// SendChatRequest sends a chat completion request to Cerebras
func (p *CerebrasProvider) SendChatRequest(messages []types.Message, tools []types.Tool, reasoning string) (*types.ChatResponse, error) {
	// Convert messages to Cerebras format
	cerebrasMessages := make([]map[string]interface{}, len(messages))
	for i, msg := range messages {
		cerebrasMessages[i] = map[string]interface{}{
			"role":    msg.Role,
			"content": msg.Content,
		}
	}

	// Calculate appropriate max_tokens based on context limits
	maxTokens := p.calculateMaxTokens(messages, tools)

	// Build request payload
	requestBody := map[string]interface{}{
		"model":       p.model,
		"messages":    cerebrasMessages,
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

	httpReq, err := http.NewRequest("POST", "https://api.cerebras.ai/v1/chat/completions", bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiToken)

	// Log the model for debugging if debug is enabled
	if p.debug {
		fmt.Printf("🔍 Using Cerebras model: %s\n", p.model)
	}
	if p.debug {
		fmt.Printf("🔍 Cerebras Request URL: %s\n", "https://api.cerebras.ai/v1/chat/completions")
		fmt.Printf("🔍 Cerebras Request Body: %s\n", string(reqBody))
	}

	return p.sendRequestWithRetry(httpReq, reqBody)
}

// CheckConnection checks if the Cerebras connection is valid
func (p *CerebrasProvider) CheckConnection() error {
	if p.apiToken == "" {
		return fmt.Errorf("CEREBRAS_API_KEY environment variable not set")
	}
	return nil
}

// SetDebug enables or disables debug mode
func (p *CerebrasProvider) SetDebug(debug bool) {
	p.debug = debug
}

// SetModel sets the model to use
func (p *CerebrasProvider) SetModel(model string) error {
	p.model = model
	return nil
}

// GetModel returns the current model
func (p *CerebrasProvider) GetModel() string {
	return p.model
}

// GetProvider returns the provider name
func (p *CerebrasProvider) GetProvider() string {
	return "cerebras"
}

// ListModels returns the currently available Cerebras models
func (p *CerebrasProvider) ListModels() ([]types.ModelInfo, error) {
	// Make request to list models endpoint
	httpReq, err := http.NewRequest("GET", "https://api.cerebras.ai/v1/models", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+p.apiToken)

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to list models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to list models, status %d: %s", resp.StatusCode, string(body))
	}

	// Parse the response
	var result struct {
		Data []struct {
			ID      string `json:"id"`
			Object  string `json:"object"`
			Created int    `json:"created"`
			Owner   string `json:"owned_by"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Convert to ModelInfo format
	models := make([]types.ModelInfo, len(result.Data))
	for i, model := range result.Data {
		models[i] = types.ModelInfo{
			ID:       model.ID,
			Name:     model.ID,
			Provider: "cerebras",
		}
	}

	return models, nil
}

// GetModelContextLimit returns the context limit for the current model
func (p *CerebrasProvider) GetModelContextLimit() (int, error) {
	model := p.model

	// Cerebras model context limits based on actual available models
	switch {
	case strings.Contains(model, "qwen-3-235b"):
		return 32768, nil // Qwen models support 32K context
	case strings.Contains(model, "qwen-3-coder-480b"):
		return 32768, nil // Qwen Coder model supports 32K context
	case strings.Contains(model, "llama3.1-8b"):
		return 8000, nil // Llama models support 8K context
	case strings.Contains(model, "llama-3.3-70b"):
		return 8000, nil // Llama models support 8K context
	case strings.Contains(model, "llama-4-"):
		return 32768, nil // Llama 4 models support 32K context
	case strings.Contains(model, "gpt-oss-120b"):
		return 32768, nil // GPT OSS model supports 32K context
	default:
		return 8000, nil // Conservative default for other models
	}
}

// sendRequestWithRetry implements exponential backoff retry logic for rate limits
func (p *CerebrasProvider) sendRequestWithRetry(httpReq *http.Request, reqBody []byte) (*types.ChatResponse, error) {
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
			fmt.Printf("🔍 Cerebras Response Status (attempt %d): %s\n", attempt+1, resp.Status)
			fmt.Printf("🔍 Cerebras Response Body: %s\n", string(respBody))
		}

		// Success case
		if resp.StatusCode == http.StatusOK {
			var chatResp types.ChatResponse
			if err := json.Unmarshal(respBody, &chatResp); err != nil {
				return nil, fmt.Errorf("failed to unmarshal response: %w", err)
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

// calculateBackoffDelay calculates the delay for exponential backoff
func (p *CerebrasProvider) calculateBackoffDelay(resp *http.Response, attempt int, baseDelay time.Duration) time.Duration {
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

// calculateMaxTokens calculates appropriate max_tokens based on input size and model limits
func (p *CerebrasProvider) calculateMaxTokens(messages []types.Message, tools []types.Tool) int {
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

// SupportsVision checks if the current model supports vision
func (p *CerebrasProvider) SupportsVision() bool {
	// Cerebras models are currently text-only
	return false
}

// SendVisionRequest sends a vision-enabled chat request
func (p *CerebrasProvider) SendVisionRequest(messages []types.Message, tools []types.Tool, reasoning string) (*types.ChatResponse, error) {
	// Cerebras doesn't support vision, so just send regular chat request
	return p.SendChatRequest(messages, tools, reasoning)
}

func (p *CerebrasProvider) GetFeaturedModels() []string {
	return []string{
		"qwen-3-480b",      // Best for coding (480B parameter model)
		"qwen-3-235b-2507", // Large general model with 2507 variant
	}
}

func (p *CerebrasProvider) GetFeaturedVisionModels() []string {
	// Cerebras models are currently text-only
	// Vision capabilities may be added in future updates
	return []string{}
}
