// Configuration types and native conversion methods for the remote provider registry.
package providerregistry

import (
	"encoding/json"
	"time"

	providers "github.com/sprout-foundry/sprout/pkg/agent_providers"
)

// RemoteAuthConfig duplicates AuthConfig without the runtime-only Key field.
type RemoteAuthConfig struct {
	Type   string `json:"type"`
	EnvVar string `json:"env_var"`
}

// RemoteRequestDefaults duplicates RequestDefaults.
type RemoteRequestDefaults struct {
	Model       string                 `json:"model"`
	Temperature *float64               `json:"temperature"`
	MaxTokens   *int                   `json:"max_tokens"`
	TopP        *float64               `json:"top_p"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
}

// RemoteMessageConversion duplicates MessageConversion.
type RemoteMessageConversion struct {
	IncludeToolCallID        bool   `json:"include_tool_call_id"`
	ConvertToolRoleToUser    bool   `json:"convert_tool_role_to_user"`
	ReasoningContentField    string `json:"reasoning_content_field"`
	ArgumentsAsJSON          bool   `json:"arguments_as_json"`
	SkipToolExecutionSummary bool   `json:"skip_tool_execution_summary"`
	ForceToolCallType        string `json:"force_tool_call_type"`
	NeutralizeSpecialTokens  bool   `json:"neutralize_special_tokens,omitempty"`
}

// RemoteStreamingConfig duplicates StreamingConfig.
type RemoteStreamingConfig struct {
	Format         string `json:"format"`
	ChunkTimeoutMs int    `json:"chunk_timeout_ms"`
	DoneMarker     string `json:"done_marker"`
}

// RemotePatternOverride duplicates PatternOverride.
type RemotePatternOverride struct {
	Pattern      string `json:"pattern"`
	ContextLimit int    `json:"context_limit"`
}

// RemoteModelInfo duplicates ModelInfo.
type RemoteModelInfo struct {
	ID            string   `json:"id"`
	Name          string   `json:"name,omitempty"`
	Description   string   `json:"description,omitempty"`
	ContextLength int      `json:"context_length"`
	Tags          []string `json:"tags,omitempty"`
}

// RemoteModelConfig duplicates ModelConfig.
type RemoteModelConfig struct {
	DefaultContextLimit        int                     `json:"default_context_limit"`
	DefaultMaxCompletionTokens int                     `json:"default_max_completion_tokens,omitempty"`
	ModelOverrides             map[string]int          `json:"model_overrides"`
	MaxCompletionOverrides     map[string]int          `json:"max_completion_overrides,omitempty"`
	PatternOverrides           []RemotePatternOverride `json:"pattern_overrides"`
	CompletionPatternOverrides []RemotePatternOverride `json:"completion_pattern_overrides,omitempty"`
	ModelInfo                  []RemoteModelInfo       `json:"model_info,omitempty"`
	ContextLimit               int                     `json:"context_limit,omitempty"`
	SupportsVision             bool                    `json:"supports_vision"`
	VisionModel                string                  `json:"vision_model"`
	DefaultModel               string                  `json:"default_model"`
	AvailableModels            []string                `json:"available_models"`
}

// RemoteRetryConfig duplicates RetryConfig.
type RemoteRetryConfig struct {
	MaxAttempts       int      `json:"max_attempts"`
	BaseDelayMs       int      `json:"base_delay_ms"`
	BackoffMultiplier float64  `json:"backoff_multiplier"`
	MaxDelayMs        int      `json:"max_delay_ms"`
	RetryableErrors   []string `json:"retryable_errors"`
}

// RemoteCostConfig duplicates CostConfig.
type RemoteCostConfig struct {
	InputTokenCost  float64 `json:"input_token_cost"`
	OutputTokenCost float64 `json:"output_token_cost"`
	Currency        string  `json:"currency"`
}

// RemoteProviderConfig duplicates ProviderConfig for remote JSON consumption.
type RemoteProviderConfig struct {
	Name        string                  `json:"name"`
	DisplayName string                  `json:"display_name,omitempty"`
	Endpoint    string                  `json:"endpoint"`
	Auth        RemoteAuthConfig        `json:"auth"`
	Headers     map[string]string       `json:"headers"`
	Defaults    RemoteRequestDefaults   `json:"defaults"`
	Conversion  RemoteMessageConversion `json:"message_conversion"`
	Streaming   RemoteStreamingConfig   `json:"streaming"`
	Models      RemoteModelConfig       `json:"models"`
	Retry       RemoteRetryConfig       `json:"retry"`
	Cost        RemoteCostConfig        `json:"cost"`
}

// ToProviderConfig converts this remote config to a providers.ProviderConfig.
// The Key field in Auth is left empty (runtime-only, set by the credential resolver).
func (r *RemoteProviderConfig) ToProviderConfig() *providers.ProviderConfig {
	if r == nil {
		return nil
	}
	return &providers.ProviderConfig{
		Name:        r.Name,
		DisplayName: r.DisplayName,
		Endpoint:    r.Endpoint,
		Auth: providers.AuthConfig{
			Type:   r.Auth.Type,
			EnvVar: r.Auth.EnvVar,
		},
		Headers:    copyStringMap(r.Headers),
		Defaults:   r.defaultsToNative(),
		Conversion: r.conversionToNative(),
		Streaming:  r.streamingToNative(),
		Models:     r.modelsToNative(),
		Retry:      r.retryToNative(),
		Cost:       r.costToNative(),
	}
}

func (r *RemoteProviderConfig) defaultsToNative() providers.RequestDefaults {
	rd := providers.RequestDefaults{
		Model:      r.Defaults.Model,
		Parameters: copyInterfaceMap(r.Defaults.Parameters),
	}
	if r.Defaults.Temperature != nil {
		v := *r.Defaults.Temperature
		rd.Temperature = &v
	}
	if r.Defaults.MaxTokens != nil {
		v := *r.Defaults.MaxTokens
		rd.MaxTokens = &v
	}
	if r.Defaults.TopP != nil {
		v := *r.Defaults.TopP
		rd.TopP = &v
	}
	return rd
}

func (r *RemoteProviderConfig) conversionToNative() providers.MessageConversion {
	mc := providers.MessageConversion{
		IncludeToolCallID:        r.Conversion.IncludeToolCallID,
		ConvertToolRoleToUser:    r.Conversion.ConvertToolRoleToUser,
		ReasoningContentField:    r.Conversion.ReasoningContentField,
		ArgumentsAsJSON:          r.Conversion.ArgumentsAsJSON,
		SkipToolExecutionSummary: r.Conversion.SkipToolExecutionSummary,
		ForceToolCallType:        r.Conversion.ForceToolCallType,
		NeutralizeSpecialTokens:  r.Conversion.NeutralizeSpecialTokens,
	}
	// Enforce standard OpenAI tool-calling defaults for remote configs that
	// omit message_conversion settings. Same rationale as custom providers.
	if !mc.IncludeToolCallID {
		mc.IncludeToolCallID = true
	}
	return mc
}

func (r *RemoteProviderConfig) streamingToNative() providers.StreamingConfig {
	return providers.StreamingConfig{
		Format:         r.Streaming.Format,
		ChunkTimeoutMs: r.Streaming.ChunkTimeoutMs,
		DoneMarker:     r.Streaming.DoneMarker,
	}
}

func (r *RemoteProviderConfig) modelsToNative() providers.ModelConfig {
	mc := providers.ModelConfig{
		DefaultContextLimit:        r.Models.DefaultContextLimit,
		DefaultMaxCompletionTokens: r.Models.DefaultMaxCompletionTokens,
		ModelOverrides:             copyIntMap(r.Models.ModelOverrides),
		MaxCompletionOverrides:     copyIntMap(r.Models.MaxCompletionOverrides),
		ContextLimit:               r.Models.ContextLimit,
		SupportsVision:             r.Models.SupportsVision,
		VisionModel:                r.Models.VisionModel,
		DefaultModel:               r.Models.DefaultModel,
		AvailableModels:            copyStringSlice(r.Models.AvailableModels),
	}

	for _, po := range r.Models.PatternOverrides {
		mc.PatternOverrides = append(mc.PatternOverrides, providers.PatternOverride{
			Pattern:      po.Pattern,
			ContextLimit: po.ContextLimit,
		})
	}
	for _, po := range r.Models.CompletionPatternOverrides {
		mc.CompletionPatternOverrides = append(mc.CompletionPatternOverrides, providers.PatternOverride{
			Pattern:      po.Pattern,
			ContextLimit: po.ContextLimit,
		})
	}
	for _, mi := range r.Models.ModelInfo {
		mc.ModelInfo = append(mc.ModelInfo, providers.ModelInfo{
			ID:            mi.ID,
			Name:          mi.Name,
			Description:   mi.Description,
			ContextLength: mi.ContextLength,
			Tags:          copyStringSlice(mi.Tags),
		})
	}
	return mc
}

func (r *RemoteProviderConfig) retryToNative() providers.RetryConfig {
	return providers.RetryConfig{
		MaxAttempts:       r.Retry.MaxAttempts,
		BaseDelayMs:       r.Retry.BaseDelayMs,
		BackoffMultiplier: r.Retry.BackoffMultiplier,
		MaxDelayMs:        r.Retry.MaxDelayMs,
		RetryableErrors:   copyStringSlice(r.Retry.RetryableErrors),
	}
}

func (r *RemoteProviderConfig) costToNative() providers.CostConfig {
	return providers.CostConfig{
		InputTokenCost:  r.Cost.InputTokenCost,
		OutputTokenCost: r.Cost.OutputTokenCost,
		Currency:        r.Cost.Currency,
	}
}

// Helper copy functions to avoid mutating shared data.
func copyStringMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func copyIntMap(m map[string]int) map[string]int {
	if m == nil {
		return nil
	}
	out := make(map[string]int, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func copyInterfaceMap(m map[string]interface{}) map[string]interface{} {
	if m == nil {
		return nil
	}
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func copyStringSlice(s []string) []string {
	if s == nil {
		return nil
	}
	out := make([]string, len(s))
	copy(out, s)
	return out
}

// cachedConfig wraps a RemoteProviderConfig with its fetch time.
type cachedConfig struct {
	config    RemoteProviderConfig
	fetchedAt time.Time
}

// cloneConfig returns a deep copy of the cached config to avoid shared mutations.
func cloneConfig(c *cachedConfig) *RemoteProviderConfig {
	data, _ := json.Marshal(&c.config)
	var out RemoteProviderConfig
	_ = json.Unmarshal(data, &out)
	return &out
}
