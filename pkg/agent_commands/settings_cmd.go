package commands

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/console"
)

// SettingsCommand implements the /settings slash command — an interactive
// settings browser that lets the user view and change configuration values.
type SettingsCommand struct{}

// Name returns the command name
func (s *SettingsCommand) Name() string {
	return "settings"
}

// Description returns the command description
func (s *SettingsCommand) Description() string {
	return "Browse and change settings interactively"
}

// Usage returns the detailed help text shown by `/help settings`.
func (s *SettingsCommand) Usage() string {
	return strings.Join([]string{
		"/settings   Interactive settings browser.",
		"",
		"Browse all configurable settings, view current values, and change",
		"them in place. Changes persist to config.json.",
		"Use /setup for a read-only configuration summary.",
	}, "\n")
}

// Execute runs the settings command
func (s *SettingsCommand) Execute(args []string, chatAgent *agent.Agent) error {
	if chatAgent == nil {
		return fmt.Errorf("agent not available")
	}

	mgr := chatAgent.GetConfigManager()
	if mgr == nil {
		return fmt.Errorf("configuration manager not available")
	}

	cfg := mgr.GetConfig()
	if cfg == nil {
		return fmt.Errorf("configuration not loaded")
	}

	settings := agent.AllSettings()
	if len(settings) == 0 {
		fmt.Fprintln(os.Stdout, "No settings available")
		return nil
	}

	// Build the initial options list (setting keys with current values)
	options := buildSettingsOptions(settings, cfg)

	for {
		// Re-read config in case it was updated externally
		cfg = mgr.GetConfig()

		// Display the summary panel
		renderSettingsSummary(os.Stdout, settings, cfg)

		// Build fresh options with current values
		options = buildSettingsOptions(settings, cfg)

		// Prompt the user to select a setting
		resp, err := tools.AskUser(tools.AskUserRequest{
			Header:   "Settings",
			Question: "Select a setting to change (or 'q' to quit)",
			Options:  options,
		})
		if err != nil {
			// Non-TTY or cancelled: print summary and exit gracefully
			if err == tools.ErrAskUserNoChannel {
				return nil
			}
			// Other errors (timeout, etc.) — also exit gracefully
			fmt.Fprintln(os.Stdout)
			return nil
		}

		// Check for quit
		if isQuit(resp) {
			return nil
		}

		// Find the setting detail for the selected key
		var selected agent.SettingDetail
		for _, sd := range settings {
			if sd.Key == resp {
				selected = sd
				break
			}
		}
		if selected.Key == "" {
			fmt.Fprintln(os.Stdout, "Unknown setting selected")
			continue
		}

		// Prompt for the new value
		newValue, err := promptSettingValue(selected, cfg, mgr)
		if err != nil {
			if err == tools.ErrAskUserNoChannel {
				return nil
			}
			fmt.Fprintf(os.Stdout, "  %s\n", err.Error())
			continue
		}

		// Check for quit in the value prompt
		if isQuit(newValue) {
			return nil
		}

		// Apply the change
		err = mgr.UpdateConfig(func(cfgCopy *configuration.Config) error {
			return agent.SetSettingValue(cfgCopy, selected.Key, newValue)
		})
		if err != nil {
			console.GlyphError.Fprintf(os.Stdout, "Failed to update %s: %s", selected.Key, err.Error())
			continue
		}

		console.GlyphSuccess.Fprintf(os.Stdout, "Updated %s to %q", selected.Key, newValue)
	}
}

// buildSettingsOptions creates AskUserOption entries for each setting,
// showing the current value in the label.
func buildSettingsOptions(settings []agent.SettingDetail, cfg *configuration.Config) []tools.AskUserOption {
	options := make([]tools.AskUserOption, 0, len(settings)+1)
	for _, sd := range settings {
		value := sd.GetValue(cfg)
		if value == "" {
			value = "(not set)"
		}
		options = append(options, tools.AskUserOption{
			Label: fmt.Sprintf("%s (%s)", sd.Key, value),
			Value: sd.Key,
		})
	}
	// Add quit option
	options = append(options, tools.AskUserOption{
		Label: "q (quit)",
		Value: "q",
	})
	return options
}

// renderSettingsSummary prints a formatted settings summary to w.
func renderSettingsSummary(w io.Writer, settings []agent.SettingDetail, cfg *configuration.Config) {
	// Find the longest key for alignment
	maxKeyLen := 0
	for _, sd := range settings {
		if len(sd.Key) > maxKeyLen {
			maxKeyLen = len(sd.Key)
		}
	}

	fmt.Fprintln(w)
	console.GlyphInfo.Fprintf(w, "Settings")
	fmt.Fprintln(w, strings.Repeat("─", 40))

	for _, sd := range settings {
		value := sd.GetValue(cfg)
		if value == "" {
			value = "(not set)"
		}
		fmt.Fprintf(w, "  %"+fmt.Sprintf("%ds", maxKeyLen)+":  %s\n", sd.Key, value)
	}
	fmt.Fprintln(w)
}

