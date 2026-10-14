package commands

import (
	"errors"
	"fmt"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/console"
)

// EconomyCommand implements /economy for inspecting or toggling the
// per-session economy mode. The CLI flag --economy sets the same field at
// startup; this command lets the user (or the web UI, which renders slash
// commands) adjust it without restarting.
//
// Economy mode routes more delegated work to the cheaper worker model. For a
// NEW session it also starts the main loop on the worker model, escalating to
// the coder model for planning and hard steps. Changing it mid-session
// affects only which subagents get the worker model — the main-loop model is
// not switched, so the prompt cache is kept.
type EconomyCommand struct {
	outputSink
}

func (c *EconomyCommand) Name() string {
	return "economy"
}

// SafeDuringSteer returns true — /economy is a config change, no turn interaction.
func (c *EconomyCommand) SafeDuringSteer() bool {
	return true
}

func (c *EconomyCommand) Description() string {
	return "Show or toggle economy mode (delegate more work to the cheaper worker model)"
}

func (c *EconomyCommand) Usage() string {
	return strings.Join([]string{
		"/economy            Show the current mode and its effect.",
		"/economy on         Enable economy mode for this session.",
		"/economy off        Disable economy mode for this session.",
		"",
		"Economy mode routes subagents that would otherwise run on the coder",
		"model onto the worker model. For a NEW session it also starts the main",
		"loop on the worker model. Changing it mid-session re-routes subagents",
		"only — the main loop's model is not switched, so the prompt cache is kept.",
		"To move the main loop back to the coder model, use /model or /provider.",
		"",
		"The worker model is the `worker` role (sprout config set role.worker.model …).",
		"Until a worker model is configured, economy mode is a no-op: the worker",
		"role falls back to the coder model.",
	}, "\n")
}

func (c *EconomyCommand) Execute(args []string, chatAgent *agent.Agent) error {
	if chatAgent == nil {
		return errors.New("agent not available")
	}

	if len(args) == 0 {
		return c.show(chatAgent)
	}

	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "on", "enable", "true":
		chatAgent.SetEconomyMode(true)
		console.GlyphSuccess.Fprintf(c.out(), "Economy mode enabled: %s", chatAgent.EconomyDescription())
		return nil
	case "off", "disable", "false":
		chatAgent.SetEconomyMode(false)
		console.GlyphSuccess.Fprintf(c.out(), "Economy mode disabled (session only — does not persist)")
		return nil
	case "show", "status":
		return c.show(chatAgent)
	default:
		return fmt.Errorf("unknown argument %q (use: on, off, show)", args[0])
	}
}

func (c *EconomyCommand) show(chatAgent *agent.Agent) error {
	console.GlyphInfo.Fprintf(c.out(), "Economy mode: %s", chatAgent.EconomyDescription())
	if !chatAgent.GetEconomyMode() {
		c.println("   Delegated work runs on the coder model; enable with /economy on.")
	} else if chatAgent.GetMainLoopEconomy() {
		c.println("   The main loop and delegated subagents run on the worker model.")
	} else {
		c.println("   Enabled mid-session: subagents run on the worker model; the main loop is unchanged.")
	}
	return nil
}

// Complete returns completions for the /economy command.
func (c *EconomyCommand) Complete(args []string, chatAgent *agent.Agent) []string {
	if len(args) > 1 {
		return nil
	}
	candidates := []string{"on", "off", "show"}
	if len(args) == 0 {
		return candidates
	}
	last := args[len(args)-1]
	if last == "" {
		return candidates
	}
	var matches []string
	for _, cand := range candidates {
		if strings.HasPrefix(strings.ToLower(cand), strings.ToLower(last)) {
			matches = append(matches, cand)
		}
	}
	return matches
}
