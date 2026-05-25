package agent

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// handleManageSettings manages application settings and provider credentials.
// Operations: get, set, list_providers, test_credential.
func handleManageSettings(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	operation, err := getStringArg(args, "operation")
	if err != nil {
		return "", fmt.Errorf("operation is required: %w", err)
	}

	switch operation {
	case "get":
		return handleSettingsGet(a, args)
	case "set":
		return handleSettingsSet(a, args)
	case "list_providers":
		return handleSettingsListProviders(a, args)
	case "test_credential":
		return handleSettingsTestCredential(a, args)
	default:
		return "", fmt.Errorf("unknown operation %q: must be one of get, set, list_providers, test_credential", operation)
	}
}

// handleSettingsGet retrieves a setting value by key.
func handleSettingsGet(a *Agent, args map[string]interface{}) (string, error) {
	key, err := getStringArg(args, "key")
	if err != nil {
		return "", fmt.Errorf("key is required for get operation: %w", err)
	}

	mgr := a.GetConfigManager()
	if mgr == nil {
		return "", fmt.Errorf("configuration manager not available")
	}

	cfg := mgr.GetConfig()
	if cfg == nil {
		return "", fmt.Errorf("configuration not loaded")
	}

	value, err := getConfigValue(cfg, key)
	if err != nil {
		return "", err
	}

	if value == "" {
		return fmt.Sprintf("Setting %q is not set", key), nil
	}
	return value, nil
}

// handleSettingsSet updates a setting value by key.
func handleSettingsSet(a *Agent, args map[string]interface{}) (string, error) {
	key, err := getStringArg(args, "key")
	if err != nil {
		return "", fmt.Errorf("key is required for set operation: %w", err)
	}

	value, err := getStringArg(args, "value")
	if err != nil {
		return "", fmt.Errorf("value is required for set operation: %w", err)
	}

	mgr := a.GetConfigManager()
	if mgr == nil {
		return "", fmt.Errorf("configuration manager not available")
	}

	if err := validateSettingKey(key); err != nil {
		return "", err
	}

	err = mgr.UpdateConfig(func(cfg *configuration.Config) error {
		return setConfigValue(cfg, key, value)
	})
	if err != nil {
		return "", fmt.Errorf("failed to set %q: %w", key, err)
	}

	return fmt.Sprintf("Setting %q updated to %q", key, value), nil
}

// handleSettingsListProviders lists available providers.
func handleSettingsListProviders(a *Agent, args map[string]interface{}) (string, error) {
	mgr := a.GetConfigManager()
	if mgr == nil {
		return "", fmt.Errorf("configuration manager not available")
	}

	providers := mgr.GetAvailableProviders()
	if len(providers) == 0 {
		return "No providers available", nil
	}

	// Optionally filter by a provider argument
	filter, _ := args["provider"].(string)
	filter = strings.TrimSpace(strings.ToLower(filter))

	var names []string
	for _, p := range providers {
		name := string(p)
		if filter != "" && !strings.Contains(strings.ToLower(name), filter) {
			continue
		}
		names = append(names, name)
	}

	sort.Strings(names)

	var lines []string
	lines = append(lines, fmt.Sprintf("Available providers (%d):", len(names)))
	for _, n := range names {
		lines = append(lines, fmt.Sprintf("  - %s", n))
	}

	return strings.Join(lines, "\n"), nil
}

// handleSettingsTestCredential tests whether a provider has valid credentials configured.
func handleSettingsTestCredential(a *Agent, args map[string]interface{}) (string, error) {
	provider, err := getStringArg(args, "provider")
	if err != nil {
		return "", fmt.Errorf("provider is required for test_credential operation: %w", err)
	}

	provider = strings.TrimSpace(strings.ToLower(provider))
	if provider == "" {
		return "", fmt.Errorf("provider cannot be empty")
	}

	if configuration.HasProviderAuth(provider) {
		return fmt.Sprintf("Provider %q has valid credentials configured", provider), nil
	}
	return fmt.Sprintf("Provider %q does not have credentials configured", provider), nil
}

