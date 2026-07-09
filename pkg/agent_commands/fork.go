package commands

import (
	"fmt"
	"strconv"

	"github.com/sprout-foundry/sprout/pkg/agent"
)

// ForkCommand handles forking the conversation at a user message breakpoint.
type ForkCommand struct{}

func (c *ForkCommand) Name() string {
	return "fork"
}

func (c *ForkCommand) Description() string {
	return "Fork the conversation at a user message breakpoint"
}

// Usage returns the detailed help text shown by `/help fork`.
func (c *ForkCommand) Usage() string {
	return "/fork           List user message breakpoints\n" +
		"/fork <number>  Fork session at breakpoint N (1-based)\n"
}

func (c *ForkCommand) Execute(args []string, chatAgent *agent.Agent) error {
	if chatAgent == nil {
		return fmt.Errorf("agent not available")
	}

	if len(args) == 0 {
		return c.listBreakpoints(chatAgent)
	}

	n, err := strconv.Atoi(args[0])
	if err != nil {
		return fmt.Errorf("invalid breakpoint number: %q (must be an integer)", args[0])
	}

	newID, err := chatAgent.ForkAtBreakpoint(n)
	if err != nil {
		return err
	}

	fmt.Printf("[fork] Forked session: %s\n", newID)
	return nil
}

func (c *ForkCommand) listBreakpoints(chatAgent *agent.Agent) error {
	bps := chatAgent.Breakpoints()
	if len(bps) == 0 {
		fmt.Println("No user messages to fork from.")
		return nil
	}

	for _, bp := range bps {
		fmt.Printf("[%d] %q\n", bp.Index, bp.Content)
	}
	fmt.Println("Use /fork <number> to branch from that point.")
	return nil
}
