package agent

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// settingDef is the single source of truth for a configurable setting.
// All exported APIs (AllSettings, getConfigValue, setConfigValue, etc.)
// are derived from the settingDefs registry below.
type settingDef struct {
	Key         string
	Description string
	ValidValues string
	GetValue    func(cfg *configuration.Config) string
	SetValue    func(cfg *configuration.Config, value string) error
	EnumValues  []string // empty = not an enum
}

// settingDefs is the single registry of all configurable settings.
// Every other data structure (supportedSettings map, AllSettings slice,
// getConfigValue/setConfigValue switches) is derived from this slice.
var settingDefs = []settingDef{
	// --- Provider & Model ---
	{
		Key:         "provider",
		Description: "Current LLM provider",
		ValidValues: "openai, anthropic, deepseek, openrouter, ollama, ollama-local, lmstudio, deepinfra, cerebras, chutes, minimax, mistral, zai, or custom provider names",
		GetValue:    func(cfg *configuration.Config) string { return cfg.LastUsedProvider },
		SetValue: func(cfg *configuration.Config, value string) error {
			cfg.LastUsedProvider = value
			return nil
		},
	},
	{
		Key:         "model",
		Description: "Current model for the active provider",
		ValidValues: "provider-specific model name",
		GetValue: func(cfg *configuration.Config) string {
			if cfg.LastUsedProvider != "" {
				if m, ok := cfg.ProviderModels[cfg.LastUsedProvider]; ok {
					return m
				}
			}
			return ""
		},
		SetValue: func(cfg *configuration.Config, value string) error {
			if cfg.LastUsedProvider == "" {
				return agenterrors.NewValidation("cannot set model: no provider selected", nil)
			}
			if cfg.ProviderModels == nil {
				cfg.ProviderModels = make(map[string]string)
			}
			cfg.ProviderModels[cfg.LastUsedProvider] = value
			return nil
		},
	},
	// --- Reasoning & Thinking ---
	{
		Key:         "reasoning_effort",
		Description: "Reasoning effort",
		ValidValues: "low, medium, high",
		GetValue:    func(cfg *configuration.Config) string { return cfg.ReasoningEffort },
		SetValue: func(cfg *configuration.Config, value string) error {
			switch strings.ToLower(value) {
			case "low", "medium", "high", "":
				cfg.ReasoningEffort = strings.ToLower(value)
				return nil
			default:
				return agenterrors.NewValidation(fmt.Sprintf("reasoning_effort must be low, medium, or high, got %q", value), nil)
			}
		},
		EnumValues: []string{"low", "medium", "high"},
	},
	{
		Key:         "disable_thinking",
		Description: "Disable thinking mode",
		ValidValues: "true, false",
		GetValue:    func(cfg *configuration.Config) string { return fmt.Sprintf("%v", cfg.DisableThinking) },
		SetValue: func(cfg *configuration.Config, value string) error {
			switch strings.ToLower(value) {
			case "true":
				cfg.DisableThinking = true
				return nil
			case "false":
				cfg.DisableThinking = false
				return nil
			default:
				return agenterrors.NewValidation(fmt.Sprintf("disable_thinking must be true or false, got %q", value), nil)
			}
		},
		EnumValues: []string{"false", "true"},
	},
	// --- Paths & Directories ---
	{
		Key:         "resource_directory",
		Description: "Directory for captured web/vision resources",
		ValidValues: "any valid file path",
		GetValue:    func(cfg *configuration.Config) string { return cfg.ResourceDirectory },
		SetValue: func(cfg *configuration.Config, value string) error {
			cfg.ResourceDirectory = value
			return nil
		},
	},
	// --- History & EA ---
	{
		Key:         "history_scope",
		Description: "Change history scope",
		ValidValues: "project, global",
		GetValue:    func(cfg *configuration.Config) string { return cfg.HistoryScope },
		SetValue: func(cfg *configuration.Config, value string) error {
			switch strings.ToLower(value) {
			case "project", "global", "":
				cfg.HistoryScope = strings.ToLower(value)
				return nil
			default:
				return agenterrors.NewValidation(fmt.Sprintf("history_scope must be project or global, got %q", value), nil)
			}
		},
		EnumValues: []string{"project", "global"},
	},
	{
		Key:         "ea_mode",
		Description: "Executive Assistant mode",
		ValidValues: "interactive, queue",
		GetValue:    func(cfg *configuration.Config) string { return cfg.EAMode },
		SetValue: func(cfg *configuration.Config, value string) error {
			switch strings.ToLower(value) {
			case "interactive", "queue", "":
				cfg.EAMode = strings.ToLower(value)
				return nil
			default:
				return agenterrors.NewValidation(fmt.Sprintf("ea_mode must be interactive or queue, got %q", value), nil)
			}
		},
		EnumValues: []string{"interactive", "queue"},
	},
	// --- Subagent settings ---
	{
		Key:         "subagent_provider",
		Description: "Provider used for subagents",
		ValidValues: "provider name or empty to inherit from provider",
		GetValue:    func(cfg *configuration.Config) string { return cfg.SubagentProvider },
		SetValue: func(cfg *configuration.Config, value string) error {
			cfg.SubagentProvider = value
			return nil
		},
	},
	{
		Key:         "subagent_model",
		Description: "Model used for subagents",
		ValidValues: "provider-specific model name or empty to use provider default",
		GetValue:    func(cfg *configuration.Config) string { return cfg.SubagentModel },
		SetValue: func(cfg *configuration.Config, value string) error {
			cfg.SubagentModel = value
			return nil
		},
	},
	{
		Key:         "default_subagent_persona",
		Description: "Persona used when run_subagent is invoked without a persona argument",
		ValidValues: "persona ID (e.g. general, coder, reviewer) or empty to fall back to 'general'",
		GetValue:    func(cfg *configuration.Config) string { return cfg.DefaultSubagentPersona },
		SetValue: func(cfg *configuration.Config, value string) error {
			v := strings.TrimSpace(value)
			if v != "" && cfg.GetSubagentType(v) == nil {
				return agenterrors.NewValidation(fmt.Sprintf("default_subagent_persona %q is not a known persona ID or alias", v), nil)
			}
			cfg.DefaultSubagentPersona = v
			return nil
		},
	},
	{
		Key:         "disabled_personas",
		Description: "Comma-separated persona IDs hidden from /persona list and subagent spawning",
		ValidValues: "comma-separated persona IDs (e.g. researcher,web_scraper) or empty to enable all",
		GetValue: func(cfg *configuration.Config) string {
			return strings.Join(cfg.DisabledPersonas, ",")
		},
		SetValue: func(cfg *configuration.Config, value string) error {
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
			return nil
		},
	},
	{
		Key:         "subagent_max_parallel",
		Description: "Maximum number of parallel subagents",
		ValidValues: "1-8",
		GetValue:    func(cfg *configuration.Config) string { return strconv.Itoa(cfg.SubagentMaxParallel) },
		SetValue: func(cfg *configuration.Config, value string) error {
			val, err := strconv.Atoi(value)
			if err != nil {
				return agenterrors.NewValidation(fmt.Sprintf("subagent_max_parallel must be an integer, got %q", value), nil)
			}
			if val < 1 || val > 8 {
				return agenterrors.NewValidation(fmt.Sprintf("subagent_max_parallel must be between 1 and 8, got %d", val), nil)
			}
			cfg.SubagentMaxParallel = val
			return nil
		},
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
		SetValue: func(cfg *configuration.Config, value string) error {
			switch strings.ToLower(value) {
			case "true":
				t := true
				cfg.SubagentParallelEnabled = &t
				return nil
			case "false":
				f := false
				cfg.SubagentParallelEnabled = &f
				return nil
			default:
				return agenterrors.NewValidation(fmt.Sprintf("subagent_parallel_enabled must be true or false, got %q", value), nil)
			}
		},
		EnumValues: []string{"true", "false"},
	},
	{
		Key:         "subagent_max_depth",
		Description: "Maximum subagent nesting depth",
		ValidValues: "1-4",
		GetValue:    func(cfg *configuration.Config) string { return strconv.Itoa(cfg.SubagentMaxDepth) },
		SetValue: func(cfg *configuration.Config, value string) error {
			val, err := strconv.Atoi(value)
			if err != nil {
				return agenterrors.NewValidation(fmt.Sprintf("subagent_max_depth must be an integer, got %q", value), nil)
			}
			if val < 1 || val > 4 {
				return agenterrors.NewValidation(fmt.Sprintf("subagent_max_depth must be between 1 and 4, got %d", val), nil)
			}
			cfg.SubagentMaxDepth = val
			return nil
		},
	},
	// --- Output ---
	{
		Key:         "output_verbosity",
		Description: "How much inter-tool-call narration the UI shows",
		ValidValues: "compact, default, verbose",
		GetValue:    func(cfg *configuration.Config) string { return cfg.OutputVerbosity },
		SetValue: func(cfg *configuration.Config, value string) error {
			switch strings.ToLower(value) {
			case "compact", "default", "verbose", "":
				cfg.OutputVerbosity = strings.ToLower(value)
				return nil
			default:
				return agenterrors.NewValidation(fmt.Sprintf("output_verbosity must be compact, default, or verbose, got %q", value), nil)
			}
		},
		EnumValues: []string{"compact", "default", "verbose"},
	},
	// --- Commit ---
	{
		Key:         "commit_provider",
		Description: "Provider for commit message generation",
		ValidValues: "provider name or empty to inherit from provider",
		GetValue:    func(cfg *configuration.Config) string { return cfg.CommitProvider },
		SetValue: func(cfg *configuration.Config, value string) error {
			cfg.CommitProvider = value
			return nil
		},
	},
	{
		Key:         "commit_model",
		Description: "Model for commit message generation",
		ValidValues: "provider-specific model name or empty to use provider default",
		GetValue:    func(cfg *configuration.Config) string { return cfg.CommitModel },
		SetValue: func(cfg *configuration.Config, value string) error {
			cfg.CommitModel = value
			return nil
		},
	},
	// --- Review ---
	{
		Key:         "review_provider",
		Description: "Provider for code review commands",
		ValidValues: "provider name or empty to inherit from provider",
		GetValue:    func(cfg *configuration.Config) string { return cfg.ReviewProvider },
		SetValue: func(cfg *configuration.Config, value string) error {
			cfg.ReviewProvider = value
			return nil
		},
	},
	{
		Key:         "review_model",
		Description: "Model for code review commands",
		ValidValues: "provider-specific model name or empty to use provider default",
		GetValue:    func(cfg *configuration.Config) string { return cfg.ReviewModel },
		SetValue: func(cfg *configuration.Config, value string) error {
			cfg.ReviewModel = value
			return nil
		},
	},
	// --- Notifications ---
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
		SetValue: func(cfg *configuration.Config, value string) error {
			switch strings.ToLower(value) {
			case "true":
				if cfg.Notifications == nil {
					cfg.Notifications = &configuration.NotificationsConfig{}
				}
				cfg.Notifications.CLIBell = true
				return nil
			case "false":
				if cfg.Notifications == nil {
					cfg.Notifications = &configuration.NotificationsConfig{}
				}
				cfg.Notifications.CLIBell = false
				return nil
			default:
				return agenterrors.NewValidation(fmt.Sprintf("notifications.cli_bell must be true or false, got %q", value), nil)
			}
		},
		EnumValues: []string{"true", "false"},
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
		SetValue: func(cfg *configuration.Config, value string) error {
			switch strings.ToLower(value) {
			case "true":
				if cfg.Notifications == nil {
					cfg.Notifications = &configuration.NotificationsConfig{}
				}
				cfg.Notifications.OSNotify = true
				return nil
			case "false":
				if cfg.Notifications == nil {
					cfg.Notifications = &configuration.NotificationsConfig{}
				}
				cfg.Notifications.OSNotify = false
				return nil
			default:
				return agenterrors.NewValidation(fmt.Sprintf("notifications.os_notify must be true or false, got %q", value), nil)
			}
		},
		EnumValues: []string{"true", "false"},
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
		SetValue: func(cfg *configuration.Config, value string) error {
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
			return nil
		},
	},
	// --- API Timeouts ---
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
		SetValue: func(cfg *configuration.Config, value string) error {
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
			return nil
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
		SetValue: func(cfg *configuration.Config, value string) error {
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
			return nil
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
		SetValue: func(cfg *configuration.Config, value string) error {
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
			return nil
		},
	},
	// --- Zsh ---
	{
		Key:         "enable_zsh_command_detection",
		Description: "Enable zsh-aware command detection",
		ValidValues: "true, false",
		GetValue:    func(cfg *configuration.Config) string { return fmt.Sprintf("%v", cfg.EnableZshCommandDetection) },
		SetValue: func(cfg *configuration.Config, value string) error {
			switch strings.ToLower(value) {
			case "true":
				cfg.EnableZshCommandDetection = true
				return nil
			case "false":
				cfg.EnableZshCommandDetection = false
				return nil
			default:
				return agenterrors.NewValidation(fmt.Sprintf("enable_zsh_command_detection must be true or false, got %q", value), nil)
			}
		},
		EnumValues: []string{"true", "false"},
	},
}

// supportedSettings is built from settingDefs at init time.
// It is used by validateSettingKey for key existence checks and
// by SupportedSettingKeys for enumeration.
var supportedSettings = buildSupportedSettings()

func buildSupportedSettings() map[string]string {
	m := make(map[string]string, len(settingDefs))
	for _, d := range settingDefs {
		m[d.Key] = d.Description
	}
	return m
}

// lookupSettingDef returns the settingDef for a key (case-insensitive), or nil.
func lookupSettingDef(key string) *settingDef {
	k := strings.ToLower(key)
	for i := range settingDefs {
		if settingDefs[i].Key == k {
			return &settingDefs[i]
		}
	}
	return nil
}

// SettingEnumValues returns the enum values for a setting key, or nil if
// the setting is not an enum (freeform input). It is used by the interactive
// settings browser to offer a picker instead of raw text input.
func SettingEnumValues(key string) []string {
	d := lookupSettingDef(key)
	if d == nil || len(d.EnumValues) == 0 {
		return nil
	}
	return d.EnumValues
}
