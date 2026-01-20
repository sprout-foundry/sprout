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

	req, err := p.buildHTTPRequest(requestBody, false)
	if err != nil {
		// Log request on build error
		logging.LogRequestPayloadOnError(requestBody, p.config.Name, p.model, false, "build_http_request", err)
		return nil, fmt.Errorf("failed to build HTTP request: %w", err)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		// Log request on HTTP error
		logging.LogRequestPayloadOnError(requestBody, p.config.Name, p.model, false, "http_request_failed", err)
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		// Log request on API error
		logging.LogRequestPayloadOnError(requestBody, p.config.Name, p.model, false,
			fmt.Sprintf("api_error_%d", resp.StatusCode), fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body)))
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var response api.ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		// Log request on decode error
		logging.LogRequestPayloadOnError(requestBody, p.config.Name, p.model, false, "decode_response", err)
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Success - don't log the request
	return &response, nil
}

// SendChatRequestStream sends a streaming chat request
func (p *GenericProvider) SendChatRequestStream(messages []api.Message, tools []api.Tool, reasoning string, callback api.StreamCallback) (*api.ChatResponse, error) {
	requestBody, err := p.buildChatRequest(messages, tools, reasoning, true)
	if err != nil {
		return nil, fmt.Errorf("failed to build chat request: %w", err)
	}

	req, err := p.buildHTTPRequest(requestBody, true)
	if err != nil {
		// Log request on build error
		logging.LogRequestPayloadOnError(requestBody, p.config.Name, p.model, true, "build_http_request", err)
		return nil, fmt.Errorf("failed to build HTTP request: %w", err)
	}

	resp, err := p.streamingClient.Do(req)
	if err != nil {
		// Log request on HTTP error
		logging.LogRequestPayloadOnError(requestBody, p.config.Name, p.model, true, "http_request_failed", err)
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		// Log request on API error
		logging.LogRequestPayloadOnError(requestBody, p.config.Name, p.model, true,
			fmt.Sprintf("api_error_%d", resp.StatusCode), fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body)))
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	response, err := p.handleStreamingResponse(resp, callback)
	if err != nil {
		// Log request on streaming error
		logging.LogRequestPayloadOnError(requestBody, p.config.Name, p.model, true, "streaming_response", err)
		return nil, err
	}

	// Success - don't log the request
	return response, nil
}

// CheckConnection tests provider connection with current model
func (p *GenericProvider) CheckConnection() error {
	// Send a minimal test request to verify the model works
	testMessages := []api.Message{
		{
			Role:    "user",
			Content: "Hi",
		},
	}

	_, err := p.SendChatRequest(testMessages, nil, "")
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
	return p.config.GetContextLimit(p.model), nil
}

