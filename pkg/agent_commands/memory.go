package commands

import (
	"fmt"
	"strconv"

	"github.com/alantheprice/ledit/pkg/agent"
	"github.com/alantheprice/ledit/pkg/ui"
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
	// Convert sessions to dropdown items (reverse order for newest first)
	items := make([]ui.DropdownItem, 0, len(sessions))
	for i := len(sessions) - 1; i >= 0; i-- {
		session := sessions[i]
		item := &ui.SessionItem{
			SessionID: session.SessionID,
			Timestamp: session.LastUpdated,
		}
		items = append(items, item)
	}

	// Try to show dropdown using the agent's UI
	selected, err := chatAgent.ShowDropdown(items, ui.DropdownOptions{
		Prompt:       "🎯 Select a Session:",
		SearchPrompt: "Search: ",
		ShowCounts:   true,
	})

	if err != nil {
		// Check if it was just cancelled
		if err == ui.ErrCancelled {
			fmt.Printf("\r\nSession selection cancelled.\r\n")
			return nil
		}
		// If dropdown is not available, we could add a fallback here
		return fmt.Errorf("failed to show session selection: %w", err)
	}

	// Get the selected session ID and load it
	sessionID := selected.Value().(string)
	state, err := chatAgent.LoadState(sessionID)
	if err != nil {
		return fmt.Errorf("failed to load session: %v", err)
	}

	chatAgent.ApplyState(state)
	fmt.Printf("\r\n✅ Conversation memory loaded for session: %s\r\n", sessionID)
	return nil
}
