package configuration

// custom_provider_lookup.go — provider conversion and known-provider lookup,
// split out of custom_provider_registry.go. ToProviderConfig turns a
// normalized CustomProviderConfig into a provider-registry ProviderConfig;
// KnownProviderInfo + LookupKnownProvider feed the `sprout custom add`
// wizard (custom-config override, then factory, then embedded configs);
// ModelsEndpoint derives the /models URL for a provider.
import (
	"fmt"
	"sort"
	"strings"

	providers "github.com/sprout-foundry/sprout/pkg/agent_providers"
)

func (c CustomProviderConfig) ModelsEndpoint() string {
	return strings.TrimSuffix(c.Endpoint, "/chat/completions") + "/models"
}

func (c CustomProviderConfig) ToProviderConfig() (*providers.ProviderConfig, error) {
	normalized, err := NormalizeCustomProviderConfig(c)
	if err != nil {
		return nil, fmt.Errorf("normalize custom provider config: %w", err)
	}

	authType := "none"
	if normalized.RequiresAPIKey || normalized.EnvVar != "" {
		authType = "api_key"
	}

	conversion := normalized.Conversion
	// Enforce standard OpenAI tool-calling defaults. These are correct for
	// virtually all OpenAI-compatible providers — the tool role and
	// tool_call_id are part of the spec. Only override if the user explicitly
	// set them (non-zero value means the user configured something).
	if !conversion.IncludeToolCallID {
		conversion.IncludeToolCallID = true
	}
	// ConvertToolRoleToUser defaults to false (Go zero value), which is correct.
	// Only providers with legacy incompatibility (none currently) should set this true.
	if !conversion.SkipToolExecutionSummary {
		conversion.SkipToolExecutionSummary = true
	}

	// Build model overrides for context sizes
	modelOverrides := make(map[string]int)
	for modelID, contextSize := range normalized.ModelContextSizes {
		if contextSize > 0 {
			modelOverrides[modelID] = contextSize
		}
	}

	// Carry per-model sampling overrides into model_info entries so the
	// resolution helpers (which look up model_info by id) find them. A user
	// config's per-model sampling therefore overrides both its own
	// provider-level defaults and any embedded entry for the same model.
	// Only entries that actually carry a configured value are emitted, so an
	// empty override never shadows another model_info source.
	var modelInfo []providers.ModelInfo
	for modelID, s := range normalized.ModelSampling {
		if s.Temperature == nil && s.TopP == nil && len(s.Parameters) == 0 {
			continue
		}
		modelInfo = append(modelInfo, providers.ModelInfo{
			ID: modelID,
			Sampling: &providers.SamplingParams{
				Temperature: s.Temperature,
				TopP:        s.TopP,
				Parameters:  s.Parameters,
			},
		})
	}
	sort.Slice(modelInfo, func(i, j int) bool { return modelInfo[i].ID < modelInfo[j].ID })

	return &providers.ProviderConfig{
		Name:     normalized.Name,
		Endpoint: normalized.Endpoint,
		Auth: providers.AuthConfig{
			Type:   authType,
			EnvVar: normalized.EnvVar,
			Key:    "",
		},
		Headers: map[string]string{},
		Defaults: providers.RequestDefaults{
			Model:       normalized.ModelName,
			Temperature: normalized.Temperature,
			TopP:        normalized.TopP,
			Parameters:  normalized.Parameters,
		},
		Conversion: conversion,
		Streaming: providers.StreamingConfig{
			Format:         "sse",
			ChunkTimeoutMs: normalized.ChunkTimeoutMs,
			DoneMarker:     "[DONE]",
			IncludeUsage:   normalized.IncludeUsage,
		},
		Models: providers.ModelConfig{
			DefaultContextLimit: normalized.ContextSize,
			ModelOverrides:      modelOverrides,
			ModelInfo:           modelInfo,
			DefaultModel:        normalized.ModelName,
			SupportsVision:      normalized.SupportsVision,
			VisionModel:         normalized.VisionModel,
		},
		Retry: providers.RetryConfig{
			MaxAttempts:       3,
			BaseDelayMs:       1000,
			BackoffMultiplier: 2,
			MaxDelayMs:        10000,
			RetryableErrors:   []string{"timeout", "connection", "rate_limit"},
		},
		Cost: providers.CostConfig{
			InputTokenCost:  0.001,
			OutputTokenCost: 0.002,
			Currency:        "USD",
		},
		// Custom providers are typically subscription gateways (flat monthly
		// fee, no marginal per-token cost). Default to subscription when the
		// user's JSON omits billing_type, otherwise BillingTypeResolved()
		// falls through to its pay_per_token heuristic and the cost tracker
		// estimates a fake "charged cost" from the live pricing catalog.
		// An explicit billing_type in the user JSON is preserved as-is.
		BillingType: defaultCustomProviderBillingType(normalized.BillingType, normalized.Endpoint),
	}, nil
}

