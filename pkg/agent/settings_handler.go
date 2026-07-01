package agent

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// handleManageSettings manages application settings and provider credentials.
// Operations: get, set, list_providers, test_credential, describe, describe_all, preview.
func handleManageSettings(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	operation, err := getStringArg(args, "operation")
	if err != nil {
		return "", agenterrors.Wrapf(err, "operation is required")
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
	case "describe":
		return handleSettingsDescribe(a, args)
	case "describe_all":
		return handleSettingsDescribeAll(a, args)
	case "preview":
		return handleSettingsPreview(a, args)
	default:
		return "", agenterrors.NewValidation(fmt.Sprintf("unknown operation %q: must be one of get, set, list_providers, test_credential, describe, describe_all, preview", operation), nil)
	}
}

// handleSettingsGet retrieves a setting value by key.
func handleSettingsGet(a *Agent, args map[string]interface{}) (string, error) {
	key, err := getStringArg(args, "key")
	if err != nil {
		return "", agenterrors.Wrapf(err, "key is required for get operation")
	}

	mgr := a.GetConfigManager()
	if mgr == nil {
		return "", agenterrors.NewConfig("configuration manager not available", nil)
	}

	cfg := mgr.GetConfig()
	if cfg == nil {
		return "", agenterrors.NewConfig("configuration not loaded", nil)
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
		return "", agenterrors.Wrapf(err, "key is required for set operation")
	}

	value, err := getStringArg(args, "value")
	if err != nil {
		return "", agenterrors.Wrapf(err, "value is required for set operation")
	}

	mgr := a.GetConfigManager()
	if mgr == nil {
		return "", agenterrors.NewConfig("configuration manager not available", nil)
	}

	if err := validateSettingKey(key); err != nil {
		return "", err
	}

	err = mgr.UpdateConfig(func(cfg *configuration.Config) error {
		return setConfigValue(cfg, key, value)
	})
	if err != nil {
		return "", agenterrors.Wrapf(err, "failed to set %q", key)
	}

	return fmt.Sprintf("Setting %q updated to %q", key, value), nil
}

// handleSettingsListProviders lists available providers.
func handleSettingsListProviders(a *Agent, args map[string]interface{}) (string, error) {
	mgr := a.GetConfigManager()
	if mgr == nil {
		return "", agenterrors.NewConfig("configuration manager not available", nil)
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
		return "", agenterrors.Wrapf(err, "provider is required for test_credential operation")
	}

	provider = strings.TrimSpace(strings.ToLower(provider))
	if provider == "" {
		return "", agenterrors.NewValidation("provider cannot be empty", nil)
	}

	if configuration.HasProviderAuth(provider) {
		return fmt.Sprintf("Provider %q has valid credentials configured", provider), nil
	}
	return fmt.Sprintf("Provider %q does not have credentials configured", provider), nil
}

// settingDetail holds metadata for a setting key used by describe and describe_all.
type settingDetail struct {
	key         string
	description string
	validValues string
	getValue    func(cfg *configuration.Config) string
}

