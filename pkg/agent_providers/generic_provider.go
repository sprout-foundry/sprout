package providers

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/logging"
)

// GenericProvider implements ClientInterface using JSON configuration
type GenericProvider struct {
	config          *ProviderConfig
	httpClient      *http.Client
	streamingClient *http.Client
	debug           bool
	model           string
	models          []api.ModelInfo
	modelsCached    bool
}

// NewGenericProvider creates a new generic provider from configuration
func NewGenericProvider(config *ProviderConfig) (*GenericProvider, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid provider config: %w", err)
	}

	timeout := config.GetTimeout()
	streamingTimeout := config.GetStreamingTimeout()

	return &GenericProvider{
		config: config,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		streamingClient: &http.Client{
			Timeout: streamingTimeout,
		},
		debug: false,
		model: config.Defaults.Model,
	}, nil
}

// SendChatRequest sends a non-streaming chat request
func (p *GenericProvider) SendChatRequest(messages []api.Message, tools []api.Tool, reasoning string) (*api.ChatResponse, error) {
	requestBody, err := p.buildChatRequest(messages, tools, reasoning, false)
	if err != nil {
		return nil, fmt.Errorf("failed to build chat request: %w", err)
	}
	logging.LogRequestPayload(requestBody, p.config.Name, p.model, false)

	req, err := p.buildHTTPRequest(requestBody, false)
	if err != nil {
		return nil, fmt.Errorf("failed to build HTTP request: %w", err)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var response api.ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &response, nil
}

// SendChatRequestStream sends a streaming chat request
func (p *GenericProvider) SendChatRequestStream(messages []api.Message, tools []api.Tool, reasoning string, callback api.StreamCallback) (*api.ChatResponse, error) {
	requestBody, err := p.buildChatRequest(messages, tools, reasoning, true)
	if err != nil {
		return nil, fmt.Errorf("failed to build chat request: %w", err)
	}
	logging.LogRequestPayload(requestBody, p.config.Name, p.model, true)

	req, err := p.buildHTTPRequest(requestBody, true)
	if err != nil {
		return nil, fmt.Errorf("failed to build HTTP request: %w", err)
	}

	resp, err := p.streamingClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	return p.handleStreamingResponse(resp, callback)
}

// CheckConnection tests provider connection
func (p *GenericProvider) CheckConnection() error {
	// Simple health check - try to list models
	_, err := p.ListModels()
	return err
}

// SetDebug enables or disables debug mode
func (p *GenericProvider) SetDebug(debug bool) {
	p.debug = debug
}

// SetModel sets the current model
func (p *GenericProvider) SetModel(model string) error {
	p.model = model
	return nil
}

// GetModel returns the current model
func (p *GenericProvider) GetModel() string {
	return p.model
}

// GetProvider returns the provider name
func (p *GenericProvider) GetProvider() string {
	return p.config.Name
}

// GetModelContextLimit returns the context limit for the current model
func (p *GenericProvider) GetModelContextLimit() (int, error) {
	if p.config.Models.ContextLimit > 0 {
		return p.config.Models.ContextLimit, nil
	}

	// Default context limits based on model
	modelLower := strings.ToLower(p.model)
	switch {
	case strings.Contains(modelLower, "gpt-4"):
		return 128000, nil
	case strings.Contains(modelLower, "gpt-3.5"):
		return 16385, nil
	case strings.Contains(modelLower, "claude-3"):
		return 200000, nil
	case strings.Contains(modelLower, "llama-3"):
		return 128000, nil
	default:
		return 32000, nil // Conservative default
	}
}

// ListModels returns available models
func (p *GenericProvider) ListModels() ([]api.ModelInfo, error) {
	if p.modelsCached && len(p.models) > 0 {
		return p.models, nil
	}

	// If config provides explicit models, use them
	if len(p.config.Models.AvailableModels) > 0 {
		p.models = make([]api.ModelInfo, len(p.config.Models.AvailableModels))
		for i, modelName := range p.config.Models.AvailableModels {
			p.models[i] = api.ModelInfo{
				ID:       modelName,
				Name:     modelName,
				Provider: p.config.Name,
			}
		}
		p.modelsCached = true
		return p.models, nil
	}

	// Try to fetch models from provider API (OpenAI-compatible endpoint)
	modelsEndpoint := strings.TrimSuffix(p.config.Endpoint, "/chat/completions") + "/models"
	req, err := http.NewRequest("GET", modelsEndpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create models request: %w", err)
	}

	token, err := p.config.GetAuthToken()
	if err != nil {
		// For local instances like LM Studio, skip auth if no token is configured
		if strings.Contains(p.config.Endpoint, "127.0.0.1") || strings.Contains(p.config.Endpoint, "localhost") {
			// No auth needed for local instances
		} else {
			return nil, fmt.Errorf("failed to get auth token: %w", err)
		}
	} else {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")

	// Add custom headers
	for key, value := range p.config.Headers {
		req.Header.Set(key, value)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// If models endpoint fails, return just the current model
		p.models = []api.ModelInfo{{
			ID:       p.model,
			Name:     p.model,
			Provider: p.config.Name,
		}}
		p.modelsCached = true
		return p.models, nil
	}

	var modelsResponse struct {
		Data []struct {
			ID            string    `json:"id"`
			Object        string    `json:"object"`
			Created       int64     `json:"created"`
			OwnedBy       string    `json:"owned_by"`
			ContextLength int       `json:"context_length,omitempty"`
			Pricing       *struct {
				Prompt     string  `json:"prompt,omitempty"`
				Completion string  `json:"completion,omitempty"`
			} `json:"pricing,omitempty"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&modelsResponse); err != nil {
		return nil, fmt.Errorf("failed to decode models response: %w", err)
	}

	p.models = make([]api.ModelInfo, len(modelsResponse.Data))
	for i, model := range modelsResponse.Data {
		p.models[i] = api.ModelInfo{
			ID:            model.ID,
			Name:          model.ID,
			Provider:      p.config.Name,
			ContextLength: model.ContextLength,
		}

		// If we have pricing info, populate it
		if model.Pricing != nil {
			if promptCost, err := strconv.ParseFloat(model.Pricing.Prompt, 64); err == nil {
				p.models[i].InputCost = promptCost
			}
			if completionCost, err := strconv.ParseFloat(model.Pricing.Completion, 64); err == nil {
				p.models[i].OutputCost = completionCost
			}
		}
	}
	p.modelsCached = true

	return p.models, nil
}

// SupportsVision returns whether the provider supports vision
func (p *GenericProvider) SupportsVision() bool {
	return p.config.Models.SupportsVision
}

// GetVisionModel returns the vision model
func (p *GenericProvider) GetVisionModel() string {
	if p.config.Models.VisionModel != "" {
		return p.config.Models.VisionModel
	}
	return p.model // Fallback to current model
}

// SendVisionRequest sends a vision request (for providers that support it)
func (p *GenericProvider) SendVisionRequest(messages []api.Message, tools []api.Tool, reasoning string) (*api.ChatResponse, error) {
	if !p.SupportsVision() {
		return nil, fmt.Errorf("provider %s does not support vision", p.config.Name)
	}

	// Use vision model if specified
	visionModel := p.GetVisionModel()
	if visionModel != p.model {
		originalModel := p.model
		p.model = visionModel
		defer func() { p.model = originalModel }()
	}

	return p.SendChatRequest(messages, tools, reasoning)
}

// TPS tracking methods - simplified for now
func (p *GenericProvider) GetLastTPS() float64 {
	return 0.0
}

func (p *GenericProvider) GetAverageTPS() float64 {
	return 0.0
}

func (p *GenericProvider) GetTPSStats() map[string]float64 {
	return map[string]float64{}
}

func (p *GenericProvider) ResetTPSStats() {
	// No-op for now
}

// buildChatRequest builds the request body for chat completion
func (p *GenericProvider) buildChatRequest(messages []api.Message, tools []api.Tool, reasoning string, stream bool) ([]byte, error) {
	// Convert messages according to provider configuration
	convertedMessages := p.convertMessages(messages, reasoning)

	request := map[string]interface{}{
		"model":    p.model,
		"messages": convertedMessages,
		"stream":   stream,
	}

	// Add default parameters
	if p.config.Defaults.Temperature != nil {
		request["temperature"] = *p.config.Defaults.Temperature
	}
	if p.config.Defaults.MaxTokens != nil {
		request["max_tokens"] = *p.config.Defaults.MaxTokens
	}
	if p.config.Defaults.TopP != nil {
		request["top_p"] = *p.config.Defaults.TopP
	}

	// Add tools if provided
	if len(tools) > 0 {
		request["tools"] = tools
	}

	return json.Marshal(request)
}

// convertMessages converts messages according to provider configuration
func (p *GenericProvider) convertMessages(messages []api.Message, reasoning string) []map[string]interface{} {
	converted := make([]map[string]interface{}, len(messages))

	for i, msg := range messages {
		convertedMsg := map[string]interface{}{
			"role":    msg.Role,
			"content": msg.Content,
		}

		// Handle tool call ID inclusion
		if msg.ToolCallId != "" && p.config.Conversion.IncludeToolCallId {
			convertedMsg["tool_call_id"] = msg.ToolCallId
		}

		// Handle tool role conversion
		if msg.Role == "tool" && p.config.Conversion.ConvertToolRoleToUser {
			convertedMsg["role"] = "user"
		}

		// Handle reasoning content
		if reasoning != "" && p.config.Conversion.ReasoningContentField != "" {
			convertedMsg[p.config.Conversion.ReasoningContentField] = reasoning
		}

		// Preserve tool calls if present
		if len(msg.ToolCalls) > 0 {
			convertedMsg["tool_calls"] = p.convertToolCalls(msg.ToolCalls)
		}

		converted[i] = convertedMsg
	}

	return converted
}

func (p *GenericProvider) convertToolCalls(toolCalls []api.ToolCall) interface{} {
	if !p.config.Conversion.ArgumentsAsJSON {
		return toolCalls
	}

	converted := make([]map[string]interface{}, 0, len(toolCalls))
	for _, tc := range toolCalls {
		function := map[string]interface{}{
			"name": tc.Function.Name,
		}

		if tc.Function.Arguments != "" {
			var parsed interface{}
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &parsed); err == nil {
				function["arguments"] = parsed
			} else {
				function["arguments"] = tc.Function.Arguments
			}
		}

		converted = append(converted, map[string]interface{}{
			"id":       tc.ID,
			"type":     tc.Type,
			"function": function,
		})
	}

	return converted
}

// buildHTTPRequest builds the HTTP request
func (p *GenericProvider) buildHTTPRequest(body []byte, streaming bool) (*http.Request, error) {
	req, err := http.NewRequest("POST", p.config.Endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	// Get authentication token
	token, err := p.config.GetAuthToken()
	if err != nil {
		// For local instances like LM Studio, skip auth if no token is configured
		if strings.Contains(p.config.Endpoint, "127.0.0.1") || strings.Contains(p.config.Endpoint, "localhost") {
			// No auth needed for local instances - continue without Authorization header
		} else {
			return nil, fmt.Errorf("authentication failed: %w", err)
		}
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")

	if token != "" {
		switch p.config.Auth.Type {
		case "bearer", "api_key":
			req.Header.Set("Authorization", "Bearer "+token)
		case "basic":
			req.Header.Set("Authorization", "Basic "+token)
		}
	}

	// Add custom headers
	for key, value := range p.config.Headers {
		req.Header.Set(key, value)
	}

	// Add streaming headers
	if streaming {
		switch p.config.Streaming.Format {
		case "sse":
			req.Header.Set("Accept", "text/event-stream")
		case "json_lines":
			req.Header.Set("Accept", "application/jsonl")
		default:
			req.Header.Set("Accept", "text/event-stream")
		}
	}

	return req, nil
}

// handleStreamingResponse processes the streaming response
func (p *GenericProvider) handleStreamingResponse(resp *http.Response, callback api.StreamCallback) (*api.ChatResponse, error) {
	// Process streaming response using shared builder to support tool_calls
	reader := bufio.NewReader(resp.Body)
	builder := api.NewStreamingResponseBuilder(callback)

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("failed to read streaming response: %w", err)
		}

		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		if chunk, err := api.ParseSSEData(data); err == nil && chunk != nil {
			_ = builder.ProcessChunk(chunk)
		}
	}

	// Finalize response from builder
	respObj := builder.GetResponse()
	if respObj == nil {
		// Fallback empty response
		respObj = &api.ChatResponse{Choices: []api.Choice{{}}}
	}
	if respObj.Model == "" {
		respObj.Model = p.model
	}

	return respObj, nil
}
