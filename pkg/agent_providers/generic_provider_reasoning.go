package providers

// generic_provider_reasoning.go — the reasoning / thinking request
// options for the generic provider: applyReasoningEffort,
// isReasoningEffortLevel, and applyDisableThinking. Split out of
// generic_provider_request.go.
import (
	"strings"

	modelsettings "github.com/sprout-foundry/sprout/pkg/model_settings"
)

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
