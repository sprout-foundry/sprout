package providers

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	api "github.com/alantheprice/ledit/pkg/agent_api"
)

// LMStudioProvider implements the OpenAI-compatible LM Studio API
// LM Studio runs locally on http://127.0.0.1:1234/v1/
// Models endpoint: http://127.0.0.1:1234/v1/models
// Chat completions: http://127.0.0.1:1234/v1/chat/completions
type LMStudioProvider struct {
	httpClient      *http.Client
	streamingClient *http.Client
	apiToken        string // Usually empty for local LM Studio
	debug           bool
	model           string
	models          []api.ModelInfo
	modelsCached    bool
	baseURL         string
}

// NewLMStudioProvider creates a new LM Studio provider instance
func NewLMStudioProvider() (*LMStudioProvider, error) {
	// LM Studio typically runs on localhost:1234
	baseURL := os.Getenv("LMSTUDIO_BASE_URL")
	if baseURL == "" {
		baseURL = "http://127.0.0.1:1234/v1"
	}

	timeout := 320 * time.Second

	return &LMStudioProvider{
		httpClient: &http.Client{
			Timeout: timeout,
		},
		streamingClient: &http.Client{
			Timeout: 900 * time.Second, // 15 minutes for streaming requests
		},
		apiToken: "", // LM Studio typically doesn't require authentication
		debug:    false,
		model:    "", // Will be set by user or default to first available model
		baseURL:  baseURL,
	}, nil
}

// NewLMStudioProviderWithModel creates an LM Studio provider with a specific model
func NewLMStudioProviderWithModel(model string) (*LMStudioProvider, error) {
	provider, err := NewLMStudioProvider()
	if err != nil {
		return nil, err
	}
	provider.model = model
	return provider, nil
}

// SendChatRequest sends a chat completion request to LM Studio
func (p *LMStudioProvider) SendChatRequest(messages []api.Message, tools []api.Tool, reasoning string) (*api.ChatResponse, error) {
	messageOpts := MessageConversionOptions{ConvertToolRoleToUser: false}
	lmstudioMessages := BuildOpenAIChatMessages(messages, messageOpts)

	// Build request payload
	requestBody := map[string]interface{}{
		"model":       p.model,
		"messages":    lmstudioMessages,
		"max_tokens":  -1, // Let LM Studio decide
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

	p.writeDebugArtifacts("lmstudio_request", reqBody, false)

	httpReq, err := http.NewRequest("POST", p.baseURL+"/chat/completions", bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if p.apiToken != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiToken)
	}

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request to LM Studio: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("LM Studio API error (status %d): %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var chatResp api.ChatResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return nil, fmt.Errorf("failed to decode LM Studio response: %w", err)
	}

	return &chatResp, nil
}

// SendChatRequestStream sends a streaming chat completion request to LM Studio
func (p *LMStudioProvider) SendChatRequestStream(messages []api.Message, tools []api.Tool, reasoning string, callback api.StreamCallback) (*api.ChatResponse, error) {
	messageOpts := MessageConversionOptions{ConvertToolRoleToUser: false}
	lmstudioMessages := BuildOpenAIStreamingMessages(messages, messageOpts)

	contextLimit, err := p.GetModelContextLimit()
	if err != nil {
		contextLimit = p.getKnownModelContextLimit()
	}
	maxTokens := CalculateMaxTokens(contextLimit, messages, tools)

	// Build request payload
	requestBody := map[string]interface{}{
		"model":       p.model,
		"messages":    lmstudioMessages,
		"max_tokens":  maxTokens,
		"temperature": 0.7,
		"stream":      true,
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

	p.writeDebugArtifacts("lmstudio_request_stream", reqBody, true)

	httpReq, err := http.NewRequest("POST", p.baseURL+"/chat/completions", bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("Cache-Control", "no-cache")
	if p.apiToken != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiToken)
	}

	resp, err := p.streamingClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send streaming request to LM Studio: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("LM Studio streaming API error (status %d): %s", resp.StatusCode, string(body))
	}

	// Process streaming response
	reader := bufio.NewReader(resp.Body)
	var fullContent strings.Builder
	var chatResp api.ChatResponse

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("failed to read streaming response: %w", err)
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var streamResp struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}

		if err := json.Unmarshal([]byte(data), &streamResp); err != nil {
			continue // Skip malformed events
		}

		if len(streamResp.Choices) > 0 && streamResp.Choices[0].Delta.Content != "" {
			content := streamResp.Choices[0].Delta.Content
			fullContent.WriteString(content)
			callback(content)
		}
	}

	// Create final response
	chatResp = api.ChatResponse{
		ID:      "lmstudio-stream-" + strconv.FormatInt(time.Now().Unix(), 10),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   p.model,
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
					Role:    "assistant",
					Content: fullContent.String(),
				},
				FinishReason: "stop",
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
			PromptTokens:     EstimateInputTokens(messages, tools),
			CompletionTokens: len(fullContent.String()) / 4,
			TotalTokens:      EstimateInputTokens(messages, tools) + len(fullContent.String())/4,
			EstimatedCost:    0.0, // Local provider, no cost
		},
	}

	return &chatResp, nil
}

// CheckConnection verifies that LM Studio is accessible
func (p *LMStudioProvider) CheckConnection() error {
	// Try to list models to verify connection
	_, err := p.ListModels()
	if err != nil {
		return fmt.Errorf("failed to connect to LM Studio: %w", err)
	}
	return nil
}

// SetDebug enables or disables debug logging
func (p *LMStudioProvider) SetDebug(debug bool) {
	p.debug = debug
}

