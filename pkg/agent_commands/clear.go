package commands

import (
	"errors"
	"fmt"

	"github.com/sprout-foundry/sprout/pkg/agent"
)

// ClearCommand handles closing the current session and starting a fresh one
type ClearCommand struct{}

func (c *ClearCommand) Name() string {
	return "clear"
}

func (c *ClearCommand) Description() string {
	return "Close the current session and start a new one"
}

// Usage returns the detailed help text shown by `/help clear`.
func (c *ClearCommand) Usage() string {
	return "/clear   Close the current session and start a new one. The previous session stays available in /sessions.\n"
}

func (c *ClearCommand) Execute(args []string, chatAgent *agent.Agent) error {
	if chatAgent == nil {
		return errors.New("agent not available")
	}

	newID, err := chatAgent.RotateSession()
	if err != nil {
		return err
	}

	fmt.Printf("[clean] New session started: %s\n", newID)
	return nil
}
