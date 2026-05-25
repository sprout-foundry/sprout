package commands

import (
	"os"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/console"
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
	console.GlyphInfo.Fprintln(os.Stdout, "Detailed Conversation Summary:")
	chatAgent.PrintConversationSummary(true)
	return nil
}