// allSettings returns the complete list of setting definitions including extended settings.
func allSettings() []settingDetail {
	return []settingDetail{
		{
			key:         "provider",
			description: "Current LLM provider",
			validValues: "openai, anthropic, deepseek, openrouter, ollama, ollama-local, lmstudio, deepinfra, cerebras, chutes, minimax, mistral, zai, or custom provider names",
			getValue:    func(cfg *configuration.Config) string { return cfg.LastUsedProvider },
		},
		{
			key:         "model",
			description: "Current model for the active provider",
			validValues: "provider-specific model name",
			getValue:    func(cfg *configuration.Config) string { m := cfg.GetModelForProvider(cfg.LastUsedProvider); return m },
		},
		{
			key:         "reasoning_effort",
			description: "Reasoning effort",
			validValues: "low, medium, high",
			getValue:    func(cfg *configuration.Config) string { return cfg.ReasoningEffort },
		},
		{
			key:         "disable_thinking",
			description: "Disable thinking mode",
			validValues: "true, false",
			getValue:    func(cfg *configuration.Config) string { return fmt.Sprintf("%v", cfg.DisableThinking) },
		},
		{
			key:         "resource_directory",
			description: "Directory for captured web/vision resources",
			validValues: "any valid file path",
			getValue:    func(cfg *configuration.Config) string { return cfg.ResourceDirectory },
		},
		{
			key:         "history_scope",
			description: "Change history scope",
			validValues: "project, global",
			getValue:    func(cfg *configuration.Config) string { return cfg.HistoryScope },
		},
		{
			key:         "ea_mode",
			description: "Executive Assistant mode",
			validValues: "interactive, queue",
			getValue:    func(cfg *configuration.Config) string { return cfg.EAMode },
		},
		{
			key:         "subagent_provider",
			description: "Provider used for subagents",
			validValues: "provider name or empty to inherit from provider",
			getValue:    func(cfg *configuration.Config) string { return cfg.SubagentProvider },
		},
		{
			key:         "subagent_model",
			description: "Model used for subagents",
			validValues: "provider-specific model name or empty to use provider default",
			getValue:    func(cfg *configuration.Config) string { return cfg.SubagentModel },
		},
		{
			key:         "default_subagent_persona",
			description: "Persona used when run_subagent is invoked without a persona argument",
			validValues: "persona ID (e.g. general, coder, reviewer) or empty to fall back to 'general'",
			getValue:    func(cfg *configuration.Config) string { return cfg.DefaultSubagentPersona },
		},
		{
			key:         "disabled_personas",
			description: "Comma-separated persona IDs hidden from /persona list and subagent spawning",
			validValues: "comma-separated persona IDs (e.g. researcher,web_scraper) or empty to enable all",
			getValue: func(cfg *configuration.Config) string {
				return strings.Join(cfg.DisabledPersonas, ",")
			},
		},
		{
			key:         "self_review_gate_mode",
			description: "Self-review gate mode",
			validValues: "off, code, always",
			getValue:    func(cfg *configuration.Config) string { return cfg.SelfReviewGateMode },
		},
		{
			key:         "output_verbosity",
			description: "How much inter-tool-call narration the UI shows",
			validValues: "compact, default, verbose",
			getValue:    func(cfg *configuration.Config) string { return cfg.OutputVerbosity },
		},
		{
			key:         "commit_provider",
			description: "Provider for commit message generation",
			validValues: "provider name or empty to inherit from provider",
			getValue:    func(cfg *configuration.Config) string { return cfg.CommitProvider },
		},
		{
			key:         "commit_model",
			description: "Model for commit message generation",
			validValues: "provider-specific model name or empty to use provider default",
			getValue:    func(cfg *configuration.Config) string { return cfg.CommitModel },
		},
		{
			key:         "review_provider",
			description: "Provider for code review commands",
			validValues: "provider name or empty to inherit from provider",
			getValue:    func(cfg *configuration.Config) string { return cfg.ReviewProvider },
		},
		{
			key:         "review_model",
			description: "Model for code review commands",
			validValues: "provider-specific model name or empty to use provider default",
			getValue:    func(cfg *configuration.Config) string { return cfg.ReviewModel },
		},
	}
}

// handleSettingsDescribe returns the description, valid values, and current value for a single setting.
func handleSettingsDescribe(a *Agent, args map[string]interface{}) (string, error) {
	key, err := getStringArg(args, "key")
	if err != nil {
		return "", agenterrors.Wrapf(err, "key is required for describe operation")
	}

	key = strings.ToLower(strings.TrimSpace(key))

	mgr := a.GetConfigManager()
	if mgr == nil {
		return "", agenterrors.NewConfig("configuration manager not available", nil)
	}

	cfg := mgr.GetConfig()
	if cfg == nil {
		return "", agenterrors.NewConfig("configuration not loaded", nil)
	}

	for _, s := range allSettings() {
		if s.key == key {
			value := s.getValue(cfg)
			if value == "" {
				value = "(not set)"
			}
			return fmt.Sprintf("%s — %s\nValid values: %s\nCurrent value: %s", s.key, s.description, s.validValues, value), nil
		}
	}

	return "", agenterrors.NewNotFound(fmt.Sprintf("setting key %q", key))
}

