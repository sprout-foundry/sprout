package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	provider_factory "github.com/sprout-foundry/sprout/pkg/agent_providers"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// manageSettingsHandler implements ToolHandler for the manage_settings tool.
// Dispatches on `operation` (get, set, list_providers, test_credential,
// describe, describe_all, preview).
type manageSettingsHandler struct{}

func (h *manageSettingsHandler) Name() string { return "manage_settings" }

func (h *manageSettingsHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name: "manage_settings",
		Description: "Manage application settings and provider credentials. " +
			"Supports get, set, list_providers, test_credential, describe, describe_all, and preview. " +
			"When list_providers is called with a provider filter, it also shows available models for that provider.",
		Required: []string{"operation"},
		Parameters: []ParameterDef{
			{Name: "operation", Type: "string", Required: true,
				Description: "Operation: get, set, list_providers, test_credential, describe, describe_all, or preview"},
			{Name: "key", Type: "string",
				Description: "Setting key (required for get/set/describe/preview). Examples: provider, model, subagent_provider, commit_model"},
			{Name: "value", Type: "string",
				Description: "Setting value (required for set/preview)"},
			{Name: "provider", Type: "string",
				Description: "Provider name (required for test_credential, optional filter for list_providers; when filter provided, also shows available models)"},
		},
	}
}

func (h *manageSettingsHandler) Validate(args map[string]any) error {
	op, _ := extractString(args, "operation")
	if op == "" {
		return fmt.Errorf("manage_settings: 'operation' is required")
	}
	op = strings.TrimSpace(strings.ToLower(op))

	switch op {
	case "get", "describe":
		if _, err := extractString(args, "key"); err != nil {
			return fmt.Errorf("manage_settings: 'key' is required for %s", op)
		}
	case "set":
		if _, err := extractString(args, "key"); err != nil {
			return fmt.Errorf("manage_settings: 'key' is required for set")
		}
		if _, err := extractString(args, "value"); err != nil {
			return fmt.Errorf("manage_settings: 'value' is required for set")
		}
	case "preview":
		if _, err := extractString(args, "key"); err != nil {
			return fmt.Errorf("manage_settings: 'key' is required for preview")
		}
		if _, err := extractString(args, "value"); err != nil {
			return fmt.Errorf("manage_settings: 'value' is required for preview")
		}
	case "test_credential":
		if _, err := extractString(args, "provider"); err != nil {
			return fmt.Errorf("manage_settings: 'provider' is required for test_credential")
		}
	case "list_providers", "describe_all":
		// No required params
	default:
		return fmt.Errorf("manage_settings: unknown operation %q", op)
	}
	return nil
}

func (h *manageSettingsHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	op, _ := extractString(args, "operation")
	op = strings.TrimSpace(strings.ToLower(op))

	cfgMgr := env.ConfigManager
	if cfgMgr == nil {
		return ToolResult{Output: "configuration manager not available", IsError: true}, nil
	}

	switch op {
	case "get":
		return h.handleGet(cfgMgr, args)
	case "set":
		return h.handleSet(cfgMgr, args)
	case "list_providers":
		return h.handleListProviders(cfgMgr, args)
	case "test_credential":
		return h.handleTestCredential(args)
	case "describe":
		return h.handleDescribe(cfgMgr, args)
	case "describe_all":
		return h.handleDescribeAll(cfgMgr)
	case "preview":
		return h.handlePreview(cfgMgr, args)
	default:
		return ToolResult{
			Output:  fmt.Sprintf("manage_settings: unknown operation %q", op),
			IsError: true,
		}, nil
	}
}

// handleGet retrieves a setting value by key.
func (h *manageSettingsHandler) handleGet(mgr *configuration.Manager, args map[string]any) (ToolResult, error) {
	key, _ := extractString(args, "key")
	cfg := mgr.GetConfig()
	if cfg == nil {
		return ToolResult{Output: "configuration not loaded", IsError: true}, nil
	}
	value, err := getConfigField(cfg, key)
	if err != nil {
		return ToolResult{Output: err.Error(), IsError: true}, nil
	}
	if value == "" {
		return ToolResult{Output: fmt.Sprintf("Setting %q is not set", key)}, nil
	}
	return ToolResult{Output: value}, nil
}

