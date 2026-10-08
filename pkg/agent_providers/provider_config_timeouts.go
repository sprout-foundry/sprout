// provider_config_timeouts.go — timeout + limit accessor methods on
// ProviderConfig, split from provider_config.go. These resolve the
// effective (per-model override > config default) values for request
// timeouts, chunk/idle timeouts, context + completion limits, and model
// info lookup.
package providers

import (
	"regexp"
	"strings"
	"time"
)

// GetTimeout returns the configured timeout duration
func (c *ProviderConfig) GetTimeout() time.Duration {
	if c.Streaming.ChunkTimeoutMs > 0 {
		return time.Duration(c.Streaming.ChunkTimeoutMs) * time.Millisecond
	}
	return 300 * time.Second // Default timeout (5 minutes)
}

// GetStreamingTimeout returns the configured streaming timeout duration
func (c *ProviderConfig) GetStreamingTimeout() time.Duration {
	if c.Streaming.ChunkTimeoutMs > 0 {
		return time.Duration(c.Streaming.ChunkTimeoutMs) * time.Millisecond
	}
	return 900 * time.Second // Default streaming timeout (15 minutes)
}

// GetFirstChunkTimeout returns the deadline for the first chunk of a
// streaming response. Defaults far above the inter-chunk deadline because
// prompt prefill on large contexts (or slow/local models) can legitimately
// run many minutes before the first token. Must stay <= the HTTP client's
// streaming timeout or the transport kills the stream first.
func (c *ProviderConfig) GetFirstChunkTimeout() time.Duration {
	if c.Streaming.FirstChunkTimeoutMs > 0 {
		d := time.Duration(c.Streaming.FirstChunkTimeoutMs) * time.Millisecond
		if streamCap := c.GetStreamingTimeout(); d > streamCap {
			return streamCap
		}
		return d
	}
	return 10 * time.Minute
}

// GetIdleChunkTimeout returns the inter-chunk idle deadline applied after
// the first chunk has arrived. A stall mid-generation (proxy idle hole,
// dead upstream) is detected here and surfaced as a retryable network
// error. IdleChunkTimeoutMs takes precedence over the legacy ChunkTimeoutMs
// knob without shortening the HTTP client timeout.
func (c *ProviderConfig) GetIdleChunkTimeout() time.Duration {
	if c.Streaming.IdleChunkTimeoutMs > 0 {
		return time.Duration(c.Streaming.IdleChunkTimeoutMs) * time.Millisecond
	}
	if c.Streaming.ChunkTimeoutMs > 0 {
		return time.Duration(c.Streaming.ChunkTimeoutMs) * time.Millisecond
	}
	return 120 * time.Second
}

// GetContextLimit returns the context limit for a given model based on configuration
// Uses the following priority:
// 1. Exact model match in model_overrides
// 2. Pattern match in pattern_overrides
// 3. Lookup in model_info (catalog — source of truth for known models)
// 4. Provider default_context_limit (conservative fallback when catalog is absent)
// 5. Legacy context_limit field (for backward compatibility)
// 6. Conservative fallback (32000)
func (c *ProviderConfig) GetContextLimit(model string) int {
	// 1. Check for exact model match in overrides
	if contextLimit, exists := c.Models.ModelOverrides[model]; exists {
		return contextLimit
	}

	// 2. Check for pattern matches in overrides
	for _, patternOverride := range c.Models.PatternOverrides {
		if matched, _ := regexp.MatchString(patternOverride.Pattern, model); matched {
			return patternOverride.ContextLimit
		}
	}

	// 3. Check model_info catalog for a matching ID — handles full provider/model
	// names like "MiniMaxAI/MiniMax-M2.7" matching ID "MiniMax-M2.7". This is the
	// primary lookup for models published in the remote registry catalog.
	if len(c.Models.ModelInfo) > 0 {
		for _, mi := range c.Models.ModelInfo {
			if mi.ContextLength > 0 && (model == mi.ID || strings.HasSuffix(model, "/"+mi.ID)) {
				return mi.ContextLength
			}
		}
	}

	// 4. Use provider default context limit (fallback when catalog lacks this model)
	if c.Models.DefaultContextLimit > 0 {
		return c.Models.DefaultContextLimit
	}

	// 5. Fall back to legacy context_limit field (for backward compatibility)
	if c.Models.ContextLimit > 0 {
		return c.Models.ContextLimit
	}

	// 6. Conservative fallback
	return 32000
}

