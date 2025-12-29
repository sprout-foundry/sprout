package commands

import (
	"fmt"

	"github.com/alantheprice/ledit/pkg/agent"
)

// ClearCommand handles clearing of conversation history
type ClearCommand struct{}

func (c *ClearCommand) Name() string {
	return "clear"
}

func (c *ClearCommand) Description() string {
	return "Clears conversation history"
}

func (c *ClearCommand) Execute(args []string, chatAgent *agent.Agent) error {
	if chatAgent == nil {
		return fmt.Errorf("agent not available")
	}

	chatAgent.ClearConversationHistory()
	fmt.Println("🧹 Conversation history cleared.")
	return nil
}
