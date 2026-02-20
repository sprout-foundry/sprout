package commands

import (
	"fmt"

	"github.com/alantheprice/ledit/pkg/agent"
)

// HelpCommand implements the /help slash command
type HelpCommand struct {
	registry *CommandRegistry
}

// Name returns the command name
func (h *HelpCommand) Name() string {
	return "help"
}

// Description returns the command description
func (h *HelpCommand) Description() string {
	return "Show help information and available slash commands"
}

// Execute runs the help command
func (h *HelpCommand) Execute(args []string, chatAgent *agent.Agent) error {
	fmt.Print(`
🤖 Ledit - AI Coding Agent

A command-line coding assistant that uses AI to help you build software.

USAGE:
  Interactive mode:     ./ledit
  Non-interactive:      ./ledit "your query here"
  Custom model:         ./ledit --provider openrouter --model qwen/qwen3-coder-30b "your query"
  Piped input:         echo "your query" | ./ledit

EXAMPLES:
  # Interactive mode
  ./ledit
  > Create a simple Go HTTP server

  # Non-interactive
  ./ledit "Create a simple Go HTTP server"

  # Use specific provider/model
  ./ledit --provider openrouter --model qwen/qwen3-coder-30b "Fix the bug"

  # Piped input
  echo "Explain this code" | ./ledit

AVAILABLE TOOLS:
  • shell_command - Execute shell commands
  • read_file - Read file contents  
  • write_file - Create new files
  • edit_file - Modify existing files
  • TodoWrite/TodoRead - Task management
  • run_subagent - Delegate to subagent
  • run_parallel_subagents - Run multiple subagents in parallel
  • list_skills/activate_skill - Load skill instructions into context

KEY COMMANDS:
  /help       - Show this help message
  /commit     - Interactive commit workflow
  /subagent-provider - Configure subagent provider
  /subagent-model - Configure subagent model
  /subagent-personas - List available subagent personas
  /subagent-persona - Configure a specific persona

Type 'exit' or 'quit' to end the session.

`)

	// List all registered commands
	commands := h.registry.ListCommands()
	fmt.Println("AVAILABLE SLASH COMMANDS:")
	for _, cmd := range commands {
		fmt.Printf("  /%s - %s\n", cmd.Name(), cmd.Description())
	}

	fmt.Println()

	return nil
}
