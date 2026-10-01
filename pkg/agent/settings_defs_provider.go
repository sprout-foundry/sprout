package agent

import (
	"fmt"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// settings_defs_provider.go — the provider/model and reasoning/thinking
// sections of the settingDefs registry. Split out of settings_defs.go.
var settingDefsProvider = []settingDef{
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
}
