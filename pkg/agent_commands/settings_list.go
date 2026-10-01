package commands

// settings_list.go — list-type setting management, split out of
// settings_cmd.go. promptListSettingValue drives the add/remove/set sub-menu
// for list-type settings (e.g. approved_shell_commands); applyListCommand
// parses the command and returns the new comma-separated value; splitCSV
// trims/splits the value.
import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/console"
)

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
		resp, err := tools.AskUser(context.Background(), tools.AskUserRequest{
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
