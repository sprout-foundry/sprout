package commands

import (
	"errors"
	"fmt"

	"github.com/sprout-foundry/sprout/pkg/agent"
)

// ClearCommand handles clearing of conversation history
type ClearCommand struct{}

func (c *ClearCommand) Name() string {
	return "clear"
}

func (c *ClearCommand) Description() string {
	return "Clears conversation history"
}

// Usage returns the detailed help text shown by `/help clear`.
func (c *ClearCommand) Usage() string {
	return "/clear   Erase all conversation messages, freeing context.\n"
}

func (c *ClearCommand) Execute(args []string, chatAgent *agent.Agent) error {
	if chatAgent == nil {
		return errors.New("agent not available")
	}

	chatAgent.ClearConversationHistory()
	fmt.Println("[clean] Conversation history cleared.")
	return nil
}