// handleSet updates a setting value by key.
func (h *manageSettingsHandler) handleSet(mgr *configuration.Manager, args map[string]any) (ToolResult, error) {
	key, _ := extractString(args, "key")
	value, _ := extractString(args, "value")

	if err := validateSettingKey(key); err != nil {
		return ToolResult{Output: err.Error(), IsError: true}, nil
	}

	err := mgr.UpdateConfig(func(cfg *configuration.Config) error {
		return setConfigField(cfg, key, value)
	})
	if err != nil {
		return ToolResult{Output: fmt.Sprintf("failed to set %q: %v", key, err), IsError: true}, nil
	}
	return ToolResult{Output: fmt.Sprintf("Setting %q updated to %q", key, value)}, nil
}

// handleListProviders lists available providers.
func (h *manageSettingsHandler) handleListProviders(mgr *configuration.Manager, args map[string]any) (ToolResult, error) {
	availableProviders := mgr.GetAvailableProviders()
	if len(availableProviders) == 0 {
		return ToolResult{Output: "No providers available"}, nil
	}

	filter, _ := extractString(args, "provider")
	filter = strings.TrimSpace(strings.ToLower(filter))

	var names []string
	for _, p := range availableProviders {
		name := string(p)
		if filter != "" && !strings.Contains(strings.ToLower(name), filter) {
			continue
		}
		names = append(names, name)
	}

	sort.Strings(names)
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Available providers (%d):\n", len(names)))
	for _, n := range names {
		sb.WriteString(fmt.Sprintf("  - %s\n", n))
	}

	// When a specific provider filter is given, also show its models.
	if filter != "" && len(names) > 0 {
		allModels := provider_factory.GlobalFactory().ListProvidersWithModels()
		// Find the matching provider(s) in the model map
		for _, name := range names {
			if models, ok := allModels[name]; ok {
				sb.WriteString(fmt.Sprintf("\nModels for %s (%d):\n", name, len(models)))
				for _, m := range models {
					sb.WriteString(fmt.Sprintf("  - %s\n", m))
				}
			}
		}
	}

	return ToolResult{Output: sb.String()}, nil
}

// handleTestCredential tests whether a provider has credentials configured.
func (h *manageSettingsHandler) handleTestCredential(args map[string]any) (ToolResult, error) {
	provider, _ := extractString(args, "provider")
	provider = strings.TrimSpace(strings.ToLower(provider))
	if provider == "" {
		return ToolResult{Output: "provider cannot be empty", IsError: true}, nil
	}

	if configuration.HasProviderAuth(provider) {
		return ToolResult{Output: fmt.Sprintf("Provider %q has valid credentials configured", provider)}, nil
	}
	return ToolResult{Output: fmt.Sprintf("Provider %q does not have credentials configured", provider)}, nil
}

// handleDescribe returns a description of a single setting.
func (h *manageSettingsHandler) handleDescribe(mgr *configuration.Manager, args map[string]any) (ToolResult, error) {
	key, _ := extractString(args, "key")
	key = strings.ToLower(strings.TrimSpace(key))

	cfg := mgr.GetConfig()
	if cfg == nil {
		return ToolResult{Output: "configuration not loaded", IsError: true}, nil
	}

	value, err := getConfigField(cfg, key)
	if err != nil {
		return ToolResult{Output: err.Error(), IsError: true}, nil
	}
	if value == "" {
		value = "(not set)"
	}
	return ToolResult{Output: fmt.Sprintf("%s\nCurrent value: %s", key, value)}, nil
}

// handleDescribeAll returns all settings with descriptions, valid values, and current values.
func (h *manageSettingsHandler) handleDescribeAll(mgr *configuration.Manager) (ToolResult, error) {
	cfg := mgr.GetConfig()
	if cfg == nil {
		return ToolResult{Output: "configuration not loaded", IsError: true}, nil
	}

	keys := settingKeys()
	var sb strings.Builder
	sb.WriteString("Settings Overview\n")
	sb.WriteString(strings.Repeat("-", 70) + "\n")

	for _, key := range keys {
		value, err := getConfigField(cfg, key)
		if err != nil {
			value = fmt.Sprintf("(error: %v)", err)
		} else if value == "" {
			value = "(not set)"
		}
		sb.WriteString(fmt.Sprintf("%-22s %s\n", key+":", value))
	}
	return ToolResult{Output: sb.String()}, nil
}