// promptSettingValue prompts the user for a new value for the given setting.
// Returns the new value string or an error.
func promptSettingValue(setting agent.SettingDetail, cfg *configuration.Config, mgr *configuration.Manager) (string, error) {
	// Check if this is an enum setting
	if enumOpts, ok := getEnumOptions(setting.Key, cfg); ok {
		currentVal := setting.GetValue(cfg)
		return tools.AskUser(tools.AskUserRequest{
			Header:   fmt.Sprintf("Change %s", setting.Key),
			Question: fmt.Sprintf("Select a value for %s", setting.Key),
			Options:  enumOpts,
			Default:  currentVal,
		})
	}

	// Check if this is a provider setting — offer a picker
	if providerOptionKeys[setting.Key] {
		if providerOpts, ok := getProviderOptions(mgr); ok {
			currentVal := setting.GetValue(cfg)
			return tools.AskUser(tools.AskUserRequest{
				Header:   fmt.Sprintf("Change %s", setting.Key),
				Question: fmt.Sprintf("Select a provider for %s", setting.Key),
				Options:  providerOpts,
				Default:  currentVal,
			})
		}
	}

	// Freeform input
	currentVal := setting.GetValue(cfg)
	return tools.AskUser(tools.AskUserRequest{
		Header:   fmt.Sprintf("Change %s", setting.Key),
		Question: fmt.Sprintf("Enter a new value for %s (valid: %s)", setting.Key, setting.ValidValues),
		Default:  currentVal,
	})
}

// getEnumOptions returns predefined options for enum-style settings.
// Returns (nil, false) for freeform settings.
func getEnumOptions(key string, cfg *configuration.Config) ([]tools.AskUserOption, bool) {
	switch key {
	case "reasoning_effort":
		return []tools.AskUserOption{
			{Label: "low", Value: "low"},
			{Label: "medium", Value: "medium"},
			{Label: "high", Value: "high"},
		}, true

	case "disable_thinking":
		return []tools.AskUserOption{
			{Label: "enabled", Value: "false"},
			{Label: "disabled", Value: "true"},
		}, true

	case "history_scope":
		return []tools.AskUserOption{
			{Label: "project", Value: "project"},
			{Label: "global", Value: "global"},
		}, true

	case "ea_mode":
		return []tools.AskUserOption{
			{Label: "interactive", Value: "interactive"},
			{Label: "queue", Value: "queue"},
		}, true

	case "output_verbosity":
		return []tools.AskUserOption{
			{Label: "compact", Value: "compact"},
			{Label: "default", Value: "default"},
			{Label: "verbose", Value: "verbose"},
		}, true

	case "subagent_parallel_enabled":
		return []tools.AskUserOption{
			{Label: "enabled", Value: "true"},
			{Label: "disabled", Value: "false"},
		}, true

	case "notifications.cli_bell":
		return []tools.AskUserOption{
			{Label: "enabled", Value: "true"},
			{Label: "disabled", Value: "false"},
		}, true

	case "notifications.os_notify":
		return []tools.AskUserOption{
			{Label: "enabled", Value: "true"},
			{Label: "disabled", Value: "false"},
		}, true

	case "enable_zsh_command_detection":
		return []tools.AskUserOption{
			{Label: "enabled", Value: "true"},
			{Label: "disabled", Value: "false"},
		}, true

	default:
		return nil, false
	}
}

// providerOptionKeys are settings that accept a provider name and should
// offer a picker built from the config's available providers.
var providerOptionKeys = map[string]bool{
	"provider":          true,
	"subagent_provider": true,
	"commit_provider":   true,
	"review_provider":   true,
}

// getProviderOptions builds an AskUser option list from available providers.
// Returns (nil, false) if no providers are configured.
func getProviderOptions(mgr *configuration.Manager) ([]tools.AskUserOption, bool) {
	if mgr == nil {
		return nil, false
	}
	available := mgr.GetAvailableProviders()
	if len(available) == 0 {
		return nil, false
	}
	opts := make([]tools.AskUserOption, 0, len(available))
	for _, p := range available {
		opts = append(opts, tools.AskUserOption{
			Label: string(p),
			Value: string(p),
		})
	}
	return opts, true
}

// isQuit checks if the user response indicates they want to quit.
func isQuit(resp string) bool {
	resp = strings.TrimSpace(strings.ToLower(resp))
	return resp == "q" || resp == "quit" || resp == "exit"
}
