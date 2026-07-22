package commands

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/console"
)

// ContextCommand implements /context for inspecting or overriding the SP-125
// Low-Context Mode setting. The context profile (full vs low_context) is
// resolved once at agent creation from config.context_mode plus the model's
// reported context window. This command persists a new selection to config
// for the next session start — the live agent's prompt and tool set are
// already baked in and are not mutated mid-session.
type ContextCommand struct {
	stdout io.Writer
}

func (c *ContextCommand) SetOutput(w io.Writer) { c.stdout = w }

func (c *ContextCommand) out() io.Writer {
	if c.stdout != nil {
		return c.stdout
	}
	return os.Stdout
}

func (c *ContextCommand) Name() string { return "context" }

// SafeDuringSteer returns true — /context is a config change with no turn
// interaction, same shape as /max-context and /risk-profile.
func (c *ContextCommand) SafeDuringSteer() bool { return true }

func (c *ContextCommand) Description() string {
	return "Show or set the context mode (full|low_context) — takes effect next session"
}

func (c *ContextCommand) Usage() string {
	return strings.Join([]string{
		"/context                 Show the effective mode, the configured value, and the active levers.",
		"/context full            Force full mode: all tools, full orchestrator prompt.",
		"/context low             Force Low-Context Mode: 8-tool allowlist, lite prompt.",
		"/context auto            Clear the override — let auto-detection decide from the model window.",
		"",
		"Modes:",
		"  full          All tools, full orchestrator system prompt, proactive context on.",
		"                Default for models with >= 64K context.",
		"  low_context   8-tool allowlist (edit-test-commit + safety net), lite prompt,",
		"                proactive context off, tighter compaction. Auto-activated for",
		"                models with 8K–64K context.",
		"",
		"The mode is resolved once at agent creation and persists to config.",
		"Changes take effect on the next session start — the running agent keeps its",
		"current prompt and tools. Models below the 8K floor are refused entirely",
		"and cannot be rescued with /context.",
	}, "\n")
}

// Execute runs the /context command. With no args it shows the current
// state; with full|low|auto it persists the selection to config.
func (c *ContextCommand) Execute(args []string, chatAgent *agent.Agent) error {
	if chatAgent == nil {
		return errors.New("agent not available")
	}

	if len(args) == 0 {
		return c.show(chatAgent)
	}

	first := strings.ToLower(strings.TrimSpace(args[0]))
	switch first {
	case "show", "list", "status":
		return c.show(chatAgent)
	case "full":
		return c.set(chatAgent, configuration.ContextModeFull)
	case "low", "low_context", "low-context", "lcm":
		return c.set(chatAgent, configuration.ContextModeLowContext)
	case "auto", "clear", "default", "":
		return c.clear(chatAgent)
	default:
		return fmt.Errorf("unknown mode %q (valid: full, low, auto)", args[0])
	}
}

func (c *ContextCommand) show(chatAgent *agent.Agent) error {
	profile := chatAgent.GetContextProfile()
	cfg := chatAgent.GetConfig()

	console.GlyphInfo.Fprintf(c.out(), "Effective context mode: %q", string(profile.Mode))
	fmt.Fprintln(c.out(), c.describeLevers(profile))

	if cfg != nil && cfg.ContextMode != "" {
		fmt.Fprintf(c.out(), "   config.context_mode: %q\n", string(cfg.ContextMode))
	} else {
		fmt.Fprintln(c.out(), "   config.context_mode: (unset — auto-detected from model context window)")
	}
	fmt.Fprintln(c.out(), "   Changes via /context persist to config and take effect on the next session start.")
	return nil
}

// describeLevers renders the active lever values compactly. Full mode reads
// as the defaults; LCM lists each lever that differs so the user can verify
// what's actually shaping the session.
func (c *ContextCommand) describeLevers(profile configuration.ContextProfile) string {
	if profile.Mode == configuration.ContextModeLowContext {
		return fmt.Sprintf("   Active levers: %d tools, lite prompt, AGENTS.md kept, proactive context off, trigger %.2f",
			len(profile.ToolAllowlist), profile.CompactionTriggerFraction)
	}
	return "   Active levers: all tools, full orchestrator prompt, proactive context on"
}

func (c *ContextCommand) set(chatAgent *agent.Agent, mode configuration.ContextMode) error {
	cm := chatAgent.GetConfigManager()
	if cm == nil {
		return errors.New("config manager not available")
	}
	if err := cm.UpdateConfig(func(cfg *configuration.Config) error {
		cfg.ContextMode = mode
		return nil
	}); err != nil {
		return fmt.Errorf("updating config: %w", err)
	}
	console.GlyphSuccess.Fprintf(c.out(), "Context mode set to %q (persists to config; takes effect on next session start)", string(mode))
	return nil
}

func (c *ContextCommand) clear(chatAgent *agent.Agent) error {
	cm := chatAgent.GetConfigManager()
	if cm == nil {
		return errors.New("config manager not available")
	}
	if err := cm.UpdateConfig(func(cfg *configuration.Config) error {
		cfg.ContextMode = ""
		return nil
	}); err != nil {
		return fmt.Errorf("updating config: %w", err)
	}
	console.GlyphSuccess.Fprintf(c.out(), "Context mode cleared — auto-detection will decide on next session start (persists to config)")
	return nil
}

// Complete returns subcommand completions for the /context command.
func (c *ContextCommand) Complete(args []string, chatAgent *agent.Agent) []string {
	subcommands := []string{"full", "low", "low_context", "auto", "clear", "show"}
	if len(args) == 0 {
		return subcommands
	}
	prefix := strings.ToLower(args[len(args)-1])
	if prefix == "" {
		return subcommands
	}
	var matches []string
	for _, sub := range subcommands {
		if strings.HasPrefix(sub, prefix) {
			matches = append(matches, sub)
		}
	}
	return matches
}