// GetMaxCompletionLimit returns the completion-token limit for a given model.
// Uses the following priority:
// 1. Exact model match in max_completion_overrides
// 2. Pattern match in completion_pattern_overrides
// 3. Provider default_max_completion_tokens
// 4. 0 (unknown/unset)
func (c *ProviderConfig) GetMaxCompletionLimit(model string) int {
	if c.Models.MaxCompletionOverrides != nil {
		if maxCompletion, exists := c.Models.MaxCompletionOverrides[model]; exists && maxCompletion > 0 {
			return maxCompletion
		}
	}

	for _, patternOverride := range c.Models.CompletionPatternOverrides {
		if matched, _ := regexp.MatchString(patternOverride.Pattern, model); matched && patternOverride.ContextLimit > 0 {
			return patternOverride.ContextLimit
		}
	}

	if c.Models.DefaultMaxCompletionTokens > 0 {
		return c.Models.DefaultMaxCompletionTokens
	}

	return 0
}

// GetModelInfo returns model information from config if available
func (c *ProviderConfig) GetModelInfo(modelID string) *ModelInfo {
	for i := range c.Models.ModelInfo {
		if c.Models.ModelInfo[i].ID == modelID {
			return &c.Models.ModelInfo[i]
		}
	}
	return nil
}

// lookupModelSampling returns the per-model sampling entry configured for a
// model. Resolution order:
//  1. Exact model ID match in model_info
//  2. Pattern-style match for provider-prefixed IDs ("vendor/<id>") via the
//     same "/suffix" rule GetContextLimit uses, so "deepinfra/model-x" resolves
//     the entry declared for "model-x"
//
// Only entries that actually carry sampling are returned, so a model_info
// entry that exists solely for pricing or context does not shadow the
// provider-level defaults.
func (c *ProviderConfig) lookupModelSampling(model string) *SamplingParams {
	if model == "" {
		return nil
	}
	for i := range c.Models.ModelInfo {
		mi := &c.Models.ModelInfo[i]
		if mi.Sampling == nil {
			continue
		}
		if mi.ID == model || strings.HasSuffix(model, "/"+mi.ID) {
			return mi.Sampling
		}
	}
	return nil
}

// ResolveTemperature returns the effective temperature for a model. Precedence:
// per-model sampling → provider-level default → nil (no value emitted). A nil
// return means no temperature field is written, leaving the backend default.
func (c *ProviderConfig) ResolveTemperature(model string) *float64 {
	if s := c.lookupModelSampling(model); s != nil && s.Temperature != nil {
		return s.Temperature
	}
	return c.Defaults.Temperature
}

// ResolveTopP returns the effective top_p for a model. Precedence mirrors
// ResolveTemperature: per-model sampling → provider-level default → nil.
func (c *ProviderConfig) ResolveTopP(model string) *float64 {
	if s := c.lookupModelSampling(model); s != nil && s.TopP != nil {
		return s.TopP
	}
	return c.Defaults.TopP
}

// ResolveParameters returns the effective free-form request parameters for a
// model as a fresh map. Per-model parameters are merged over (and win over)
// the provider-level defaults; keys present only in the defaults survive.
// Unknown keys pass through untouched — the caller copies every entry onto the
// request body. Returns nil when neither level configures any parameters.
func (c *ProviderConfig) ResolveParameters(model string) map[string]interface{} {
	modelParams := map[string]interface{}(nil)
	if s := c.lookupModelSampling(model); s != nil {
		modelParams = s.Parameters
	}
	if len(modelParams) == 0 && len(c.Defaults.Parameters) == 0 {
		return nil
	}
	merged := make(map[string]interface{}, len(c.Defaults.Parameters)+len(modelParams))
	for k, v := range c.Defaults.Parameters {
		merged[k] = v
	}
	for k, v := range modelParams {
		merged[k] = v
	}
	return merged
}