// KnownProviderInfo describes a provider the runtime already knows about,
// either from the user's custom provider config or the embedded factory.
// The `sprout custom add` wizard uses this to detect when a user is
// "registering credentials for an existing provider" rather than
// "registering a brand-new OpenAI-compatible endpoint".
type KnownProviderInfo struct {
	// Source identifies where the metadata came from.
	// "custom" = user's ~/.config/sprout/providers/<name>.json
	// "factory" = embedded config in pkg/agent_providers/configs/
	//            or upserted via refreshFromRemote
	Source string

	// Name is the canonical provider name (lowercase, trimmed).
	Name string

	// DisplayName is the friendly label shown in UI surfaces.
	DisplayName string

	// EnvVar is the environment variable the provider expects for
	// authentication (e.g. "OPENAI_API_KEY"). Empty when no auth
	// is required.
	EnvVar string

	// RequiresAPIKey reports whether the provider needs an API key.
	RequiresAPIKey bool

	// Endpoint is the chat endpoint URL when known.
	Endpoint string

	// DefaultModel is the configured default model when known.
	DefaultModel string

	// ContextSize is the configured default context size in tokens.
	ContextSize int
}

// LookupKnownProvider returns metadata for a provider the runtime knows
// about, checking both the user's custom provider config and the embedded
// factory. Returns ok=false when the name doesn't match any known provider
// — in which case the wizard should run the full URL/discovery flow.
//
// The factory lookup uses GetProviderAuthMetadata, which the runtime
// factory populates via SetProviderConfigLookup. This is safe to call
// from the wizard because configuration init() ensures the package
// compiles even without a registered factory.
func LookupKnownProvider(name string) (info KnownProviderInfo, ok bool) {
	normalized, err := CanonicalizeCustomProviderName(name)
	if err != nil {
		return KnownProviderInfo{}, false
	}

	// User's custom provider config takes precedence — it overrides
	// any embedded config the user may have customized.
	// LoadCustomProviders reads directly from the providers/ directory
	// (both SPROUT_CONFIG-scoped and global), which is where custom
	// provider JSON files live. LoadOrInitConfig/Load() only reads
	// config.json and does NOT populate CustomProviders from the
	// providers/ directory — that merge happens in the layered manager
	// path (LoadConfigWithLayers).
	customProviders, err := LoadCustomProviders()
	if err == nil {
		if custom, exists := customProviders[normalized]; exists {
			envVar := strings.TrimSpace(custom.EnvVar)
			displayName := strings.TrimSpace(custom.Name)
			if displayName == "" {
				displayName = normalized
			}
			return KnownProviderInfo{
				Source:         "custom",
				Name:           normalized,
				DisplayName:    displayName,
				EnvVar:         envVar,
				RequiresAPIKey: custom.RequiresAPIKey || envVar != "",
				Endpoint:       strings.TrimSpace(custom.Endpoint),
				DefaultModel:   strings.TrimSpace(custom.ModelName),
				ContextSize:    custom.ContextSize,
			}, true
		}
	}

	// Fallback to the embedded / factory view. This catches skill-installed
	// providers (e.g. the deepinfra defaults shipped with sprout) and
	// remote-refresh providers. We bypass GetProviderAuthMetadata because
	// it returns a synthetic default (RequiresAPIKey=true, AuthType=bearer)
	// for ANY unknown name, which would falsely mark typos as "known".
	if providerConfigLookup != nil {
		if envVar, authType, ok := providerConfigLookup(normalized); ok {
			return KnownProviderInfo{
				Source:         "factory",
				Name:           normalized,
				DisplayName:    GetProviderDisplayName(normalized),
				EnvVar:         strings.TrimSpace(envVar),
				RequiresAPIKey: authType != "" && authType != "none",
			}, true
		}
	}

	// Last resort: check the embedded configs directly. This branch is
	// only reachable when no factory lookup was registered (e.g. narrow
	// unit tests that import configuration without importing factory).
	embeddedFactory := providers.NewProviderFactory()
	if err := embeddedFactory.LoadEmbeddedConfigs(); err == nil {
		if cfg, err := embeddedFactory.GetProviderConfig(normalized); err == nil && cfg != nil {
			authType := strings.TrimSpace(cfg.Auth.Type)
			envVar := strings.TrimSpace(cfg.Auth.EnvVar)
			return KnownProviderInfo{
				Source:         "factory",
				Name:           normalized,
				DisplayName:    GetProviderDisplayName(normalized),
				EnvVar:         envVar,
				RequiresAPIKey: authType != "" && authType != "none",
			}, true
		}
	}

	return KnownProviderInfo{}, false
}