// SetModel sets the model to use
func (p *LMStudioProvider) SetModel(model string) error {
	p.model = model
	return nil
}

// GetModel returns the current model
func (p *LMStudioProvider) GetModel() string {
	return p.model
}

// GetProvider returns the provider name
func (p *LMStudioProvider) GetProvider() string {
	return "lmstudio"
}

// GetModelContextLimit returns the context limit for the current model
func (p *LMStudioProvider) GetModelContextLimit() (int, error) {
	// First, try to get the actual context length from the models API
	if p.model != "" {
		models, err := p.ListModels()
		if err == nil {
			for _, model := range models {
				if model.ID == p.model || model.Name == p.model {
					if model.ContextLength > 0 {
						return model.ContextLength, nil
					}
				}
			}
		}
	}

	// If that fails, try to get context limit from known models
	if limit := p.getKnownModelContextLimit(); limit > 0 {
		return limit, nil
	}

	// Fallback to a larger default context length (32k)
	return 32768, nil
}

// getKnownModelContextLimit returns context limit for known models
func (p *LMStudioProvider) getKnownModelContextLimit() int {
	// Common LM Studio model context limits
	switch {
	case strings.Contains(strings.ToLower(p.model), "128k"):
		return 131072
	case strings.Contains(strings.ToLower(p.model), "64k"):
		return 65536
	case strings.Contains(strings.ToLower(p.model), "32k"):
		return 32768
	case strings.Contains(strings.ToLower(p.model), "16k"):
		return 16384
	case strings.Contains(strings.ToLower(p.model), "8k"):
		return 8192
	default:
		return 32768 // Default to 32k context for modern models
	}
}

// SupportsVision indicates if the provider supports vision
func (p *LMStudioProvider) SupportsVision() bool {
	// LM Studio models vary in vision support
	// Check if model name suggests vision capability
	return strings.Contains(strings.ToLower(p.model), "vision") ||
		strings.Contains(strings.ToLower(p.model), "vl") ||
		strings.Contains(strings.ToLower(p.model), "multimodal")
}

// GetVisionModel returns the vision model to use
func (p *LMStudioProvider) GetVisionModel() string {
	if p.SupportsVision() {
		return p.model
	}
	return ""
}

// SendVisionRequest sends a vision request to LM Studio
func (p *LMStudioProvider) SendVisionRequest(messages []api.Message, tools []api.Tool, reasoning string) (*api.ChatResponse, error) {
	if !p.SupportsVision() {
		return nil, fmt.Errorf("vision not supported by model %s", p.model)
	}
	// Vision requests use the same endpoint as chat completions
	return p.SendChatRequest(messages, tools, reasoning)
}

// ListModels returns available models from LM Studio
func (p *LMStudioProvider) ListModels() ([]api.ModelInfo, error) {
	if p.modelsCached && len(p.models) > 0 {
		return p.models, nil
	}

	httpReq, err := http.NewRequest("GET", p.baseURL+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create models request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if p.apiToken != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiToken)
	}

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch LM Studio models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("LM Studio models API error (status %d): %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read models response body: %w", err)
	}

	var modelsResp struct {
		Data []struct {
			ID            string `json:"id"`
			Name          string `json:"name"`
			Description   string `json:"description"`
			ContextLength int    `json:"context_length"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &modelsResp); err != nil {
		return nil, fmt.Errorf("failed to decode LM Studio models: %w", err)
	}

	// Convert to ModelInfo format
	models := make([]api.ModelInfo, 0, len(modelsResp.Data))
	for _, model := range modelsResp.Data {
		modelInfo := api.ModelInfo{
			ID:            model.ID,
			Name:          model.Name,
			Description:   model.Description,
			Provider:      "lmstudio",
			ContextLength: model.ContextLength,
		}
		models = append(models, modelInfo)
	}

	p.models = models
	p.modelsCached = true

	return models, nil
}

// GetLastTPS returns the last tokens per second measurement
func (p *LMStudioProvider) GetLastTPS() float64 {
	return 0.0 // LM Studio doesn't provide TPS metrics
}

// GetAverageTPS returns the average tokens per second
func (p *LMStudioProvider) GetAverageTPS() float64 {
	return 0.0 // LM Studio doesn't provide TPS metrics
}

// GetTPSStats returns TPS statistics
func (p *LMStudioProvider) GetTPSStats() map[string]float64 {
	return map[string]float64{} // LM Studio doesn't provide TPS metrics
}

// ResetTPSStats resets TPS statistics
func (p *LMStudioProvider) ResetTPSStats() {
	// No-op for LM Studio
}

// writeDebugArtifacts writes the request JSON and a ready-to-run curl
// command to local files for quick reproduction. Files are placed in the
// current working directory and overwritten each call.
func (p *LMStudioProvider) writeDebugArtifacts(filenameBase string, body []byte, streaming bool) {
	// Only when debug is enabled to avoid leaking data unintentionally
	if !p.debug {
		return
	}
	jsonName := filenameBase + ".json"
	curlName := filenameBase + ".curl"
	_ = os.WriteFile(jsonName, body, 0644)

	// Build curl using env var for auth
	var headers []string
	if p.apiToken != "" {
		headers = append(headers, "-H \"Authorization: Bearer $LMSTUDIO_API_KEY\"")
	}
	headers = append(headers, "-H 'Content-Type: application/json'")
	if streaming {
		headers = append(headers, "-H 'Accept: text/event-stream'")
	}

	curlCmd := "curl -X POST " + p.baseURL + "/chat/completions " + strings.Join(headers, " ") + " -d @" + jsonName

	_ = os.WriteFile(curlName, []byte(curlCmd), 0644)
}
