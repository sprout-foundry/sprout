package commands

import (
	"fmt"
	"sort"
	"strings"

	"github.com/alantheprice/ledit/pkg/agent"
	"github.com/alantheprice/ledit/pkg/ui"
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
func ShowCommandSelector(registry *CommandRegistry) (string, error) {
	// Get all commands
	commands := registry.ListCommands()

	// Create dropdown items
	items := make([]ui.DropdownItem, 0, len(commands))

	// Build a map of command names for sorting
	cmdMap := make(map[string]Command)
	names := make([]string, 0, len(commands))
	for _, cmd := range commands {
		cmdMap[cmd.Name()] = cmd
		names = append(names, cmd.Name())
	}
	sort.Strings(names)

	// Build items
	for _, name := range names {
		cmd := cmdMap[name]

		// Common aliases based on command name
		aliases := []string{}
		switch name {
		case "help":
			aliases = []string{"h", "?"}
		case "exit":
			aliases = []string{"quit", "q"}
		case "models":
			aliases = []string{"model"}
		case "provider":
			aliases = []string{"providers"}
		case "changes":
			aliases = []string{"diff"}
		case "status":
			aliases = []string{"st"}
		case "exec":
			aliases = []string{"run", "e"}
		case "shell":
			aliases = []string{"sh", "bash"}
		}

		item := &CommandItem{
			Name:        name,
			Description: cmd.Description(),
			Aliases:     aliases,
		}
		items = append(items, item)
	}

	// Create and show dropdown
	dropdown := ui.NewDropdown(items, ui.DropdownOptions{
		Prompt:       "Select a command:",
		SearchPrompt: "Search commands: ",
		ShowCounts:   false,
	})

	selected, err := dropdown.Show()
	if err != nil {
		return "", err
	}

	return selected.Value().(string), nil
}

// SelectAndExecuteCommand shows command selector and executes the selected command
func SelectAndExecuteCommand(registry *CommandRegistry, chatAgent *agent.Agent) error {
	selectedCmd, err := ShowCommandSelector(registry)
	if err != nil {
		return fmt.Errorf("command selection cancelled")
	}

	// Parse the command (remove the leading slash)
	cmdName := strings.TrimPrefix(selectedCmd, "/")

	// Execute the command with no arguments (user can add args later if needed)
	return registry.Execute("/"+cmdName, chatAgent)
}
