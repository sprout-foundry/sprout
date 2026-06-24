package commands

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/console"
)

// MaxContextCommand implements /max-context for inspecting or setting the
// user-defined context window cap (Config.MaxContextTokens). When set, the
// agent treats the model as if it has at most this many context tokens,
// limiting input/output budgets as a cost-control measure.
type MaxContextCommand struct{}

func (c *MaxContextCommand) Name() string { return "max-context" }

func (c *MaxContextCommand) Description() string {
	return "Show or set the max context token cap for cost control (0 = no cap)"
}

func (c *MaxContextCommand) Usage() string {
	return strings.Join([]string{
		"/max-context             Show the current cap and the model's native context window.",
		"/max-context <tokens>    Set the cap (must be ≥ 1024; 0 or 'clear' removes it).",
		"/max-context clear       Remove the cap — use the model's full context window.",
		"",
		"This caps the effective context window used when building each request.",
		"Useful for very large-context models (e.g. 1M-token) where you want to",
		"limit how many input tokens can be claimed per call to control API costs.",
		"The cap persists to config.json.",
	}, "\n")
}

func (c *MaxContextCommand) Execute(args []string, chatAgent *agent.Agent) error {
	if chatAgent == nil {
		return errors.New("agent not available")
	}

	if len(args) == 0 {
		return c.show(chatAgent)
	}

	first := strings.TrimSpace(args[0])
	if strings.EqualFold(first, "clear") || first == "0" {
		return c.clear(chatAgent)
	}

	n, err := strconv.Atoi(first)
	if err != nil || n < 0 {
		return fmt.Errorf("invalid value %q: must be a non-negative integer (0 or 'clear' removes the cap)", args[0])
	}
	if n > 0 && n < 1024 {
		return fmt.Errorf("value must be at least 1024 when setting a cap (got %d)", n)
	}
	if n == 0 {
		return c.clear(chatAgent)
	}
	return c.set(chatAgent, n)
}

func (c *MaxContextCommand) show(chatAgent *agent.Agent) error {
	cfg := chatAgent.GetConfig()
	native := chatAgent.GetMaxContextTokens()

	fmt.Printf("  Native context window: %s\n", fmtTokens(native))
	if cfg != nil && cfg.MaxContextTokens != nil && *cfg.MaxContextTokens > 0 {
		cap := *cfg.MaxContextTokens
		pct := float64(cap) / float64(native) * 100
		console.GlyphInfo.Printf("Max context cap: %s (%.0f%% of native window)", fmtTokens(cap), pct)
	} else {
		console.GlyphInfo.Printf("Max context cap: not set (using full native window of %s)", fmtTokens(native))
	}
	return nil
}

func (c *MaxContextCommand) set(chatAgent *agent.Agent, n int) error {
	cm := chatAgent.GetConfigManager()
	if cm == nil {
		return errors.New("config manager not available")
	}
	if err := cm.UpdateConfig(func(cfg *configuration.Config) error {
		cfg.MaxContextTokens = &n
		return nil
	}); err != nil {
		return fmt.Errorf("updating config: %w", err)
	}
	console.GlyphSuccess.Printf("Max context cap set to %s (persisted to config)", fmtTokens(n))
	return nil
}

func (c *MaxContextCommand) clear(chatAgent *agent.Agent) error {
	cm := chatAgent.GetConfigManager()
	if cm == nil {
		return errors.New("config manager not available")
	}
	if err := cm.UpdateConfig(func(cfg *configuration.Config) error {
		cfg.MaxContextTokens = nil
		return nil
	}); err != nil {
		return fmt.Errorf("updating config: %w", err)
	}
	console.GlyphSuccess.Printf("Max context cap cleared — using full native context window")
	return nil
}

// fmtTokens formats a token count with K/M suffix.
func fmtTokens(n int) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM tokens", float64(n)/1_000_000)
	}
	if n >= 1000 {
		return fmt.Sprintf("%dk tokens", n/1000)
	}
	return fmt.Sprintf("%d tokens", n)
}
