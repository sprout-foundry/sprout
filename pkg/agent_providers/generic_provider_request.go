package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	modelsettings "github.com/sprout-foundry/sprout/pkg/model_settings"
)

// buildChatRequest builds the request body for chat completion
func (p *GenericProvider) buildChatRequest(messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool, stream bool) ([]byte, error) {
	if err := p.ensureModel(); err != nil {
		return nil, fmt.Errorf("ensure model: %w", err)
	}

	// Convert messages according to provider configuration
	convertedMessages := p.convertMessages(messages, reasoning)

	// Defense in depth: strip any leading assistant messages that might conflict
	// with thinking mode. This catches edge cases where the message preparation
	// didn't fully strip prefill messages before reaching the provider layer.
	// We only need to do this when thinking is NOT disabled (i.e., thinking is enabled).
	if !disableThinking && len(convertedMessages) > 1 {
		// Find first non-system message index
		nonSystemStart := 0
		for nonSystemStart < len(convertedMessages) && convertedMessages[nonSystemStart]["role"] == "system" {
			nonSystemStart++
		}
		if nonSystemStart < len(convertedMessages) && convertedMessages[nonSystemStart]["role"] == "assistant" {
			// Count leading assistant messages to strip (without tool_calls)
			stripEnd := nonSystemStart
			for stripEnd < len(convertedMessages) && convertedMessages[stripEnd]["role"] == "assistant" {
				if tc, hasToolCalls := convertedMessages[stripEnd]["tool_calls"]; hasToolCalls && tc != nil {					break // Preserve assistant messages with tool_calls
				}
				stripEnd++
			}
			if stripEnd > nonSystemStart {
				// Keep system messages + everything after the stripped assistant prefills
				system := convertedMessages[:nonSystemStart]
				rest := convertedMessages[stripEnd:]
				newMessages := make([]map[string]interface{}, 0, len(system)+len(rest))
				newMessages = append(newMessages, system...)
				newMessages = append(newMessages, rest...)
				convertedMessages = newMessages
			}
		}
	}

	request := map[string]interface{}{
		"model":    p.model,
		"messages": convertedMessages,
		"stream":   stream,
	}

	// Add default parameters
	if p.config.Defaults.Temperature != nil {
		request["temperature"] = *p.config.Defaults.Temperature
	}
	if p.config.Defaults.MaxTokens != nil && *p.config.Defaults.MaxTokens > 0 {
		request["max_tokens"] = *p.config.Defaults.MaxTokens
	} else {
		contextLimit, _ := p.GetModelContextLimit()
		completionLimit := p.getModelCompletionLimit()
		request["max_tokens"] = CalculateMaxTokensWithLimits(contextLimit, completionLimit, messages, tools)
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

	// Apply model-specific defaults and suppress unsupported fields.
	applyModelSpecificSettings(p.model, request)
	applyReasoningEffort(p.model, reasoning, request)
	applyDisableThinking(p.model, disableThinking, request)

	// Add tools if provided
	if len(tools) > 0 {
		request["tools"] = tools
	}

	return json.Marshal(request)
}

func applyReasoningEffort(model, reasoning string, request map[string]interface{}) {
	effort := strings.ToLower(strings.TrimSpace(reasoning))
	if effort == "" {
		return
	}
	if effort != "low" && effort != "medium" && effort != "high" {
		return
	}
	if !strings.Contains(strings.ToLower(model), "gpt-oss") {
		return
	}
	request["reasoning_effort"] = effort
}

// applyDisableThinking applies the disable_thinking setting to the request for models that support it.
// Different model families use different parameter names to disable thinking:
func applyDisableThinking(model string, disableThinking bool, request map[string]interface{}) {
	if !disableThinking {
		return
	}

	modelLower := strings.ToLower(model)

	// Check for known reasoning-only models that cannot disable thinking
	// DeepSeek-R1, DeepSeek-Reasoner, QwQ, QwenVL are pure reasoning models - they always think
	if strings.HasPrefix(modelLower, "deepseek-r1") ||
		strings.HasPrefix(modelLower, "deepseek-reasoner") ||
		strings.HasPrefix(modelLower, "qwq") ||
		strings.HasPrefix(modelLower, "qwenvl") ||
		strings.HasPrefix(modelLower, "kimi-k2-thinking") ||
		strings.HasPrefix(modelLower, "kimi-thinking") {
		// These are reasoning-only models - cannot disable thinking
		return
	}

	// GPT-OSS models don't support disabling thinking - they use reasoning_effort instead
	// (This is handled via applyReasoningEffort, so we skip here)
	if strings.Contains(modelLower, "gpt-oss") {
		return
	}

	// OpenAI o-series and reasoning models use reasoning_effort parameter
	// (Handled by applyReasoningEffort - this function is for models that use thinking enable/disable)
	// Skip OpenAI reasoning models here as they use different mechanism
	if strings.HasPrefix(modelLower, "o1") || strings.HasPrefix(modelLower, "o2") ||
		strings.HasPrefix(modelLower, "o3") || strings.HasPrefix(modelLower, "o4") {
		return // Use reasoning_effort instead
	}

	// DeepSeek - chat, coder, V3, and V4 models support disabling thinking
	// V4 models (deepseek-v4-flash, deepseek-v4-pro) default to thinking enabled
	if strings.Contains(modelLower, "deepseek-chat") ||
		strings.Contains(modelLower, "deepseek-coder") ||
		strings.Contains(modelLower, "deepseek-v3") ||
		strings.Contains(modelLower, "deepseek-v4") {
		request["thinking"] = map[string]interface{}{
			"type": "disabled",
		}
		return
	}

	// Anthropic Claude - models with extended thinking support
	if strings.Contains(modelLower, "claude-4") ||
		strings.Contains(modelLower, "claude-opus-4.6") ||
		strings.Contains(modelLower, "claude-sonnet-4.6") ||
		strings.Contains(modelLower, "claude-haiku-4.6") {
		// For Claude 4/Opus 4.6, use adaptive with low effort to minimize thinking
		// Note: Older deprecated syntax was type: "disabled"
		request["thinking"] = map[string]interface{}{
			"type":   "adaptive",
			"effort": "low",
		}
		return
	}

	// Qwen models (Alibaba) - Qwen3, Qwen3.5, Qwen2.5 use enable_thinking
	if strings.Contains(modelLower, "qwen3") || strings.Contains(modelLower, "qwen2.5") || strings.Contains(modelLower, "qwen2") {
		request["enable_thinking"] = false
		return
	}

	// GLM models (zai provider) - use thinking.type = "disabled"
	if strings.Contains(modelLower, "glm") {
		request["thinking"] = map[string]interface{}{
			"type": "disabled",
		}
		return
	}

	// MiniMax models - use reasoning_split parameter
	if strings.Contains(modelLower, "minimax") {
		request["reasoning_split"] = false
		return
	}

	// Google Gemini 2.5+ models - use thinking_config with thinking_budget
	// Gemini 3 series uses thinking_level instead (cannot fully disable)
	if strings.Contains(modelLower, "gemini-2") || strings.Contains(modelLower, "gemma-3") {
		// For Gemini 2.5 series, set thinking_budget to 0 to disable thinking
		request["thinking_config"] = map[string]interface{}{
			"thinking_budget": 0,
		}
		return
	}

	// Google Gemini 3 series - use thinking_level (cannot fully disable, only minimize)
	if strings.Contains(modelLower, "gemini-3") {
		// For Gemini 3 series, set thinking_level to "minimal" to reduce thinking
		// Note: Cannot fully disable thinking on Gemini 3
		request["thinking_config"] = map[string]interface{}{
			"thinking_level": "minimal",
		}
		return
	}

	// MoonShot (Kimi) models - standard kimi models (not thinking-only)
	if strings.Contains(modelLower, "kimi") {
		// kimi-k2.5 and similar non-thinking models support enable_thinking
		request["enable_thinking"] = false
		return
	}

	// If we reach here, the model might not support disabling thinking
	// We simply don't add any parameter (models will use their default behavior)
}

func (p *GenericProvider) ensureModel() error {
	if strings.TrimSpace(p.model) != "" {
		return nil
	}

	models, err := p.ListModels(context.Background())
	if err != nil {
		return fmt.Errorf("failed to discover models for provider %s: %w", p.config.Name, err)
	}
	if len(models) == 0 || strings.TrimSpace(models[0].ID) == "" {
		return fmt.Errorf("provider %s did not return any models", p.config.Name)
	}

	p.model = strings.TrimSpace(models[0].ID)
	return nil
}

func applyModelSpecificSettings(model string, request map[string]interface{}) {
	settings := modelsettings.ResolveModelSettings(model)
	if !settings.Known {
		return
	}
	for param := range settings.Unsupported {
		delete(request, param)
	}
	for param, value := range settings.Parameters {
		if !settings.Supported[param] {
			continue
		}
		if value == nil {
			delete(request, param)
			continue
		}
		request[param] = value
	}
}

// buildMultiModalContent creates a multi-part content array for messages with images
func (p *GenericProvider) buildMultiModalContent(text string, images []api.ImageData) interface{} {
	parts := make([]map[string]interface{}, 0, len(images)+1)

	// Add text part if present
	if strings.TrimSpace(text) != "" {
		parts = append(parts, map[string]interface{}{
			"type": "text",
			"text": text,
		})
	}

	// Add image parts
	for _, img := range images {
		imageURL := p.buildImageURL(img)
		if imageURL == "" {
			// Skip invalid images - caller should ensure valid image data
			continue
		}
		parts = append(parts, map[string]interface{}{
			"type": "image_url",
			"image_url": map[string]interface{}{
				"url": imageURL,
			},
		})
	}

	if len(parts) == 0 {
		return text // Fall back to text if no valid parts
	}
	return parts
}

// buildImageURL constructs the image URL from either a URL or base64 data
func (p *GenericProvider) buildImageURL(img api.ImageData) string {
	imageURL := strings.TrimSpace(img.URL)
	if imageURL == "" && strings.TrimSpace(img.Base64) != "" {
		mimeType := strings.TrimSpace(img.Type)
		if mimeType == "" {
			mimeType = "image/png"
		}
		imageURL = "data:" + mimeType + ";base64," + img.Base64
	}
	return imageURL
}

func (p *GenericProvider) getModelCompletionLimit() int {
	// First honor explicit config overrides.
	if limit := p.config.GetMaxCompletionLimit(p.model); limit > 0 {
		return limit
	}

	// Then apply provider/model-specific known limits.
	provider := strings.ToLower(p.config.Name)
	model := strings.ToLower(p.model)

	switch provider {
	case "openrouter":
		if strings.Contains(model, "gpt-5") {
			return 128000
		}
	case "minimax":
		if strings.Contains(model, "minimax-m2") {
			return 196608
		}
	}

	return 0
}

// convertMessages converts messages according to provider configuration
func (p *GenericProvider) convertMessages(messages []api.Message, reasoning string) []map[string]interface{} {
	converted := make([]map[string]interface{}, len(messages))
	reasoningField := p.config.Conversion.ReasoningContentField
	skipReasoningHistory := p.shouldSkipReasoningContentHistory()

	for i, msg := range messages {
		content := interface{}(msg.Content)
		if len(msg.Images) > 0 {
			content = p.buildMultiModalContent(msg.Content, msg.Images)
		}

		convertedMsg := map[string]interface{}{
			"role":    msg.Role,
			"content": content,
		}

		// Handle tool call ID inclusion
		if msg.ToolCallID != "" && p.config.Conversion.IncludeToolCallID {
			convertedMsg["tool_call_id"] = msg.ToolCallID
		}

		// Handle tool role conversion
		if msg.Role == "tool" && p.config.Conversion.ConvertToolRoleToUser {
			convertedMsg["role"] = "user"
		}

		// Preserve reasoning content from previous assistant messages
		// This is critical for models like z-ai/glm-4.7-flash that use reasoning tokens
		if !skipReasoningHistory && msg.ReasoningContent != "" && reasoningField != "" {
			convertedMsg[reasoningField] = msg.ReasoningContent
		}

		// Preserve tool calls if present
		if len(msg.ToolCalls) > 0 {
			convertedMsg["tool_calls"] = p.convertToolCalls(msg.ToolCalls)
		}

		converted[i] = convertedMsg
	}

	_ = reasoning // reasoning effort is sent via provider/model-specific request params, not message fields

	return converted
}

func (p *GenericProvider) shouldSkipReasoningContentHistory() bool {
	// MiniMax expects reasoning_details to be a structured array, not a plain string.
	// Replaying historical ReasoningContent verbatim causes type mismatch 400s.
	return strings.EqualFold(p.config.Name, "minimax") &&
		strings.EqualFold(p.config.Conversion.ReasoningContentField, "reasoning_details")
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
		return nil, fmt.Errorf("get model context limit: %w", err)
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
		if line == "" {
			continue
		}
		var data string
		if strings.HasPrefix(line, "data: ") {
			data = strings.TrimPrefix(line, "data: ")
		} else if strings.HasPrefix(line, "data:") {
			data = strings.TrimPrefix(line, "data:")
			data = strings.TrimSpace(data)
		} else {
			continue
		}
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
