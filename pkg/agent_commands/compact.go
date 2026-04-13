package commands

import (
	"errors"
	"fmt"

	"github.com/alantheprice/ledit/pkg/agent"
)

// CompactCommand implements the /compact slash command
type CompactCommand struct{}

// Name returns the command name
func (c *CompactCommand) Name() string {
	return "compact"
}

// Description returns the command description
func (c *CompactCommand) Description() string {
	return "Force immediate context compaction to reduce token usage"
}

// Execute runs the compact command
func (c *CompactCommand) Execute(args []string, chatAgent *agent.Agent) error {
	if chatAgent == nil {
		return errors.New("agent not available")
	}

	// Check if there are turn checkpoints to compact
	if !chatAgent.HasTurnCheckpoints() {
		fmt.Println("\n[info] No turn checkpoints available for compaction.")
		fmt.Println("       Compaction requires conversation history with multiple turns.")
		return nil
	}

	// Get current message count before compaction
	initialMessageCount := len(chatAgent.GetMessages())

	// Force checkpoint compaction
	compactedMessages, remainingCheckpoints := chatAgent.BuildCheckpointCompactedMessages(chatAgent.GetMessages())

	// Validate compaction result - ensure we actually reduced messages
	if len(compactedMessages) >= initialMessageCount {
		fmt.Println("\n[info] Compaction did not reduce message count.")
		fmt.Println("       Checkpoints may already be applied or none available to compact.")
		return nil
	}

	// Update the agent's message list
	chatAgent.SetMessages(compactedMessages)

	// Update the remaining checkpoints
	chatAgent.ReplaceTurnCheckpoints(remainingCheckpoints)

	// Calculate the reduction
	messageDiff := initialMessageCount - len(compactedMessages)

	fmt.Println("\n[compact] Context compaction complete:")
	fmt.Printf("       Before: %d messages\n", initialMessageCount)
	fmt.Printf("       After:  %d messages\n", len(compactedMessages))
	fmt.Printf("       Removed: %d messages\n", messageDiff)
	fmt.Printf("       Remaining checkpoints: %d\n", len(remainingCheckpoints))

	return nil
}