// handleSettingsDescribeAll returns all settings with descriptions, valid values, and current values.
func handleSettingsDescribeAll(a *Agent, args map[string]interface{}) (string, error) {
	mgr := a.GetConfigManager()
	if mgr == nil {
		return "", agenterrors.NewConfig("configuration manager not available", nil)
	}

	cfg := mgr.GetConfig()
	if cfg == nil {
		return "", agenterrors.NewConfig("configuration not loaded", nil)
	}

	var lines []string
	lines = append(lines, "Settings Overview")
	lines = append(lines, strings.Repeat("-", 70))

	for _, s := range allSettings() {
		value := s.getValue(cfg)
		if value == "" {
			value = "(not set)"
		}
		lines = append(lines, fmt.Sprintf("%-22s %s", s.key+":", s.description))
		lines = append(lines, fmt.Sprintf("  %-14s %s", "Valid values:", s.validValues))
		lines = append(lines, fmt.Sprintf("  %-14s %s", "Current:", value))
		lines = append(lines, "")
	}

	return strings.Join(lines, "\n"), nil
}

// handleSettingsPreview shows what would change before applying a setting, without actually changing it.
func handleSettingsPreview(a *Agent, args map[string]interface{}) (string, error) {
	key, err := getStringArg(args, "key")
	if err != nil {
		return "", agenterrors.Wrapf(err, "key is required for preview operation")
	}

	value, err := getStringArg(args, "value")
	if err != nil {
		return "", agenterrors.Wrapf(err, "value is required for preview operation")
	}

	key = strings.ToLower(strings.TrimSpace(key))

	mgr := a.GetConfigManager()
	if mgr == nil {
		return "", agenterrors.NewConfig("configuration manager not available", nil)
	}

	cfg := mgr.GetConfig()
	if cfg == nil {
		return "", agenterrors.NewConfig("configuration not loaded", nil)
	}

	// Get current value
	currentValue, err := getConfigValue(cfg, key)
	if err != nil {
		// Maybe it's an extended setting
		for _, s := range allSettings() {
			if s.key == key {
				currentValue = s.getValue(cfg)
				break
			}
		}
		if err != nil && currentValue == "" {
			return "", agenterrors.NewNotFound(fmt.Sprintf("setting key %q", key))
		}
	}

	if currentValue == "" {
		currentValue = "(not set)"
	}

	// Validate the proposed value by dry-running setConfigValue on a minimal copy
	// Only fields that setConfigValue reads are needed for validation
	previewCfg := configuration.Config{
		LastUsedProvider: cfg.LastUsedProvider,
		ProviderModels:   cfg.ProviderModels,
	}
	setErr := setConfigValue(&previewCfg, key, value)

	var notes []string

	// Check credential status for provider-related keys
	providerKeys := map[string]bool{"provider": true, "subagent_provider": true, "commit_provider": true, "review_provider": true}
	if providerKeys[key] && value != "" {
		provider := strings.TrimSpace(strings.ToLower(value))
		if configuration.HasProviderAuth(provider) {
			notes = append(notes, fmt.Sprintf("credential check — %s has valid credentials", provider))
		} else {
			notes = append(notes, fmt.Sprintf("WARNING — %s does not have credentials configured", provider))
		}
	}

	var result strings.Builder
	result.WriteString(fmt.Sprintf("Preview: Changing %s\n", key))
	result.WriteString(fmt.Sprintf("  Current:  %s\n", currentValue))
	result.WriteString(fmt.Sprintf("  Proposed: %s\n", value))

	if setErr != nil {
		result.WriteString(fmt.Sprintf("  ERROR: %s\n", setErr.Error()))
	} else if len(notes) > 0 {
		for _, n := range notes {
			result.WriteString(fmt.Sprintf("  Notes:  %s\n", n))
		}
	} else {
		result.WriteString("  Notes:  no issues detected\n")
	}

	return result.String(), nil
}

