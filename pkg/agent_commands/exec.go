package commands

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/console"
)

// ExecCommand handles the /exec slash command
// Usage: /exec <shell-command-to-execute>
// Alias: !<shell-command-to-execute> (matches other tools like Jupyter, R, etc.)
type ExecCommand struct{}

func (c *ExecCommand) Name() string {
	return "exec"
}

func (c *ExecCommand) Description() string {
	return "Execute a shell command directly (also use !<command> as shortcut)"
}

// Usage returns the detailed help text shown by `/help exec`.
func (c *ExecCommand) Usage() string {
	return strings.Join([]string{
		"/exec <command>    Run a shell command and show output.",
		"!<command>         Shortcut alias.",
		"",
		"Example:",
		"  /exec ls -la",
		"  !git status",
		"",
		"Git checkout, switch, restore, and reset operations are blocked.",
	}, "\n")
}

func (c *ExecCommand) Execute(args []string, chatAgent *agent.Agent) error {
	if len(args) == 0 {
		return errors.New("usage: /exec <shell-command-to-execute>")
	}

	command := strings.Join(args, " ")

	// Security check: block git checkout/switch operations
	if IsGitCheckoutSubcommand(command) {
		return fmt.Errorf("git checkout/switch/restore operations are not allowed via /exec. Use the git tool to require explicit user approval (command: '%s')", command)
	}

	// Security check: block git discard operations (reset, restore)
	if IsGitDiscardCommand(command) {
		return fmt.Errorf("git %s operations are not allowed via /exec. Use the git tool with operation='restore' or operation='reset' to require explicit user approval (command: '%s')", ExtractGitSubcommand(command), command)
	}

	// Execute the shell command using the same pattern as direct shell execution
	console.GlyphShell.Fprintf(os.Stdout, "Executing: %s", command)
	result, err := tools.ExecuteShellCommand(context.Background(), command)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return nil
	}

	fmt.Printf("----------------------------\n")
	fmt.Print(result)
	if !strings.HasSuffix(result, "\n") {
		fmt.Print("\n")
	}
	fmt.Printf("----------------------------\n")

	return nil
}
