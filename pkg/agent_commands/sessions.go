package commands

import (
	"fmt"
	"strconv"

	"github.com/alantheprice/ledit/pkg/agent"
	"github.com/alantheprice/ledit/pkg/ui"
)

// SessionsCommand handles session management with auto-tracking and session recovery
type SessionsCommand struct{}

func (c *SessionsCommand) Name() string {
	return "sessions"
}

func (c *SessionsCommand) Description() string {
	return "Show and load previous conversation sessions"
}

func (c *SessionsCommand) Execute(args []string, chatAgent *agent.Agent) error {
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
		fmt.Printf("✓ Conversation session loaded: %s\n", sessionID)
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
func (c *SessionsCommand) selectSessionWithDropdown(sessions []agent.SessionInfo, chatAgent *agent.Agent) error {
	// Simple numeric selector - display sessions and prompt for choice
	fmt.Println("\n📂 Available Sessions:")
	fmt.Println("=====================")

	// Display sessions in reverse order (newest first)
	for i := len(sessions) - 1; i >= 0; i-- {
		session := sessions[i]
		sessionNum := len(sessions) - i
		name := session.Name
		if name == "" {
			name = agent.GetSessionPreview(session.SessionID)
		}
		label := session.LastUpdated.Format("2006-01-02 15:04:05")
		if name != "" {
			label = fmt.Sprintf("[%s] - %s", name, session.LastUpdated.Format("2006-01-02 15:04:05"))
		}
		fmt.Printf("%d. %s\n", sessionNum, label)
	}

	// Build simple options list for display
	var sessionOptions []string
	for i := len(sessions) - 1; i >= 0; i-- {
		session := sessions[i]
		name := session.Name
		if name == "" {
			name = agent.GetSessionPreview(session.SessionID)
		}
		label := session.LastUpdated.Format("2006-01-02 15:04:05")
		if name != "" {
			label = fmt.Sprintf("[%s] - %s", name, session.LastUpdated.Format("2006-01-02 15:04:05"))
		}
		sessionOptions = append(sessionOptions, label)
	}

	// Use shared numeric selector
	selection, ok := ui.PromptForSelection(sessionOptions, "Enter session number (or 0 to cancel): ")
	if !ok || selection == 0 {
		return nil
	}

	// Load the selected session (convert selection back to session index)
	sessionIndex := len(sessions) - selection
	sessionID := sessions[sessionIndex].SessionID
	state, err := chatAgent.LoadState(sessionID)
	if err != nil {
		return fmt.Errorf("failed to load session: %v", err)
	}

	chatAgent.ApplyState(state)
	fmt.Printf("\r\n✅ Conversation session loaded: %s\r\n", sessionID)
	return nil
}
