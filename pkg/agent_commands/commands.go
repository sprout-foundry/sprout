package commands

import (
	"fmt"
	"strings"

	"github.com/alantheprice/ledit/pkg/agent"
)

// Command represents a slash command
type Command interface {
	Name() string
	Description() string
	Execute(args []string, chatAgent *agent.Agent) error
}

// CommandRegistry manages all available slash commands
type CommandRegistry struct {
	commands map[string]Command
}

// NewCommandRegistry creates a new command registry
func NewCommandRegistry() *CommandRegistry {
	registry := &CommandRegistry{
		commands: make(map[string]Command),
	}

	// Register built-in commands
	registry.Register(&HelpCommand{registry: registry})
	registry.Register(&ModelsCommand{})
	registry.Register(&ProvidersCommand{})
	registry.Register(&MemoryCommand{})
	registry.Register(&InitCommand{})
	registry.Register(&ExitCommand{})
	registry.Register(&CommitCommand{})
	registry.Register(&ExecCommand{})
	registry.Register(&ShellCommand{})
	registry.Register(&InfoCommand{})

	// Register change tracking commands
	registry.Register(&ChangesCommand{})
	registry.Register(&StatusCommand{})
	registry.Register(&LogCommand{})
	registry.Register(&RollbackCommand{})

	// Register MCP commands
	registry.Register(&MCPCommand{})

	return registry
}

// Register adds a command to the registry
func (r *CommandRegistry) Register(cmd Command) {
	r.commands[cmd.Name()] = cmd
}

// Execute processes a slash command input
func (r *CommandRegistry) Execute(input string, chatAgent *agent.Agent) error {
	if !strings.HasPrefix(input, "/") {
		return fmt.Errorf("not a slash command")
	}

	// Parse command and arguments
	parts := strings.Fields(input[1:]) // Remove leading slash
	if len(parts) == 0 {
		return fmt.Errorf("empty command")
	}

	commandName := parts[0]
	args := parts[1:]

	// Find and execute command
	cmd, exists := r.commands[commandName]
	if !exists {
		return fmt.Errorf("unknown command: %s", commandName)
	}

	return cmd.Execute(args, chatAgent)
}

// IsSlashCommand checks if input starts with a slash
func (r *CommandRegistry) IsSlashCommand(input string) bool {
	return strings.HasPrefix(strings.TrimSpace(input), "/")
}

// GetCommand returns a command by name
func (r *CommandRegistry) GetCommand(name string) (Command, bool) {
	cmd, exists := r.commands[name]
	return cmd, exists
}

// ListCommands returns all available commands
func (r *CommandRegistry) ListCommands() []Command {
	commands := make([]Command, 0, len(r.commands))
	for _, cmd := range r.commands {
		commands = append(commands, cmd)
	}
	return commands
}
