package configuration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	providers "github.com/alantheprice/ledit/pkg/agent_providers"
	"github.com/alantheprice/ledit/pkg/credentials"
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

func LoadCustomProviders() (map[string]CustomProviderConfig, error) {
	providersDir, err := GetProvidersDir()
	if err != nil {
		return nil, fmt.Errorf("get providers directory: %w", err)
	}

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
		return fmt.Errorf("normalize custom provider: %w", err)
	}

	path, err := GetCustomProviderPath(normalized.Name)
	if err != nil {
		return fmt.Errorf("get custom provider path: %w", err)
	}

	data, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal custom provider: %w", err)
	}

	return os.WriteFile(path, data, 0600)
}

func DeleteCustomProvider(name string) error {
	path, err := GetCustomProviderPath(name)
	if err != nil {
		return fmt.Errorf("get custom provider path: %w", err)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove custom provider %s: %w", name, err)
	}
	return nil
}

func MigrateLegacyCustomProviders(cfg *Config) (map[string]CustomProviderConfig, error) {
	if cfg == nil || len(cfg.CustomProviders) == 0 {
		return LoadCustomProviders()
	}

	fileProviders, err := LoadCustomProviders()
	if err != nil {
		return nil, fmt.Errorf("load custom providers: %w", err)
	}

	for name, provider := range cfg.CustomProviders {
		if _, exists := fileProviders[name]; exists {
			continue
		}
		legacy := provider
		if legacy.Name == "" {
			legacy.Name = name
		}
		if err := SaveCustomProvider(legacy); err != nil {
			return nil, fmt.Errorf("failed to migrate custom provider %s: %w", name, err)
		}
		fileProviders[name] = legacy
	}

	return fileProviders, nil
}

func NormalizeCustomProviderConfig(cfg CustomProviderConfig) (CustomProviderConfig, error) {
	name, err := CanonicalizeCustomProviderName(cfg.Name)
	if err != nil {
		return CustomProviderConfig{}, fmt.Errorf("canonicalize provider name: %w", err)
	}

	endpoint, err := normalizeOpenAIEndpoint(cfg.Endpoint)
	if err != nil {
		return CustomProviderConfig{}, fmt.Errorf("normalize endpoint: %w", err)
	}

	cfg.Name = name
	cfg.Endpoint = endpoint
	cfg.EnvVar = strings.TrimSpace(cfg.EnvVar)
	cfg.ModelName = strings.TrimSpace(cfg.ModelName)
	cfg.ReasoningEffort = strings.ToLower(strings.TrimSpace(cfg.ReasoningEffort))
	cfg.VisionModel = strings.TrimSpace(cfg.VisionModel)
	cfg.VisionFallbackProvider = strings.TrimSpace(cfg.VisionFallbackProvider)
	cfg.VisionFallbackModel = strings.TrimSpace(cfg.VisionFallbackModel)
	cfg.ToolCalls = normalizeUniqueStrings(cfg.ToolCalls)

	// Initialize model context sizes map if nil
	if cfg.ModelContextSizes == nil {
		cfg.ModelContextSizes = make(map[string]int)
	}

	if cfg.ContextSize <= 0 {
		cfg.ContextSize = 32768
	}
	if cfg.EnvVar != "" {
		cfg.RequiresAPIKey = true
	}
	if cfg.ChunkTimeoutMs <= 0 {
		cfg.ChunkTimeoutMs = 300000
	}

	return cfg, nil
}

func DiscoverCustomProviderModels(cfg CustomProviderConfig) ([]ProviderDiscoveryModel, error) {
	normalized, err := NormalizeCustomProviderConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("normalize custom provider config: %w", err)
	}

	req, err := http.NewRequest(http.MethodGet, normalized.ModelsEndpoint(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create discovery request: %w", err)
	}

	if resolved, err := credentials.ResolveProvider(normalized.Name); err == nil && strings.TrimSpace(resolved.Value) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(resolved.Value))
	} else if strings.TrimSpace(normalized.APIKey) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(normalized.APIKey))
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("models endpoint returned HTTP %d", resp.StatusCode)
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
		return nil, fmt.Errorf("failed to decode models response: %w", err)
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
	if normalized.RequiresAPIKey || normalized.EnvVar != "" || normalized.APIKey != "" {
		authType = "api_key"
	}

	conversion := normalized.Conversion
	if !conversion.IncludeToolCallId &&
		!conversion.ConvertToolRoleToUser &&
		conversion.ReasoningContentField == "" &&
		!conversion.ArgumentsAsJSON &&
		!conversion.SkipToolExecutionSummary &&
		conversion.ForceToolCallType == "" {
		conversion = providers.MessageConversion{
			IncludeToolCallId:        true,
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
			Key:    normalized.APIKey,
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
	}, nil
}

func CanonicalizeCustomProviderName(name string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if normalized == "" {
		return "", fmt.Errorf("provider name cannot be empty")
	}
	for _, r := range normalized {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return "", fmt.Errorf("provider name must contain only lowercase letters, numbers, '-' or '_'")
	}
	return normalized, nil
}

func normalizeOpenAIEndpoint(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("endpoint cannot be empty")
	}

	u, err := url.Parse(trimmed)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("endpoint must be a valid absolute URL")
	}

	path := strings.TrimRight(u.Path, "/")
	switch {
	case path == "":
		path = "/v1/chat/completions"
	case strings.HasSuffix(path, "/v1"):
		path += "/chat/completions"
	case strings.HasSuffix(path, "/v1/models"):
		path = strings.TrimSuffix(path, "/models") + "/chat/completions"
	case strings.HasSuffix(path, "/v1/chat/completions"):
	default:
		if strings.HasSuffix(path, "/models") {
			path = strings.TrimSuffix(path, "/models") + "/chat/completions"
		} else if strings.HasSuffix(path, "/chat/completions") {
		} else {
			path += "/chat/completions"
		}
	}

	u.Path = path
	u.RawPath = ""
	u.RawQuery = ""
	u.Fragment = ""
	return strings.TrimRight(u.String(), "/"), nil
}

func normalizeUniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}