// handlePreview shows what would change before applying a setting.
func (h *manageSettingsHandler) handlePreview(mgr *configuration.Manager, args map[string]any) (ToolResult, error) {
	key, _ := extractString(args, "key")
	value, _ := extractString(args, "value")
	key = strings.ToLower(strings.TrimSpace(key))

	cfg := mgr.GetConfig()
	if cfg == nil {
		return ToolResult{Output: "configuration not loaded", IsError: true}, nil
	}

	currentValue, err := getConfigField(cfg, key)
	if err != nil {
		return ToolResult{Output: err.Error(), IsError: true}, nil
	}
	if currentValue == "" {
		currentValue = "(not set)"
	}

	// Check credential status for provider-related keys
	var notes []string
	providerKeys := map[string]bool{"provider": true, "subagent_provider": true, "commit_provider": true, "review_provider": true}
	if providerKeys[key] && value != "" {
		p := strings.TrimSpace(strings.ToLower(value))
		if configuration.HasProviderAuth(p) {
			notes = append(notes, fmt.Sprintf("credential check — %s has valid credentials", p))
		} else {
			notes = append(notes, fmt.Sprintf("WARNING — %s does not have credentials configured", p))
		}
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Preview: Changing %s\n", key))
	sb.WriteString(fmt.Sprintf("  Current:  %s\n", currentValue))
	sb.WriteString(fmt.Sprintf("  Proposed: %s\n", value))

	if len(notes) > 0 {
		for _, n := range notes {
			sb.WriteString(fmt.Sprintf("  Notes:  %s\n", n))
		}
	} else {
		sb.WriteString("  Notes:  no issues detected\n")
	}
	return ToolResult{Output: sb.String()}, nil
}

func (h *manageSettingsHandler) Aliases() []string      { return nil }
func (h *manageSettingsHandler) Timeout() time.Duration { return 0 }
func (h *manageSettingsHandler) MaxResultSize() int     { return 0 }
func (h *manageSettingsHandler) SafeForParallel() bool  { return false }
func (h *manageSettingsHandler) Interactive() bool      { return false }

// ---------------------------------------------------------------------------
// Config field helpers (adapted from pkg/agent/settings_handler.go to avoid
// circular imports)
// ---------------------------------------------------------------------------

// supportedSettings contains the list of valid setting keys.
var supportedSettings = map[string]string{
	"provider":                 "Current LLM provider",
	"model":                    "Current model for the active provider",
	"reasoning_effort":         "Reasoning effort (low/medium/high)",
	"disable_thinking":         "Disable thinking mode (true/false)",
	"resource_directory":       "Directory for captured web/vision resources",
	"history_scope":            "Change history scope (project/global)",
	"subagent_provider":        "Provider used for subagents",
	"subagent_model":           "Model used for subagents",
	"default_subagent_persona": "Persona used when run_subagent omits the persona argument",
	"disabled_personas":        "Comma-separated persona IDs hidden from /persona list and spawning",
	"output_verbosity":         "Output verbosity level (compact/default/verbose)",
	"commit_provider":          "Provider for commit message generation",
	"commit_model":             "Model for commit message generation",
	"review_provider":          "Provider for code review commands",
	"review_model":             "Model for code review commands",
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

// settingKeys returns a sorted list of all supported setting keys.
func settingKeys() []string {
	keys := make([]string, 0, len(supportedSettings))
	for k := range supportedSettings {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// getConfigField returns the string representation of a config setting by key.
func getConfigField(cfg *configuration.Config, key string) (string, error) {
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
	default:
		return "", validateSettingKey(key)
	}
}

// setConfigField updates a config setting by key and value string.
func setConfigField(cfg *configuration.Config, key, value string) error {
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
	case "subagent_provider":
		cfg.SubagentProvider = value
	case "subagent_model":
		cfg.SubagentModel = value
	case "default_subagent_persona":
		cfg.DefaultSubagentPersona = strings.TrimSpace(value)
	case "disabled_personas":
		var ids []string
		for _, raw := range strings.Split(value, ",") {
			trimmed := strings.TrimSpace(raw)
			if trimmed == "" {
				continue
			}
			ids = append(ids, trimmed)
		}
		cfg.DisabledPersonas = ids
	case "output_verbosity":
		switch strings.ToLower(value) {
		case "compact", "default", "verbose", "":
			cfg.OutputVerbosity = strings.ToLower(value)
		default:
			return fmt.Errorf("output_verbosity must be compact, default, or verbose, got %q", value)
		}
	case "commit_provider":
		cfg.CommitProvider = value
	case "commit_model":
		cfg.CommitModel = value
	case "review_provider":
		cfg.ReviewProvider = value
	case "review_model":
		cfg.ReviewModel = value
	default:
		return validateSettingKey(key)
	}
	return nil
}
