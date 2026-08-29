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

	// Snapshot the model name under lock after ensureModel() resolved it.
	p.mu.RLock()
	model := p.model
	p.mu.RUnlock()

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
		"model":    model,
		"messages": convertedMessages,
		"stream":   stream,
	}

	// Add default parameters
	if p.config.Defaults.Temperature != nil {
		request["temperature"] = *p.config.Defaults.Temperature
	}

	// Apply output token budgeting against context limits.
	contextLimit, _ := p.GetModelContextLimit()
	completionLimit := p.getModelCompletionLimit()

	p.maxTokensHintMu.RLock()
	hint := p.maxTokensHint
	p.maxTokensHintMu.RUnlock()

	var budgetedMax int
	if hint > 0 {
		// Use the caller's pre-computed hint (from the token anchor) — it's
		// more accurate than recomputing from the raw heuristic.
		budgetedMax = hint
	} else {
		budgetedMax = CalculateMaxTokensWithLimits(contextLimit, completionLimit, messages, tools)
	}
	if p.config.Defaults.MaxTokens != nil && *p.config.Defaults.MaxTokens > 0 {
		if budgetedMax > *p.config.Defaults.MaxTokens {
			budgetedMax = *p.config.Defaults.MaxTokens
		}
	}

	// Apply a global 64K cap to max_tokens for safety across all providers.
	// Most providers support up to 64K output tokens; capping here prevents
	// errors from providers with stricter limits (e.g., ZAI's 131072 limit).
	const maxRequestCompletionTokens = 64000
	if budgetedMax > maxRequestCompletionTokens {
		budgetedMax = maxRequestCompletionTokens
	}

	request["max_tokens"] = budgetedMax

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
	// instruct=true when thinking is disabled, so models with a distinct
	// non-thinking recommendation (e.g. Qwen3.6, Qwen3.8) get the correct
	// parameters.
	applyModelSpecificSettings(model, request, disableThinking)
	// Disable wins: when thinking is being turned off, the effort level is
	// meaningless and would overwrite the disable-targeting reasoning object.
	if !disableThinking {
		p.applyReasoningEffort(model, reasoning, request)
	}
	p.applyDisableThinking(model, disableThinking, request)
	p.applyChatTemplateKwargs(model, disableThinking, request)

	// Add tools if provided
	if len(tools) > 0 {
		// Defense in depth for strict function-calling validators (Gemini 3.x):
		// fill any array property missing "items" so the request is always
		// well-formed. No-op for providers that haven't opted in.
		if p.config.Conversion.FillMissingArrayItems {
			tools = fillMissingArrayItems(tools, p.config.Conversion.ArrayItemsFallback)
		}
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

func (p *GenericProvider) applyReasoningEffort(model, reasoning string, request map[string]interface{}) {
	effort := strings.ToLower(strings.TrimSpace(reasoning))
	if effort == "" {
		return
	}
	if !isReasoningEffortLevel(effort) {
		return
	}
	// OpenRouter's unified reasoning object is the canonical control surface
	// and the only knob that reaches Anthropic models through it. Emit it
	// only for models the catalog says accept `reasoning`.
	if p.config.Conversion.UnifiedReasoningParam && modelsettings.ResolveModelSettings(model).Supported["reasoning"] {
		request["reasoning"] = map[string]interface{}{"effort": effort}
		return
	}
	if strings.Contains(strings.ToLower(model), "gpt-oss") {
		request["reasoning_effort"] = effort
	}
}

func isReasoningEffortLevel(effort string) bool {
	switch effort {
	case "low", "medium", "high", "xhigh", "max", "minimal", "none":
		return true
	}
	return false
}

// applyChatTemplateKwargs merges configured chat_template_kwargs into the
// request for template-driven local servers (vLLM, llama.cpp, LM Studio),
// where template flags like preserve_thinking/enable_thinking only take
// effect inside that object. Restricted to localhost endpoints: hosted APIs
// reject unknown request fields. For Qwen3.6+ models preserve_thinking is
// defaulted on (unless explicitly configured false) so prior-turn reasoning
// replayed via reasoning_content renders back into the prompt; it is
// suppressed when thinking is disabled for the request.
func (p *GenericProvider) applyChatTemplateKwargs(model string, disableThinking bool, request map[string]interface{}) {
	if !p.isLoopbackEndpoint() {
		return
	}
	merged, _ := request["chat_template_kwargs"].(map[string]interface{})
	if merged == nil {
		merged = map[string]interface{}{}
	}
	for k, v := range p.config.Conversion.ChatTemplateKwargs {
		merged[k] = v
	}
	if !disableThinking && isQwen36PlusModel(model) {
		if _, explicit := merged["preserve_thinking"]; !explicit {
			merged["preserve_thinking"] = true
		}
	} else if disableThinking {
		delete(merged, "preserve_thinking")
	}
	if len(merged) == 0 {
		return
	}
	request["chat_template_kwargs"] = merged
}

// isLoopbackEndpoint reports whether the configured endpoint targets a
// local server. Template kwargs (and other server-specific extras) are only
// safe to send there.
func (p *GenericProvider) isLoopbackEndpoint() bool {
	endpoint := strings.ToLower(p.config.Endpoint)
	return strings.Contains(endpoint, "127.0.0.1") ||
		strings.Contains(endpoint, "localhost") ||
		strings.Contains(endpoint, "[::1]")
}

// isQwen36PlusModel matches Qwen families whose chat templates implement
// preserve_thinking (Qwen3.6 and newer, including 3.7+/Qwen3.8).
func isQwen36PlusModel(model string) bool {
	m := strings.ToLower(model)
	if !strings.Contains(m, "qwen") {
		return false
	}
	for _, prefix := range []string{"qwen3.6", "qwen3.7", "qwen3.8", "qwen3.9", "qwen4"} {
		if strings.Contains(m, prefix) {
			return true
		}
	}
	return false
}

// applyDisableThinking applies the disable_thinking setting to the request for models that support it.
// Different model families use different parameter names to disable thinking:
func (p *GenericProvider) applyDisableThinking(model string, disableThinking bool, request map[string]interface{}) {
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
		// Via OpenRouter the unified reasoning object is the correct knob;
		// Anthropic-native `thinking` syntax is only for direct
		// Anthropic-compatible endpoints.
		if p.config.Conversion.UnifiedReasoningParam {
			request["reasoning"] = map[string]interface{}{"effort": "low"}
			return
		}
		request["thinking"] = map[string]interface{}{
			"type":   "adaptive",
			"effort": "low",
		}
		return
	}

	// Qwen models (Alibaba) - Qwen3, Qwen3.5, Qwen2.5 use enable_thinking
	if strings.Contains(modelLower, "qwen3") || strings.Contains(modelLower, "qwen2.5") || strings.Contains(modelLower, "qwen2") {
		// vLLM/llama.cpp only honor template flags inside
		// chat_template_kwargs; hosted DashScope-style APIs take the
		// top-level field.
		if p.isLoopbackEndpoint() {
			kwargs, _ := request["chat_template_kwargs"].(map[string]interface{})
			if kwargs == nil {
				kwargs = map[string]interface{}{}
			}
			kwargs["enable_thinking"] = false
			request["chat_template_kwargs"] = kwargs
		} else {
			request["enable_thinking"] = false
		}
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
	p.mu.RLock()
	currentModel := p.model
	p.mu.RUnlock()
	if strings.TrimSpace(currentModel) != "" {
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

	p.mu.Lock()
	p.model = strings.TrimSpace(models[0].ID)
	p.mu.Unlock()
	return nil
}

func applyModelSpecificSettings(model string, request map[string]interface{}, disableThinking bool) {
	settings := modelsettings.ResolveModelSettingsForMode(model, disableThinking)
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
	req, _, err := p.buildHTTPRequestCtx(context.Background(), body, streaming)
	return req, err
}

// buildHTTPRequestCtx builds the HTTP request bound to ctx.
func (p *GenericProvider) buildHTTPRequestCtx(ctx context.Context, body []byte, streaming bool) (*http.Request, []byte, error) {
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
		return nil, body, agenterrors.NewNetwork("failed to build HTTP request", err)
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
			return nil, body, agenterrors.Wrap(authErr, "authentication failed")
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

	return req, body, nil
}