// ListModels returns available models
// Priority:
// 1. Fetch from provider API models endpoint (primary source of truth)
// 2. Enrich endpoint data with config (context_length, tags, name)
// 3. Fall back to config model_info if endpoint fails
// 4. Final fallback: return just current model
func (p *GenericProvider) ListModels() ([]api.ModelInfo, error) {
	if p.modelsCached && len(p.models) > 0 {
		return p.models, nil
	}

	var models []api.ModelInfo

	// Try to fetch models from provider API (OpenAI-compatible endpoint)
	modelsEndpoint := strings.TrimSuffix(p.config.Endpoint, "/chat/completions") + "/models"
	req, err := http.NewRequest("GET", modelsEndpoint, nil)
	if err != nil {
		// Endpoint construction failed, skip to fallback
		return p.fallbackToConfigOrCurrent()
	}

	token, err := p.config.GetAuthToken()
	if err != nil {
		// For local instances like LM Studio, skip auth if no token is configured
		if strings.Contains(p.config.Endpoint, "127.0.0.1") || strings.Contains(p.config.Endpoint, "localhost") {
			// No auth needed for local instances
		} else {
			// Auth failed and not local, skip to fallback
			return p.fallbackToConfigOrCurrent()
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
		// Request failed, skip to fallback
		return p.fallbackToConfigOrCurrent()
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Models endpoint not available or failed, use fallback
		return p.fallbackToConfigOrCurrent()
	}

	var modelsResponse struct {
		Data []struct {
			ID            string `json:"id"`
			Object        string `json:"object"`
			Created       int64  `json:"created"`
			OwnedBy       string `json:"owned_by"`
			ContextLength int    `json:"context_length,omitempty"`
			Pricing       *struct {
				Prompt     string `json:"prompt,omitempty"`
				Completion string `json:"completion,omitempty"`
			} `json:"pricing,omitempty"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&modelsResponse); err != nil {
		return nil, fmt.Errorf("failed to decode models response: %w", err)
	}

	// Build model list from endpoint data, enriched with config
	models = make([]api.ModelInfo, 0, len(modelsResponse.Data))
	for _, model := range modelsResponse.Data {
		modelInfo := api.ModelInfo{
			ID:       model.ID,
			Name:     model.ID,
			Provider: p.config.Name,
		}

		// Use context_length from endpoint if available
		if model.ContextLength > 0 {
			modelInfo.ContextLength = model.ContextLength
		}

		// Enrich with pricing info if available from endpoint
		if model.Pricing != nil {
			if promptCost, err := strconv.ParseFloat(model.Pricing.Prompt, 64); err == nil {
				modelInfo.InputCost = promptCost
			}
			if completionCost, err := strconv.ParseFloat(model.Pricing.Completion, 64); err == nil {
				modelInfo.OutputCost = completionCost
			}
		}

		// Enrich with config model_info data (context_length, tags, name, description)
		if configModelInfo := p.config.GetModelInfo(model.ID); configModelInfo != nil {
			// Only override if endpoint didn't provide context_length
			if modelInfo.ContextLength <= 0 && configModelInfo.ContextLength > 0 {
				modelInfo.ContextLength = configModelInfo.ContextLength
			}
			// Override name/description from config
			if configModelInfo.Name != "" {
				modelInfo.Name = configModelInfo.Name
			}
			if configModelInfo.Description != "" {
				modelInfo.Description = configModelInfo.Description
			}
			// Add tags from config
			if len(configModelInfo.Tags) > 0 {
				modelInfo.Tags = configModelInfo.Tags
			}
		}

		// If still no context_length, use config fallback
		if modelInfo.ContextLength <= 0 {
			modelInfo.ContextLength = p.config.GetContextLimit(model.ID)
		}

		models = append(models, modelInfo)
	}

	// If we got no models from endpoint, use fallback
	if len(models) == 0 {
		return p.fallbackToConfigOrCurrent()
	}

	p.models = models
	p.modelsCached = true
	return p.models, nil
}

// fallbackToConfigOrCurrent returns config model_info or current model as fallback
func (p *GenericProvider) fallbackToConfigOrCurrent() ([]api.ModelInfo, error) {
	// First try to use config model_info
	if len(p.config.Models.ModelInfo) > 0 {
		models := make([]api.ModelInfo, len(p.config.Models.ModelInfo))
		for i, mi := range p.config.Models.ModelInfo {
			models[i] = api.ModelInfo{
				ID:            mi.ID,
				Name:          mi.Name,
				Description:   mi.Description,
				Provider:      p.config.Name,
				ContextLength: mi.ContextLength,
				Tags:          mi.Tags,
			}
		}
		p.models = models
		p.modelsCached = true
		return p.models, nil
	}

	// Next try to use available_models list (legacy)
	if len(p.config.Models.AvailableModels) > 0 {
		models := make([]api.ModelInfo, len(p.config.Models.AvailableModels))
		for i, modelName := range p.config.Models.AvailableModels {
			// Try to enrich with config model_info
			modelInfo := api.ModelInfo{
				ID:       modelName,
				Name:     modelName,
				Provider: p.config.Name,
			}
			if configMi := p.config.GetModelInfo(modelName); configMi != nil {
				if configMi.Name != "" {
					modelInfo.Name = configMi.Name
				}
				if configMi.ContextLength > 0 {
					modelInfo.ContextLength = configMi.ContextLength
				}
				modelInfo.Description = configMi.Description
				modelInfo.Tags = configMi.Tags
			}
			if modelInfo.ContextLength <= 0 {
				modelInfo.ContextLength = p.config.GetContextLimit(modelName)
			}
			models[i] = modelInfo
		}
		p.models = models
		p.modelsCached = true
		return p.models, nil
	}

	// Final fallback: return just the current model
	p.models = []api.ModelInfo{{
		ID:            p.model,
		Name:          p.model,
		Provider:      p.config.Name,
		ContextLength: p.config.GetContextLimit(p.model),
	}}
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

	// Add provider-specific parameters
	if p.config.Defaults.Parameters != nil {
		for key, value := range p.config.Defaults.Parameters {
			request[key] = value
		}
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

		// Preserve reasoning content from previous assistant messages
		// This is critical for models like z-ai/glm-4.7-flash that use reasoning tokens
		if msg.ReasoningContent != "" && p.config.Conversion.ReasoningContentField != "" {
			convertedMsg[p.config.Conversion.ReasoningContentField] = msg.ReasoningContent
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
		// For providers like Minimax that expect arguments as string,
		// ensure the JSON string is properly formatted and escaped
		converted := make([]map[string]interface{}, 0, len(toolCalls))
		for _, tc := range toolCalls {
			// Validate and clean the arguments JSON string
			arguments := tc.Function.Arguments
			if arguments != "" {
				// Try to parse and re-marshal to ensure it's valid JSON
				var parsed interface{}
				if err := json.Unmarshal([]byte(arguments), &parsed); err == nil {
					// Re-marshal to ensure proper formatting and escaping
					if remarshaled, err := json.Marshal(parsed); err == nil {
						arguments = string(remarshaled)
					}
					// If re-marshaling fails, keep original (it was valid)
				} else {
					// If parsing fails, fall back to empty object
					arguments = "{}"
				}
			}

			toolCallType := tc.Type
			// Force tool call type if specified (needed for providers like Mistral)
			if p.config.Conversion.ForceToolCallType != "" {
				toolCallType = p.config.Conversion.ForceToolCallType
			}

			converted = append(converted, map[string]interface{}{
				"id":   tc.ID,
				"type": toolCallType,
				"function": map[string]interface{}{
					"name":      tc.Function.Name,
					"arguments": arguments,
				},
			})
		}
		return converted
	}

	// For providers that expect arguments as JSON object (original behavior)
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

		toolCallType := tc.Type
		// Force tool call type if specified (needed for providers like Mistral)
		if p.config.Conversion.ForceToolCallType != "" {
			toolCallType = p.config.Conversion.ForceToolCallType
		}

		converted = append(converted, map[string]interface{}{
			"id":       tc.ID,
			"type":     toolCallType,
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

	// Check if authentication is needed
	var token string

	// For local instances like LM Studio, skip auth check entirely if it would fail
	isLocalInstance := strings.Contains(p.config.Endpoint, "127.0.0.1") || strings.Contains(p.config.Endpoint, "localhost")

	if isLocalInstance && (p.config.Auth.Type == "bearer" || p.config.Auth.Type == "api_key") &&
		p.config.Auth.EnvVar == "" && p.config.Auth.Key == "" {
		// Local instance with no auth token configured - skip auth entirely
		token = ""
	} else {
		// Get authentication token normally
		var authErr error
		token, authErr = p.config.GetAuthToken()
		if authErr != nil {
			return nil, fmt.Errorf("authentication failed: %w", authErr)
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

	// If the provider didn't send a finish_reason but we received content and the stream
	// ended normally (not due to error), default to "stop" to prevent false incompleteness detection
	// This handles providers like DeepInfra that don't always send finish_reason in streaming mode
	if len(respObj.Choices) > 0 {
		choice := &respObj.Choices[0]
		if choice.FinishReason == "" && choice.Message.Content != "" {
			// Stream ended normally with content but no explicit finish_reason
			// Default to "stop" since the provider completed the response
			choice.FinishReason = "stop"
		}
	}

	return respObj, nil
}