// supportedSettings contains the list of valid setting keys.
var supportedSettings = map[string]string{
	"provider":                 "Current LLM provider",
	"model":                    "Current model for the active provider",
	"reasoning_effort":         "Reasoning effort (low/medium/high)",
	"disable_thinking":         "Disable thinking mode (true/false)",
	"resource_directory":       "Directory for captured web/vision resources",
	"history_scope":            "Change history scope (project/global)",
	"ea_mode":                  "Coordinator persona startup mode (interactive/queue). Legacy name retained for compatibility.",
	"subagent_provider":        "Provider used for subagents",
	"subagent_model":           "Model used for subagents",
	"default_subagent_persona": "Persona used when run_subagent omits the persona argument",
	"disabled_personas":        "Comma-separated persona IDs hidden from /persona list and spawning",
	"self_review_gate_mode":    "Self-review gate mode (off/code/always)",
	"output_verbosity":         "Output verbosity level (compact/default/verbose)",
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
	return agenterrors.NewValidation(fmt.Sprintf("unknown setting key %q; valid keys: %s", key, strings.Join(validKeys, ", ")), nil)
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
	case "default_subagent_persona":
		return cfg.DefaultSubagentPersona, nil
	case "disabled_personas":
		return strings.Join(cfg.DisabledPersonas, ","), nil
	case "self_review_gate_mode":
		return cfg.SelfReviewGateMode, nil
	case "output_verbosity":
		return cfg.OutputVerbosity, nil
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
			return agenterrors.NewValidation("cannot set model: no provider selected", nil)
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
			return agenterrors.NewValidation(fmt.Sprintf("reasoning_effort must be low, medium, or high, got %q", value), nil)
		}
	case "disable_thinking":
		switch strings.ToLower(value) {
		case "true":
			cfg.DisableThinking = true
		case "false":
			cfg.DisableThinking = false
		default:
			return agenterrors.NewValidation(fmt.Sprintf("disable_thinking must be true or false, got %q", value), nil)
		}
	case "resource_directory":
		cfg.ResourceDirectory = value
	case "history_scope":
		switch strings.ToLower(value) {
		case "project", "global", "":
			cfg.HistoryScope = strings.ToLower(value)
		default:
			return agenterrors.NewValidation(fmt.Sprintf("history_scope must be project or global, got %q", value), nil)
		}
	case "ea_mode":
		switch strings.ToLower(value) {
		case "interactive", "queue", "":
			cfg.EAMode = strings.ToLower(value)
		default:
			return agenterrors.NewValidation(fmt.Sprintf("ea_mode must be interactive or queue, got %q", value), nil)
		}
	case "subagent_provider":
		cfg.SubagentProvider = value
	case "subagent_model":
		cfg.SubagentModel = value
	case "default_subagent_persona":
		v := strings.TrimSpace(value)
		if v != "" && cfg.GetSubagentType(v) == nil {
			return agenterrors.NewValidation(fmt.Sprintf("default_subagent_persona %q is not a known persona ID or alias", v), nil)
		}
		cfg.DefaultSubagentPersona = v
	case "disabled_personas":
		// Comma-separated list. Empty value clears the list.
		var ids []string
		for _, raw := range strings.Split(value, ",") {
			trimmed := strings.TrimSpace(raw)
			if trimmed == "" {
				continue
			}
			if cfg.GetSubagentType(trimmed) == nil && !cfg.IsPersonaDisabled(trimmed) {
				return agenterrors.NewValidation(fmt.Sprintf("disabled_personas: %q is not a known persona ID or alias", trimmed), nil)
			}
			ids = append(ids, trimmed)
		}
		cfg.DisabledPersonas = ids
	case "self_review_gate_mode":
		switch strings.ToLower(value) {
		case "off", "code", "always", "":
			cfg.SelfReviewGateMode = strings.ToLower(value)
		default:
			return agenterrors.NewValidation(fmt.Sprintf("self_review_gate_mode must be off, code, or always, got %q", value), nil)
		}
	case "output_verbosity":
		switch strings.ToLower(value) {
		case "compact", "default", "verbose", "":
			cfg.OutputVerbosity = strings.ToLower(value)
		default:
			return agenterrors.NewValidation(fmt.Sprintf("output_verbosity must be compact, default, or verbose, got %q", value), nil)
		}
	default:
		return validateSettingKey(key)
	}
	return nil
}
