package commands

import (
	"fmt"
	"strconv"

	"github.com/alantheprice/ledit/pkg/agent"
)

// MemoryCommand handles memory features with auto-tracking and session recovery
type MemoryCommand struct{}

func (c *MemoryCommand) Name() string {
	return "memory"
}

func (c *MemoryCommand) Description() string {
	return "Show and load previous conversation sessions"
}

func (c *MemoryCommand) Execute(args []string, chatAgent *agent.Agent) error {
	// List sessions immediately in reverse order (newest first)
	sessions, err := agent.ListSessionsWithTimestamps()
	if err != nil {
		return fmt.Errorf("failed to list sessions: %v", err)
	}

	if len(sessions) == 0 {
		fmt.Println("No saved sessions found.")
		return nil
	}

	// If user provided a session number, load it directly
	if len(args) > 0 {
		sessionNum, err := strconv.Atoi(args[0])
		if err != nil || sessionNum < 1 || sessionNum > len(sessions) {
			return fmt.Errorf("invalid session number. Please select 1-%d", len(sessions))
		}

		sessionID := sessions[sessionNum-1].SessionID
		state, err := chatAgent.LoadState(sessionID)
		if err != nil {
			return fmt.Errorf("failed to load session: %v", err)
		}

		chatAgent.ApplyState(state)
		fmt.Printf("✓ Conversation memory loaded for session: %s\n", sessionID)
		return nil
	}

	// If no args and no agent (e.g., in tests), just print sessions and return
	if chatAgent == nil {
		fmt.Printf("Found %d saved sessions. Run with a session number to load.\n", len(sessions))
		return nil
	}

	// Use dropdown for interactive selection
	return c.selectSessionWithDropdown(sessions, chatAgent)
}

// selectSessionWithDropdown provides interactive session selection with dropdown
func (c *MemoryCommand) selectSessionWithDropdown(sessions []agent.SessionInfo, chatAgent *agent.Agent) error {
	// UI not available - select newest session or return
	fmt.Println("Interactive session selection not available.")
	var sessionID string
	if len(sessions) > 0 {
		sessionID = sessions[len(sessions)-1].SessionID // Get newest session
		fmt.Printf("Auto-selected newest session: %s\n", sessionID)
	} else {
		fmt.Println("No sessions available.")
		return nil
	}
	state, err := chatAgent.LoadState(sessionID)
	if err != nil {
		return fmt.Errorf("failed to load session: %v", err)
	}

	chatAgent.ApplyState(state)
	fmt.Printf("\r\n✅ Conversation memory loaded for session: %s\r\n", sessionID)
	return nil
}
