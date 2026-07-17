package commands

import (
	"errors"
	"fmt"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/console"
)

// VerboseCommand implements /verbose for inspecting or cycling the
// output_verbosity setting (compact / default / verbose).
type VerboseCommand struct{}

func (c *VerboseCommand) Name() string { return "verbose" }

// SafeDuringSteer returns true - /verbose is config change
func (c *VerboseCommand) SafeDuringSteer() bool {
	return true
}

func (c *VerboseCommand) Description() string {
	return "Show or set output verbosity (compact|default|verbose)"
}

func (c *VerboseCommand) Usage() string {
	return strings.Join([]string{
		"/verbose               Show the current output verbosity level.",
		"/verbose <level>       Set verbosity directly (compact|default|verbose).",
		"/verbose cycle         Cycle to the next level: default → verbose → compact → default.",
		"",
		"  compact   Hide interim model messages; show only tool results and final text.",
		"  default   Show tool calls with results; show streaming final text.",
		"  verbose   Show everything including interim narration.",
		"",
		"Changes persist to config. Equivalent to /settings set output_verbosity <level>.",
	}, "\n")
}

var verbosityOrder = []string{
	configuration.OutputVerbosityDefault,
	configuration.OutputVerbosityVerbose,
	configuration.OutputVerbosityCompact,
}

func (c *VerboseCommand) Execute(args []string, chatAgent *agent.Agent) error {
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

	if len(args) == 0 {
		// Show current value
		current := cfg.OutputVerbosity
		if current == "" {
			current = configuration.OutputVerbosityDefault
		}
		console.GlyphInfo.Printf("Output verbosity: %q", current)
		fmt.Printf("  Valid values: %s\n", strings.Join(verbosityOrder, ", "))
		return nil
	}

	first := strings.ToLower(strings.TrimSpace(args[0]))

	var newValue string
	if first == "cycle" || first == "next" {
		// Cycle to the next level
		current := cfg.OutputVerbosity
		if current == "" {
			current = configuration.OutputVerbosityDefault
		}
		for i, v := range verbosityOrder {
			if v == current {
				newValue = verbosityOrder[(i+1)%len(verbosityOrder)]
				break
			}
		}
		if newValue == "" {
			newValue = verbosityOrder[0]
		}
	} else {
		// Direct set
		switch first {
		case "compact", "default", "verbose":
			newValue = first
		default:
			return fmt.Errorf("invalid verbosity level %q (valid: compact, default, verbose)", first)
		}
	}

	if err := cm.UpdateConfig(func(cfgCopy *configuration.Config) error {
		cfgCopy.OutputVerbosity = newValue
		return nil
	}); err != nil {
		return fmt.Errorf("updating config: %w", err)
	}

	console.GlyphSuccess.Printf("Output verbosity set to %q (persisted to config)", newValue)
	return nil
}

// Complete implements CompletableCommand for argument tab completion.
func (c *VerboseCommand) Complete(args []string, _ *agent.Agent) []string {
	if len(args) == 0 {
		// First argument: offer verbosity levels + cycle
		return verbosityOrder
	}
	if len(args) == 1 {
		// Filter by prefix
		prefix := strings.ToLower(args[0])
		var matches []string
		for _, v := range verbosityOrder {
			if strings.HasPrefix(v, prefix) {
				matches = append(matches, v)
			}
		}
		return matches
	}
	return nil
}
