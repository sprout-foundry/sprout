package commands

import (
	"errors"
	"fmt"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/console"
)

// MouseCommand implements /mouse: turn click-to-place-caret in the CLI input
// on or off (the mouse_click_to_position setting).
type MouseCommand struct {
	outputSink
}

func (c *MouseCommand) Name() string { return "mouse" }

// SafeDuringSteer returns true — a config change; the input surfaces read it
// the next time they take the terminal.
func (c *MouseCommand) SafeDuringSteer() bool { return true }

func (c *MouseCommand) Description() string {
	return "Click in the input to place the cursor (on|off); captures the scroll wheel while on"
}

func (c *MouseCommand) Usage() string {
	return strings.Join([]string{
		"/mouse          Show whether click-to-place-cursor is on.",
		"/mouse on       Clicking in the input box places the cursor.",
		"/mouse off      Leave the mouse to the terminal (the default).",
		"/mouse toggle   Switch between the two.",
		"",
		"Terminals only report clicks together with the scroll wheel, so while",
		"this is on the plain wheel no longer scrolls the terminal's history",
		"(most terminals scroll with a modifier held, which varies by terminal).",
		"Takes effect at the next prompt. Persists as mouse_click_to_position.",
	}, "\n")
}

func (c *MouseCommand) Execute(args []string, chatAgent *agent.Agent) error {
	if chatAgent == nil {
		return errors.New("agent not available")
	}
	cm := chatAgent.GetConfigManager()
	if cm == nil || cm.GetConfig() == nil {
		return errors.New("config not available")
	}
	current := cm.GetConfig().MouseClickToPosition
	if len(args) == 0 {
		state := "off"
		if current {
			state = "on"
		}
		console.GlyphInfo.Fprintf(c.out(), "Click to place cursor: %s (/mouse on|off)", state)
		return nil
	}
	var want bool
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "on", "true":
		want = true
	case "off", "false":
		want = false
	case "toggle":
		want = !current
	default:
		return fmt.Errorf("unknown /mouse argument %q (on, off, toggle)", args[0])
	}
	if err := cm.UpdateConfig(func(cfg *configuration.Config) error {
		cfg.MouseClickToPosition = want
		return nil
	}); err != nil {
		return fmt.Errorf("updating config: %w", err)
	}
	if want {
		console.GlyphSuccess.Fprintf(c.out(), "Click to place cursor: on from the next prompt — the plain scroll wheel now goes to sprout, not the terminal's history")
	} else {
		console.GlyphSuccess.Fprintf(c.out(), "Click to place cursor: off from the next prompt — the scroll wheel is the terminal's again")
	}
	return nil
}

// Complete implements CompletableCommand for argument tab completion.
func (c *MouseCommand) Complete(args []string, _ *agent.Agent) []string {
	opts := []string{"on", "off", "toggle"}
	if len(args) == 0 {
		return opts
	}
	var out []string
	for _, o := range opts {
		if strings.HasPrefix(o, strings.ToLower(args[0])) {
			out = append(out, o)
		}
	}
	return out
}
