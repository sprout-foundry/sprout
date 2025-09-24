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
		
		// Show conversation preview
		c.displayConversationPreview(chatAgent)
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

	// Create and show dropdown
	dropdown := ui.NewDropdown(items, ui.DropdownOptions{
		Prompt:       "=== SESSION SELECTOR ===",
		SearchPrompt: "🔍 Search: ",
		ShowCounts:   true,
	})

	selected, err := dropdown.Show()
	if err != nil {
		fmt.Printf("\r\nSession selection cancelled.\r\n")
		return nil
	}

	// Get the selected session ID and load it
	sessionID := selected.Value().(string)
	state, err := chatAgent.LoadState(sessionID)
	if err != nil {
		return fmt.Errorf("failed to load session: %v", err)
	}

	chatAgent.ApplyState(state)
	fmt.Printf("\r\n✅ Conversation memory loaded for session: %s\r\n", sessionID)
	
	// Show conversation preview
	c.displayConversationPreview(chatAgent)
	return nil
}

// displayConversationPreview shows recent messages from the restored session
func (c *MemoryCommand) displayConversationPreview(agent *agent.Agent) {
	// Get last few messages for preview (e.g., last 5)
	lastMessages := agent.GetLastMessages(5)
	
	if len(lastMessages) > 0 {
		fmt.Println("\n📋 Recent conversation preview:")
		fmt.Println("================================")
		for _, msg := range lastMessages {
			if msg.Role == "user" {
				fmt.Printf("👤 You: %s\n", msg.Content)
			} else if msg.Role == "assistant" {
				fmt.Printf("🤖 Assistant: %s\n", msg.Content)
			}
		}
		fmt.Println("================================")
	}
}