package agent

import (
	"context"
	"fmt"
	"sort"
	"strconv"
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

// SettingDetail holds metadata for a setting key used by describe and describe_all.
type SettingDetail struct {
	Key         string
	Description string
	ValidValues string
	GetValue    func(cfg *configuration.Config) string
}

// AllSettings returns the complete list of setting definitions including extended settings.
func AllSettings() []SettingDetail {
	return []SettingDetail{
		{
			Key:         "provider",
			Description: "Current LLM provider",
			ValidValues: "openai, anthropic, deepseek, openrouter, ollama, ollama-local, lmstudio, deepinfra, cerebras, chutes, minimax, mistral, zai, or custom provider names",
			GetValue:    func(cfg *configuration.Config) string { return cfg.LastUsedProvider },
		},
		{
			Key:         "model",
			Description: "Current model for the active provider",
			ValidValues: "provider-specific model name",
			GetValue:    func(cfg *configuration.Config) string { m := cfg.GetModelForProvider(cfg.LastUsedProvider); return m },
		},
		{
			Key:         "reasoning_effort",
			Description: "Reasoning effort",
			ValidValues: "low, medium, high",
			GetValue:    func(cfg *configuration.Config) string { return cfg.ReasoningEffort },
		},
		{
			Key:         "disable_thinking",
			Description: "Disable thinking mode",
			ValidValues: "true, false",
			GetValue:    func(cfg *configuration.Config) string { return fmt.Sprintf("%v", cfg.DisableThinking) },
		},
		{
			Key:         "resource_directory",
			Description: "Directory for captured web/vision resources",
			ValidValues: "any valid file path",
			GetValue:    func(cfg *configuration.Config) string { return cfg.ResourceDirectory },
		},
		{
			Key:         "history_scope",
			Description: "Change history scope",
			ValidValues: "project, global",
			GetValue:    func(cfg *configuration.Config) string { return cfg.HistoryScope },
		},
		{
			Key:         "ea_mode",
			Description: "Executive Assistant mode",
			ValidValues: "interactive, queue",
			GetValue:    func(cfg *configuration.Config) string { return cfg.EAMode },
		},
		{
			Key:         "subagent_provider",
			Description: "Provider used for subagents",
			ValidValues: "provider name or empty to inherit from provider",
			GetValue:    func(cfg *configuration.Config) string { return cfg.SubagentProvider },
		},
		{
			Key:         "subagent_model",
			Description: "Model used for subagents",
			ValidValues: "provider-specific model name or empty to use provider default",
			GetValue:    func(cfg *configuration.Config) string { return cfg.SubagentModel },
		},
		{
			Key:         "default_subagent_persona",
			Description: "Persona used when run_subagent is invoked without a persona argument",
			ValidValues: "persona ID (e.g. general, coder, reviewer) or empty to fall back to 'general'",
			GetValue:    func(cfg *configuration.Config) string { return cfg.DefaultSubagentPersona },
		},
		{
			Key:         "disabled_personas",
			Description: "Comma-separated persona IDs hidden from /persona list and subagent spawning",
			ValidValues: "comma-separated persona IDs (e.g. researcher,web_scraper) or empty to enable all",
			GetValue: func(cfg *configuration.Config) string {
				return strings.Join(cfg.DisabledPersonas, ",")
			},
		},
		{
			Key:         "output_verbosity",
			Description: "How much inter-tool-call narration the UI shows",
			ValidValues: "compact, default, verbose",
			GetValue:    func(cfg *configuration.Config) string { return cfg.OutputVerbosity },
		},
		{
			Key:         "commit_provider",
			Description: "Provider for commit message generation",
			ValidValues: "provider name or empty to inherit from provider",
			GetValue:    func(cfg *configuration.Config) string { return cfg.CommitProvider },
		},
		{
			Key:         "commit_model",
			Description: "Model for commit message generation",
			ValidValues: "provider-specific model name or empty to use provider default",
			GetValue:    func(cfg *configuration.Config) string { return cfg.CommitModel },
		},
		{
			Key:         "review_provider",
			Description: "Provider for code review commands",
			ValidValues: "provider name or empty to inherit from provider",
			GetValue:    func(cfg *configuration.Config) string { return cfg.ReviewProvider },
		},
		{
			Key:         "review_model",
			Description: "Model for code review commands",
			ValidValues: "provider-specific model name or empty to use provider default",
			GetValue:    func(cfg *configuration.Config) string { return cfg.ReviewModel },
		},
		{
			Key:         "subagent_max_parallel",
			Description: "Maximum number of parallel subagents",
			ValidValues: "1-8",
			GetValue:    func(cfg *configuration.Config) string { return strconv.Itoa(cfg.SubagentMaxParallel) },
		},
		{
			Key:         "subagent_parallel_enabled",
			Description: "Enable parallel subagent execution",
			ValidValues: "true, false",
			GetValue: func(cfg *configuration.Config) string {
				if cfg.SubagentParallelEnabled != nil {
					return fmt.Sprintf("%v", *cfg.SubagentParallelEnabled)
				}
				return "false"
			},
		},
		{
			Key:         "subagent_max_depth",
			Description: "Maximum subagent nesting depth",
			ValidValues: "1-4",
			GetValue:    func(cfg *configuration.Config) string { return strconv.Itoa(cfg.SubagentMaxDepth) },
		},
		{
			Key:         "notifications.cli_bell",
			Description: "Terminal bell on completion",
			ValidValues: "true, false",
			GetValue: func(cfg *configuration.Config) string {
				if cfg.Notifications != nil {
					return fmt.Sprintf("%v", cfg.Notifications.CLIBell)
				}
				return "false"
			},
		},
		{
			Key:         "notifications.os_notify",
			Description: "OS desktop notification on completion",
			ValidValues: "true, false",
			GetValue: func(cfg *configuration.Config) string {
				if cfg.Notifications != nil {
					return fmt.Sprintf("%v", cfg.Notifications.OSNotify)
				}
				return "false"
			},
		},
		{
			Key:         "notifications.min_seconds",
			Description: "Min turn duration before notification (seconds)",
			ValidValues: "0-300 (supports fractional seconds)",
			GetValue: func(cfg *configuration.Config) string {
				if cfg.Notifications != nil {
					return fmt.Sprintf("%v", cfg.Notifications.MinSeconds)
				}
				return ""
			},
		},
		{
			Key:         "api_timeouts.overall_timeout_sec",
			Description: "Overall API timeout (seconds)",
			ValidValues: "60-3600",
			GetValue: func(cfg *configuration.Config) string {
				if cfg.APITimeouts != nil {
					return strconv.Itoa(cfg.APITimeouts.OverallTimeoutSec)
				}
				return ""
			},
		},
		{
			Key:         "api_timeouts.connection_timeout_sec",
			Description: "Connection timeout (seconds)",
			ValidValues: "10-600",
			GetValue: func(cfg *configuration.Config) string {
				if cfg.APITimeouts != nil {
					return strconv.Itoa(cfg.APITimeouts.ConnectionTimeoutSec)
				}
				return ""
			},
		},
		{
			Key:         "api_timeouts.first_chunk_timeout_sec",
			Description: "First chunk timeout (seconds)",
			ValidValues: "30-1200",
			GetValue: func(cfg *configuration.Config) string {
				if cfg.APITimeouts != nil {
					return strconv.Itoa(cfg.APITimeouts.FirstChunkTimeoutSec)
				}
				return ""
			},
		},
		{
			Key:         "enable_zsh_command_detection",
			Description: "Enable zsh-aware command detection",
			ValidValues: "true, false",
			GetValue:    func(cfg *configuration.Config) string { return fmt.Sprintf("%v", cfg.EnableZshCommandDetection) },
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

	for _, s := range AllSettings() {
		if s.Key == key {
			value := s.GetValue(cfg)
			if value == "" {
				value = "(not set)"
			}
			return fmt.Sprintf("%s — %s\nValid values: %s\nCurrent value: %s", s.Key, s.Description, s.ValidValues, value), nil
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

	for _, s := range AllSettings() {
		value := s.GetValue(cfg)
		if value == "" {
			value = "(not set)"
		}
		lines = append(lines, fmt.Sprintf("%-22s %s", s.Key+":", s.Description))
		lines = append(lines, fmt.Sprintf("  %-14s %s", "Valid values:", s.ValidValues))
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
		for _, s := range AllSettings() {
			if s.Key == key {
				currentValue = s.GetValue(cfg)
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

	// Validate the proposed value by dry-running setConfigValue on a shallow
	// copy of the config. We must copy the full struct so persona validation
	// (GetSubagentType / IsPersonaDisabled) has access to the real registries.
	// Deep-copy pointer fields that setConfigValue mutates in-place so the
	// preview never accidentally mutates the real config.
	previewCfg := *cfg
	if cfg.APITimeouts != nil {
		copy := *cfg.APITimeouts
		previewCfg.APITimeouts = &copy
	}
	if cfg.Notifications != nil {
		copy := *cfg.Notifications
		previewCfg.Notifications = &copy
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
	"provider":                             "Current LLM provider",
	"model":                                "Current model for the active provider",
	"reasoning_effort":                     "Reasoning effort (low/medium/high)",
	"disable_thinking":                     "Disable thinking mode (true/false)",
	"resource_directory":                   "Directory for captured web/vision resources",
	"history_scope":                        "Change history scope (project/global)",
	"ea_mode":                              "Coordinator persona startup mode (interactive/queue). Legacy name retained for compatibility.",
	"subagent_provider":                    "Provider used for subagents",
	"subagent_model":                       "Model used for subagents",
	"default_subagent_persona":             "Persona used when run_subagent omits the persona argument",
	"disabled_personas":                    "Comma-separated persona IDs hidden from /persona list and spawning",
	"output_verbosity":                     "Output verbosity level (compact/default/verbose)",
	"commit_provider":                      "Provider for commit message generation",
	"commit_model":                         "Model for commit message generation",
	"review_provider":                      "Provider for code review commands",
	"review_model":                         "Model for code review commands",
	"subagent_max_parallel":                "Maximum number of parallel subagents",
	"subagent_parallel_enabled":            "Enable parallel subagent execution",
	"subagent_max_depth":                   "Maximum subagent nesting depth",
	"notifications.cli_bell":               "Terminal bell on completion",
	"notifications.os_notify":              "OS desktop notification on completion",
	"notifications.min_seconds":            "Min turn duration before notification (seconds)",
	"api_timeouts.overall_timeout_sec":     "Overall API timeout (seconds)",
	"api_timeouts.connection_timeout_sec":  "Connection timeout (seconds)",
	"api_timeouts.first_chunk_timeout_sec": "First chunk timeout (seconds)",
	"enable_zsh_command_detection":         "Enable zsh-aware command detection",
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
	case "output_verbosity":
		return cfg.OutputVerbosity, nil
	case "commit_provider":
		return cfg.CommitProvider, nil
	case "commit_model":
		return cfg.CommitModel, nil
	case "review_provider":
		return cfg.ReviewProvider, nil
	case "review_model":
		return cfg.ReviewModel, nil
	case "subagent_max_parallel":
		return strconv.Itoa(cfg.SubagentMaxParallel), nil
	case "subagent_parallel_enabled":
		if cfg.SubagentParallelEnabled != nil {
			return fmt.Sprintf("%v", *cfg.SubagentParallelEnabled), nil
		}
		return "false", nil
	case "subagent_max_depth":
		return strconv.Itoa(cfg.SubagentMaxDepth), nil
	case "notifications.cli_bell":
		if cfg.Notifications != nil {
			return fmt.Sprintf("%v", cfg.Notifications.CLIBell), nil
		}
		return "false", nil
	case "notifications.os_notify":
		if cfg.Notifications != nil {
			return fmt.Sprintf("%v", cfg.Notifications.OSNotify), nil
		}
		return "false", nil
	case "notifications.min_seconds":
		if cfg.Notifications != nil {
			return fmt.Sprintf("%v", cfg.Notifications.MinSeconds), nil
		}
		return "", nil
	case "api_timeouts.overall_timeout_sec":
		if cfg.APITimeouts != nil {
			return strconv.Itoa(cfg.APITimeouts.OverallTimeoutSec), nil
		}
		return "", nil
	case "api_timeouts.connection_timeout_sec":
		if cfg.APITimeouts != nil {
			return strconv.Itoa(cfg.APITimeouts.ConnectionTimeoutSec), nil
		}
		return "", nil
	case "api_timeouts.first_chunk_timeout_sec":
		if cfg.APITimeouts != nil {
			return strconv.Itoa(cfg.APITimeouts.FirstChunkTimeoutSec), nil
		}
		return "", nil
	case "enable_zsh_command_detection":
		return fmt.Sprintf("%v", cfg.EnableZshCommandDetection), nil
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
	case "output_verbosity":
		switch strings.ToLower(value) {
		case "compact", "default", "verbose", "":
			cfg.OutputVerbosity = strings.ToLower(value)
		default:
			return agenterrors.NewValidation(fmt.Sprintf("output_verbosity must be compact, default, or verbose, got %q", value), nil)
		}
	case "commit_provider":
		cfg.CommitProvider = value
	case "commit_model":
		cfg.CommitModel = value
	case "review_provider":
		cfg.ReviewProvider = value
	case "review_model":
		cfg.ReviewModel = value
	case "subagent_max_parallel":
		val, err := strconv.Atoi(value)
		if err != nil {
			return agenterrors.NewValidation(fmt.Sprintf("subagent_max_parallel must be an integer, got %q", value), nil)
		}
		if val < 1 || val > 8 {
			return agenterrors.NewValidation(fmt.Sprintf("subagent_max_parallel must be between 1 and 8, got %d", val), nil)
		}
		cfg.SubagentMaxParallel = val
	case "subagent_parallel_enabled":
		switch strings.ToLower(value) {
		case "true":
			t := true
			cfg.SubagentParallelEnabled = &t
		case "false":
			f := false
			cfg.SubagentParallelEnabled = &f
		default:
			return agenterrors.NewValidation(fmt.Sprintf("subagent_parallel_enabled must be true or false, got %q", value), nil)
		}
	case "subagent_max_depth":
		val, err := strconv.Atoi(value)
		if err != nil {
			return agenterrors.NewValidation(fmt.Sprintf("subagent_max_depth must be an integer, got %q", value), nil)
		}
		if val < 1 || val > 4 {
			return agenterrors.NewValidation(fmt.Sprintf("subagent_max_depth must be between 1 and 4, got %d", val), nil)
		}
		cfg.SubagentMaxDepth = val
	case "notifications.cli_bell":
		switch strings.ToLower(value) {
		case "true":
			if cfg.Notifications == nil {
				cfg.Notifications = &configuration.NotificationsConfig{}
			}
			cfg.Notifications.CLIBell = true
		case "false":
			if cfg.Notifications == nil {
				cfg.Notifications = &configuration.NotificationsConfig{}
			}
			cfg.Notifications.CLIBell = false
		default:
			return agenterrors.NewValidation(fmt.Sprintf("notifications.cli_bell must be true or false, got %q", value), nil)
		}
	case "notifications.os_notify":
		switch strings.ToLower(value) {
		case "true":
			if cfg.Notifications == nil {
				cfg.Notifications = &configuration.NotificationsConfig{}
			}
			cfg.Notifications.OSNotify = true
		case "false":
			if cfg.Notifications == nil {
				cfg.Notifications = &configuration.NotificationsConfig{}
			}
			cfg.Notifications.OSNotify = false
		default:
			return agenterrors.NewValidation(fmt.Sprintf("notifications.os_notify must be true or false, got %q", value), nil)
		}
	case "notifications.min_seconds":
		val, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return agenterrors.NewValidation(fmt.Sprintf("notifications.min_seconds must be a number, got %q", value), nil)
		}
		if val < 0 || val > 300 {
			return agenterrors.NewValidation(fmt.Sprintf("notifications.min_seconds must be between 0 and 300, got %v", val), nil)
		}
		if cfg.Notifications == nil {
			cfg.Notifications = &configuration.NotificationsConfig{}
		}
		cfg.Notifications.MinSeconds = val
	case "api_timeouts.overall_timeout_sec":
		val, err := strconv.Atoi(value)
		if err != nil {
			return agenterrors.NewValidation(fmt.Sprintf("api_timeouts.overall_timeout_sec must be an integer, got %q", value), nil)
		}
		if val < 60 || val > 3600 {
			return agenterrors.NewValidation(fmt.Sprintf("api_timeouts.overall_timeout_sec must be between 60 and 3600, got %d", val), nil)
		}
		if cfg.APITimeouts == nil {
			cfg.APITimeouts = &configuration.APITimeoutConfig{}
		}
		cfg.APITimeouts.OverallTimeoutSec = val
	case "api_timeouts.connection_timeout_sec":
		val, err := strconv.Atoi(value)
		if err != nil {
			return agenterrors.NewValidation(fmt.Sprintf("api_timeouts.connection_timeout_sec must be an integer, got %q", value), nil)
		}
		if val < 10 || val > 600 {
			return agenterrors.NewValidation(fmt.Sprintf("api_timeouts.connection_timeout_sec must be between 10 and 600, got %d", val), nil)
		}
		if cfg.APITimeouts == nil {
			cfg.APITimeouts = &configuration.APITimeoutConfig{}
		}
		cfg.APITimeouts.ConnectionTimeoutSec = val
	case "api_timeouts.first_chunk_timeout_sec":
		val, err := strconv.Atoi(value)
		if err != nil {
			return agenterrors.NewValidation(fmt.Sprintf("api_timeouts.first_chunk_timeout_sec must be an integer, got %q", value), nil)
		}
		if val < 30 || val > 1200 {
			return agenterrors.NewValidation(fmt.Sprintf("api_timeouts.first_chunk_timeout_sec must be between 30 and 1200, got %d", val), nil)
		}
		if cfg.APITimeouts == nil {
			cfg.APITimeouts = &configuration.APITimeoutConfig{}
		}
		cfg.APITimeouts.FirstChunkTimeoutSec = val
	case "enable_zsh_command_detection":
		switch strings.ToLower(value) {
		case "true":
			cfg.EnableZshCommandDetection = true
		case "false":
			cfg.EnableZshCommandDetection = false
		default:
			return agenterrors.NewValidation(fmt.Sprintf("enable_zsh_command_detection must be true or false, got %q", value), nil)
		}
	default:
		return validateSettingKey(key)
	}
	return nil
}

// GetSettingValue returns the string representation of a config setting by key.
// It's an exported wrapper around getConfigValue for use by other packages.
func GetSettingValue(cfg *configuration.Config, key string) (string, error) {
	return getConfigValue(cfg, key)
}

// SetSettingValue updates a config setting by key and value string.
// It's an exported wrapper around setConfigValue for use by other packages.
func SetSettingValue(cfg *configuration.Config, key, value string) error {
	return setConfigValue(cfg, key, value)
}

// SupportedSettingKeys returns a sorted slice of all supported setting keys.
func SupportedSettingKeys() []string {
	keys := make([]string, 0, len(supportedSettings))
	for k := range supportedSettings {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
