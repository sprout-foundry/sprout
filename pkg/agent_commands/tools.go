package commands

import (
	"errors"
	"fmt"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/console"
)

// ToolsCommand implements /tools for toggling per-tool invocation detail
// visibility in the UI. Maps to the show_tool_invocations config setting.
type ToolsCommand struct{}

func (c *ToolsCommand) Name() string { return "tools" }

func (c *ToolsCommand) Description() string {
	return "Show or toggle per-tool invocation detail visibility"
}

func (c *ToolsCommand) Usage() string {
	return strings.Join([]string{
		"/tools              Show whether per-tool details are visible.",
		"/tools on           Show per-tool invocation details (default).",
		"/tools off          Hide per-tool invocation details.",
		"/tools toggle       Toggle the current state.",
		"",
		"When hidden, tool invocations are collapsed in the conversation",
		"output. Only the final response text is shown. This is a UI display",
		"preference that persists to config.",
		"Equivalent to /settings set show_tool_invocations true|false.",
	}, "\n")
}

func (c *ToolsCommand) Execute(args []string, chatAgent *agent.Agent) error {
	if chatAgent == nil {
		return errors.New("agent not available")
	}

	cm := chatAgent.GetConfigManager()
	if cm == nil {
		return errors.New("config manager not available")
	}

	cfg := cm.GetConfig()
	if cfg == nil {
		return errors.New("config not loaded")
	}

	current := cfg.ShowToolInvocations

	if len(args) == 0 {
		if current {
			console.GlyphInfo.Printf("Per-tool invocation details: shown (use /tools off or /tools toggle to hide)\n")
		} else {
			console.GlyphInfo.Printf("Per-tool invocation details: hidden (use /tools on or /tools toggle to show)\n")
		}
		return nil
	}

	var newValue bool
	first := strings.ToLower(strings.TrimSpace(args[0]))

	switch first {
	case "on", "show", "true", "1":
		newValue = true
	case "off", "hide", "false", "0":
		newValue = false
	case "toggle":
		newValue = !current
	default:
		return fmt.Errorf("invalid value %q (valid: on, off, toggle)", first)
	}

	if newValue == current {
		console.GlyphInfo.Printf("Per-tool invocation details already %s", boolToShowState(newValue))
		return nil
	}

	if err := cm.UpdateConfig(func(cfgCopy *configuration.Config) error {
		cfgCopy.ShowToolInvocations = newValue
		return nil
	}); err != nil {
		return fmt.Errorf("updating config: %w", err)
	}

	console.GlyphSuccess.Printf("Per-tool invocation details: %s (persisted to config)", boolToShowState(newValue))
	return nil
}

func boolToShowState(v bool) string {
	if v {
		return "shown"
	}
	return "hidden"
}

// Complete implements CompletableCommand for argument tab completion.
func (c *ToolsCommand) Complete(args []string, _ *agent.Agent) []string {
	if len(args) == 0 {
		return []string{"on", "off", "toggle"}
	}
	if len(args) == 1 {
		prefix := strings.ToLower(args[0])
		candidates := []string{"on", "off", "toggle"}
		var matches []string
		for _, cand := range candidates {
			if strings.HasPrefix(cand, prefix) {
				matches = append(matches, cand)
			}
		}
		return matches
	}
	return nil
}
