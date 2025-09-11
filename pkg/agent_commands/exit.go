package commands

import (
	"fmt"
	"os"

	"github.com/alantheprice/ledit/pkg/agent"
)

// ExitCommand implements the /exit slash command
type ExitCommand struct{}

// Name returns the command name
func (e *ExitCommand) Name() string {
	return "exit"
}

// Description returns the command description
func (e *ExitCommand) Description() string {
	return "Exit the interactive session"
}

// Execute runs the exit command
func (e *ExitCommand) Execute(args []string, chatAgent *agent.Agent) error {
	// Print full session summary before exiting
	fmt.Println("\n👋 Goodbye! Here's your session summary:")
	fmt.Println("=====================================")
	chatAgent.PrintConversationSummary(true)
	os.Exit(0)
	return nil // This line won't be reached due to os.Exit
}