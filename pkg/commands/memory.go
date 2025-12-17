package commands

import (
	"fmt"
	"os"
	"strconv"

	"github.com/alantheprice/ledit/pkg/agent"
	"golang.org/x/term"
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
	// Check if we're in agent console - if so, show list with help
	if os.Getenv("LEDIT_AGENT_CONSOLE") == "1" {
		fmt.Println("\n📂 Available Sessions:")
		fmt.Println("=====================")

		// Display sessions in reverse order (newest first)
		for i := len(sessions) - 1; i >= 0; i-- {
			session := sessions[i]
			sessionNum := len(sessions) - i
			fmt.Printf("%d. Session %s - %s\n", sessionNum, session.SessionID, session.LastUpdated.Format("2006-01-02 15:04:05"))
		}

		fmt.Println("\n💡 To load a session, use: /memory <session_number>")
		fmt.Println("   Example: /memory 1")
		return nil
	}

	// Check if we're not in a terminal
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Println("Interactive session selection requires a terminal.")
		fmt.Println("Use /memory <session_number> to load a specific session.")
		return nil
	}

	// Convert sessions to dropdown items (reverse order for newest first)
	items := make([]agent.SessionItem, 0, len(sessions))
	for i := len(sessions) - 1; i >= 0; i-- {
		session := sessions[i]
		preview := agent.GetSessionPreview(session.SessionID)
		item := agent.SessionItem{
			Label:     fmt.Sprintf("%s - %s", session.LastUpdated.Format("2006-01-02 15:04:05"), preview),
			Value:     session.SessionID,
			SessionID: session.SessionID,
			Timestamp: session.LastUpdated,
			Preview:   preview,
		}
		items = append(items, item)
	}

	// Use agent's integrated dropdown UI if available
	if chatAgent == nil || !chatAgent.IsInteractiveMode() {
		fmt.Println("Interactive selection not available. Use /memory <session_number> instead.")
		return nil
	}

	// UI not available - select newest session or return
	fmt.Println("Interactive session selection not available.")
	var sessionID string
	if len(sessions) > 0 {
		sessionID = sessions[len(sessions)-1].SessionID // Get newest
		fmt.Printf("Auto-selected newest session: %s\n", sessionID)
	} else {
		fmt.Println("No sessions available to load.")
		return nil
	}
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