// supportedSettings contains the list of valid setting keys.
var supportedSettings = map[string]string{
	"provider":              "Current LLM provider",
	"model":                 "Current model for the active provider",
	"reasoning_effort":      "Reasoning effort (low/medium/high)",
	"disable_thinking":      "Disable thinking mode (true/false)",
	"resource_directory":    "Directory for captured web/vision resources",
	"history_scope":         "Change history scope (project/global)",
	"ea_mode":               "Executive Assistant mode (interactive/queue)",
	"subagent_provider":     "Provider used for subagents",
	"subagent_model":        "Model used for subagents",
	"self_review_gate_mode": "Self-review gate mode (off/code/always)",
}

// validateSettingKey checks that a key is a recognized setting.
func validateSettingKey(key string) error {
	normalized := strings.ToLower(key)
	if _, ok := supportedSettings[normalized]; ok {
		return nil
	}
	validKeys := make([]string, 0, len(supportedSettings))
	for k := range supportedSettings {
		validKeys = append(validKeys, k)
	}
	sort.Strings(validKeys)
	return fmt.Errorf("unknown setting key %q; valid keys: %s", key, strings.Join(validKeys, ", "))
}

// getConfigValue returns the string representation of a config setting by key.
func getConfigValue(cfg *configuration.Config, key string) (string, error) {
	switch strings.ToLower(key) {
	case "provider":
		return cfg.LastUsedProvider, nil
	case "model":
		if cfg.LastUsedProvider != "" {
			if m, ok := cfg.ProviderModels[cfg.LastUsedProvider]; ok {
				return m, nil
			}
		}
		return "", nil
	case "reasoning_effort":
		return cfg.ReasoningEffort, nil
	case "disable_thinking":
		return fmt.Sprintf("%v", cfg.DisableThinking), nil
	case "resource_directory":
		return cfg.ResourceDirectory, nil
	case "history_scope":
		return cfg.HistoryScope, nil
	case "ea_mode":
		return cfg.EAMode, nil
	case "subagent_provider":
		return cfg.SubagentProvider, nil
	case "subagent_model":
		return cfg.SubagentModel, nil
	case "self_review_gate_mode":
		return cfg.SelfReviewGateMode, nil
	default:
		return "", validateSettingKey(key)
	}
}

// setConfigValue updates a config setting by key and value string.
func setConfigValue(cfg *configuration.Config, key, value string) error {
	switch strings.ToLower(key) {
	case "provider":
		cfg.LastUsedProvider = value
	case "model":
		if cfg.LastUsedProvider == "" {
			return fmt.Errorf("cannot set model: no provider selected")
		}
		if cfg.ProviderModels == nil {
			cfg.ProviderModels = make(map[string]string)
		}
		cfg.ProviderModels[cfg.LastUsedProvider] = value
	case "reasoning_effort":
		switch strings.ToLower(value) {
		case "low", "medium", "high", "":
			cfg.ReasoningEffort = strings.ToLower(value)
		default:
			return fmt.Errorf("reasoning_effort must be low, medium, or high, got %q", value)
		}
	case "disable_thinking":
		switch strings.ToLower(value) {
		case "true":
			cfg.DisableThinking = true
		case "false":
			cfg.DisableThinking = false
		default:
			return fmt.Errorf("disable_thinking must be true or false, got %q", value)
		}
	case "resource_directory":
		cfg.ResourceDirectory = value
	case "history_scope":
		switch strings.ToLower(value) {
		case "project", "global", "":
			cfg.HistoryScope = strings.ToLower(value)
		default:
			return fmt.Errorf("history_scope must be project or global, got %q", value)
		}
	case "ea_mode":
		switch strings.ToLower(value) {
		case "interactive", "queue", "":
			cfg.EAMode = strings.ToLower(value)
		default:
			return fmt.Errorf("ea_mode must be interactive or queue, got %q", value)
		}
	case "subagent_provider":
		cfg.SubagentProvider = value
	case "subagent_model":
		cfg.SubagentModel = value
	case "self_review_gate_mode":
		switch strings.ToLower(value) {
		case "off", "code", "always", "":
			cfg.SelfReviewGateMode = strings.ToLower(value)
		default:
			return fmt.Errorf("self_review_gate_mode must be off, code, or always, got %q", value)
		}
	default:
		return validateSettingKey(key)
	}
	return nil
}
