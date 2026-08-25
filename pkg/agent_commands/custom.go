package commands

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// CustomCommand implements the /custom slash command. It wraps
// `sprout custom {add,remove,list}` so users don't have to leave the
// chat to manage custom providers (e.g. after `/provider <name>` fails
// with a not-registered error).
type CustomCommand struct{}

// Name returns the command name.
func (c *CustomCommand) Name() string {
	return "custom"
}

// SafeDuringSteer returns true - /custom is config, independent of active turn
func (c *CustomCommand) SafeDuringSteer() bool {
	return true
}

// Description returns the command description.
func (c *CustomCommand) Description() string {
	return "Manage custom OpenAI-compatible providers"
}

// Usage returns the detailed help text shown by `/help custom`.
func (c *CustomCommand) Usage() string {
	return strings.Join([]string{
		"/custom              List custom providers (same as `sprout custom list`).",
		"/custom list         List custom providers.",
		"/custom add          Launch the interactive add wizard.",
		"/custom remove [name]",
		"                     Remove a custom provider (prompts if name omitted).",
		"",
		"The wizard prompts for endpoint URL, API key env var, and preferred model.",
		"It will offer to set the API key via the credential backend when saved.",
	}, "\n")
}

// Execute runs the custom command by shelling out to the sprout binary's
// `custom` subcommand. We use os.Executable() so the same wizard code runs
// without duplicating it in this package.
func (c *CustomCommand) Execute(args []string, chatAgent *agent.Agent) error {
	if len(args) == 0 {
		return c.runSubcommand("list", nil)
	}
	switch args[0] {
	case "list":
		return c.runSubcommand("list", nil)
	case "add":
		// `add` is interactive; the wizard reads stdin interactively,
		// so we just hand control over with stdin attached.
		return c.runSubcommand("add", nil)
	case "remove", "rm", "delete":
		return c.runSubcommand("remove", args[1:])
	case "help", "--help", "-h":
		fmt.Println(c.Usage())
		return nil
	default:
		return fmt.Errorf("unknown action %q. Use: list, add, remove", args[0])
	}
}

// Complete provides argument completions for /custom.
func (c *CustomCommand) Complete(args []string, chatAgent *agent.Agent) []string {
	if len(args) <= 1 {
		return []string{"list", "add", "remove"}
	}
	if args[0] == "remove" && len(args) == 2 {
		cfg, err := configuration.LoadOrInitConfig(false)
		if err != nil {
			return nil
		}
		names := make([]string, 0, len(cfg.CustomProviders))
		for name := range cfg.CustomProviders {
			names = append(names, name)
		}
		sort.Strings(names)
		return names
	}
	return nil
}

// runSubcommand shells out to `os.Executable() custom <subcommand> [args...]`
// and streams stdin/stdout/stderr to the current terminal. The interactive
// add wizard reads stdin directly, so attaching it lets the user answer
// prompts naturally from inside the chat.
func (c *CustomCommand) runSubcommand(subcommand string, rest []string) error {
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to resolve sprout binary: %w", err)
	}

	args := []string{"custom", subcommand}
	args = append(args, rest...)

	cmd := exec.Command(execPath, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		// Preserve the wizard's exit code without wrapping the error in a
		// generic "slash command failed: …" prefix — the wizard already
		// printed its own context to stderr.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return fmt.Errorf("custom %s exited with code %d", subcommand, exitErr.ExitCode())
		}
		return fmt.Errorf("custom %s failed: %w", subcommand, err)
	}
	return nil
}
