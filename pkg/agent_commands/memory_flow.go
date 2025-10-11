package commands

import (
	"fmt"
	"os"
	"strconv"

	"github.com/alantheprice/ledit/pkg/agent"
	"github.com/alantheprice/ledit/pkg/ui"
	"golang.org/x/term"
)

// MemoryFlow manages enhanced memory operations
type MemoryFlow struct {
	agent *agent.Agent
}

// NewMemoryFlow creates a new memory flow
func NewMemoryFlow(chatAgent *agent.Agent) *MemoryFlow {
	return &MemoryFlow{
		agent: chatAgent,
	}
}

// Execute runs the simplified memory flow - just load sessions
func (mf *MemoryFlow) Execute(args []string) error {
	// Handle direct session loading by number/ID
	if len(args) > 0 {
		return mf.loadSessionDirectly(args[0])
	}

	// Get sessions
	sessions, err := agent.ListSessionsWithTimestamps()
	if err != nil {
		return fmt.Errorf("failed to list sessions: %w", err)
	}

	if len(sessions) == 0 {
		fmt.Printf("\r\n📭 No saved sessions found.\r\n")
		return nil
	}

	// Check for terminal support
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		// Fallback to simple session list
		fmt.Printf("Available sessions:\r\n")
		for i, session := range sessions {
			fmt.Printf("%d. %s (%s)\r\n", i+1, session.SessionID,
				session.LastUpdated.Format("2006-01-02 15:04:05"))
		}
		return nil
	}

	// Show session selection dropdown
	return mf.selectAndLoadSession(sessions)
}

// loadSessionDirectly loads a session by number or ID
func (mf *MemoryFlow) loadSessionDirectly(arg string) error {
	sessions, err := agent.ListSessionsWithTimestamps()
	if err != nil {
		return fmt.Errorf("failed to list sessions: %w", err)
	}

	if len(sessions) == 0 {
		fmt.Printf("No saved sessions found.\r\n")
		return nil
	}

	var sessionID string

	// Try to parse as session number (1-based)
	if sessionNum, err := strconv.Atoi(arg); err == nil {
		if sessionNum < 1 || sessionNum > len(sessions) {
			return fmt.Errorf("invalid session number. Please select 1-%d", len(sessions))
		}
		sessionID = sessions[sessionNum-1].SessionID
	} else {
		// Try as direct session ID
		sessionID = arg
	}

	// Load the session
	state, err := mf.agent.LoadState(sessionID)
	if err != nil {
		return fmt.Errorf("failed to load session: %w", err)
	}

	mf.agent.ApplyState(state)
	fmt.Printf("✅ Conversation memory loaded for session: %s\r\n", sessionID)
	return nil
}

// selectAndLoadSession shows session selection dropdown
func (mf *MemoryFlow) selectAndLoadSession(sessions []agent.SessionInfo) error {
	// Check if we're in agent console - show help instead
	if os.Getenv("LEDIT_AGENT_CONSOLE") == "1" {
		fmt.Println("\n📂 Available Sessions:")
		fmt.Println("=====================")

		// Display sessions in reverse order (newest first)
		for i := len(sessions) - 1; i >= 0; i-- {
			session := sessions[i]
			sessionNum := len(sessions) - i
			fmt.Printf("%d. %s - %s\n", sessionNum, session.SessionID,
				session.LastUpdated.Format("2006-01-02 15:04:05"))
		}

		fmt.Println("\n💡 To load a session, use: /memory <session_number>")
		fmt.Println("   Example: /memory 1")
		return nil
	}

	// Check if we're not in a terminal
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Printf("Available sessions:\r\n")
		for i, session := range sessions {
			fmt.Printf("%d. %s (%s)\r\n", i+1, session.SessionID,
				session.LastUpdated.Format("2006-01-02 15:04:05"))
		}
		return nil
	}

	// Convert sessions to dropdown items (newest first)
	items := make([]ui.DropdownItem, 0, len(sessions))
	for i := len(sessions) - 1; i >= 0; i-- {
		session := sessions[i]
		preview := agent.GetSessionPreview(session.SessionID)
		item := &ui.SessionItem{
			SessionID: session.SessionID,
			Timestamp: session.LastUpdated,
			Preview:   preview,
		}
		items = append(items, item)
	}

	// Temporarily disable ESC monitoring during dropdown
	mf.agent.DisableEscMonitoring()
	defer mf.agent.EnableEscMonitoring()

	// Create and show dropdown
	selected, err := mf.agent.ShowDropdown(items, ui.DropdownOptions{
		Prompt:       "📂 Select Session to Load:",
		SearchPrompt: "Search: ",
		ShowCounts:   false,
	})

	if err != nil {
		if err == ui.ErrCancelled {
			fmt.Printf("\r\nSession loading cancelled.\r\n")
			return nil
		}
		return fmt.Errorf("failed to show session selection: %w", err)
	}

	// Load the selected session
	sessionID := selected.Value().(string)
	state, err := mf.agent.LoadState(sessionID)
	if err != nil {
		return fmt.Errorf("failed to load session: %w", err)
	}

	mf.agent.ApplyState(state)
	fmt.Printf("\r\n✅ Conversation memory loaded for session: %s\r\n", sessionID)
	return nil
}
