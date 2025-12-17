package commands

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/alantheprice/ledit/pkg/agent"
	"golang.org/x/term"
)

// CommandItem adapts a slash command for dropdown display
type CommandItem struct {
	Name        string
	Description string
	Aliases     []string
}

func (c *CommandItem) Display() string {
	display := fmt.Sprintf("/%s", c.Name)
	if len(c.Aliases) > 0 {
		aliasStr := strings.Join(c.Aliases, ", /")
		display += fmt.Sprintf(" (/%s)", aliasStr)
	}
	display += " - " + c.Description
	return display
}

func (c *CommandItem) SearchText() string {
	parts := []string{c.Name}
	parts = append(parts, c.Aliases...)
	parts = append(parts, strings.ToLower(c.Description))
	return strings.Join(parts, " ")
}

func (c *CommandItem) Value() interface{} {
	return "/" + c.Name
}

// ShowCommandSelector displays a dropdown for slash command selection
func ShowCommandSelector(registry *CommandRegistry, chatAgent *agent.Agent) (string, error) {
	// Get all commands
	commands := registry.ListCommands()

	// Build a map of command names for sorting
	cmdMap := make(map[string]Command)
	names := make([]string, 0, len(commands))
	for _, cmd := range commands {
		cmdMap[cmd.Name()] = cmd
		names = append(names, cmd.Name())
	}
	sort.Strings(names)

	// Check if we're in agent console - show help instead
	if os.Getenv("LEDIT_AGENT_CONSOLE") == "1" {
		fmt.Println("\n📋 Available Commands:")
		fmt.Println("=====================")

		for _, name := range names {
			cmd := cmdMap[name]
			fmt.Printf("/%s - %s\n", name, cmd.Description())
		}

		fmt.Println("\n💡 Type any command to use it")
		return "", fmt.Errorf("command selector not available in agent console")
	}

	// Check if we're not in a terminal
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", fmt.Errorf("interactive command selection requires a terminal")
	}

	// UI not available - return first command as default
	fmt.Println("Interactive command selection not available.")
	if len(names) > 0 {
		return "/" + names[0], nil // Return first available command
	}
	return "", fmt.Errorf("no commands available")
}

// SelectAndExecuteCommand shows command selector and executes the selected command
func SelectAndExecuteCommand(registry *CommandRegistry, chatAgent *agent.Agent) error {
	selectedCmd, err := ShowCommandSelector(registry, chatAgent)
	if err != nil {
		return fmt.Errorf("command selection cancelled")
	}

	// Parse the command (remove the leading slash)
	cmdName := strings.TrimPrefix(selectedCmd, "/")

	// Execute the command with no arguments (user can add args later if needed)
	return registry.Execute("/"+cmdName, chatAgent)
}
