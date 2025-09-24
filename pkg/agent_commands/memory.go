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

	fmt.Println("=== Available Sessions (newest first) ===")
	for i, session := range sessions {
		fmt.Printf("%d. %s\n", i+1, session.SessionID)
	}
	fmt.Println("=========================================")

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

	// If no session number provided, prompt user to select one
	fmt.Printf("Enter session number (1-%d) to load: ", len(sessions))
	var input string
	fmt.Scanln(&input)

	sessionNum, err := strconv.Atoi(input)
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