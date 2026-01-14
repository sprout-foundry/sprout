package commands

import (
	"fmt"

	"github.com/alantheprice/ledit/pkg/agent"
)

// StatsCommand implements the /stats slash command
type StatsCommand struct{}

// Name returns the command name
func (s *StatsCommand) Name() string {
	return "stats"
}

// Description returns the command description
func (s *StatsCommand) Description() string {
	return "Show detailed conversation summary and token usage"
}

// Execute runs the stats command
func (s *StatsCommand) Execute(args []string, chatAgent *agent.Agent) error {
	fmt.Println("\n📊 Detailed Conversation Summary:")
	fmt.Println("=====================================")
	chatAgent.PrintConversationSummary(true)
	return nil
}
