package commands

import (
	"fmt"

	"github.com/alantheprice/ledit/pkg/agent"
)

// InfoCommand implements the /info slash command
type InfoCommand struct{}

// Name returns the command name
func (i *InfoCommand) Name() string {
	return "info"
}

// Description returns the command description
func (i *InfoCommand) Description() string {
	return "Show detailed conversation summary and token usage"
}

// Execute runs the info command
func (i *InfoCommand) Execute(args []string, chatAgent *agent.Agent) error {
	fmt.Println("\n📊 Detailed Conversation Summary:")
	fmt.Println("=====================================")
	chatAgent.PrintConversationSummary(true)
	return nil
}
