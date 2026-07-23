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

// SafeDuringSteer returns true. /clear rotates to a new session — it
// does NOT destroy conversation state. The prior session stays available
// via /sessions (see ClearCommand.Usage below), so the operation is
// reversible-by-design. Returning false here was wrong: this is the
// canonical "start a new conversation" action for any chat UI, and
// blocking it from the WebUI breaks the most basic functionality of a
// chat application. If RotateSession is ever changed to do a hard-delete
// instead of rotating, this must flip back to false.
func (c *ClearCommand) SafeDuringSteer() bool {
	return true
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
