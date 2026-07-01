package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	modelsettings "github.com/sprout-foundry/sprout/pkg/model_settings"
	"github.com/sprout-foundry/sprout/pkg/secretdetect"
	"github.com/sprout-foundry/sprout/pkg/utils"
)

// buildChatRequest builds the request body for chat completion
func (p *GenericProvider) buildChatRequest(messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool, stream bool) ([]byte, error) {
	if err := p.ensureModel(); err != nil {
		return nil, agenterrors.NewNetwork("ensure model", err)
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
				if tc, hasToolCalls := convertedMessages[stripEnd]["tool_calls"]; hasToolCalls && tc != nil {
					break // Preserve assistant messages with tool_calls
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
		if p.config.Conversion.CacheControl {
			// Convert tools to map form so we can attach cache_control to the
			// last tool, marking the tool definitions as cacheable prefix.
			toolMaps := make([]map[string]interface{}, 0, len(tools))
			for i, tool := range tools {
				tm := map[string]interface{}{
					"type": tool.Type,
					"function": map[string]interface{}{
						"name":        tool.Function.Name,
						"description": tool.Function.Description,
						"parameters":  tool.Function.Parameters,
					},
				}
				if i == len(tools)-1 {
					tm["cache_control"] = map[string]interface{}{"type": "ephemeral"}
				}
				toolMaps = append(toolMaps, tm)
			}
			request["tools"] = toolMaps
		} else {
			request["tools"] = tools
		}
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
		return agenterrors.NewNetwork(fmt.Sprintf("failed to discover models for provider %s", p.config.Name), err)
	}
	if len(models) == 0 || strings.TrimSpace(models[0].ID) == "" {
	// NOTE: Kept as fmt.Errorf — test TestGenericProviderErrorsWhenNoModelConfiguredOrDiscoverable
	// asserts strings.Contains(err.Error(), "did not return any models") which would break
	// with NewNotFound's auto-appended " not found" suffix
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

func shouldRetryWithMaxCompletionTokens(errBody []byte) bool {
	bodyLower := strings.ToLower(string(errBody))
	return strings.Contains(bodyLower, "max_tokens") &&
		strings.Contains(bodyLower, "max_completion_tokens") &&
		strings.Contains(bodyLower, "unsupported")
}

func rewriteMaxTokensToMaxCompletionTokens(requestBody []byte) ([]byte, bool, error) {
	var payload map[string]interface{}
	if err := json.Unmarshal(requestBody, &payload); err != nil {
		return nil, false, agenterrors.NewValidation(fmt.Sprintf("parse request body: %v", err), nil)
	}

	maxTokens, hasMaxTokens := payload["max_tokens"]
	if !hasMaxTokens {
		return requestBody, false, nil
	}
	if _, exists := payload["max_completion_tokens"]; exists {
		return requestBody, false, nil
	}

	payload["max_completion_tokens"] = maxTokens
	delete(payload, "max_tokens")

	updated, err := json.Marshal(payload)
	if err != nil {
		return nil, false, agenterrors.NewValidation(fmt.Sprintf("marshal updated request body: %v", err), nil)
	}
	return updated, true, nil
}

// buildHTTPRequest is a context.Background convenience wrapper kept for
// internal callers that don't carry a context (e.g. the retry path).
// New callers should use buildHTTPRequestCtx so the user's Stop button
// can abort in-flight LLM requests — see SP-034.
func (p *GenericProvider) buildHTTPRequest(body []byte, streaming bool) (*http.Request, error) {
	return p.buildHTTPRequestCtx(context.Background(), body, streaming)
}

// buildHTTPRequestCtx builds the HTTP request bound to ctx.
func (p *GenericProvider) buildHTTPRequestCtx(ctx context.Context, body []byte, streaming bool) (*http.Request, error) {
	// For local instances like LM Studio, skip auth check entirely if it would fail
	isLocalInstance := strings.Contains(p.config.Endpoint, "127.0.0.1") || strings.Contains(p.config.Endpoint, "localhost")

	// Egress redaction backstop: scan the outbound payload for any secrets
	// that escaped per-tool redaction (Layer 5) and replace them with opaque
	// [REDACTED] tokens. Skipped for local providers since the threat model
	// — third-party logging/training — only applies to remote endpoints.
	if !isLocalInstance && len(body) > 0 {
		if redacted := secretdetect.RedactOpaque(string(body)); redacted != string(body) {
			utils.GetLogger(false).Logf("[security] egress backstop redacted secrets from outbound LLM request payload (per-tool redaction missed something — investigate if frequent)")
			body = []byte(redacted)
		}
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.config.Endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, agenterrors.NewNetwork("failed to build HTTP request", err)
	}

	// Check if authentication is needed
	var token string

	if isLocalInstance && (p.config.Auth.Type == "bearer" || p.config.Auth.Type == "api_key") &&
		p.config.Auth.EnvVar == "" && p.config.Auth.Key == "" {
		// Local instance with no auth token configured - skip auth entirely
		token = ""
	} else {
		// Get authentication token normally
		var authErr error
		token, authErr = p.config.GetAuthToken()
		if authErr != nil {
			return nil, agenterrors.Wrap(authErr, "authentication failed")
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
