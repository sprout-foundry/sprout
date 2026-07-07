package commands

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
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
		"/settings                  Interactive settings browser.",
		"/settings set <key> <val>  Set a setting directly (non-interactive).",
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

	// Fast path: /settings set <key> <value>
	if len(args) > 0 && args[0] == "set" {
		if len(args) < 3 {
			return fmt.Errorf("usage: /settings set <key> <value>")
		}
		key := args[1]
		value := strings.Join(args[2:], " ")

		if err := mgr.UpdateConfig(func(cfgCopy *configuration.Config) error {
			return agent.SetSettingValue(cfgCopy, key, value)
		}); err != nil {
			return err
		}

		console.GlyphSuccess.Fprintf(os.Stdout, "Updated %s to %q", key, value)
		return nil
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

		// List-type settings get a dedicated add/remove/set sub-menu
		if selected.ListType {
			if err := promptListSettingValue(selected, cfg, mgr); err != nil {
				if err == tools.ErrAskUserNoChannel {
					return nil
				}
				fmt.Fprintf(os.Stdout, "  %s\n", err.Error())
			}
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
	values := agent.SettingEnumValues(key)
	if len(values) == 0 {
		return nil, false
	}
	opts := make([]tools.AskUserOption, len(values))
	for i, v := range values {
		opts[i] = tools.AskUserOption{
			Label: enumLabel(key, v),
			Value: v,
		}
	}
	return opts, true
}

// enumLabel returns a human-readable label for a setting enum value.
func enumLabel(key, value string) string {
	// disable_thinking has inverted semantics: "true" means thinking is disabled.
	if key == "disable_thinking" {
		if value == "true" {
			return "disabled"
		}
		return "enabled"
	}
	switch value {
	case "true":
		return "enabled"
	case "false":
		return "disabled"
	default:
		return value
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

// promptListSettingValue handles add/remove/set operations for list-type
// settings (e.g. approved_shell_commands). It shows the current list items
// numbered, then accepts commands: add <item>, remove <number>, set
// <comma,separated,list>, or q to cancel. Any other input is treated as a
// comma-separated replacement (equivalent to set).
func promptListSettingValue(setting agent.SettingDetail, cfg *configuration.Config, mgr *configuration.Manager) error {
	currentValue := setting.GetValue(cfg)
	var items []string
	if currentValue != "" {
		items = splitCSV(currentValue)
	}

	for {
		// Show current list state
		fmt.Fprintln(os.Stdout)
		fmt.Fprintf(os.Stdout, "  %s (%s)\n", setting.Key, setting.Description)
		if len(items) == 0 {
			fmt.Fprintln(os.Stdout, "  (empty list)")
		} else {
			for i, item := range items {
				fmt.Fprintf(os.Stdout, "  %d. %s\n", i+1, item)
			}
		}
		fmt.Fprintln(os.Stdout)
		fmt.Fprintln(os.Stdout, "  Commands: add <item>, remove <number>, set <comma,list>, or q to finish")

		// Prompt
		defaultVal := setting.GetValue(cfg)
		resp, err := tools.AskUser(tools.AskUserRequest{
			Header:   fmt.Sprintf("Manage %s", setting.Key),
			Question: fmt.Sprintf("Action for %s", setting.Key),
			Default:  defaultVal,
		})
		if err != nil {
			return err
		}
		resp = strings.TrimSpace(resp)

		if isQuit(resp) {
			return nil
		}

		// Parse the command
		newValue, err := applyListCommand(resp, items)
		if err != nil {
			fmt.Fprintf(os.Stdout, "  ERROR: %s\n", err.Error())
			continue
		}

		// Apply the change
		err = mgr.UpdateConfig(func(cfgCopy *configuration.Config) error {
			return agent.SetSettingValue(cfgCopy, setting.Key, newValue)
		})
		if err != nil {
			fmt.Fprintf(os.Stdout, "  ERROR: %s\n", err.Error())
			continue
		}

		// Refresh items from the updated config
		cfg = mgr.GetConfig()
		currentValue = setting.GetValue(cfg)
		if currentValue != "" {
			items = splitCSV(currentValue)
		} else {
			items = nil
		}

		console.GlyphSuccess.Fprintf(os.Stdout, "Updated %s", setting.Key)
	}
}

// applyListCommand parses a list management command and returns the new
// comma-separated value to persist.
func applyListCommand(input string, current []string) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", fmt.Errorf("empty command")
	}

	lower := strings.ToLower(input)

	// "add <item>"
	if strings.HasPrefix(lower, "add ") {
		item := strings.TrimSpace(input[4:])
		if item == "" {
			return "", fmt.Errorf("add requires an item: add <item>")
		}
		newItems := make([]string, len(current)+1)
		copy(newItems, current)
		newItems[len(current)] = item
		return strings.Join(newItems, ","), nil
	}

	// "remove <number>"
	if strings.HasPrefix(lower, "remove ") {
		numStr := strings.TrimSpace(input[7:])
		n, err := strconv.Atoi(numStr)
		if err != nil {
			return "", fmt.Errorf("remove requires a number: remove <number>")
		}
		if n < 1 || n > len(current) {
			return "", fmt.Errorf("invalid index %d: must be between 1 and %d", n, len(current))
		}
		idx := n - 1
		newItems := make([]string, 0, len(current)-1)
		newItems = append(newItems, current[:idx]...)
		newItems = append(newItems, current[idx+1:]...)
		return strings.Join(newItems, ","), nil
	}

	// "set <comma,separated,list>"
	if strings.HasPrefix(lower, "set ") {
		rest := strings.TrimSpace(input[4:])
		// Validate that the comma-separated list is non-empty after trimming
		items := splitCSV(rest)
		if len(items) == 0 && rest != "" {
			return "", fmt.Errorf("set requires at least one item")
		}
		return rest, nil
	}

	// Default: treat as comma-separated replacement
	items := splitCSV(input)
	if len(items) == 0 {
		return "", fmt.Errorf("unrecognized command %q — use add <item>, remove <number>, or set <comma,list>", input)
	}
	return input, nil
}

// splitCSV splits a comma-separated string into trimmed, non-empty parts.
func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	for _, raw := range strings.Split(s, ",") {
		trimmed := strings.TrimSpace(raw)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// Complete returns completions for the /settings command.
//
// Completion stages:
//  1. No args → return ["set"] (subcommand).
//  2. args=["set"] → return all setting keys.
//  3. args=["set", key, ...] where key exactly matches a setting with enum
//     values → return matching enum values.
//  4. args=["set", partialKey] → return setting keys with matching prefix.
func (s *SettingsCommand) Complete(args []string, chatAgent *agent.Agent) []string {
	if len(args) == 0 {
		return []string{"set"}
	}
	if args[0] != "set" {
		return nil
	}

	// Stage 2: just "set" — show all setting keys (already sorted).
	if len(args) == 1 {
		return agent.SupportedSettingKeys()
	}

	// Stage 3+4: we have args[1] — a (possibly partial) setting key.
	key := args[1]

	// Stage 3: exact known key with enum values → value completion.
	enumVals := agent.SettingEnumValues(key)
	if len(enumVals) > 0 {
		prefix := ""
		if len(args) > 2 {
			prefix = args[2]
		}
		var matches []string
		for _, v := range enumVals {
			if prefix == "" || strings.HasPrefix(strings.ToLower(v), strings.ToLower(prefix)) {
				matches = append(matches, v)
			}
		}
		sort.Strings(matches)
		return matches
	}

	// Stage 4: key prefix matching (partial keys or keys without enums).
	keys := agent.SupportedSettingKeys()
	var matches []string
	for _, sk := range keys {
		if strings.HasPrefix(strings.ToLower(sk), strings.ToLower(key)) {
			matches = append(matches, sk)
		}
	}
	return matches
}
