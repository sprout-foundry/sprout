package configuration

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	providers "github.com/sprout-foundry/sprout/pkg/agent_providers"
)

const ProvidersDirName = "providers"

type ProviderDiscoveryModel struct {
	ID            string   `json:"id"`
	Name          string   `json:"name,omitempty"`
	Description   string   `json:"description,omitempty"`
	ContextLength int      `json:"context_length,omitempty"`
	Tags          []string `json:"tags,omitempty"`
}

func GetProvidersDir() (string, error) {
	configDir, err := GetConfigDir()
	if err != nil {
		return "", fmt.Errorf("failed to get config directory: %w", err)
	}
	providersDir := filepath.Join(configDir, ProvidersDirName)
	if err := os.MkdirAll(providersDir, 0700); err != nil {
		return "", fmt.Errorf("failed to create providers directory: %w", err)
	}
	return providersDir, nil
}

func GetCustomProviderPath(name string) (string, error) {
	providersDir, err := GetProvidersDir()
	if err != nil {
		return "", fmt.Errorf("failed to get providers directory: %w", err)
	}
	normalized, err := CanonicalizeCustomProviderName(name)
	if err != nil {
		return "", fmt.Errorf("failed to normalize provider name: %w", err)
	}
	return filepath.Join(providersDir, normalized+".json"), nil
}

// LoadCustomProviders loads all custom provider configs from the global
// providers directory (~/.config/sprout/providers/).
func LoadCustomProviders() (map[string]CustomProviderConfig, error) {
	providersDir, err := GetProvidersDir()
	if err != nil {
		return nil, fmt.Errorf("get providers directory: %w", err)
	}
	return LoadCustomProvidersFromDir(providersDir)
}

// LoadCustomProvidersFromDir loads all custom provider JSON files from the
// given directory. Use this when SPROUT_CONFIG is temporarily overridden
// (e.g. inside NewManagerWithLayers) and custom providers must still be read
// from the true global location.
func LoadCustomProvidersFromDir(providersDir string) (map[string]CustomProviderConfig, error) {

	files, err := filepath.Glob(filepath.Join(providersDir, "*.json"))
	if err != nil {
		return nil, fmt.Errorf("failed to list custom provider files: %w", err)
	}

	result := make(map[string]CustomProviderConfig, len(files))
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("failed to read custom provider file %s: %w", path, err)
		}

		var cfg CustomProviderConfig
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("failed to parse custom provider file %s: %w", path, err)
		}

		cfg, err = NormalizeCustomProviderConfig(cfg)
		if err != nil {
			return nil, fmt.Errorf("invalid custom provider file %s: %w", path, err)
		}
		result[cfg.Name] = cfg
	}

	return result, nil
}

func SaveCustomProvider(cfg CustomProviderConfig) error {
	normalized, err := NormalizeCustomProviderConfig(cfg)
	if err != nil {
		return fmt.Errorf("normalize custom provider config: %w", err)
	}

	path, err := GetCustomProviderPath(normalized.Name)
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal custom provider config: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("write custom provider config: %w", err)
	}

	return nil
}

func DeleteCustomProvider(name string) error {
	path, err := GetCustomProviderPath(name)
	if err != nil {
		return fmt.Errorf("get custom provider path: %w", err)
	}

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove custom provider %s: %w", name, err)
	}
	return nil
}

func DiscoverCustomProviderModels(cfg CustomProviderConfig) ([]ProviderDiscoveryModel, error) {
	normalized, err := NormalizeCustomProviderConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("normalize custom provider config: %w", err)
	}

	if normalized.Endpoint == "" {
		return nil, fmt.Errorf("endpoint URL cannot be empty")
	}

	url := strings.TrimSuffix(normalized.Endpoint, "/") + "/models"

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build discovery request: %w", err)
	}
	if normalized.EnvVar != "" {
		if key := strings.TrimSpace(os.Getenv(normalized.EnvVar)); key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("model discovery request failed: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("model discovery returned HTTP %d", resp.StatusCode)
	}

	var payload struct {
		Data []struct {
			ID            string   `json:"id"`
			Name          string   `json:"name,omitempty"`
			Description   string   `json:"description,omitempty"`
			ContextLength int      `json:"context_length,omitempty"`
			Tags          []string `json:"tags,omitempty"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode discovery response: %w", err)
	}

	models := make([]ProviderDiscoveryModel, 0, len(payload.Data))
	for _, model := range payload.Data {
		id := strings.TrimSpace(model.ID)
		if id == "" {
			continue
		}
		models = append(models, ProviderDiscoveryModel{
			ID:            id,
			Name:          strings.TrimSpace(model.Name),
			Description:   strings.TrimSpace(model.Description),
			ContextLength: model.ContextLength,
			Tags:          normalizeUniqueStrings(model.Tags),
		})
	}

	sort.Slice(models, func(i, j int) bool {
		return models[i].ID < models[j].ID
	})

	return models, nil
}

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
	if !conversion.IncludeToolCallID &&
		!conversion.ConvertToolRoleToUser &&
		conversion.ReasoningContentField == "" &&
		!conversion.ArgumentsAsJSON &&
		!conversion.SkipToolExecutionSummary &&
		conversion.ForceToolCallType == "" {
		conversion = providers.MessageConversion{
			IncludeToolCallID:        true,
			SkipToolExecutionSummary: true,
		}
	}

	// Build model overrides for context sizes
	modelOverrides := make(map[string]int)
	for modelID, contextSize := range normalized.ModelContextSizes {
		if contextSize > 0 {
			modelOverrides[modelID] = contextSize
		}
	}

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
		},
		Models: providers.ModelConfig{
			DefaultContextLimit: normalized.ContextSize,
			ModelOverrides:      modelOverrides,
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
	cfg, err := LoadOrInitConfig(false)
	if err == nil {
		if custom, exists := cfg.CustomProviders[normalized]; exists {
			envVar := strings.TrimSpace(custom.EnvVar)
			displayName := strings.TrimSpace(custom.Name)
			if displayName == "" {
				displayName = normalized
			}
			return KnownProviderInfo{
				Source:        "custom",
				Name:          normalized,
				DisplayName:   displayName,
				EnvVar:        envVar,
				RequiresAPIKey: custom.RequiresAPIKey || envVar != "",
				Endpoint:      strings.TrimSpace(custom.Endpoint),
				DefaultModel:  strings.TrimSpace(custom.ModelName),
				ContextSize:   custom.ContextSize,
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